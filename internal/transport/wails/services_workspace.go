package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/usecase/workspace"
)

// WorkspaceService is the switcher: which spaces exist, which one the window is showing, and what a
// new one is called. Every call answers with the whole set, the way the environments and the tree
// do — the window draws all of it at once, and a partial answer would only be a second thing to
// keep.
type WorkspaceService struct {
	workspaces *workspace.UseCase
}

func NewWorkspaceService(uc *workspace.UseCase) *WorkspaceService {
	return &WorkspaceService{workspaces: uc}
}

func (s *WorkspaceService) Snapshot(ctx context.Context) (domain.WorkspaceState, error) {
	return s.workspaces.Snapshot(ctx)
}

// Switch points the app at another workspace. The data below the window is replaced by the caller:
// this says which space is on screen, and the window reloads what that space holds.
func (s *WorkspaceService) Switch(ctx context.Context, id string) (domain.WorkspaceState, error) {
	return s.workspaces.Switch(ctx, id)
}

// Create makes a space, and answers with what became of the people written in the form beside it: a
// team's space is one other people are invited into, and the invitations go out with it.
func (s *WorkspaceService) Create(
	ctx context.Context,
	in workspace.CreateInput,
) (workspace.Created, error) {
	return s.workspaces.Create(ctx, in)
}

func (s *WorkspaceService) Update(
	ctx context.Context,
	id string,
	patch workspace.Patch,
) (domain.WorkspaceState, error) {
	return s.workspaces.Update(ctx, id, patch)
}

// Delete takes a workspace and everything in it. The one the app is born with is refused by the use
// case, with a code the window words itself.
func (s *WorkspaceService) Delete(ctx context.Context, id string) (domain.WorkspaceState, error) {
	return s.workspaces.Delete(ctx, id)
}

// Leave steps out of a space somebody else's: the place in the account goes, and the space goes
// from this device with it. A space that is not in the account has no place to leave, which the use
// case refuses as one that is not there.
func (s *WorkspaceService) Leave(ctx context.Context, id string) (domain.WorkspaceState, error) {
	return s.workspaces.Leave(ctx, id)
}
