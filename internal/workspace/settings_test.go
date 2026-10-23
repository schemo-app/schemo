package workspace

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"schemo/internal/db"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

type heard struct{ events []any }

func (h *heard) Publish(topic string, payload any) {
	if topic == domain.TopicChanged {
		h.events = append(h.events, payload)
	}
}

type fixture struct {
	*Settings
	db     *db.Store
	heard  *heard
	picked string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.New(t)
	f := &fixture{db: db, heard: &heard{}}
	f.Settings = NewSettings(db, f.heard, nil, platform.NewFileReader(), func(string) (string, error) { return f.picked, nil })
	return f
}

func (f *fixture) save(t *testing.T, patch string) (domain.Settings, error) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(patch), &decoded); err != nil {
		t.Fatal(err)
	}
	return f.Save(t.Context(), decoded)
}

func (f *fixture) mustSave(t *testing.T, patch string) domain.Settings {
	t.Helper()
	saved, err := f.save(t, patch)
	if err != nil {
		t.Fatalf("Save(%s): %v", patch, err)
	}
	return saved
}

func (f *fixture) stored(t *testing.T) domain.Settings {
	t.Helper()
	prefs, err := f.db.Preferences(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return prefs
}

func TestAFreshDatabaseAnswersWithTheDefaults(t *testing.T) {
	f := newFixture(t)
	got, err := f.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, domain.DefaultSettings()) {
		t.Errorf("Get on a fresh database =\n%+v\nwant the defaults\n%+v", got, domain.DefaultSettings())
	}
}

func TestADamagedDocumentFallsBackFieldByField(t *testing.T) {
	f := newFixture(t)
	if err := f.db.SaveSetting(t.Context(), domain.SettingPreferences, `{
		"theme": "purple", "language": "not a tag", "sideWidth": 9999, "requestHeight": 3,
		"listSide": "up", "historyRetention": "30", "mcp": {"enabled": true, "port": 80, "mode": "x"},
		"shortcuts": {"palette": "mod+K", "broken": "nonsense+"}, "appearance": {"accent": "plaid", "monoSize": 99},
		"onboarding": {"done": ["send", "send", "unknown"]}
	}`); err != nil {
		t.Fatal(err)
	}
	got, err := f.Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	def := domain.DefaultSettings()
	if got.Theme != def.Theme || got.Language != def.Language || got.ListSide != def.ListSide {
		t.Errorf("unknown words were kept: %q %q %q", got.Theme, got.Language, got.ListSide)
	}
	if got.SideWidth != domain.MaxSideWidth || got.RequestHeight != domain.MinRequestHeight {
		t.Errorf("lengths not clamped: side %d, request %d", got.SideWidth, got.RequestHeight)
	}
	if got.HistoryRetention != domain.RetainMonth {
		t.Errorf("a valid value was lost: retention %q", got.HistoryRetention)
	}
	if got.MCP.Enabled || got.MCP.Port != domain.DefaultMCPPort || got.MCP.Mode != domain.MCPSend {
		t.Errorf("an MCP server without a token or with a bad port reads as on: %+v", got.MCP)
	}
	if !reflect.DeepEqual(got.Shortcuts, map[string]string{"palette": "mod+K"}) {
		t.Errorf("shortcuts = %v, want only the valid one", got.Shortcuts)
	}
	if got.Appearance.Accent != domain.AccentBlue || got.Appearance.MonoSize != domain.MaxMonoSize {
		t.Errorf("appearance = %+v", got.Appearance)
	}
	if !reflect.DeepEqual(got.Onboarding.Done, []string{"send"}) {
		t.Errorf("onboarding steps = %v, want a set of known steps", got.Onboarding.Done)
	}

	if err := f.db.SaveSetting(t.Context(), domain.SettingPreferences, `not json`); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.Get(t.Context()); !reflect.DeepEqual(got, def) {
		t.Errorf("an unreadable document = %+v, want the defaults", got)
	}
}

func TestSaveChangesOnlyWhatThePatchNames(t *testing.T) {
	f := newFixture(t)
	f.mustSave(t, `{"theme": "dark", "requests": {"timeout": 5}}`)
	got := f.mustSave(t, `{"requests": {"userAgent": "  probe/1  "}}`)

	if got.Theme != domain.ThemeDark {
		t.Errorf("theme = %q, an earlier save was undone", got.Theme)
	}
	want := domain.DefaultRequests()
	want.Timeout, want.UserAgent = 5, "probe/1"
	if got.Requests != want {
		t.Errorf("requests = %+v, want %+v", got.Requests, want)
	}
	if !reflect.DeepEqual(f.stored(t), got) {
		t.Errorf("the answer is not what was stored")
	}
	if len(f.heard.events) != 2 || f.heard.events[0] != (domain.Changed{Kind: SettingsKind}) {
		t.Errorf("events = %v, want one changed{settings} per save", f.heard.events)
	}
}

func TestShortcutsAreAddedAndRemovedOneByOne(t *testing.T) {
	f := newFixture(t)
	f.mustSave(t, `{"shortcuts": {"palette": "mod+K", "send": "mod+enter"}}`)
	got := f.mustSave(t, `{"shortcuts": {"palette": null}}`)
	if !reflect.DeepEqual(got.Shortcuts, map[string]string{"send": "mod+enter"}) {
		t.Errorf("shortcuts = %v", got.Shortcuts)
	}
	if got = f.mustSave(t, `{"shortcuts": null}`); got.Shortcuts != nil {
		t.Errorf("a reset left %v", got.Shortcuts)
	}
}

func TestSaveKeepsNumbersInRange(t *testing.T) {
	f := newFixture(t)
	got := f.mustSave(t, `{"requestHeight": 5, "inspectorWidth": 10000,
		"requests": {"timeout": 9999, "maxRedirects": -3}, "appearance": {"monoSize": 1}}`)
	if got.RequestHeight != domain.MinRequestHeight || got.InspectorWidth != domain.MaxPanelWidth {
		t.Errorf("layout = %d / %d", got.RequestHeight, got.InspectorWidth)
	}
	if got.Requests.Timeout != domain.MaxRequestTimeout || got.Requests.MaxRedirects != 0 {
		t.Errorf("requests = %+v", got.Requests)
	}
	if got.Appearance.MonoSize != domain.MinMonoSize {
		t.Errorf("mono size = %d", got.Appearance.MonoSize)
	}
	if got = f.mustSave(t, `{"requestHeight": 0}`); got.RequestHeight != 0 {
		t.Errorf("zero is a pane that fits itself, got %d", got.RequestHeight)
	}
}

func TestTheListRemembersTheSideItWasPutAwayFrom(t *testing.T) {
	f := newFixture(t)
	f.mustSave(t, `{"listSide": "right"}`)
	got := f.mustSave(t, `{"listSide": "hidden"}`)
	if got.ListSide != domain.ListSideHidden || got.ListSideLast != domain.ListSideRight {
		t.Errorf("side = %q, last = %q", got.ListSide, got.ListSideLast)
	}
}

func TestSaveRefusesAChoiceTheAppDoesNotHave(t *testing.T) {
	for patch, code := range map[string]domain.Code{
		`{"theme": "sepia"}`:                           domain.CodeUnknownTheme,
		`{"language": "english"}`:                      domain.CodeUnknownLanguage,
		`{"listSide": "top"}`:                          domain.CodeUnknownListSide,
		`{"historyRetention": "1"}`:                    domain.CodeUnknownRetention,
		`{"updateChannel": "nightly"}`:                 domain.CodeUnknownChannel,
		`{"indent": "three"}`:                          domain.CodeUnknownField,
		`{"mcp": {"port": 80}}`:                        domain.CodeUnknownPort,
		`{"mcp": {"mode": "admin"}}`:                   domain.CodeUnknownField,
		`{"shortcuts": {"palette": "k+"}}`:             domain.CodeUnknownShortcut,
		`{"proxy": {"mode": "manual", "host": "a b"}}`: domain.CodeBadProxyAddress,
		`{"requests": {"httpVersion": "h9"}}`:          domain.CodeUnknownHTTPVersion,
		`{"appearance": {"accent": "plaid"}}`:          domain.CodeUnknownField,
		`{"sideWidth": "wide"}`:                        domain.CodeUnknownField,
	} {
		t.Run(patch, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.save(t, patch)
			if failure := domain.AsFailure(err); failure == nil || failure.Code != code {
				t.Fatalf("Save = %v, want refusal %s", err, code)
			}
			if !reflect.DeepEqual(f.stored(t), domain.DefaultSettings()) || len(f.heard.events) != 0 {
				t.Errorf("a refused save left a trace: %+v events %v", f.stored(t), f.heard.events)
			}
		})
	}
}

func TestAnyCanonicalLanguageTagIsAccepted(t *testing.T) {
	f := newFixture(t)
	if got := f.mustSave(t, `{"language": "pt-BR"}`); got.Language != "pt-BR" {
		t.Errorf("language = %q", got.Language)
	}
}

func TestTheProxyPasswordIsKeptButNeverShown(t *testing.T) {
	f := newFixture(t)
	got := f.mustSave(t, `{"proxy": {"mode": "manual", "host": "proxy.test", "port": 3128, "password": "hunter2"}}`)
	if got.Proxy.Password != "" || !got.Proxy.HasPassword {
		t.Errorf("answer carries the password or does not say it is set: %+v", got.Proxy)
	}
	got = f.mustSave(t, `{"proxy": {"user": "me"}}`)
	if revealed, _ := f.RevealProxyPassword(t.Context()); revealed != "hunter2" || !got.Proxy.HasPassword {
		t.Errorf("a save without the password lost it: %q", revealed)
	}
	if raw, _, _ := f.db.Setting(t.Context(), domain.SettingPreferences); strings.Contains(raw, "hasPassword\":true") {
		t.Errorf("a derived flag was stored: %s", raw)
	}
	f.mustSave(t, `{"proxy": {"password": ""}}`)
	if revealed, _ := f.RevealProxyPassword(t.Context()); revealed != "" {
		t.Errorf("an empty password did not clear it")
	}
}

func TestTurningTheMCPOnMintsATokenOnlyGoKnows(t *testing.T) {
	f := newFixture(t)
	got := f.mustSave(t, `{"mcp": {"enabled": true, "token": "chosen-by-the-window"}}`)
	token, _ := f.RevealMCPToken(t.Context())
	if !strings.HasPrefix(token, domain.MCPTokenPrefix) || len(token) != len(domain.MCPTokenPrefix)+28 {
		t.Fatalf("token = %q", token)
	}
	if got.MCP.Token != "" || !got.MCP.HasToken || got.MCP.TokenTail != token[len(token)-4:] {
		t.Errorf("answer = %+v", got.MCP)
	}

	f.mustSave(t, `{"mcp": {"enabled": false}}`)
	if kept, _ := f.RevealMCPToken(t.Context()); kept != token {
		t.Errorf("switching off changed the token")
	}
	if _, err := f.RotateMCPToken(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rotated, _ := f.RevealMCPToken(t.Context()); rotated == token || rotated == "" {
		t.Errorf("rotation kept %q", rotated)
	}
}

func TestWatchersSeeTheChangeAndCanUndoIt(t *testing.T) {
	f := newFixture(t)
	var calls []string
	f.Watch(func(_ context.Context, before, after domain.Settings) error {
		calls = append(calls, string(before.Theme)+">"+string(after.Theme))
		return nil
	})
	f.mustSave(t, `{"theme": "dark"}`)
	if !reflect.DeepEqual(calls, []string{"system>dark"}) {
		t.Fatalf("calls = %v", calls)
	}

	refused := errors.New("port taken")
	f.Watch(func(_ context.Context, _, after domain.Settings) error {
		if after.Theme == domain.ThemeLight {
			return refused
		}
		return nil
	})
	calls = nil
	if _, err := f.save(t, `{"theme": "light"}`); !errors.Is(err, refused) {
		t.Fatalf("Save = %v", err)
	}
	if f.stored(t).Theme != domain.ThemeDark {
		t.Errorf("a refused watcher left the change stored")
	}
	if !reflect.DeepEqual(calls, []string{"dark>light", "light>dark"}) {
		t.Errorf("calls = %v, want the earlier watcher told to go back", calls)
	}
	if len(f.heard.events) != 1 {
		t.Errorf("a refused save was announced")
	}
}

func TestParseProxyAddress(t *testing.T) {
	f := newFixture(t)
	got, err := f.ParseProxyAddress(" socks5h://me:pw@proxy.test:1080 ")
	if err != nil {
		t.Fatal(err)
	}
	if got.Protocol != domain.ProxySOCKS5 || got.Host != "proxy.test" || got.Port != 1080 ||
		got.User != "me" || got.Password != "pw" {
		t.Errorf("parsed = %+v", got)
	}
	if got, err = f.ParseProxyAddress("proxy.test"); err != nil || got.Protocol != domain.ProxyHTTP ||
		got.Port != 0 || got.User != "" {
		t.Errorf("a bare host = %+v, %v", got, err)
	}
	for raw, code := range map[string]domain.Code{
		"ftp://x": domain.CodeUnknownProxyProtocol, "://": domain.CodeBadProxyAddress,
	} {
		if _, err := f.ParseProxyAddress(raw); domain.AsFailure(err) == nil || domain.AsFailure(err).Code != code {
			t.Errorf("ParseProxyAddress(%q) = %v, want %s", raw, err, code)
		}
	}
}

func TestRootCertificatesAreNamedAndReplaced(t *testing.T) {
	f := newFixture(t)
	dir := t.TempDir()

	if got, err := f.AddRootCertificate(t.Context(), "Pick"); err != nil || len(got.Certificates.Roots) != 0 {
		t.Fatalf("a closed dialog = %+v, %v", got.Certificates, err)
	}
	if len(f.heard.events) != 0 {
		t.Errorf("a closed dialog was announced")
	}

	f.picked = filepath.Join(dir, "junk.pem")
	writeFile(t, f.picked, "not a certificate")
	if _, err := f.AddRootCertificate(t.Context(), "Pick"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a junk file = %v", err)
	}

	f.picked = filepath.Join(dir, "a.pem")
	writeFile(t, f.picked, certificate(t, "Example CA"))
	f.mustAdd(t)
	f.picked = filepath.Join(dir, "b.pem")
	writeFile(t, f.picked, certificate(t, "Example CA"))
	got := f.mustAdd(t)
	if len(got.Certificates.Roots) != 1 || got.Certificates.Roots[0].Name != "Example CA" {
		t.Errorf("roots = %+v, want the reissued one replacing the old", got.Certificates.Roots)
	}

	got = f.mustSave(t, `{"certificates": {"verify": false}}`)
	if len(got.Certificates.Roots) != 1 {
		t.Errorf("a switch dropped the authorities")
	}
	if got = f.mustSave(t, `{"certificates": {"roots": []}}`); len(got.Certificates.Roots) != 0 {
		t.Errorf("roots were not removed")
	}
}

func (f *fixture) mustAdd(t *testing.T) domain.Settings {
	t.Helper()
	got, err := f.AddRootCertificate(t.Context(), "Pick")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func certificate(t *testing.T, name string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour), IsCA: true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
