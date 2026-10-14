package domain

import "time"

const (
	SettingTheme            = "theme"
	SettingLanguage         = "language"
	SettingInspectorOpen    = "ui.inspectorOpen"
	SettingInspectorWidth   = "ui.inspectorWidth"
	SettingSideWidth        = "ui.sideWidth"
	SettingRequestHeight    = "ui.requestHeight"
	SettingListSide         = "ui.listSide"
	SettingListSideLast     = "ui.listSideLast"
	SettingHistoryRetention = "history.retention"
	SettingUpdateAuto       = "update.checkAutomatically"
	SettingUpdateChannel    = "update.channel"
	SettingCaptureFilters   = "capture.filters"
	SettingExtension        = "extension"
	SettingShortcuts        = "shortcuts"
	SettingSyncScopes       = "sync.scopes"
	SettingMCP              = "mcp"
	SettingWrapLines        = "editor.wrapLines"
	SettingLineNumbers      = "editor.lineNumbers"
	SettingIndent           = "editor.indent"
	SettingReopenWorkspace  = "workspace.reopenLast"
	SettingRestoreTabs      = "tabs.restore"
	SettingProxy            = "proxy"
	SettingCertificates     = "certificates"
	SettingRequests         = "requests"
	SettingAppearance       = "appearance"
	SettingOnboarding       = "onboarding"
)

type CaptureFilters struct {
	Hosts     []string `json:"hosts"`
	Static    bool     `json:"static"`
	Analytics bool     `json:"analytics"`
	JSON      bool     `json:"json"`
}

func DefaultCaptureFilters() CaptureFilters {
	return CaptureFilters{Hosts: []string{}, Static: true, Analytics: false, JSON: false}
}

// ExtensionSettings is how the app and the browser extension behave together: whether the two
// connect on their own when both are running, and whether the addresses capture was armed on are
// armed again after the browser restarts. Both are the extension's to carry out — the app keeps
// them and hands them over — which is why nothing here decides anything on its own.
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
	StepSend    OnboardingStep = "send"
	StepSave    OnboardingStep = "save"
	StepEnv     OnboardingStep = "env"
	StepImport  OnboardingStep = "import"
	StepCapture OnboardingStep = "capture"
	StepSpace   OnboardingStep = "ws"
)

// OnboardingSteps is how many steps the checklist holds — the window draws the count against it.
const OnboardingSteps = 6

// Valid reports whether the name is one of the checklist's own steps.
func (s OnboardingStep) Valid() bool {
	switch s {
	case StepSend, StepSave, StepEnv, StepImport, StepCapture, StepSpace:
		return true
	}
	return false
}

// Onboarding is what the welcome has been told: whether the overview has been offered once, whether
// the checklist was put away for good, and the steps already walked. It is kept here rather than in
// the window because it outlives a run — a restart finds the checklist where it was left.
type Onboarding struct {
	Seen      bool     `json:"seen"`
	Dismissed bool     `json:"dismissed"`
	Done      []string `json:"done"`
}

func DefaultOnboarding() Onboarding {
	return Onboarding{Done: []string{}}
}

// SyncScopes is what of this device is allowed to leave it: the results of collection runs, and the
// values of secret variables. Both are things a person decides about knowingly — a run's report is
// what a teammate reads, and a secret is a secret — so they are switches rather than policies, and
// both come on, which is what the app has always done.
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

// HistoryKeepCount is how many records a space's history keeps, however young they are.
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
	// Shortcuts holds only what somebody changed: a command that is not in it wears the combination
	// it comes with, and a command whose entry is ShortcutNone has none at all. The registry of
	// commands is the window's, because the labels are the catalogue's and the actions are the
	// window's — this is what the two agree on.
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
