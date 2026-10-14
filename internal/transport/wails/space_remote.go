package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/infra/server"
	"schemo/internal/usecase/account"
	"schemo/internal/usecase/members"
)

// spaceRemote is the account's side of a space, as the workspace feature asks for it.
//
// It is here because this is the only layer that knows both halves: the workspace feature has no
// notion of a token — a token expires and is refreshed behind the account's door — and the wire has
// no notion of a space, only of a call. Neither of them should learn the other's shape, so the two
// lines that join them live in the composition root.
type spaceRemote struct {
	client  *server.Client
	account *account.UseCase
}

func (r spaceRemote) Delete(ctx context.Context, workspaceID string) error {
	caller, err := r.account.Caller(ctx)
	if err != nil {
		return err
	}
	return r.client.DeleteSpace(ctx, caller, workspaceID)
}

func (r spaceRemote) ChangeKind(
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

func (r spaceRemote) Leave(ctx context.Context, workspaceID string) error {
	caller, err := r.account.Caller(ctx)
	if err != nil {
		return err
	}
	return r.client.Leave(ctx, caller, workspaceID)
}

// inviteSender is the team of a space in the shape a form making one asks for.
//
// The members feature answers the whole team again on every change — a window draws the block from
// one answer rather than patching a row into a list it holds — and a form that is making a space
// wants only to know whether each letter went out. That is the whole of the difference, and it is
// why this is an adapter rather than a second method there.
type inviteSender struct{ members *members.UseCase }

func (s inviteSender) Invite(ctx context.Context, workspaceID, email, roleID string) error {
	_, err := s.members.Invite(ctx, workspaceID, domain.Invitation{Email: email, RoleID: roleID})
	return err
}
