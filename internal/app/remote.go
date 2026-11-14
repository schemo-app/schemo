package app

import (
	"context"

	"schemo/internal/cloud"
	"schemo/internal/domain"
)

// workspaceRemote joins the workspace feature, which knows no tokens, to the account client, which
// knows no workspaces.
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

// inviteSender adapts Members for the create form, which needs per-address results rather than the
// whole team.
type inviteSender struct{ members *cloud.Members }

func (s inviteSender) Invite(ctx context.Context, workspaceID, email, roleID string) error {
	_, err := s.members.Invite(ctx, workspaceID, email, roleID)
	return err
}
