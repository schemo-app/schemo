package workspace

import (
	"context"

	"schemo/internal/domain"
)

// Store is the persistence this feature needs. It is declared here, next to its user, so the
// feature does not have to know whether the rows come from SQLite or a fake in a test.
type Store interface {
	Workspaces(ctx context.Context) ([]domain.Workspace, error)
	Workspace(ctx context.Context, id string) (domain.Workspace, error)
	SaveWorkspace(ctx context.Context, w domain.Workspace) error
	DeleteWorkspace(ctx context.Context, id string) error

	// The pointer to the workspace on screen, and the one setting that names it. It is a
	// preference of this installation rather than data of a workspace, which is why it lives in
	// the settings table and not on a row here.
	ActiveWorkspace(ctx context.Context) (string, error)
	SetActiveWorkspace(ctx context.Context, id string) error

	// ReopenLast answers whether the app comes back to the space it was left in — the setting that
	// decides where a launch starts. Read here because this is the feature that acts on it.
	ReopenLast(ctx context.Context) (bool, error)

	// What each workspace holds — collections, environments, runs — counted in one call because the
	// window draws all the rows at once. A reading over other features' tables, which is why it is
	// here beside the list rather than a column on a workspace.
	Counts(ctx context.Context) (map[string]domain.WorkspaceCounts, error)
}

// Remote is the account's side of a space that lives there. Ending one is not this device's to do
// alone: the space is not this device's alone, and a copy deleted only here would be handed back by
// the next round — empty, and with the person's work in it gone from where they deleted it.
//
// It is declared here and not taken from the engine, because it is not the engine's question: what
// a space is to the account belongs to the space, and nothing about it is on the path of a push.
type Remote interface {
	// Delete ends a space in the account. It names the space by its id and nothing else: the act is
	// made deliberate by the sheet that asked for it, and a name travelling with it would be the
	// name this device happens to draw — one that a rename still in flight would be refused over.
	Delete(ctx context.Context, workspaceID string) error
	// ChangeKind asks the account to change what a space is for. It is asked rather than decided
	// because the rule is about who is in the space, and only the account knows that: a space a
	// second person works in may not be turned into somebody's own, and the refusal is the server's
	// to give.
	ChangeKind(ctx context.Context, workspaceID string, kind domain.WorkspaceKind) error
	// Leave takes this person out of a space somebody else's. It is the one way a member exits: a
	// member who may not delete the space cannot end it for everybody, and a row deleted only here
	// would be handed back by the next round, with the account still holding their place in it.
	Leave(ctx context.Context, workspaceID string) error
}

// Linker makes a space in the account now and joins this device's row to it. It is the sync
// engine's own road, asked for rather than copied: a create whose answer was lost is found again by
// the id the engine names and writes down before the call (see docs/SYNC.md), and a second
// implementation of that would be a second set of ways for it to go wrong. It is asked only where a
// space has to exist in the account at once — a team's, which other people are about to be invited
// into. A space of somebody's own links when the engine next runs, as it always has.
type Linker interface {
	LinkNow(ctx context.Context, workspaceID string) error
}

// Inviter is the team of a space, as far as making one is concerned.
type Inviter interface {
	Invite(ctx context.Context, workspaceID, email, roleID string) error
}

// Notifier is how every window hears that the workspace on screen moved. The switcher is the only
// caller, but the event is a broadcast: the news is not for the window that asked.
