// Package members is the team of a space: the people in it, what each of them may do, and the links
// that let more in.
//
// All of it belongs to the cloud. Who is in a space is decided there — by roles an organisation
// wrote, and by the rule that a space of somebody's own holds one person — so nothing here is kept
// in the device's database: a copy of a membership would be a copy that can disagree about who may
// read what. What this feature does is ask, and word what comes back.
//
// The application is complete without an account, and a space never linked has no team
// at all: there is no account for anybody to have been invited through, and the window asks for
// nothing rather than for an empty answer.
package members

import (
	"context"

	"schemo/internal/domain"
)

// UseCase is the team of a space over one store and one server.
type UseCase struct {
	store  Store
	server Server
	caller domain.Principal
}

func NewUseCase(store Store, server Server, caller domain.Principal) *UseCase {
	return &UseCase{store: store, server: server, caller: caller}
}

// Show is the team of one space. A space that is not in the account answers as a space with no team
// rather than as a failure: it has never been anywhere and nobody can be in it.
func (u *UseCase) Show(ctx context.Context, workspaceID string) (Team, error) {
	space, err := u.store.Workspace(ctx, workspaceID)
	if err != nil {
		return Team{}, err
	}
	empty := Team{SpaceID: space.ID, Personal: space.Kind == domain.WorkspacePersonal}
	if space.RemoteID == "" {
		return empty, nil
	}

	caller, err := u.caller.Caller(ctx)
	if err != nil {
		return Team{}, err
	}
	held, err := u.of(ctx, caller, space.RemoteID)
	if err != nil {
		return Team{}, err
	}
	held.SpaceID = space.ID
	return held, nil
}

// Roles is the roles that may be handed out in the organisation's spaces, asked without naming one.
//
// A form needs it while it is still making a space: a team's is made with the people to invite
// already written down, and the role each will hold is chosen before the space they will hold it in
// exists. The organisation is the caller's own — read from the account's answer the way `of` reads
// it for a space, because a device holds no id for the organisation it works in.
func (u *UseCase) Roles(ctx context.Context) ([]domain.Role, error) {
	caller, err := u.caller.Caller(ctx)
	if err != nil {
		return nil, err
	}
	account, err := u.server.Spaces(ctx, caller)
	if err != nil {
		return nil, err
	}
	if account.OwnOrgID == "" {
		// The account has no organisation of its own to invite anybody into: a device that has just
		// signed in before anything was made for it is the ordinary case, and the round that makes
		// the organisation is not this call's to wait for.
		return nil, domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}
	return u.server.Roles(ctx, caller, account.OwnOrgID, domain.ScopeWorkspace)
}

// Invite makes a link for somebody, in this space. The address is what the letter goes to, and the
// role is what they will hold here: an invitation is the one way a person is given access without
// being in the organisation already.
func (u *UseCase) Invite(
	ctx context.Context,
	workspaceID string,
	asked domain.Invitation,
) (Team, error) {
	space, account, caller, err := u.linked(ctx, workspaceID)
	if err != nil {
		return Team{}, err
	}
	if _, err := u.server.Invite(ctx, caller, account.OrgID, space.RemoteID, asked); err != nil {
		return Team{}, err
	}
	held, err := u.of(ctx, caller, space.RemoteID)
	if err != nil {
		return Team{}, err
	}
	held.SpaceID = space.ID
	return held, nil
}

// Revoke takes a link back before anybody has used it.
func (u *UseCase) Revoke(ctx context.Context, workspaceID, inviteID string) (Team, error) {
	space, account, caller, err := u.linked(ctx, workspaceID)
	if err != nil {
		return Team{}, err
	}
	if _, err := u.server.RevokeInvite(ctx, caller, account.OrgID, inviteID); err != nil {
		return Team{}, err
	}
	held, err := u.of(ctx, caller, space.RemoteID)
	if err != nil {
		return Team{}, err
	}
	held.SpaceID = space.ID
	return held, nil
}

// SetRole gives somebody another role here. Their place in the organisation is untouched: the two
// are separate, and a space's owner is not an org's.
func (u *UseCase) SetRole(
	ctx context.Context,
	workspaceID, userID, roleID string,
) (Team, error) {
	space, _, caller, err := u.linked(ctx, workspaceID)
	if err != nil {
		return Team{}, err
	}
	if _, err := u.server.ChangeRole(ctx, caller, space.RemoteID, userID, roleID); err != nil {
		return Team{}, err
	}
	held, err := u.of(ctx, caller, space.RemoteID)
	if err != nil {
		return Team{}, err
	}
	held.SpaceID = space.ID
	return held, nil
}

// Remove takes somebody out of the space. They stay in the organisation, and their devices stop
// syncing it — a refusal rather than a reset: there is nothing left for them to start over.
func (u *UseCase) Remove(ctx context.Context, workspaceID, userID string) (Team, error) {
	space, _, caller, err := u.linked(ctx, workspaceID)
	if err != nil {
		return Team{}, err
	}
	if err := u.server.RemoveMember(ctx, caller, space.RemoteID, userID); err != nil {
		return Team{}, err
	}
	held, err := u.of(ctx, caller, space.RemoteID)
	if err != nil {
		return Team{}, err
	}
	held.SpaceID = space.ID
	return held, nil
}

// of reads one space whole: its people, its links and the roles that may be handed out here. Three
// questions of one subject, asked together because a window draws them in one block — and the
// invitations are the organisation's, narrowed to the ones that name this space, which is what a
// link made for it does.
func (u *UseCase) of(
	ctx context.Context,
	caller domain.Caller,
	accountID string,
) (Team, error) {
	held, err := u.space(ctx, caller, accountID)
	if err != nil {
		return Team{}, err
	}

	people, err := u.server.Members(ctx, caller, accountID)
	if err != nil {
		return Team{}, err
	}
	// Which row is the person reading is this device's to say and not the server's: it answers the
	// people of a space and not "you", and a row marked as the reader is one the window does not
	// offer to remove from a space they are in.
	mine, err := u.caller.Me(ctx)
	if err != nil {
		return Team{}, err
	}
	for at := range people {
		people[at].Self = people[at].UserID == mine.UserID
	}

	made, err := u.server.Invites(ctx, caller, held.OrgID)
	if err != nil {
		return Team{}, err
	}
	waiting := make([]domain.Invite, 0, len(made))
	for _, one := range made {
		if one.WorkspaceID == accountID {
			waiting = append(waiting, one)
		}
	}

	roles, err := u.server.Roles(ctx, caller, held.OrgID, domain.ScopeWorkspace)
	if err != nil {
		return Team{}, err
	}
	return Team{
		AccountID: accountID,
		Personal:  held.Kind == domain.WorkspacePersonal,
		Members:   people,
		Invites:   waiting,
		Roles:     roles,
	}, nil
}

// space is the space as the account describes it, which is where the organisation and the kind
// come from: both are the server's, and a device that kept its own word about either would be
// a second opinion.
func (u *UseCase) space(
	ctx context.Context,
	caller domain.Caller,
	accountID string,
) (domain.RemoteSpace, error) {
	account, err := u.server.Spaces(ctx, caller)
	if err != nil {
		return domain.RemoteSpace{}, err
	}
	for _, one := range account.Spaces {
		if one.ID == accountID {
			return one, nil
		}
	}
	// The account no longer has it: somebody was removed from the space, or it was deleted in the
	// panel. That is not a failure of this screen — there is nothing here to show — and the round
	// that reads the account's list says so on the space itself.
	return domain.RemoteSpace{}, domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
}

// linked is a space this device has an account id for, with the caller asking and the space as the
// account describes it: every change here is made in the account, and the organisation a link is
// made in is the account's answer and not anything stored here. A space that has never
// been linked has no team and no way to invite anybody into it — there is no account for a link to
// be made in — and the window is told rather than shown controls that cannot work.
func (u *UseCase) linked(
	ctx context.Context,
	workspaceID string,
) (domain.Workspace, domain.RemoteSpace, domain.Caller, error) {
	space, err := u.store.Workspace(ctx, workspaceID)
	if err != nil {
		return domain.Workspace{}, domain.RemoteSpace{}, domain.Caller{}, err
	}
	if space.RemoteID == "" {
		return domain.Workspace{}, domain.RemoteSpace{}, domain.Caller{},
			domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}

	caller, err := u.caller.Caller(ctx)
	if err != nil {
		return domain.Workspace{}, domain.RemoteSpace{}, domain.Caller{}, err
	}
	account, err := u.space(ctx, caller, space.RemoteID)
	if err != nil {
		return domain.Workspace{}, domain.RemoteSpace{}, domain.Caller{}, err
	}
	return space, account, caller, nil
}
