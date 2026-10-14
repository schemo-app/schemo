package scripting

import (
	"context"

	"schemo/internal/domain"
)

// Engine runs one script and answers with its report. What leaves the sandbox — the request a
// pre-request script changed, the variables it wrote — is what the engine put back where it found
// it.
type Engine interface {
	Run(in domain.ScriptInput) domain.ScriptRun
}

// Tree is where the levels above a request come from: the tree a node sits in, and what each level
// runs. Both are read as the collection feature reads them, which is why the store satisfies this
// port unchanged.
type Tree interface {
	Collections(ctx context.Context, workspaceID string) ([]domain.Collection, error)
	Scripts(ctx context.Context, workspaceID, id string) (*domain.Scripts, error)
}

// Store is where the reports go, and where the level's own code is read and written. Reports hang
// off the record of the request they ran around, which is what the response viewer asks by; a
// level's code is addressed by its id, and which table that id names is not the editor's business.
type Store interface {
	Scripts(ctx context.Context, workspaceID, id string) (*domain.Scripts, error)
	SaveScripts(ctx context.Context, workspaceID, id string, scripts *domain.Scripts) error

	SaveScriptRun(ctx context.Context, workspaceID string, run domain.ScriptRun) error
	ScriptRuns(ctx context.Context, recordID string) ([]domain.ScriptRun, error)
}

// Variables is what a script writes and reads outside its own run: the environment this app is
// working in, and the globals under it. The run's own scope is kept by this feature — it is nothing
// but this run's memory — so it is not here.
type Variables interface {
	Variable(ctx context.Context, scope domain.VarScope, name string) (string, bool, error)
	SetVariable(ctx context.Context, scope domain.VarScope, name string, value string) error
	// CaptureVariable is SetVariable that can also hide the value.
	CaptureVariable(
		ctx context.Context, scope domain.VarScope, name string, value string, secret bool,
	) error
}
