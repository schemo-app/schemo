package domain

import (
	"encoding/json"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
)

// SettingPreferences is the key holding Settings as one JSON document. The secrets inside it (the
// MCP token, the proxy password) never leave Go except through the explicit reveal calls.
const SettingPreferences = "preferences"

type CaptureFilters struct {
	Hosts     []string `json:"hosts"`
	Static    bool     `json:"static"`
	Analytics bool     `json:"analytics"`
	JSON      bool     `json:"json"`
}

func DefaultCaptureFilters() CaptureFilters {
	return CaptureFilters{Hosts: []string{}, Static: true, Analytics: false, JSON: false}
}

// ExtensionSettings is how the app and the browser extension behave together: whether they connect
// on their own, and whether the addresses capture was armed on are armed again after a browser
// restart. The extension carries both out; the app only keeps and hands them over.
type ExtensionSettings struct {
	AutoConnect  bool `json:"autoConnect"`
	RememberTabs bool `json:"rememberTabs"`
}

func DefaultExtension() ExtensionSettings {
	return ExtensionSettings{AutoConnect: false, RememberTabs: true}
}

// OnboardingStep names one thing the window's own welcome walks a new person through. The window
// says which one it just did; a name outside this list is not a step and is dropped.
type OnboardingStep string

const (
	StepSend      OnboardingStep = "send"
	StepSave      OnboardingStep = "save"
	StepEnv       OnboardingStep = "env"
	StepImport    OnboardingStep = "import"
	StepCapture   OnboardingStep = "capture"
	StepWorkspace OnboardingStep = "ws"
)

// OnboardingSteps is how many steps the checklist holds — the window draws the count against it.
const OnboardingSteps = 6

// Valid reports whether the name is one of the checklist's own steps.
func (s OnboardingStep) Valid() bool {
	switch s {
	case StepSend, StepSave, StepEnv, StepImport, StepCapture, StepWorkspace:
		return true
	}
	return false
}

// Onboarding is what the welcome has been told: whether the overview was offered, whether the
// checklist was dismissed and the steps already walked. It is kept here because it outlives a run.
type Onboarding struct {
	Seen      bool     `json:"seen"`
	Dismissed bool     `json:"dismissed"`
	Done      []string `json:"done"`
}

func DefaultOnboarding() Onboarding {
	return Onboarding{Done: []string{}}
}

// SyncScopes is what of this device may leave it: collection-run results and secret variable
// values. Both default to on, which is what the app has always done.
type SyncScopes struct {
	Runs    bool `json:"runs"`
	Secrets bool `json:"secrets"`
}

func DefaultSyncScopes() SyncScopes {
	return SyncScopes{Runs: true, Secrets: true}
}

type Theme string

const (
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
	ThemeSystem Theme = "system"
)

func (t Theme) Valid() bool {
	return t == ThemeLight || t == ThemeDark || t == ThemeSystem
}

type Language = string

const LanguageSystem = "system"

type Retention string

const (
	RetainWeek    Retention = "7"
	RetainMonth   Retention = "30"
	RetainForever Retention = "forever"
)

func (r Retention) Valid() bool {
	return r == RetainWeek || r == RetainMonth || r == RetainForever
}

// Age is how old a record may grow before it is pruned; zero keeps them for ever.
func (r Retention) Age() time.Duration {
	switch r {
	case RetainWeek:
		return 7 * 24 * time.Hour
	case RetainMonth:
		return 30 * 24 * time.Hour
	default:
		return 0
	}
}

// HistoryKeepCount is how many records a workspace's history keeps, however young they are.
const HistoryKeepCount = 200

type ListSide string

const (
	ListSideLeft   ListSide = "left"
	ListSideRight  ListSide = "right"
	ListSideHidden ListSide = "hidden"
)

func (s ListSide) Valid() bool {
	return s == ListSideLeft || s == ListSideRight || s == ListSideHidden
}

// Indent is what one level of indentation is in the editors: two spaces, four, or a tab. It is the
// Editor section's third answer, after wrapping and line numbers.
type Indent string

const (
	IndentTwo  Indent = "two"
	IndentFour Indent = "four"
	IndentTab  Indent = "tab"
)

func (i Indent) Valid() bool {
	return i == IndentTwo || i == IndentFour || i == IndentTab
}

// Unit is the whitespace one level is written with. A word this build does not know reads as the
// default, so an editor and a formatter given a stale setting still agree with each other.
func (i Indent) Unit() string {
	switch i {
	case IndentFour:
		return "    "
	case IndentTab:
		return "	"
	default:
		return "  "
	}
}

type Settings struct {
	Theme            Theme             `json:"theme"`
	Language         Language          `json:"language"`
	InspectorOpen    bool              `json:"inspectorOpen"`
	InspectorWidth   int               `json:"inspectorWidth"`
	SideWidth        int               `json:"sideWidth"`
	RequestHeight    int               `json:"requestHeight"`
	ListSide         ListSide          `json:"listSide"`
	ListSideLast     ListSide          `json:"listSideLast"`
	HistoryRetention Retention         `json:"historyRetention"`
	CaptureFilters   CaptureFilters    `json:"captureFilters"`
	Extension        ExtensionSettings `json:"extension"`
	SyncScopes       SyncScopes        `json:"syncScopes"`
	MCP              MCPSettings       `json:"mcp"`
	// Shortcuts holds only what was changed: a command not in it uses its default combination, and
	// ShortcutNone means no combination. The command registry lives in the window, which owns the
	// labels and the actions.
	Shortcuts       map[string]string `json:"shortcuts,omitempty"`
	UpdateCheckAuto bool              `json:"updateCheckAuto"`
	UpdateChannel   UpdateChannel     `json:"updateChannel"`
	WrapLines       bool              `json:"wrapLines"`
	LineNumbers     bool              `json:"lineNumbers"`
	Indent          Indent            `json:"indent"`
	ReopenWorkspace bool              `json:"reopenWorkspace"`
	RestoreTabs     bool              `json:"restoreTabs"`
	Proxy           ProxySettings     `json:"proxy"`
	Certificates    Certificates      `json:"certificates"`
	Requests        Requests          `json:"requests"`
	Appearance      Appearance        `json:"appearance"`
	Onboarding      Onboarding        `json:"onboarding"`
}

func DefaultSettings() Settings {
	return Settings{
		Theme:            ThemeSystem,
		Language:         LanguageSystem,
		InspectorOpen:    false,
		InspectorWidth:   DefaultInspectorWidth,
		SideWidth:        DefaultSideWidth,
		ListSide:         ListSideLeft,
		ListSideLast:     ListSideLeft,
		HistoryRetention: RetainForever,
		CaptureFilters:   DefaultCaptureFilters(),
		Extension:        DefaultExtension(),
		SyncScopes:       DefaultSyncScopes(),
		MCP:              DefaultMCP(),
		UpdateCheckAuto:  true,
		UpdateChannel:    ChannelStable,
		WrapLines:        true,
		LineNumbers:      true,
		Indent:           IndentTwo,
		ReopenWorkspace:  true,
		RestoreTabs:      true,
		Proxy:            DefaultProxy(),
		Certificates:     DefaultCertificates(),
		Requests:         DefaultRequests(),
		Appearance:       DefaultAppearance(),
		Onboarding:       DefaultOnboarding(),
	}
}

const (
	MaxRequestTimeout  = 600
	MaxRequestRedirect = 50
)

const (
	DefaultInspectorWidth = 300
	DefaultSideWidth      = 232
	MinPanelWidth         = 220
	MaxPanelWidth         = 720
	MinSideWidth          = 200
	MaxSideWidth          = 360
	MinRequestHeight      = 72
	MaxRequestHeight      = 560
)

// ParseSettings reads the stored document. Missing fields keep their defaults and a value this
// build cannot use falls back to its default, so a damaged or older document still opens the app.
func ParseSettings(raw string) Settings {
	out := DefaultSettings()
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			out = DefaultSettings()
		}
	}
	out.Normalize()
	return out
}

// Check refuses a document with a choice the app does not have. Numbers are not refused: Normalize
// keeps them in range.
func (s Settings) Check() error {
	switch {
	case !s.Theme.Valid():
		return Refuse(CodeUnknownTheme, ErrNotAllowed, Args{"theme": string(s.Theme)})
	case !ValidLanguage(s.Language):
		return Refuse(CodeUnknownLanguage, ErrNotAllowed, Args{"language": s.Language})
	case !s.ListSide.Valid():
		return Refuse(CodeUnknownListSide, ErrNotAllowed, Args{"side": string(s.ListSide)})
	case !s.HistoryRetention.Valid():
		return Refuse(CodeUnknownRetention, ErrNotAllowed, Args{"retention": string(s.HistoryRetention)})
	case !s.UpdateChannel.Valid():
		return Refuse(CodeUnknownChannel, ErrNotAllowed, Args{"channel": string(s.UpdateChannel)})
	case !s.Indent.Valid():
		return Refuse(CodeUnknownField, ErrNotAllowed, Args{"field": "indent"})
	case !ValidMCPPort(s.MCP.Port):
		return Refuse(CodeUnknownPort, ErrNotAllowed, Args{
			"port": strconv.Itoa(s.MCP.Port),
			"min":  strconv.Itoa(MinMCPPort),
			"max":  strconv.Itoa(MaxMCPPort),
		})
	case !s.MCP.Mode.Valid():
		return Refuse(CodeUnknownField, ErrNotAllowed, Args{"field": "mode"})
	}
	for id, combo := range s.Shortcuts {
		if id == "" || !ValidShortcut(combo) {
			return Refuse(CodeUnknownShortcut, ErrNotAllowed, Args{"combo": combo})
		}
	}
	if err := s.Proxy.Valid(); err != nil {
		return err
	}
	if err := s.Requests.Valid(); err != nil {
		return err
	}
	return s.Appearance.Valid()
}

// Normalize replaces what cannot be used with defaults, keeps numbers in range and tidies lists.
func (s *Settings) Normalize() {
	def := DefaultSettings()
	orDefault(&s.Theme, def.Theme)
	if !ValidLanguage(s.Language) {
		s.Language = def.Language
	}
	orDefault(&s.ListSide, def.ListSide)
	if !s.ListSideLast.Valid() || s.ListSideLast == ListSideHidden {
		s.ListSideLast = def.ListSideLast
	}
	orDefault(&s.HistoryRetention, def.HistoryRetention)
	orDefault(&s.UpdateChannel, def.UpdateChannel)
	orDefault(&s.Indent, def.Indent)
	s.InspectorWidth = clamp(s.InspectorWidth, MinPanelWidth, MaxPanelWidth)
	s.SideWidth = clamp(s.SideWidth, MinSideWidth, MaxSideWidth)
	if s.RequestHeight > 0 {
		s.RequestHeight = clamp(s.RequestHeight, MinRequestHeight, MaxRequestHeight)
	} else {
		s.RequestHeight = 0 // the pane sizes itself
	}

	s.CaptureFilters.Hosts = OrEmpty(s.CaptureFilters.Hosts)
	s.Onboarding.Done = onboardingSet(s.Onboarding.Done)
	for id, combo := range s.Shortcuts {
		if id == "" || !ValidShortcut(combo) {
			delete(s.Shortcuts, id)
		}
	}
	if len(s.Shortcuts) == 0 {
		s.Shortcuts = nil
	}

	// These say whether a secret is set; they are worked out for the window, never stored.
	s.MCP.HasToken, s.MCP.TokenTail, s.Proxy.HasPassword = false, "", false
	s.MCP.Token = strings.TrimSpace(s.MCP.Token)
	s.MCP.Workspaces = cleanList(s.MCP.Workspaces, true)
	if !ValidMCPPort(s.MCP.Port) {
		s.MCP.Port = DefaultMCPPort
	}
	orDefault(&s.MCP.Mode, MCPSend)
	if s.MCP.Enabled && s.MCP.Token == "" {
		// Without a token the guard refuses every call, so the server is not really on.
		s.MCP.Enabled = false
	}

	orDefault(&s.Proxy.Mode, ProxySystem)
	orDefault(&s.Proxy.Protocol, ProxyHTTP)
	s.Proxy.Host = strings.TrimSpace(s.Proxy.Host)
	s.Proxy.User = strings.TrimSpace(s.Proxy.User)
	s.Proxy.Bypass = cleanList(s.Proxy.Bypass, false)

	roots := make([]Root, 0, len(s.Certificates.Roots))
	for _, root := range s.Certificates.Roots {
		if strings.TrimSpace(root.PEM) != "" {
			roots = append(roots, root)
		}
	}
	s.Certificates.Roots = roots

	orDefault(&s.Requests.HTTPVersion, HTTPAuto)
	s.Requests.UserAgent = strings.TrimSpace(s.Requests.UserAgent)
	s.Requests.Timeout = clamp(s.Requests.Timeout, 0, MaxRequestTimeout)
	s.Requests.MaxRedirects = clamp(s.Requests.MaxRedirects, 0, MaxRequestRedirect)

	orDefault(&s.Appearance.Accent, AccentBlue)
	s.Appearance.Size = SizeOf(string(s.Appearance.Size))
	orDefault(&s.Appearance.MonoFont, MonoSystem)
	s.Appearance.MonoSize = clamp(s.Appearance.MonoSize, MinMonoSize, MaxMonoSize)
}

// Network is the part of the settings the HTTP engine applies.
func (s Settings) Network() Network {
	return Network{Proxy: s.Proxy, Certificates: s.Certificates, Requests: s.Requests}
}

// ValidLanguage accepts "system" or a canonical BCP 47 tag. Catalogues live in the window, so a new
// language needs no change here.
func ValidLanguage(value string) bool {
	if value == LanguageSystem {
		return true
	}
	tag, err := language.Parse(value)
	return err == nil && tag != language.Und && tag.String() == value
}

func orDefault[T interface {
	~string
	Valid() bool
}](value *T, fallback T) {
	if !(*value).Valid() {
		*value = fallback
	}
}

func clamp(value, low, high int) int { return min(max(value, low), high) }

// cleanList trims entries and drops blanks, and duplicates when unique is set. It never returns nil.
func cleanList(items []string, unique bool) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || (unique && seen[item]) {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// onboardingSet keeps the steps this build knows, each once.
func onboardingSet(done []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, step := range done {
		if OnboardingStep(step).Valid() && !seen[step] {
			seen[step] = true
			out = append(out, step)
		}
	}
	return out
}

// Accent is which family of the palette the window is drawn in. These are families the app already
// had; the choice moves the accent onto one of them and mixes nothing new.
type Accent string

const (
	AccentBlue   Accent = "blue"
	AccentPurple Accent = "purple"
	AccentGreen  Accent = "green"
	AccentOrange Accent = "orange"
	AccentRed    Accent = "red"
	AccentGrey   Accent = "grey"
)

// Valid reports whether a stored or supplied accent is one the palette has.
func (a Accent) Valid() bool {
	switch a {
	case AccentBlue, AccentPurple, AccentGreen, AccentOrange, AccentRed, AccentGrey:
		return true
	}
	return false
}

// UISize is how large the interface is drawn: the root the window is measured against, which is why
// every size in the app is written in rem.
type UISize string

const (
	SizeSmall  UISize = "small"
	SizeMedium UISize = "medium"
	SizeLarge  UISize = "large"
)

func (s UISize) Valid() bool {
	return s == SizeSmall || s == SizeMedium || s == SizeLarge
}

// SizeOf reads a scale by its saved name. Percentage steps written by older builds map to the
// nearest named size, so an upgrade never resizes the window.
func SizeOf(name string) UISize {
	switch UISize(name) {
	case "90":
		return SizeSmall
	case "100":
		return SizeMedium
	case "110", "125":
		return SizeLarge
	}
	if UISize(name).Valid() {
		return UISize(name)
	}
	return SizeMedium
}

// MonoFont is which family the code is set in. These are the fonts' own names, spelled the same in
// every language; the system one is whatever the machine has.
type MonoFont string

const (
	MonoJetBrains MonoFont = "jetbrains"
	MonoCascadia  MonoFont = "cascadia"
	MonoFira      MonoFont = "fira"
	MonoSystem    MonoFont = "system"
)

func (m MonoFont) Valid() bool {
	switch m {
	case MonoJetBrains, MonoCascadia, MonoFira, MonoSystem:
		return true
	}
	return false
}

// Appearance is how the window is drawn. None of it reaches the wire: it is the window's clothes.
type Appearance struct {
	Accent   Accent   `json:"accent"`
	Size     UISize   `json:"size"`
	MonoFont MonoFont `json:"monoFont"`
	// MonoSize is the code's size in the units of the base the stylesheet is written against.
	MonoSize int `json:"monoSize"`
}

func DefaultAppearance() Appearance {
	return Appearance{
		Accent:   AccentBlue,
		Size:     SizeMedium,
		MonoFont: MonoSystem,
		MonoSize: 13,
	}
}

// Valid refuses an appearance with a part the window has no way of drawing.
func (a Appearance) Valid() error {
	for _, held := range []struct {
		field string
		ok    bool
	}{
		{"accent", a.Accent.Valid()},
		{"size", a.Size.Valid()},
		{"monoFont", a.MonoFont.Valid()},
	} {
		if !held.ok {
			return Refuse(CodeUnknownField, ErrNotAllowed, Args{"field": held.field})
		}
	}
	return nil
}

// The code's size is kept between a size that is still readable and one a window can hold.
const (
	MinMonoSize = 10
	MaxMonoSize = 20
)

// ShortcutNone marks a command whose combination was taken for another one. The window draws it as
// "not assigned" rather than as a missing row.
const ShortcutNone = "none"

// shortcutModifiers are the names a combination is written with, in the only order it is kept in.
// `mod` is the platform's own: ⌘ on macOS, Ctrl everywhere else.
var shortcutModifiers = []string{"mod", "alt", "shift"}

// shortcutKeys are the keys a combination may end with, beyond a single letter or digit: the named
// ones and the punctuation the recording of a combination writes down.
var shortcutKeys = map[string]bool{
	"enter": true, ",": true, ".": true, "/": true, ";": true, "'": true,
	"[": true, "]": true, "-": true, "=": true, "`": true,
}

// ValidShortcut reports whether combo can be carried out: modifiers in canonical order, each at
// most once, and one key (a letter, a digit, a named key or an F-key).
func ValidShortcut(combo string) bool {
	if combo == ShortcutNone {
		return true
	}
	parts := strings.Split(combo, "+")
	key := parts[len(parts)-1]
	if !validShortcutKey(key) {
		return false
	}
	if len(parts) == 1 {
		// A bare key is one only the function keys are: an unmodified letter would swallow typing.
		return isFunctionKey(key)
	}

	// The modifiers are looked up strictly left to right, so one that is out of order, sent twice,
	// or not a modifier at all leaves the walk where it cannot go on.
	next := 0
	for _, mod := range parts[:len(parts)-1] {
		at := slices.Index(shortcutModifiers[next:], mod)
		if at < 0 {
			return false
		}
		next += at + 1
	}
	return true
}

func isFunctionKey(key string) bool {
	rest, ok := strings.CutPrefix(key, "F")
	if !ok {
		return false
	}
	n, err := strconv.Atoi(rest)
	return err == nil && n >= 1 && n <= 24
}

func validShortcutKey(key string) bool {
	if len(key) == 1 {
		switch {
		case key >= "A" && key <= "Z", key >= "0" && key <= "9", shortcutKeys[key]:
			return true
		}
		return false
	}
	return isFunctionKey(key) || shortcutKeys[key]
}

type ProxyMode string

const (
	ProxySystem ProxyMode = "system"
	ProxyManual ProxyMode = "manual"
	ProxyOff    ProxyMode = "off"
)

func (m ProxyMode) Valid() bool {
	return m == ProxySystem || m == ProxyManual || m == ProxyOff
}

type ProxyProtocol string

const (
	ProxyHTTP   ProxyProtocol = "http"
	ProxyHTTPS  ProxyProtocol = "https"
	ProxySOCKS5 ProxyProtocol = "socks5"
)

func (p ProxyProtocol) Valid() bool {
	return p == ProxyHTTP || p == ProxyHTTPS || p == ProxySOCKS5
}

type ProxySettings struct {
	Mode        ProxyMode     `json:"mode"`
	Protocol    ProxyProtocol `json:"protocol"`
	Host        string        `json:"host"`
	Port        int           `json:"port"`
	User        string        `json:"user"`
	Password    string        `json:"password,omitempty"`
	HasPassword bool          `json:"hasPassword"`
	Bypass      []string      `json:"bypass"`
}

// DefaultProxy is what a fresh installation does: follow the environment, as it always has.
func DefaultProxy() ProxySettings {
	return ProxySettings{Mode: ProxySystem, Protocol: ProxyHTTP, Bypass: []string{}}
}

func (p ProxySettings) Address() string {
	if p.Host == "" {
		return ""
	}
	if p.Port <= 0 {
		return p.Host
	}
	return net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
}

func (p ProxySettings) Valid() error {
	if !p.Mode.Valid() {
		return Refuse(CodeUnknownProxyMode, ErrNotAllowed, Args{"mode": string(p.Mode)})
	}
	if p.Mode != ProxyManual {
		return nil
	}
	if !p.Protocol.Valid() {
		return Refuse(CodeUnknownProxyProtocol, ErrNotAllowed, Args{"protocol": string(p.Protocol)})
	}
	if !isProxyHost(p.Host) {
		return Refuse(CodeBadProxyAddress, ErrNotAllowed, Args{"address": p.Host})
	}
	if p.Port < 0 || p.Port > 65535 {
		return Refuse(CodeBadProxyAddress, ErrNotAllowed, Args{"address": strconv.Itoa(p.Port)})
	}
	return nil
}

func isProxyHost(host string) bool {
	if host == "" {
		return true
	}
	if strings.ContainsAny(host, " /\\?#@") {
		return false
	}
	return !strings.Contains(host, ":") || net.ParseIP(host) != nil
}

type ProxyProbe struct {
	Error     string `json:"error,omitempty"`
	Millis    int    `json:"millis"`
	Status    int    `json:"status"`
	OK        bool   `json:"ok"`
	NeedsAuth bool   `json:"needsAuth"`
}

type EnvEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// MCPTokenPrefix begins every access token this app hands out, so one found somewhere is recognised
// as this app's.
const MCPTokenPrefix = "sk-sch-"

const (
	DefaultMCPPort = 9300
	MinMCPPort     = 1024
	MaxMCPPort     = 65535
)

// MCPMode is how much an agent may do: read the workspaces, collections, requests and history, or read
// and send — sending being real calls made with the person's environments.
type MCPMode string

const (
	MCPRead MCPMode = "read"
	MCPSend MCPMode = "send"
)

func (m MCPMode) Valid() bool { return m == MCPRead || m == MCPSend }

// MCPSettings is the server as it was left: off until turned on, with the token minted at that
// moment. Mode, asking and workspaces are read on every call, so a change applies to the next one. The
// token never leaves on its own: a window gets HasToken, and only an explicit reveal returns the
// value.
type MCPSettings struct {
	Token         string   `json:"token,omitempty"`
	TokenTail     string   `json:"tokenTail,omitempty"`
	Mode          MCPMode  `json:"mode"`
	Workspaces    []string `json:"workspaces,omitempty"`
	Port          int      `json:"port"`
	Enabled       bool     `json:"enabled"`
	HasToken      bool     `json:"hasToken"`
	Ask           bool     `json:"ask"`
	Write         bool     `json:"write"`
	AllWorkspaces bool     `json:"allWorkspaces"`
}

func DefaultMCP() MCPSettings {
	return MCPSettings{
		Enabled:       false,
		Port:          DefaultMCPPort,
		Mode:          MCPSend,
		Ask:           true,
		AllWorkspaces: true,
		Workspaces:    []string{},
	}
}

func ValidMCPPort(port int) bool { return port >= MinMCPPort && port <= MaxMCPPort }

// Allows is whether a workspace is one the agent may reach. A workspace list without the flag is still a
// list: the two answers are kept apart, and only "all" makes the list moot.
func (m MCPSettings) Allows(workspaceID string) bool {
	return m.AllWorkspaces || slices.Contains(m.Workspaces, workspaceID)
}

// UpdateChannel is which releases the app is willing to be offered. It is the user's choice: a
// pre-release suits one user and wrecks another's install, so the release source cannot pick.
type UpdateChannel string

const (
	ChannelStable UpdateChannel = "stable"
	ChannelBeta   UpdateChannel = "beta"
)

// Valid reports whether a stored or supplied channel is one the release source can serve.
func (c UpdateChannel) Valid() bool {
	return c == ChannelStable || c == ChannelBeta
}

// Release is one version offered for installation. Notes is sentences rather than the source's
// markup, shaped once next to the fetch instead of in every window.
type Release struct {
	Version string   `json:"version"`
	Notes   []string `json:"notes,omitempty"`
}

// UpdateState is what the last check saw and what the user has since said about it. It is written
// and read whole so the version, its notes and the check time never get out of step. Preferences
// are separate (Settings), though both share a table.
type UpdateState struct {
	// LastCheckAt is when a check last succeeded, in ms since the epoch; zero if none ever has.
	LastCheckAt int64 `json:"lastCheckAt,omitempty"`
	// Latest is the newest release the source reported, whether or not it was offered.
	Latest string   `json:"latest,omitempty"`
	Notes  []string `json:"notes,omitempty"`
	// Skipped is the version the user asked not to hear about again. A later release is still
	// offered, which is what separates skipping a version from turning updates off.
	Skipped string `json:"skipped,omitempty"`
}

// SettingUpdateState is the key holding UpdateState as JSON, in the same table as the preferences.
// It is a single key so the store hands the value back unchanged and a half-written state cannot be
// expressed.
const SettingUpdateState = "update.state"
