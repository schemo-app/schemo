package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/usecase/members"
)

// MembersService is the team of a space as a window sees it: the people in it, the links that are
// waiting to let more in, and the roles they may be given.
//
// Every call answers the whole team again, the way every other service here does — the window draws
// the block from one answer rather than patching a row into a list it holds. It is the cloud's
// subject and not this device's: nothing about a membership is written here, and a space with no
// account answers as a space with no team rather than as a failure.
type MembersService struct {
	members *members.UseCase
}

func NewMembersService(uc *members.UseCase) *MembersService {
	return &MembersService{members: uc}
}

// Team is who is in a space and what is waiting to let more in.
func (s *MembersService) Team(ctx context.Context, workspaceID string) (members.Team, error) {
	return s.members.Show(ctx, workspaceID)
}

// Roles is what may be handed out in the organisation's spaces, asked without naming one.
//
// It is what the create form needs: a team's space is made with the people to invite already
// written down, and the role each will hold is chosen before the space they will hold it in exists.
func (s *MembersService) Roles(ctx context.Context) ([]domain.Role, error) {
	return s.members.Roles(ctx)
}

// Invite makes a link for somebody. The address is what the letter goes to; an empty one makes a
// link to hand over by hand.
func (s *MembersService) Invite(
	ctx context.Context,
	workspaceID, email, roleID string,
) (members.Team, error) {
	return s.members.Invite(ctx, workspaceID, domain.Invitation{Email: email, RoleID: roleID})
}

// Revoke takes a link back before anybody has used it.
func (s *MembersService) Revoke(
	ctx context.Context,
	workspaceID, inviteID string,
) (members.Team, error) {
	return s.members.Revoke(ctx, workspaceID, inviteID)
}

// SetRole gives somebody another role in one space. Their place in the organisation is untouched.
func (s *MembersService) SetRole(
	ctx context.Context,
	workspaceID, userID, roleID string,
) (members.Team, error) {
	return s.members.SetRole(ctx, workspaceID, userID, roleID)
}

// Remove takes somebody out of the space. They stay in the organisation.
func (s *MembersService) Remove(
	ctx context.Context,
	workspaceID, userID string,
) (members.Team, error) {
	return s.members.Remove(ctx, workspaceID, userID)
}
