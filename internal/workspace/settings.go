package workspace

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"schemo/internal/db"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// SettingsKind is what the changed event names after a save.
const SettingsKind = "settings"

// SettingsWatcher reacts to a saved change. It gets both documents with secrets and decides itself what
// differs. An error undoes the save: the document goes back, and watchers that already ran are
// called again with the documents swapped.
type SettingsWatcher func(ctx context.Context, before, after domain.Settings) error

type Settings struct {
	db       *db.Store
	notifier domain.Notifier
	engine   Network
	files    *platform.FileReader
	// pickRoot opens the file dialog for a root certificate; an empty path means it was closed.
	pickRoot func(title string) (string, error)

	mu       sync.Mutex
	watchers []SettingsWatcher
}

func NewSettings(
	db *db.Store,
	notifier domain.Notifier,
	engine Network,
	reader *platform.FileReader,
	pickRoot func(title string) (string, error),
) *Settings {
	return &Settings{db: db, notifier: notifier, engine: engine, files: reader, pickRoot: pickRoot}
}

// Watch registers a reaction to saved changes. Watchers run in the order they were added.
//
//wails:ignore
func (s *Settings) Watch(w SettingsWatcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watchers = append(s.watchers, w)
}

// Get is the settings as the window sees them: secrets replaced by whether they are set.
func (s *Settings) Get(ctx context.Context) (domain.Settings, error) {
	settings, err := s.db.Preferences(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	return visible(settings), nil
}

// Save applies a JSON merge patch (RFC 7386) to the stored document: objects merge, other values
// replace, null removes. Sending only what changed keeps two windows from undoing each other.
// The MCP token and the has*/tail fields are Go's and are ignored in the patch.
func (s *Settings) Save(ctx context.Context, patch map[string]any) (domain.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := s.db.Preferences(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	after, err := patched(before, patch)
	if err != nil {
		return domain.Settings{}, err
	}
	after.MCP.Token = before.MCP.Token
	if after.ListSide != domain.ListSideHidden {
		after.ListSideLast = after.ListSide
	}
	if after.MCP.Enabled && after.MCP.Token == "" {
		// A server switched on gets its token at that moment: without one it accepts nobody.
		if after.MCP.Token, err = mintToken(); err != nil {
			return domain.Settings{}, err
		}
	}
	if err := after.Check(); err != nil {
		return domain.Settings{}, err
	}
	after.Normalize()
	return s.commit(ctx, before, after)
}

// RotateMCPToken replaces the MCP token. The MCP watcher drops the agents that held the old one.
func (s *Settings) RotateMCPToken(ctx context.Context) (domain.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := s.db.Preferences(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	after := before
	if after.MCP.Token, err = mintToken(); err != nil {
		return domain.Settings{}, err
	}
	return s.commit(ctx, before, after)
}

// RevealMCPToken is the token itself, for the eye in the MCP section and the agent config.
func (s *Settings) RevealMCPToken(ctx context.Context) (string, error) {
	settings, err := s.db.Preferences(ctx)
	return settings.MCP.Token, err
}

// RevealProxyPassword is the proxy password itself, for the eye beside the field.
func (s *Settings) RevealProxyPassword(ctx context.Context) (string, error) {
	settings, err := s.db.Preferences(ctx)
	return settings.Proxy.Password, err
}

// AddRootCertificate asks for a PEM file and trusts the authority in it, named by its subject or,
// failing that, by the file. A closed dialog changes nothing.
func (s *Settings) AddRootCertificate(ctx context.Context, title string) (domain.Settings, error) {
	path, err := s.pickRoot(title)
	if err != nil {
		return domain.Settings{}, err
	}
	if path == "" {
		return s.Get(ctx)
	}
	contents, err := s.files.Read(path)
	if err != nil {
		return domain.Settings{}, err
	}
	name, ok := rootName(filepath.Base(path), string(contents))
	if !ok {
		return domain.Settings{}, domain.Refuse(domain.CodeBadCertificate, domain.ErrNotAllowed,
			domain.Args{"name": filepath.Base(path)})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := s.db.Preferences(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	after := before
	// A reissued authority replaces the one with the same name or the same contents.
	after.Certificates.Roots = []domain.Root{}
	for _, root := range before.Certificates.Roots {
		if root.Name != name && root.PEM != string(contents) {
			after.Certificates.Roots = append(after.Certificates.Roots, root)
		}
	}
	after.Certificates.Roots = append(after.Certificates.Roots, domain.Root{Name: name, PEM: string(contents)})
	return s.commit(ctx, before, after)
}

// ProxyAddress is a proxy line read into its parts. Fields the line does not have are left out, so
// the window can merge it into the proxy settings it holds.
type ProxyAddress struct {
	Protocol domain.ProxyProtocol `json:"protocol"`
	Host     string               `json:"host"`
	Port     int                  `json:"port,omitempty"`
	User     string               `json:"user,omitempty"`
	Password string               `json:"password,omitempty"`
}

// ParseProxyAddress reads a pasted proxy line such as socks5://user:pass@host:1080.
func (s *Settings) ParseProxyAddress(raw string) (ProxyAddress, error) {
	held := strings.TrimSpace(raw)
	if !strings.Contains(held, "://") {
		held = "http://" + held
	}
	parsed, err := url.Parse(held)
	if err != nil || parsed.Hostname() == "" {
		return ProxyAddress{}, domain.Refuse(domain.CodeBadProxyAddress, domain.ErrNotAllowed,
			domain.Args{"address": raw})
	}
	out := ProxyAddress{Host: parsed.Hostname()}
	switch strings.ToLower(parsed.Scheme) {
	case "http":
		out.Protocol = domain.ProxyHTTP
	case "https":
		out.Protocol = domain.ProxyHTTPS
	case "socks5", "socks5h":
		out.Protocol = domain.ProxySOCKS5
	default:
		return ProxyAddress{}, domain.Refuse(domain.CodeUnknownProxyProtocol, domain.ErrNotAllowed,
			domain.Args{"protocol": parsed.Scheme})
	}
	out.Port, _ = strconv.Atoi(parsed.Port())
	if parsed.User != nil {
		out.User = parsed.User.Username()
		out.Password, _ = parsed.User.Password()
	}
	return out, nil
}

// Network is the HTTP engine as the network settings see it.
type Network interface {
	Probe(ctx context.Context) domain.ProxyProbe
	ProxyEnvironment() []domain.EnvEntry
}

// TestProxy sends one request through the configured proxy.
func (s *Settings) TestProxy(ctx context.Context) domain.ProxyProbe {
	return s.engine.Probe(ctx)
}

// ProxyEnvironment is the proxy variables the system mode follows.
func (s *Settings) ProxyEnvironment() []domain.EnvEntry {
	return s.engine.ProxyEnvironment()
}

// commit stores after, runs the watchers and announces the change. Callers hold s.mu.
func (s *Settings) commit(ctx context.Context, before, after domain.Settings) (domain.Settings, error) {
	if err := s.db.SavePreferences(ctx, after); err != nil {
		return domain.Settings{}, err
	}
	for i, watch := range s.watchers {
		if err := watch(ctx, before, after); err != nil {
			undo := s.db.SavePreferences(ctx, before)
			for j := i - 1; j >= 0; j-- {
				_ = s.watchers[j](ctx, after, before)
			}
			return domain.Settings{}, errors.Join(err, undo)
		}
	}
	s.notifier.Publish(domain.TopicChanged, domain.Changed{Kind: SettingsKind})
	return visible(after), nil
}

// patched applies a merge patch to the document.
func patched(settings domain.Settings, patch map[string]any) (domain.Settings, error) {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return domain.Settings{}, err
	}
	var doc map[string]any
	if err := json.Unmarshal(encoded, &doc); err != nil {
		return domain.Settings{}, err
	}
	merge(doc, patch)
	if encoded, err = json.Marshal(doc); err != nil {
		return domain.Settings{}, err
	}
	out := domain.DefaultSettings()
	if err := json.Unmarshal(encoded, &out); err != nil {
		field := ""
		if typed, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
			field = typed.Field
		}
		return domain.Settings{}, domain.Refuse(domain.CodeUnknownField, domain.ErrNotAllowed,
			domain.Args{"field": field})
	}
	return out, nil
}

func merge(target, patch map[string]any) {
	for key, value := range patch {
		switch value := value.(type) {
		case nil:
			delete(target, key)
		case map[string]any:
			into, _ := target[key].(map[string]any)
			if into == nil {
				into = map[string]any{}
			}
			merge(into, value)
			target[key] = into
		default:
			target[key] = value
		}
	}
}

// visible strips the secrets and says instead whether they are set.
func visible(settings domain.Settings) domain.Settings {
	settings.MCP.HasToken = settings.MCP.Token != ""
	settings.MCP.TokenTail = ""
	if runes := []rune(settings.MCP.Token); len(runes) > 4 {
		settings.MCP.TokenTail = string(runes[len(runes)-4:])
	}
	settings.MCP.Token = ""
	settings.Proxy.HasPassword = settings.Proxy.Password != ""
	settings.Proxy.Password = ""
	return settings
}

// mintToken is 14 random bytes as hex behind the app's prefix: short enough to paste into a
// client's config, far too long to guess.
func mintToken() (string, error) {
	raw := make([]byte, 14)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("minting an MCP token: %w", err)
	}
	return domain.MCPTokenPrefix + hex.EncodeToString(raw), nil
}

// rootName is the subject of the first certificate in the PEM, or fallback when it has none.
func rootName(fallback, contents string) (string, bool) {
	for rest := []byte(contents); ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return "", false
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		parsed, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if common := strings.TrimSpace(parsed.Subject.CommonName); common != "" {
			return common, true
		}
		return strings.TrimSpace(fallback), true
	}
}
