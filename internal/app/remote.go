package app

import (
	"context"

	"schemo/internal/cloud"
	"schemo/internal/domain"
)

// workspaceRemote is the account's side of a workspace, as the workspace feature asks for it.
//
// It lives here because this is the only layer that knows both halves: the workspace feature has no
// notion of a token (it expires and is refreshed behind the account's door) and the wire has no
// notion of a workspace, only of a call. The two lines that join them live in the composition root.
type workspaceRemote struct {
	client  *cloud.Client
	account *cloud.Account
}

func (r workspaceRemote) Delete(ctx context.Context, workspaceID string) error {
	caller, err := r.account.Caller(ctx)
	if err != nil {
		return err
	}
	return r.client.DeleteWorkspace(ctx, caller, workspaceID)
}

func (r workspaceRemote) ChangeKind(
	ctx context.Context,
	workspaceID string,
	kind domain.WorkspaceKind,
) error {
	caller, err := r.account.Caller(ctx)
	if err != nil {
		return err
	}
	return r.client.SetKind(ctx, caller, workspaceID, kind)
}

func (r workspaceRemote) Leave(ctx context.Context, workspaceID string) error {
	caller, err := r.account.Caller(ctx)
	if err != nil {
		return err
	}
	return r.client.Leave(ctx, caller, workspaceID)
}

// inviteSender is the team of a workspace in the shape a form making one asks for.
//
// Members returns the whole team again on every change, while a form making a workspace
// only wants to know whether each letter went out. That difference is why this is an adapter rather
// than a second method there.
type inviteSender struct{ members *cloud.Members }

func (s inviteSender) Invite(ctx context.Context, workspaceID, email, roleID string) error {
	_, err := s.members.Invite(ctx, workspaceID, email, roleID)
	return err
}
