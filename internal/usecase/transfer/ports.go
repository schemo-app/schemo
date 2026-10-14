package transfer

// The I/O this feature needs, declared here on the side that needs it: a use case names what it
// needs, and only the composition root wires the implementations (docs/ARCHITECTURE.md).

import (
	"context"

	"schemo/internal/domain"
)

// Collections is the tree as bringing collections in and taking copies out reads it.
type Collections interface {
	// Import writes the collections of a document at the end of a level, or of the top level when
	// the id is empty, and answers with the tree to draw.
	Import(
		ctx context.Context,
		collections []domain.Collection,
		parentID string,
	) ([]domain.Collection, error)
	// Full is a collection — or one request, when the id names one — read whole: what an export
	// writes down.
	Full(ctx context.Context, id string) (domain.Collection, error)
	// FullOf is Full for a named space, which the window may not be looking at.
	FullOf(ctx context.Context, workspace, id string) (domain.Collection, error)
	// TreeOf is the tree of a named space, as the source list draws it.
	TreeOf(ctx context.Context, workspace string) ([]domain.Collection, error)
	// RequestsOf is every request under a collection, with the authorization and the variables its
	// levels answer.
	RequestsOf(ctx context.Context, collectionID string) ([]domain.ResolvedRequest, error)
}

// Environments is the environment list as importing one and exporting the values reads it.
type Environments interface {
	// Import writes one environment a document described, variables and all, and answers with the
	// list to draw.
	Import(ctx context.Context, draft domain.EnvironmentDraft) (domain.EnvState, error)
	// Snapshot is the whole list, with secret values withheld.
	Snapshot(ctx context.Context) (domain.EnvState, error)
	// Reveal is the one way a secret's value leaves the database — here, into a file the person
	// asked to carry it.
	Reveal(ctx context.Context, variableID string) (string, error)
}

// Workspaces is the spaces of the app, as the source that copies from another one lists them.
type Workspaces interface {
	Snapshot(ctx context.Context) (domain.WorkspaceState, error)
}

// Files reads a file the person picked: the dialog is the window's, and what it answered is read
// here.
type Files interface {
	Read(path string) ([]byte, error)
}

// Remote reads a document from a URL — a specification or a collection somebody published.
type Remote interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// Commands is the draft feature's reading and writing of commands: a command is a request in
// another notation, and that notation lives there.
type Commands interface {
	// Curl writes one saved request as a curl command line, with its secrets as the caller asks.
	Curl(ctx context.Context, request domain.ResolvedRequest, secrets bool) (string, error)
	// Read reads pasted text as the requests it holds, one per curl command.
	Read(text string) ([]domain.CollectionNode, error)
}
