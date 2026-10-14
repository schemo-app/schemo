package settings

import "schemo/internal/domain"

// The shapes that exist only for the window: the patches it sends, and the events it is told. What
// is stored as well — Appearance, Requests, ProxySettings — is a domain type, not repeated here.

type AppearancePatch struct {
	Accent   *domain.Accent   `json:"accent,omitempty"`
	Size     *domain.UISize   `json:"size,omitempty"`
	MonoFont *domain.MonoFont `json:"monoFont,omitempty"`
	MonoSize *int             `json:"monoSize,omitempty"`
}

type LayoutPatch struct {
	InspectorOpen  *bool `json:"inspectorOpen,omitempty"`
	InspectorWidth *int  `json:"inspectorWidth,omitempty"`
	SideWidth      *int  `json:"sideWidth,omitempty"`
	// RequestHeight of zero is a request to fit the pane to what it holds again.
	RequestHeight *int             `json:"requestHeight,omitempty"`
	ListSide      *domain.ListSide `json:"listSide,omitempty"`
}

type EditorPatch struct {
	WrapLines   *bool          `json:"wrapLines,omitempty"`
	LineNumbers *bool          `json:"lineNumbers,omitempty"`
	Indent      *domain.Indent `json:"indent,omitempty"`
}

type RequestsPatch struct {
	Timeout         *int                `json:"timeout,omitempty"`
	FollowRedirects *bool               `json:"followRedirects,omitempty"`
	MaxRedirects    *int                `json:"maxRedirects,omitempty"`
	Cookies         *bool               `json:"cookies,omitempty"`
	HTTPVersion     *domain.HTTPVersion `json:"httpVersion,omitempty"`
	UserAgent       *string             `json:"userAgent,omitempty"`
	AcceptEncoding  *bool               `json:"acceptEncoding,omitempty"`
	ShowAuto        *bool               `json:"showAuto,omitempty"`
}

type CertificatesPatch struct {
	Verify         *bool `json:"verify,omitempty"`
	TrustLocalhost *bool `json:"trustLocalhost,omitempty"`
}

// ExtensionPatch is the two switches of the extension section: how the app and the extension
// behave together. What is captured is CaptureFilters, a domain type of its own.
type ExtensionPatch struct {
	AutoConnect  *bool `json:"autoConnect,omitempty"`
	RememberTabs *bool `json:"rememberTabs,omitempty"`
}

// SyncScopesPatch is the two scopes a person decides about: whether the results of collection runs
// and the values of secret variables may leave this device.
type SyncScopesPatch struct {
	Runs    *bool `json:"runs,omitempty"`
	Secrets *bool `json:"secrets,omitempty"`
}

// OnboardingPatch is what the window tells the welcome: that the overview was offered, that the
// checklist was put away, or which steps have been walked. Done replaces the whole set — it is a
// set of names, and a patch that omits it keeps what stands.
type OnboardingPatch struct {
	Seen      *bool     `json:"seen,omitempty"`
	Dismissed *bool     `json:"dismissed,omitempty"`
	Done      *[]string `json:"done,omitempty"`
}

// MCPPatch is what of the MCP server the window changes. The token is not here: it is minted on the
// first switch-on and reissued by an action of its own, not typed.
type MCPPatch struct {
	Enabled   *bool           `json:"enabled,omitempty"`
	Port      *int            `json:"port,omitempty"`
	Mode      *domain.MCPMode `json:"mode,omitempty"`
	Ask       *bool           `json:"ask,omitempty"`
	AllSpaces *bool           `json:"allSpaces,omitempty"`
	Spaces    *[]string       `json:"spaces,omitempty"`
}

type ProxyPatch struct {
	Mode     *domain.ProxyMode     `json:"mode,omitempty"`
	Protocol *domain.ProxyProtocol `json:"protocol,omitempty"`
	Host     *string               `json:"host,omitempty"`
	Port     *int                  `json:"port,omitempty"`
	User     *string               `json:"user,omitempty"`
	Password *string               `json:"password,omitempty"`
	Bypass   *[]string             `json:"bypass,omitempty"`
}

type ThemeChanged struct {
	Theme domain.Theme `json:"theme"`
}

type LanguageChanged struct {
	Language domain.Language `json:"language"`
}
