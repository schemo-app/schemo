package request

import (
	"context"
	"sync"
	"sync/atomic"

	"schemo/internal/db"
	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Event topics. There is no "started" topic: Send returns the id.
const (
	TopicRecordAdded     = "record:added"
	TopicRequestFinished = "request:finished"
	TopicRequestFailed   = "request:failed"
	TopicStreamOpened    = "request:stream-opened"
	// The settings window clears history; the main window's list must drop its rows too.
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

	// sincePrune counts saves between retention checks; atomic because sends and runs save concurrently.
	sincePrune atomic.Int32
	attemptMu  sync.Mutex
	attempts   map[string]*attempt
	stopping   bool
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
		attempts: make(map[string]*attempt),
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
	// Failure is the app's own refusal in the window's terms; network errors have none.
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

// Request is the prepared exchange passed to the engine.
type Request struct {
	Protocol domain.Protocol
	rpc      rpcCall
	Stream   *Stream
	// ID uniquely identifies the request attempt and is used for cancellation.
	ID      string
	Method  string
	URL     string
	Headers []domain.HeaderPair
	Body    string

	// Workspace specifies the cookie jar scope for the request chain.
	Workspace string
	Jar       EngineJar

	// Digest holds credentials passed directly to the HTTP engine.
	Digest *domain.DigestCredentials
}

// Executor sends a request with the attempt's context.
type Executor interface {
	Execute(ctx context.Context, req Request) (*domain.Response, error)
}

// Screener runs collection pre/post-request scripts for an attempt.
type Screener interface {
	Before(ctx context.Context, workspace string, pass *domain.ScriptPass) (bool, error)
	After(ctx context.Context, workspace string, pass domain.ScriptPass)
}

// SendInput is the request to send and the masked copy for history; only the draft knows what a
// secret was, so it supplies both.
type SendInput struct {
	Protocol      domain.Protocol `json:"-"`
	rpc           rpcCall
	message       messageResolver
	EnvironmentID string                `json:"-"`
	Variables     []domain.Variable     `json:"-"`
	RecordOptions *domain.ReplayOptions `json:"-"`
	Method        string                `json:"method"`
	URL           string                `json:"url"`
	Headers       []domain.HeaderPair   `json:"headers"`
	Body          string                `json:"body"`
	// BodyKind and the next two are what the body was rendered from: forms and files must be
	// re-rendered to mask, not read from text.
	BodyKind domain.BodyKind  `json:"bodyKind,omitempty"`
	Form     []domain.FormRow `json:"form,omitempty"`
	BodyFile string           `json:"bodyFile,omitempty"`

	MaskedURL     string              `json:"maskedUrl"`
	MaskedHeaders []domain.HeaderPair `json:"maskedHeaders"`
	MaskedBody    string              `json:"maskedBody"`

	// Digest is a credential the request cannot carry and the engine has to: see Prepared.
	Digest *domain.DigestCredentials `json:"-"`

	// Node is the source request and Run the scope of the run's variables; both find the scripts
	// around the attempt and are empty for a command-line request.
	Node string `json:"node,omitempty"`
	Run  string `json:"run,omitempty"`

	// Agent names the MCP agent that sent this; empty for the person's own sends.
	Agent string `json:"agent,omitempty"`
}

// Masker masks sensitive values in requests modified by scripts, and in the texts a capture brought
// in from outside the app.
type Masker interface {
	Mask(ctx context.Context, workspace string, in SendInput, sent domain.ScriptRequest) (Masked, error)

	// MaskTexts masks secret values in the texts, keeping order. The workspace is the record's, not
	// necessarily the one on screen.
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
