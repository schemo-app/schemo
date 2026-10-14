package domain

import "time"

// RecordSource defines the origin of a request record (manual builder or browser extension).
type RecordSource string

const (
	SourceManual  RecordSource = "manual"
	SourceBrowser RecordSource = "browser"
)

// BodySide indicates whether a record payload belongs to the request or response side.
type BodySide string

const (
	SideRequest  BodySide = "request"
	SideResponse BodySide = "response"
)

// InlineBodyLimit specifies the maximum body payload size (64KB) kept inline with a record.
const InlineBodyLimit = 64 << 10

// BodyRef points to a request/response body payload, either inline or referenced by size.
type BodyRef struct {
	Inline    string `json:"inline,omitempty"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated,omitempty"`
}

// RecordFetched contains request and response body references for remote records.
type RecordFetched struct {
	Bodies map[BodySide]BodyRef `json:"bodies,omitempty"`

	// Scripts are what the code around the record reported, and they come with it for the reason they
	// are stored beside it: a report whose record is missing is a report of nothing.
	Scripts []ScriptRun `json:"scripts,omitempty"`

	// Run is the collection run the record was sent by, when this device has not been told about it.
	// It is absent for a request sent on its own, and for a run the log already brought.
	Run *CollectionRun `json:"run,omitempty"`
}

// PruneOptions specifies retention limits for local history and cached remote records.
type PruneOptions struct {
	MaxCount   int
	MaxAge     time.Duration
	CacheCount int
	CacheAge   time.Duration
}

// HistoryStats provides historical record count and body storage metrics for a workspace.
type HistoryStats struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

// RecordSummary contains lightweight metadata for rendering record lists.
type RecordSummary struct {
	ID          string       `json:"id"`
	WorkspaceID string       `json:"workspaceId,omitempty"`
	Source      RecordSource `json:"source"`
	Method      string       `json:"method"`
	URL         string       `json:"url"`
	Status      int          `json:"status"`
	StatusText  string       `json:"statusText"`
	ContentType string       `json:"contentType,omitempty"`
	Error       string       `json:"error,omitempty"`
	DurationUs  int64        `json:"durationUs"`
	StartedAt   int64        `json:"startedAt"`
	NodeID      string       `json:"nodeId,omitempty"`
	// RunID is the collection run this record was sent by, empty for a request sent on its own. The
	// run travels as a document of its own, and the record names it rather than carrying a copy.
	RunID string `json:"runId,omitempty"`
	// Agent is who sent this through the MCP server, when it was not the person: the record is
	// theirs to read either way, and the name is what says whose doing it was. Empty for a send the
	// window, a run or the command line made.
	Agent      string `json:"agent,omitempty"`
	TabID      int    `json:"tabId,omitempty"`
	TabTitle   string `json:"tabTitle,omitempty"`
	TabURL     string `json:"tabURL,omitempty"`
	FavIconURL string `json:"favIconUrl,omitempty"`
	IsRemote   bool   `json:"isRemote,omitempty"`
}

// Record represents full execution history, including timing phases, headers, and bodies.
type Record struct {
	RecordSummary

	Cancelled       bool          `json:"cancelled,omitempty"`
	DNSUs           *int64        `json:"dnsUs,omitempty"`
	ConnectUs       *int64        `json:"connectUs,omitempty"`
	TLSUs           *int64        `json:"tlsUs,omitempty"`
	WaitUs          *int64        `json:"waitUs,omitempty"`
	DownloadUs      *int64        `json:"downloadUs,omitempty"`
	RequestBytes    int64         `json:"requestBytes,omitempty"`
	ResponseBytes   int64         `json:"responseBytes,omitempty"`
	RequestHeaders  []HeaderPair  `json:"requestHeaders"`
	ResponseHeaders []HeaderPair  `json:"responseHeaders"`
	CookieWrites    []CookieWrite `json:"cookieWrites,omitempty"`
	RequestBody     *BodyRef      `json:"requestBody,omitempty"`
	ResponseBody    *BodyRef      `json:"responseBody,omitempty"`
	Skipped         bool          `json:"skipped,omitempty"`
}
