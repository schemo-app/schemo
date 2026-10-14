package record

import (
	"context"

	"schemo/internal/domain"
)

// Store defines persistence operations for execution records and bodies.
type Store interface {
	SaveRecord(ctx context.Context, workspaceID string, rec domain.Record) error

	// QueueRecord offers a saved record to the server, after the code around it has reported.
	QueueRecord(ctx context.Context, workspaceID, id string) error

	// SaveCookieWrites updates cookie mutation state for an existing record.
	SaveCookieWrites(ctx context.Context, workspaceID, id string, writes []domain.CookieWrite) error

	Record(ctx context.Context, id string) (domain.Record, error)

	Records(
		ctx context.Context,
		workspaceID string,
		source domain.RecordSource,
		limit int,
	) ([]domain.Record, error)

	// RecordsBefore returns workspace records started before a timestamp, newest first.
	RecordsBefore(
		ctx context.Context,
		workspaceID string,
		before int64,
		limit int,
	) ([]domain.Record, error)

	// SaveCached stores remote records requested during pagination.
	SaveCached(ctx context.Context, workspaceID string, records []domain.Record) error

	ReadBody(ctx context.Context, id string, side domain.BodySide) (string, error)

	// SaveFetched stores what was fetched for a remote record: its bodies, the reports of the code
	// around it, and the run that produced it. Existing sides are preserved.
	SaveFetched(ctx context.Context, workspaceID, id string, fetched domain.RecordFetched) error

	// DeleteRecords marks specific records as deleted and queues sync updates.
	DeleteRecords(ctx context.Context, workspaceID string, ids []string) error

	// DeleteAllRecords removes all history for a given workspace.
	DeleteAllRecords(ctx context.Context, workspaceID string) (int, error)

	HistoryStats(ctx context.Context, workspaceID string) (domain.HistoryStats, error)

	Prune(ctx context.Context, workspaceID string, opts domain.PruneOptions) (int, error)
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

// SendInput is one attempt: the request ready to go, and the masked copy history keeps. The two
// exist because the draft is still the window's — it holds the tokens, so only it can say what
// a secret was. It is here rather than in dto.go because a port takes it, not the window alone.
type SendInput struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers []domain.HeaderPair `json:"headers"`
	Body    string              `json:"body"`
	// BodyKind and the two below it are what the body was rendered FROM. They travel beside the text
	// because a masked copy of a form or a file cannot be made from the text: it has to be rendered
	// again out of what the request is made of.
	BodyKind domain.BodyKind  `json:"bodyKind,omitempty"`
	Form     []domain.FormRow `json:"form,omitempty"`
	BodyFile string           `json:"bodyFile,omitempty"`

	MaskedURL     string              `json:"maskedUrl"`
	MaskedHeaders []domain.HeaderPair `json:"maskedHeaders"`
	MaskedBody    string              `json:"maskedBody"`

	// Digest is a credential the request cannot carry and the engine has to: see draft.Prepared.
	Digest *domain.DigestCredentials `json:"-"`

	// Node is the request this came from, when it came from a collection, and Run is the scope the
	// run's own variables live in — a collection run's id, or the send itself for a request sent on
	// its own. Both are what the scripts around the attempt are found by; both are empty for a
	// request composed on the command line, which has nothing above it.
	Node string `json:"node,omitempty"`
	Run  string `json:"run,omitempty"`

	// Agent is who sent this through the MCP server, when it was not the person. Empty for every
	// send the window, a run or the command line makes — the field exists so that history can tell
	// the agent's doing from the person's, and it is the only place that difference is written down.
	Agent string `json:"agent,omitempty"`
}

// Masker masks sensitive values in requests modified by scripts, and in the texts a capture brought
// in from outside the app.
type Masker interface {
	Mask(ctx context.Context, in SendInput, sent domain.ScriptRequest) (Masked, error)

	// MaskTexts leaves a secret's value as its mask wherever it sits in one of the texts, and answers
	// them in the order they came. The space is named because the values a mask is made of are its
	// own: a record is filed under the space it was captured in, not the one on screen.
	MaskTexts(ctx context.Context, workspace string, texts []string) ([]string, error)
}

// Masked represents a request payload with sensitive values redacted.
type Masked struct {
	URL     string
	Headers []domain.HeaderPair
	Body    string
}

// Jar provides cookie mutation rollbacks for history records.
type Jar interface {
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

// RetentionSourceFunc adapts a function to the RetentionSource interface.
type RetentionSourceFunc func(ctx context.Context) (domain.Retention, error)

func (f RetentionSourceFunc) Retention(ctx context.Context) (domain.Retention, error) {
	return f(ctx)
}
