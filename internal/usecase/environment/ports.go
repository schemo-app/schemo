package environment

import (
	"context"

	"schemo/internal/domain"
)

// Store is the persistence this feature needs, declared next to its user so the feature does not
// have to know whether the rows come from SQLite or a fake in a test. Every call names the
// workspace it works in: a name may be taken in each of them.
type Store interface {
	EnvState(ctx context.Context, workspaceID string) (domain.EnvState, error)
	SaveEnvironment(ctx context.Context, workspaceID string, env domain.Environment) error
	DeleteEnvironment(ctx context.Context, workspaceID, id string) error
	SaveVariable(ctx context.Context, workspaceID string, scope domain.EnvScope,
		v domain.Variable) error
	DeleteVariable(ctx context.Context, workspaceID, id string) error
	VariableValue(ctx context.Context, workspaceID, id string) (string, error)

	// Which environment the workspace is working in is kept on the workspace itself: two spaces are
	// two answers, and a switch that kept one would resolve against the other space's variables.
	SetActiveEnvironment(ctx context.Context, workspaceID, id string) error

	// AwaitsFirstPull says the space is one of an account's that this device has not read yet: what
	// it holds is on the server, and an environment made here before that would be a second one.
	AwaitsFirstPull(ctx context.Context, workspaceID string) (bool, error)
}
