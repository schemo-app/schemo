package record

import (
	"schemo/internal/domain"
)

// The shapes that exist only for the window: what became of an attempt, what a browser hands over,
// and the word to forget history. SendInput is the port's, and stays beside the port.

// RequestFinished carries the record the attempt produced, in the same shape history lists.
type RequestFinished struct {
	ID     string        `json:"id"`
	Record domain.Record `json:"record"`
}

// RequestFailed is an attempt that produced no record at all, which is a failure of this side
// rather than of the network: a request that never reached the server still has a record.
type RequestFailed struct {
	ID    string `json:"id"`
	Error string `json:"error"`
	// Failure is the same news in the window's terms — the code and the values its sentence needs —
	// when the app is the side that refused. An error from the network has none, and the window shows
	// the message beside it.
	Failure *domain.Failure `json:"failure,omitempty"`
}

// IngestInput is a request this app did not send. An empty Source means the browser — the
// extension is the only thing that reports one from the outside.
type IngestInput struct {
	Source          domain.RecordSource `json:"source"`
	Method          string              `json:"method"`
	URL             string              `json:"url"`
	Status          int                 `json:"status"`
	StatusText      string              `json:"statusText"`
	ContentType     string              `json:"contentType,omitempty"`
	RequestHeaders  []domain.HeaderPair `json:"requestHeaders"`
	ResponseHeaders []domain.HeaderPair `json:"responseHeaders"`
	RequestBody     string              `json:"requestBody"`
	ResponseBody    string              `json:"responseBody"`
	DurationMs      int64               `json:"durationMs"`
	// Everything below only exists for a record that came from a browser tab. `omitempty` is what
	// says so on the wire, so a caller with no tab names none of them instead of spelling out zeros.
	StartedAt  int64  `json:"startedAt,omitempty"`
	WaitMs     int64  `json:"waitMs,omitempty"`
	DownloadMs int64  `json:"downloadMs,omitempty"`
	TabID      int    `json:"tabId,omitempty"`
	TabTitle   string `json:"tabTitle,omitempty"`
	TabURL     string `json:"tabURL,omitempty"`
	FavIconURL string `json:"favIconUrl,omitempty"`
}

// HistoryCleared says whose history went, because every open window has to decide whether the
// list it draws is the one that was cleared.
type HistoryCleared struct {
	WorkspaceID string `json:"workspaceId"`
	Removed     int    `json:"removed"`
}
