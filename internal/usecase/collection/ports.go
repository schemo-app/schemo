package collection

import (
	"context"

	"schemo/internal/domain"
)

// Store is the tree as this feature keeps it. The whole thing is read at once — a collection is a
// document, not a list to page through. Anything that changes the tree's shape is told its
// workspace; a node is reached by its own id, which is unique across the database.
type Store interface {
	Collections(ctx context.Context, workspaceID string) ([]domain.Collection, error)
	Node(ctx context.Context, id string) (domain.CollectionNode, error)
	// SpaceOf answers which space a collection belongs to, for a caller that reached the id from
	// outside the window and has to say whether it may look at it at all.
	SpaceOf(ctx context.Context, collectionID string) (string, error)
	// The requests of one collection's own level, narrowed to what the collection page's table draws:
	// the tree carries no request payload, and this is the one read that needs an address.
	ContentRows(ctx context.Context, collectionID string) ([]domain.LevelRow, error)

	SaveCollection(ctx context.Context, workspaceID string, collection domain.Collection) error
	SaveNode(ctx context.Context, node domain.CollectionNode) error
	DeleteCollection(ctx context.Context, workspaceID, id string) error
	DeleteNode(ctx context.Context, id string) error

	// The prose a collection carries for the page it is published as: a write of its own, because
	// nothing else writes these three columns and a sentence is not a reason to rewrite a row.

	// A level is a collection or a request, addressed in one id space — as is the command line's own
	// draft, which is why the workspace has to be named here: the draft's id is the same word in every
	// workspace. Nil is "not set here" — the level inherits.
	Scripts(ctx context.Context, workspaceID, id string) (*domain.Scripts, error)
	SaveScripts(ctx context.Context, workspaceID, id string, scripts *domain.Scripts) error

	NextPosition(ctx context.Context, workspaceID, collectionID string) (int64, error)

	// Moving is a place in a level, counted the way the tree is drawn: the requests of a collection
	// and the collections inside it are one list, and the position is an index in it.
	MoveNode(ctx context.Context, workspaceID, id string, collectionID string, position int64) error
	MoveCollection(ctx context.Context, workspaceID, id string, parentID string, position int64) error

	SaveRun(ctx context.Context, run domain.CollectionRun) error
	AppendRunResult(ctx context.Context, runID string, result domain.CollectionRunResult) error
	LastRun(ctx context.Context, collectionID string, nodeID string) (domain.CollectionRun, bool,
		error)
}

// A RunRequest is one saved request on its way out, its rows narrowed to the ones switched on.
// Run and Node travel with it although the sender does not use them: whoever sends it must be able
// to say which node it was.
type RunRequest struct {
	Run    string
	NodeID string

	// Workspace is the space the run resolved once, at the moment it started, and carried here as a
	// value. The sender must not ask for it again: a run outlives the call that started it, and the
	// user may well have switched spaces by the time the twentieth request goes out.
	Workspace string

	Method  string
	URL     string
	Body    string
	Headers []domain.HeaderPair

	// The body travels with what it was rendered from, exactly as a draft's does: a form or a file
	// body cannot be sent from its text alone, and a node that has one would otherwise go out as raw
	// text with no Content-Type. See draft.Prepared.
	BodyKind domain.BodyKind
	Form     []domain.FormRow
	BodyFile string

	// The answer of the nearest level above, resolved here: the walk up the tree is the run's own, and
	// by the time a request is on its way out, where it sits is no longer known. The variables of the
	// same levels travel beside it, for the same reason and in the same shape.
	Auth      *domain.Auth
	Variables []domain.Variable

	// EnvironmentID is the environment this one request pinned itself to, when it has one. It travels
	// for the same reason the variables do: a run reaches requests the window is not looking at, and
	// the node it read the pin from is out of reach by then. Empty means the run's own environment.
	EnvironmentID string

	// Agent is who is sending this, when it is not the person — the name an MCP agent calls itself.
	// It travels all the way into the record, which is where history tells whose doing the send was.
	Agent string
}

// Sender sends one saved request and answers with what it produced. A run does not know how a
// request leaves the process — it goes out the way the command line's does, through the draft that
// fills in its `{{tokens}}` and keeps a secret's value out of what is written down.
type Sender interface {
	Send(ctx context.Context, req RunRequest) (domain.Record, error)
}

// Environment answers which environment a run is going out under. A run keeps that answer: what it
// was sent with is part of what happened, and the environment on screen when the page is read later
// is a different thing that would be a lie to draw in its place. The empty name is "the globals,
// nothing selected", which is an answer too.
type Environment interface {
	ActiveEnvironment(ctx context.Context) (string, error)
}

// Assertions answers what a record's scripts asserted, and how many of those held. The reports are
// the scripting feature's rows and a run does not keep them: it carries the two counts, because
// that is what its table draws.
type Assertions interface {
	AssertionCounts(ctx context.Context, recordID string) (passed, total int, err error)
}
