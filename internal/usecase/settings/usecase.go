package settings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/text/language"

	"schemo/internal/domain"
)

const TopicThemeChanged = "settings:theme"
const TopicLanguageChanged = "settings:language"
const TopicChanged = "settings:changed"

type UseCase struct {
	store    Store
	notifier domain.Notifier
	outbound Outbound
}

func NewUseCase(store Store, notifier domain.Notifier, outbound Outbound) *UseCase {
	return &UseCase{store: store, notifier: notifier, outbound: outbound}
}

func (u *UseCase) Snapshot(ctx context.Context) (domain.Settings, error) {
	stored, err := u.store.Settings(ctx)
	if err != nil {
		return domain.Settings{}, err
	}

	out := domain.DefaultSettings()
	keepEnum(stored[domain.SettingTheme], &out.Theme)
	if choice := stored[domain.SettingLanguage]; validLanguage(choice) {
		out.Language = choice
	}
	keepBool(stored[domain.SettingInspectorOpen], &out.InspectorOpen)
	out.InspectorWidth = readLength(stored[domain.SettingInspectorWidth],
		domain.MinPanelWidth, domain.MaxPanelWidth, out.InspectorWidth)
	out.SideWidth = readLength(stored[domain.SettingSideWidth],
		domain.MinSideWidth, domain.MaxSideWidth, out.SideWidth)
	out.RequestHeight = heightOf(stored[domain.SettingRequestHeight])
	keepEnum(stored[domain.SettingListSide], &out.ListSide)
	if last := domain.ListSide(stored[domain.SettingListSideLast]); last.Valid() &&
		last != domain.ListSideHidden {
		out.ListSideLast = last
	}
	keepEnum(stored[domain.SettingHistoryRetention], &out.HistoryRetention)
	keepJSON(stored[domain.SettingCaptureFilters], &out.CaptureFilters)
	out.CaptureFilters.Hosts = domain.OrEmpty(out.CaptureFilters.Hosts)
	keepJSON(stored[domain.SettingExtension], &out.Extension)
	// The scopes are read on their own — the engine asks for them from another feature, and this is
	// the same answer it gets. A read that fails fails the snapshot rather than falling back to the
	// defaults, which are the permissive ones: a window must not draw secrets as staying behind when
	// nobody knows whether they do.
	scopes, err := u.store.SyncScopes(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	out.SyncScopes = scopes
	if shortcuts, ok := parseShortcuts(stored[domain.SettingShortcuts]); ok {
		out.Shortcuts = shortcuts
	}
	if mcp, ok := parseMCP(stored[domain.SettingMCP]); ok {
		out.MCP = mcp
	}
	out.MCP.HasToken = out.MCP.Token != ""
	out.MCP.TokenTail = tokenTail(out.MCP.Token)
	out.MCP.Token = ""
	keepBool(stored[domain.SettingUpdateAuto], &out.UpdateCheckAuto)
	keepEnum(stored[domain.SettingUpdateChannel], &out.UpdateChannel)
	keepBool(stored[domain.SettingWrapLines], &out.WrapLines)
	keepBool(stored[domain.SettingLineNumbers], &out.LineNumbers)
	keepEnum(stored[domain.SettingIndent], &out.Indent)
	keepBool(stored[domain.SettingReopenWorkspace], &out.ReopenWorkspace)
	keepBool(stored[domain.SettingRestoreTabs], &out.RestoreTabs)
	out.Proxy = parseProxy(stored[domain.SettingProxy])
	out.Proxy.HasPassword = out.Proxy.Password != ""
	out.Proxy.Password = ""
	out.Certificates = parseCertificates(stored[domain.SettingCertificates])
	out.Requests = parseRequests(stored[domain.SettingRequests])
	out.Appearance = parseAppearance(stored[domain.SettingAppearance])
	keepJSON(stored[domain.SettingOnboarding], &out.Onboarding)
	out.Onboarding.Done = domain.OrEmpty(out.Onboarding.Done)
	return out, nil
}

func (u *UseCase) announced(current domain.Settings, err error) (domain.Settings, error) {
	if err != nil {
		return current, err
	}
	u.notifier.Publish(TopicChanged, current)
	return current, nil
}

func (u *UseCase) SetTheme(ctx context.Context, theme domain.Theme) (domain.Settings, error) {
	if !theme.Valid() {
		return domain.Settings{}, domain.Refuse(domain.CodeUnknownTheme, domain.ErrNotAllowed,
			domain.Args{"theme": string(theme)})
	}
	if err := u.save(ctx, domain.SettingTheme, string(theme)); err != nil {
		return domain.Settings{}, err
	}
	u.notifier.Publish(TopicThemeChanged, ThemeChanged{Theme: theme})
	return u.Snapshot(ctx)
}

func (u *UseCase) SetLanguage(
	ctx context.Context,
	language domain.Language,
) (domain.Settings, error) {
	if !validLanguage(language) {
		return domain.Settings{}, domain.Refuse(domain.CodeUnknownLanguage, domain.ErrNotAllowed,
			domain.Args{"language": string(language)})
	}
	if err := u.save(ctx, domain.SettingLanguage, string(language)); err != nil {
		return domain.Settings{}, err
	}
	u.notifier.Publish(TopicLanguageChanged, LanguageChanged{Language: language})
	return u.Snapshot(ctx)
}

func validLanguage(value string) bool {
	if value == domain.LanguageSystem {
		return true
	}
	tag, err := language.Parse(value)
	return err == nil && tag != language.Und && tag.String() == value
}

func (u *UseCase) SetLayout(ctx context.Context, patch LayoutPatch) (domain.Settings, error) {
	if patch.InspectorOpen != nil {
		if err := u.save(ctx, domain.SettingInspectorOpen,
			strconv.FormatBool(*patch.InspectorOpen)); err != nil {
			return domain.Settings{}, err
		}
	}
	if patch.InspectorWidth != nil {
		if err := u.save(ctx, domain.SettingInspectorWidth, strconv.Itoa(
			between(*patch.InspectorWidth, domain.MinPanelWidth, domain.MaxPanelWidth))); err != nil {
			return domain.Settings{}, err
		}
	}
	if patch.SideWidth != nil {
		if err := u.save(ctx, domain.SettingSideWidth, strconv.Itoa(
			between(*patch.SideWidth, domain.MinSideWidth, domain.MaxSideWidth))); err != nil {
			return domain.Settings{}, err
		}
	}
	if patch.RequestHeight != nil {
		if err := u.save(ctx, domain.SettingRequestHeight, strconv.Itoa(
			requestHeight(*patch.RequestHeight))); err != nil {
			return domain.Settings{}, err
		}
	}

	if patch.ListSide != nil {
		if !patch.ListSide.Valid() {
			return domain.Settings{}, domain.Refuse(domain.CodeUnknownListSide, domain.ErrNotAllowed,
				domain.Args{"side": string(*patch.ListSide)})
		}
		if err := u.save(ctx, domain.SettingListSide, string(*patch.ListSide)); err != nil {
			return domain.Settings{}, err
		}
		if *patch.ListSide != domain.ListSideHidden {
			if err := u.save(ctx, domain.SettingListSideLast, string(*patch.ListSide)); err != nil {
				return domain.Settings{}, err
			}
		}
		return u.announced(u.Snapshot(ctx))
	}
	return u.Snapshot(ctx)
}

// Retention is how long the history is kept.
func (u *UseCase) Retention(ctx context.Context) (domain.Retention, error) {
	current, err := u.Snapshot(ctx)
	if err != nil {
		return "", err
	}
	return current.HistoryRetention, nil
}

// RestoreTabs is whether the strip of the last run comes back.
func (u *UseCase) RestoreTabs(ctx context.Context) (bool, error) {
	current, err := u.Snapshot(ctx)
	if err != nil {
		return false, err
	}
	return current.RestoreTabs, nil
}

func (u *UseCase) SetRetention(
	ctx context.Context,
	retention domain.Retention,
) (domain.Settings, error) {
	if !retention.Valid() {
		return domain.Settings{}, domain.Refuse(domain.CodeUnknownRetention, domain.ErrNotAllowed,
			domain.Args{"retention": string(retention)})
	}
	return u.saveOne(ctx, domain.SettingHistoryRetention, string(retention))
}

func (u *UseCase) SetCaptureFilters(
	ctx context.Context,
	filters domain.CaptureFilters,
) (domain.Settings, error) {
	filters.Hosts = domain.OrEmpty(filters.Hosts)
	return u.saveJSON(ctx, domain.SettingCaptureFilters, filters)
}

// SetShortcut writes down the combination one command wears. An empty one takes the command back
// to the combination it comes with, and there is no third thing to say: the window draws the
// registry, and this keeps what was changed from it.
func (u *UseCase) SetShortcut(
	ctx context.Context,
	id, combo string,
) (domain.Settings, error) {
	if id == "" || (combo != "" && !domain.ValidShortcut(combo)) {
		return domain.Settings{}, domain.Refuse(domain.CodeUnknownShortcut, domain.ErrNotAllowed,
			domain.Args{"combo": combo})
	}

	before, err := u.Snapshot(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	held := make(map[string]string, len(before.Shortcuts)+1)
	for key, value := range before.Shortcuts {
		held[key] = value
	}
	if combo == "" {
		delete(held, id)
	} else {
		held[id] = combo
	}

	return u.saveJSON(ctx, domain.SettingShortcuts, held)
}

// SetSyncScopes changes what of this device is allowed to leave it. A field left out is left as it
// stands, and the engine reads the answer before its next push — nothing already sent is revisited.
func (u *UseCase) SetSyncScopes(
	ctx context.Context,
	patch SyncScopesPatch,
) (domain.Settings, error) {
	held, err := u.store.SyncScopes(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	if patch.Runs != nil {
		held.Runs = *patch.Runs
	}
	if patch.Secrets != nil {
		held.Secrets = *patch.Secrets
	}

	return u.saveJSON(ctx, domain.SettingSyncScopes, held)
}

// ResetShortcuts takes every command back to the combination it comes with.
func (u *UseCase) ResetShortcuts(ctx context.Context) (domain.Settings, error) {
	if err := u.save(ctx, domain.SettingShortcuts, ""); err != nil {
		return domain.Settings{}, err
	}
	return u.announced(u.Snapshot(ctx))
}

// MCPSettings is the MCP server as it stands, read whole by the server itself. The window gets the
// same in Snapshot; this is the caller that must not wait for a broadcast.
func (u *UseCase) MCPSettings(ctx context.Context) (domain.MCPSettings, error) {
	stored, err := u.store.Settings(ctx)
	if err != nil {
		return domain.MCPSettings{}, err
	}
	held, _ := parseMCP(stored[domain.SettingMCP])
	return held, nil
}

// RevealMCPToken is the token itself, for the eye in the MCP section. It is the one call that hands
// the value over: every answer carries HasToken, and the configuration an agent is given is built
// from what this returns rather than from a snapshot.
func (u *UseCase) RevealMCPToken(ctx context.Context) (string, error) {
	held, err := u.MCPSettings(ctx)
	if err != nil {
		return "", err
	}
	return held.Token, nil
}

// SetMCP changes the MCP server. A field left out is left as it stands; switching the server on
// mints the token if there is none, because a server nobody can call is not one that was turned on.
//
// The applier is the server itself, and it is called between the write and the telling: taking a
// port can fail — somebody else has it — and a row promising a door nobody can open is the one
// state worth undoing. It is a function rather than a port because the server is built out of this
// use case, and an interface declared here would be a cycle in the wiring.
func (u *UseCase) SetMCP(
	ctx context.Context,
	patch MCPPatch,
	apply func(domain.MCPSettings) error,
) (domain.Settings, error) {
	held, err := u.MCPSettings(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	before, err := json.Marshal(held)
	if err != nil {
		return domain.Settings{}, err
	}
	if patch.Port != nil {
		if !domain.ValidMCPPort(*patch.Port) {
			return domain.Settings{}, domain.Refuse(domain.CodeUnknownPort, domain.ErrNotAllowed,
				domain.Args{
					"port": strconv.Itoa(*patch.Port),
					"min":  strconv.Itoa(domain.MinMCPPort),
					"max":  strconv.Itoa(domain.MaxMCPPort),
				})
		}
		held.Port = *patch.Port
	}
	if patch.Mode != nil {
		if !patch.Mode.Valid() {
			return domain.Settings{}, domain.Refuse(domain.CodeUnknownField, domain.ErrNotAllowed,
				domain.Args{"field": "mode"})
		}
		held.Mode = *patch.Mode
	}
	if patch.Ask != nil {
		held.Ask = *patch.Ask
	}
	if patch.AllSpaces != nil {
		held.AllSpaces = *patch.AllSpaces
	}
	if patch.Spaces != nil {
		held.Spaces = cleanSpaces(*patch.Spaces)
	}
	if patch.Enabled != nil {
		held.Enabled = *patch.Enabled
		if held.Enabled && held.Token == "" {
			token, err := mintMCPToken()
			if err != nil {
				return domain.Settings{}, err
			}
			held.Token = token
		}
	}

	if err := u.writeJSON(ctx, domain.SettingMCP, held); err != nil {
		return domain.Settings{}, err
	}
	if err := apply(held); err != nil {
		// The row goes back to what it said. Nothing is listening on the new answer, and a window
		// reading the row would draw a server that is not there — about who may call this app, which
		// is the worst thing to be wrong about.
		if back := u.save(ctx, domain.SettingMCP, string(before)); back != nil {
			return domain.Settings{}, errors.Join(err, back)
		}
		return domain.Settings{}, err
	}
	return u.announced(u.Snapshot(ctx))
}

// RotateMCPToken mints a fresh token for a server that is already handed out. What happens to the
// agents holding the old one is the server's: this only changes what the guard accepts.
//
// The new value reaches the guard here and not through the answer, because a snapshot carries none:
// the one caller that has to know it is the server being re-keyed.
func (u *UseCase) RotateMCPToken(
	ctx context.Context,
	accept func(token string),
) (domain.Settings, error) {
	held, err := u.MCPSettings(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	token, err := mintMCPToken()
	if err != nil {
		return domain.Settings{}, err
	}
	held.Token = token

	if err := u.writeJSON(ctx, domain.SettingMCP, held); err != nil {
		return domain.Settings{}, err
	}
	accept(token)
	return u.announced(u.Snapshot(ctx))
}

// SetExtension changes how the app and the extension behave together. The switches are kept here,
// and the window is what hands them to the extension; a patch that says nothing keeps what stands.
func (u *UseCase) SetExtension(
	ctx context.Context,
	patch ExtensionPatch,
) (domain.Settings, error) {
	before, err := u.Snapshot(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	held := before.Extension
	if patch.AutoConnect != nil {
		held.AutoConnect = *patch.AutoConnect
	}
	if patch.RememberTabs != nil {
		held.RememberTabs = *patch.RememberTabs
	}

	return u.saveJSON(ctx, domain.SettingExtension, held)
}

// SetOnboarding changes what the window's welcome has been told. The steps are held to the domain's
// own list, so a name this build does not know never lands in the row, and the list is a set: the
// same step said twice is one step. A patch that says nothing keeps what stands.
func (u *UseCase) SetOnboarding(
	ctx context.Context,
	patch OnboardingPatch,
) (domain.Settings, error) {
	before, err := u.Snapshot(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	held := before.Onboarding
	if patch.Seen != nil {
		held.Seen = *patch.Seen
	}
	if patch.Dismissed != nil {
		held.Dismissed = *patch.Dismissed
	}
	if patch.Done != nil {
		held.Done = nil
		worn := map[string]bool{}
		for _, raw := range *patch.Done {
			if domain.OnboardingStep(raw).Valid() && !worn[raw] {
				worn[raw] = true
				held.Done = append(held.Done, raw)
			}
		}
		held.Done = domain.OrEmpty(held.Done)
	}

	return u.saveJSON(ctx, domain.SettingOnboarding, held)
}

func (u *UseCase) SetUpdateCheck(ctx context.Context, auto bool) (domain.Settings, error) {
	return u.saveOne(ctx, domain.SettingUpdateAuto, strconv.FormatBool(auto))
}

func (u *UseCase) SetUpdateChannel(
	ctx context.Context,
	channel domain.UpdateChannel,
) (domain.Settings, error) {
	if !channel.Valid() {
		return domain.Settings{}, domain.Refuse(domain.CodeUnknownChannel, domain.ErrNotAllowed,
			domain.Args{"channel": string(channel)})
	}
	return u.saveOne(ctx, domain.SettingUpdateChannel, string(channel))
}

func (u *UseCase) SetEditor(ctx context.Context, patch EditorPatch) (domain.Settings, error) {
	if patch.WrapLines != nil {
		if err := u.save(ctx, domain.SettingWrapLines,
			strconv.FormatBool(*patch.WrapLines)); err != nil {
			return domain.Settings{}, err
		}
	}
	if patch.LineNumbers != nil {
		if err := u.save(ctx, domain.SettingLineNumbers,
			strconv.FormatBool(*patch.LineNumbers)); err != nil {
			return domain.Settings{}, err
		}
	}
	if patch.Indent != nil {
		if !patch.Indent.Valid() {
			return domain.Settings{}, domain.Refuse(domain.CodeUnknownField, domain.ErrNotAllowed,
				domain.Args{"field": "indent"})
		}
		if err := u.save(ctx, domain.SettingIndent, string(*patch.Indent)); err != nil {
			return domain.Settings{}, err
		}
	}
	return u.announced(u.Snapshot(ctx))
}

func (u *UseCase) SetReopenWorkspace(ctx context.Context, reopen bool) (domain.Settings, error) {
	return u.saveOne(ctx, domain.SettingReopenWorkspace, strconv.FormatBool(reopen))
}

// SetRestoreTabs is the strip's coming back. Switching it on does not bring back the strip of a run
// that happened while it was off: that strip was never written down, and the switch only says what
// happens from now on.
func (u *UseCase) SetRestoreTabs(ctx context.Context, restore bool) (domain.Settings, error) {
	return u.saveOne(ctx, domain.SettingRestoreTabs, strconv.FormatBool(restore))
}

func (u *UseCase) save(ctx context.Context, key, value string) error {
	return u.store.SaveSetting(ctx, key, value)
}

// saveOne writes a single row and tells every window what the settings are now.
func (u *UseCase) saveOne(ctx context.Context, key, value string) (domain.Settings, error) {
	if err := u.save(ctx, key, value); err != nil {
		return domain.Settings{}, err
	}
	return u.announced(u.Snapshot(ctx))
}

// writeJSON puts a whole value into its row: a field added to one of these types lands in the row
// without a migration, because the stored shape is the shape the feature writes.
func (u *UseCase) writeJSON(ctx context.Context, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return u.save(ctx, key, string(encoded))
}

// saveJSON is writeJSON for a setting that nothing has to act on before the answer: the row is
// written, and every window is told what the settings are now.
func (u *UseCase) saveJSON(ctx context.Context, key string, value any) (domain.Settings, error) {
	if err := u.writeJSON(ctx, key, value); err != nil {
		return domain.Settings{}, err
	}
	return u.announced(u.Snapshot(ctx))
}

// The three keepers below are the whole of "a row that says nothing this build understands leaves
// the default": an empty row, a word that is not one of ours and a value that does not parse all
// read the same, and each leaves what the caller already holds.

// keepEnum takes the stored word when it is one the type knows.
func keepEnum[T interface {
	~string
	Valid() bool
}](raw string, into *T) {
	if word := T(raw); word.Valid() {
		*into = word
	}
}

// keepBool takes the stored switch when it reads as one.
func keepBool(raw string, into *bool) {
	if value, err := strconv.ParseBool(raw); err == nil {
		*into = value
	}
}

// keepJSON takes the stored document when it parses.
func keepJSON[T any](raw string, into *T) {
	if value, ok := readJSON[T](raw); ok {
		*into = value
	}
}

// readJSON reads a stored value back, the second answer saying whether there was one: an empty row
// and a row that does not parse both mean the setting was never written, and what falls back to the
// default is the caller's — only it knows what the default is.
func readJSON[T any](raw string) (T, bool) {
	var held T
	if raw == "" {
		return held, false
	}
	if err := json.Unmarshal([]byte(raw), &held); err != nil {
		return held, false
	}
	return held, true
}

// parseMCP reads what was written down and keeps of it only answers that still hold: a row that
// names a port nothing may listen on, or a mode this app does not have, is not a server that was
// ever configured. A row that cannot be read at all leaves the defaults, which is a server that is
// off — the safe way for a broken row to fall.
func parseMCP(raw string) (domain.MCPSettings, bool) {
	held := domain.DefaultMCP()
	if raw == "" {
		return held, false
	}
	if err := json.Unmarshal([]byte(raw), &held); err != nil {
		return domain.DefaultMCP(), false
	}
	held.Token = strings.TrimSpace(held.Token)
	held.Spaces = cleanSpaces(held.Spaces)
	if !domain.ValidMCPPort(held.Port) {
		held.Port = domain.DefaultMCPPort
	}
	if !held.Mode.Valid() {
		held.Mode = domain.MCPSend
	}
	if held.Enabled && held.Token == "" {
		// A server that is on and could not tell anybody how to reach it is one that was not really
		// turned on: without a token the guard refuses every call.
		held.Enabled = false
	}
	return held, true
}

// cleanSpaces is the space list as it is kept: no blanks, nothing twice, and never nil — the empty
// list means «none picked», which is not the same as the list being forgotten.
func cleanSpaces(ids []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// mintMCPToken is 14 random bytes written as hex: long enough that guessing it is not a thing one
// does, short enough to paste into a client's config.
func mintMCPToken() (string, error) {
	raw := make([]byte, 14)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("minting an MCP token: %w", err)
	}
	return domain.MCPTokenPrefix + hex.EncodeToString(raw), nil
}

// tokenTail is the last four characters of a token: enough to tell one token from another in the
// section, too little to be worth anything to a reader. A token too short to have a tail has none:
// a reading that gives the value away is not a reading.
func tokenTail(token string) string {
	const tail = 4
	runes := []rune(token)
	if len(runes) <= tail {
		return ""
	}
	return string(runes[len(runes)-tail:])
}

func parseShortcuts(raw string) (map[string]string, bool) {
	if raw == "" {
		return nil, false
	}
	var held map[string]string
	if err := json.Unmarshal([]byte(raw), &held); err != nil || len(held) == 0 {
		return nil, false
	}
	// An entry nothing can carry out is one no keyboard can ever match, and a window that drew it
	// would be drawing a promise: only combinations this app accepts are read back.
	for id, combo := range held {
		if id == "" || !domain.ValidShortcut(combo) {
			delete(held, id)
		}
	}
	if len(held) == 0 {
		return nil, false
	}
	return held, true
}

// readLength is a stored length: nothing readable leaves the fallback, and what is readable is kept
// inside the range that length has. An empty row is not readable, so it needs no saying of its own.
func readLength(raw string, low, high, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return between(value, low, high)
}

func between(value, low, high int) int { return min(max(value, low), high) }

func requestHeight(value int) int {
	if value <= 0 {
		return 0
	}
	return between(value, domain.MinRequestHeight, domain.MaxRequestHeight)
}

// heightOf is that height as it is stored: a row that cannot be read is a pane that sizes itself.
func heightOf(raw string) int {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return requestHeight(value)
}
