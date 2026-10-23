package request

import (
	"context"
	"sync/atomic"

	"schemo/internal/db"
	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Event topics. A record appearing and a send finishing are different things: the first is one the
// app was told about, the second is an answer to something the window asked for. There is no
// "started" topic: the caller gets the id from Send itself.
const (
	TopicRecordAdded     = "record:added"
	TopicRequestFinished = "request:finished"
	TopicRequestFailed   = "request:failed"
	// A whole history going, which happens in the settings window and is drawn in the main one: a
	// list told about every record one by one has to be told about this too, or it goes on drawing
	// rows that are no longer there.
	TopicHistoryCleared = "history:cleared"
)

// History is the history the window binds: the records, their bodies and what a send left behind.
type History struct {
	store     *db.Store
	scope     domain.Scope
	jar       CookieUndo
	executor  Executor
	notifier  domain.Notifier
	retention RetentionSource
	source    Source
	screen    Screener
	mask      Masker
	ids       platform.IDGen
	build     platform.BuildInfo
	host      *desktop.Host

	// sincePrune counts saves between two looks at the retention rules. It is atomic because a
	// window's send and a collection run save from goroutines of their own, and two sends landing
	// on the same counter would be a data race on a number that decides when history is trimmed.
	sincePrune atomic.Int32
}

// HistoryDeps is what history is assembled from. Jar, Source, Screen, Mask, Retention and Host may be nil
// in a test that does not reach them.
type HistoryDeps struct {
	Store     *db.Store
	Scope     domain.Scope
	Jar       CookieUndo
	Executor  Executor
	Notifier  domain.Notifier
	Retention RetentionSource
	Source    Source
	Screen    Screener
	Mask      Masker
	IDs       platform.IDGen
	Build     platform.BuildInfo
	Host      *desktop.Host
}

func NewHistory(d HistoryDeps) *History {
	return &History{
		store: d.Store, scope: d.Scope, jar: d.Jar, executor: d.Executor, notifier: d.Notifier,
		retention: d.Retention, source: d.Source, screen: d.Screen, mask: d.Mask, ids: d.IDs,
		build: d.Build, host: d.Host,
	}
}

// The shapes that exist only for the window: what became of an attempt, what a browser hands over,
// and the word to forget history.

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
	// Failure is the same news in the window's terms (the code and the values its sentence needs)
	// when the app is the side that refused. An error from the network has none, and the window
	// shows the message beside it.
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

// Request represents an outgoing HTTP execution request.
type Request struct {
	// ID uniquely identifies the request attempt and is used for cancellation.
	ID      string
	Method  string
	URL     string
	Headers []domain.HeaderPair
	Body    string

	// Workspace specifies the cookie jar scope for the request chain.
	Workspace string

	// Digest holds credentials passed directly to the HTTP engine.
	Digest *domain.DigestCredentials
}

// Executor handles sending HTTP requests and canceling running attempts.
type Executor interface {
	Execute(ctx context.Context, req Request) (*domain.Response, error)
	Cancel(id string) bool
}

// Screener runs collection pre/post-request scripts for an attempt.
type Screener interface {
	Before(ctx context.Context, workspace string, pass *domain.ScriptPass) (bool, error)
	After(ctx context.Context, workspace string, pass domain.ScriptPass)
}

// SendInput is one attempt: the request ready to go and the masked copy history keeps. Both exist
// because the draft is still the window's: it holds the tokens, so only it can say what a secret
// was.
type SendInput struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers []domain.HeaderPair `json:"headers"`
	Body    string              `json:"body"`
	// BodyKind and the two below it are what the body was rendered FROM. They travel beside the
	// text because a masked copy of a form or a file cannot be made from the text: it has to be
	// rendered again out of what the request is made of.
	BodyKind domain.BodyKind  `json:"bodyKind,omitempty"`
	Form     []domain.FormRow `json:"form,omitempty"`
	BodyFile string           `json:"bodyFile,omitempty"`

	MaskedURL     string              `json:"maskedUrl"`
	MaskedHeaders []domain.HeaderPair `json:"maskedHeaders"`
	MaskedBody    string              `json:"maskedBody"`

	// Digest is a credential the request cannot carry and the engine has to: see Prepared.
	Digest *domain.DigestCredentials `json:"-"`

	// Node is the request this came from, when it came from a collection, and Run is the scope the
	// run's own variables live in: a collection run's id, or the send itself for a request sent on
	// its own. Both are what the scripts around the attempt are found by, and both are empty for a
	// request composed on the command line, which has nothing above it.
	Node string `json:"node,omitempty"`
	Run  string `json:"run,omitempty"`

	// Agent is who sent this through the MCP server, when it was not the person. Empty for every
	// send the window, a run or the command line makes: it lets history tell the agent's doing from
	// the person's, and is the only place that difference is written down.
	Agent string `json:"agent,omitempty"`
}

// Masker masks sensitive values in requests modified by scripts, and in the texts a capture brought
// in from outside the app.
type Masker interface {
	Mask(ctx context.Context, in SendInput, sent domain.ScriptRequest) (Masked, error)

	// MaskTexts leaves a secret's value as its mask wherever it sits in one of the texts, and
	// returns them in the order they came. The workspace is named because the values a mask is made of
	// are its own: a record is filed under the workspace it was captured in, not the one on screen.
	MaskTexts(ctx context.Context, workspace string, texts []string) ([]string, error)
}

// Masked represents a request payload with sensitive values redacted.
type Masked struct {
	URL     string
	Headers []domain.HeaderPair
	Body    string
}

// CookieUndo provides cookie mutation rollbacks for history records.
type CookieUndo interface {
	// Undo reverts cookie changes made by a specific record response.
	Undo(ctx context.Context, workspace string, writes []domain.CookieWrite) error
}

// RetentionSource provides retention policy settings for history pruning.
type RetentionSource interface {
	Retention(ctx context.Context) (domain.Retention, error)
}

// Source fetches remote records and bodies not present on the local device.
type Source interface {
	Fetched(ctx context.Context, workspaceID, id string) (domain.RecordFetched, error)

	// Older fetches remote workspace history prior to a timestamp, newest first.
	Older(
		ctx context.Context,
		workspaceID string,
		before int64,
		limit int,
	) ([]domain.Record, error)
}
