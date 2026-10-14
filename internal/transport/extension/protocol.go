package extension

import "schemo/internal/domain"

// CapturedRequest represents a captured HTTP/HTTPS request and response payload recorded by the browser extension.
type CapturedRequest struct {
	Type            string            `json:"type"`
	ID              string            `json:"id"`
	Method          string            `json:"method"`
	URL             string            `json:"url"`
	RequestHeaders  map[string]string `json:"requestHeaders"`
	RequestBody     string            `json:"requestBody"`
	Status          int               `json:"status"`
	StatusText      string            `json:"statusText"`
	ResponseHeaders map[string]string `json:"responseHeaders"`
	ResponseBody    string            `json:"responseBody"`
	DurationMs      int64             `json:"durationMs"`
	StartedAt       int64             `json:"startedAt"`
	TabID           int               `json:"tabId"`
	TabTitle        string            `json:"tabTitle"`
	TabURL          string            `json:"tabURL"`
	FavIconURL      string            `json:"favIconUrl"`
	WaitMs          int64             `json:"waitMs,omitempty"`
	DownloadMs      int64             `json:"downloadMs,omitempty"`
}

// CaptureState represents the current operational status and active capture configuration of the browser extension.
type CaptureState struct {
	Type      string `json:"type"`
	Recording bool   `json:"recording"`
	Paused    bool   `json:"paused"`
	Tabs      int    `json:"tabs"`
	Browser   string `json:"browser"`
	// Version is the extension's own version, which is what the window names it by beside the
	// browser. An extension that does not send one is still a connected one.
	Version string `json:"version,omitempty"`
	// TabList contains details for each individual tab currently under capture.
	TabList []TabState `json:"tabList,omitempty"`
}

// TabState represents a single captured tab along with the Unix timestamp (in milliseconds) when it was armed.
type TabState struct {
	TabID int   `json:"tabId"`
	Since int64 `json:"since"`
}

// FocusRequest represents a command from the extension asking the window to focus on a specific tab.
type FocusRequest struct {
	Type string `json:"type"`
	Tab  int    `json:"tab"`
}

// FiltersFrame transmits capture filter rules from the application to the extension.
type FiltersFrame struct {
	Type    string                `json:"type"`
	Filters domain.CaptureFilters `json:"filters"`
}

// SettingsFrame carries the two switches of the extension section to the extension: whether it
// connects on its own when both sides are running, and whether it arms the addresses capture was
// armed on again after the browser restarts. It is the extension that carries them out.
type SettingsFrame struct {
	Type         string `json:"type"`
	AutoConnect  bool   `json:"autoConnect"`
	RememberTabs bool   `json:"rememberTabs"`
}

// HelloFrame represents the initial handshake message sent by the application upon establishing a connection.
type HelloFrame struct {
	Type    string `json:"type"`
	App     string `json:"app"`
	Version string `json:"version"`
}
