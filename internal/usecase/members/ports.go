package members

import (
	"context"

	"schemo/internal/domain"
)

// Store is the one thing this feature reads from this device: the space itself, for the id the
// account knows it by. Nothing about a team is kept here — a membership is the cloud's, and a copy
// of one would be a copy that can disagree about who may read what.
type Store interface {
	// Workspace reads one space as this device holds it. Its `RemoteID` is empty for a space that has
	// never been linked, and a space with no account id has no team to show.
	Workspace(ctx context.Context, id string) (domain.Workspace, error)
}

// Server is the account's side: who is in a space, what a person may do there, and the links that
// let more in. Every method takes the caller, because the token is the account's own and expires
// behind its door.
type Server interface {
	// Spaces is the spaces of the account. It is how the organisation a space stands in is read: an
	// invitation is made in it, and the roles that may be handed out are its own.
	Spaces(ctx context.Context, caller domain.Caller) (domain.AccountSpaces, error)

	Members(ctx context.Context, caller domain.Caller, workspaceID string) ([]domain.Member, error)
	SetMember(
		ctx context.Context,
		caller domain.Caller,
		workspaceID, userID, roleID string,
	) (domain.Member, error)
	ChangeRole(
		ctx context.Context,
		caller domain.Caller,
		workspaceID, userID, roleID string,
	) (domain.Member, error)
	RemoveMember(ctx context.Context, caller domain.Caller, workspaceID, userID string) error

	// Invites is the invitations an organisation has made; the ones for one space are those that
	// name it. Invite makes one, and the letter goes out inside the call.
	Invites(ctx context.Context, caller domain.Caller, orgID string) ([]domain.Invite, error)
	Invite(
		ctx context.Context,
		caller domain.Caller,
		orgID, workspaceID string,
		asked domain.Invitation,
	) (domain.Invite, error)
	RevokeInvite(
		ctx context.Context, caller domain.Caller, orgID, inviteID string,
	) (domain.Invite, error)

	// Roles is the roles of an organisation in one scope: what may be handed to somebody in a space.
	Roles(ctx context.Context, caller domain.Caller, orgID, scope string) ([]domain.Role, error)
}
