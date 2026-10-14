package server

import (
	"context"

	"google.golang.org/grpc"

	"github.com/schemo-app/proto/go/invite/v1"
	"github.com/schemo-app/proto/go/role/v1"
	"github.com/schemo-app/proto/go/workspace/v1"

	"schemo/internal/domain"
)

// The team of a space: who is in it, what a person may do there, and the links that let more in.
//
// Three services on the wire and one subject here — a space of a team's is made to be worked in
// together, and a window that draws its people asks all three at once. Nothing here decides
// anything: whether somebody may be added, whether a link is good for its address, whether a role
// may be handed out are the server's to refuse, and what comes back is its answer worded.

// Members is the people in a space.
func (c *Client) Members(
	ctx context.Context,
	caller domain.Caller,
	workspaceID string,
) ([]domain.Member, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (
			*workspace.WorkspaceMembershipServiceListMembersResponse, error,
		) {
			return workspace.NewWorkspaceMembershipServiceClient(conn).ListMembers(ctx,
				workspace.WorkspaceMembershipServiceListMembersRequest_builder{
					WorkspaceId: workspaceID,
				}.Build())
		})
	if err != nil {
		return nil, err
	}

	out := make([]domain.Member, 0, len(answer.GetMembers()))
	for _, one := range answer.GetMembers() {
		out = append(out, memberOf(one))
	}
	// Who the person reading is is not the server's to say: it answers members and not "you", and a
	// row marked as the reader is a row the window does not offer to remove from their own space.
	return out, nil
}

// SetMember puts somebody in a space at a role, or moves them to another one. Adding and changing
// are one call because they are one row — and the answer is the row as it stands afterwards.
func (c *Client) SetMember(
	ctx context.Context,
	caller domain.Caller,
	workspaceID, userID, roleID string,
) (domain.Member, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (
			*workspace.WorkspaceMembershipServiceAddMemberResponse, error,
		) {
			return workspace.NewWorkspaceMembershipServiceClient(conn).AddMember(ctx,
				workspace.WorkspaceMembershipServiceAddMemberRequest_builder{
					WorkspaceId:  workspaceID,
					TargetUserId: userID,
					RoleId:       roleID,
				}.Build())
		})
	if err != nil {
		return domain.Member{}, err
	}
	return memberOf(answer.GetMember()), nil
}

// ChangeRole moves somebody to another role in a space only: their place in the organisation is
// untouched, because the two are separate and a space's owner is not an org's.
func (c *Client) ChangeRole(
	ctx context.Context,
	caller domain.Caller,
	workspaceID, userID, roleID string,
) (domain.Member, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (
			*workspace.WorkspaceMembershipServiceChangeMemberRoleResponse, error,
		) {
			return workspace.NewWorkspaceMembershipServiceClient(conn).ChangeMemberRole(ctx,
				workspace.WorkspaceMembershipServiceChangeMemberRoleRequest_builder{
					WorkspaceId:  workspaceID,
					TargetUserId: userID,
					RoleId:       roleID,
				}.Build())
		})
	if err != nil {
		return domain.Member{}, err
	}
	return memberOf(answer.GetMember()), nil
}

// RemoveMember takes somebody out of a space. They stay in the organisation, and their devices stop
// syncing the space because they are no longer in it.
func (c *Client) RemoveMember(
	ctx context.Context,
	caller domain.Caller,
	workspaceID, userID string,
) error {
	_, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (
			*workspace.WorkspaceMembershipServiceRemoveMemberResponse, error,
		) {
			return workspace.NewWorkspaceMembershipServiceClient(conn).RemoveMember(ctx,
				workspace.WorkspaceMembershipServiceRemoveMemberRequest_builder{
					WorkspaceId:  workspaceID,
					TargetUserId: userID,
				}.Build())
		})
	return err
}

// Leave takes the caller out of a space. It names nobody: the person leaving is the one asking, the
// whole of the difference between this and `RemoveMember` — one is a right over another's place,
// and this is their own. Whether they are the last one who may manage it is a rule
// the server keeps, and its refusal travels back as it is.
func (c *Client) Leave(ctx context.Context, caller domain.Caller, workspaceID string) error {
	_, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (
			*workspace.WorkspaceMembershipServiceLeaveResponse, error,
		) {
			return workspace.NewWorkspaceMembershipServiceClient(conn).Leave(ctx,
				workspace.WorkspaceMembershipServiceLeaveRequest_builder{
					WorkspaceId: workspaceID,
				}.Build())
		})
	return err
}

// Roles is the roles of an organisation in one scope: the ones a person may be given in a space, or
// the ones they may hold in the org itself. This application asks for the first: it hands roles out
// and does not write them.
func (c *Client) Roles(
	ctx context.Context,
	caller domain.Caller,
	orgID, scope string,
) ([]domain.Role, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*role.ListRolesResponse, error) {
			return role.NewRoleServiceClient(conn).ListRoles(ctx,
				role.ListRolesRequest_builder{OrgId: orgID, Scope: scope}.Build())
		})
	if err != nil {
		return nil, err
	}

	out := make([]domain.Role, 0, len(answer.GetRoles()))
	for _, one := range answer.GetRoles() {
		out = append(out, domain.Role{
			ID:          one.GetId(),
			Name:        one.GetName(),
			Description: one.GetDescription(),
			BuiltIn:     one.GetIsBuiltIn(),
		})
	}
	return out, nil
}

// Invites is the invitations an organisation has made. A space's own list is this one read for that
// space: an invitation that names a space carries it, and the server answers the org at a time
// because that is the scope a link is made in.
func (c *Client) Invites(
	ctx context.Context,
	caller domain.Caller,
	orgID string,
) ([]domain.Invite, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*invite.ListInvitesResponse, error) {
			return invite.NewInviteServiceClient(conn).ListInvites(ctx,
				invite.ListInvitesRequest_builder{OrgId: orgID}.Build())
		})
	if err != nil {
		return nil, err
	}

	out := make([]domain.Invite, 0, len(answer.GetInvites()))
	for _, one := range answer.GetInvites() {
		out = append(out, inviteOf(one))
	}
	return out, nil
}

// Invite makes a link and answers it. The letter goes out inside this call when the invitation
// an address, which is why nothing here reports one: a letter that did not leave is not a link that
// did not work, and the link is what the caller is handed.
func (c *Client) Invite(
	ctx context.Context,
	caller domain.Caller,
	orgID, workspaceID string,
	asked domain.Invitation,
) (domain.Invite, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*invite.CreateInviteResponse, error) {
			return invite.NewInviteServiceClient(conn).CreateInvite(ctx,
				invite.CreateInviteRequest_builder{
					OrgId:          orgID,
					WorkspaceId:    workspaceID,
					RoleId:         asked.RoleID,
					InviteeEmail:   asked.Email,
					ExpiresInHours: asked.ExpiresHours,
				}.Build())
		})
	if err != nil {
		return domain.Invite{}, err
	}
	return inviteOf(answer.GetInvite()), nil
}

// RevokeInvite kills a link that has not been used up.
func (c *Client) RevokeInvite(
	ctx context.Context,
	caller domain.Caller,
	orgID, inviteID string,
) (domain.Invite, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*invite.RevokeInviteResponse, error) {
			return invite.NewInviteServiceClient(conn).RevokeInvite(ctx,
				invite.RevokeInviteRequest_builder{OrgId: orgID, InviteId: inviteID}.Build())
		})
	if err != nil {
		return domain.Invite{}, err
	}
	return inviteOf(answer.GetInvite()), nil
}

func memberOf(one *workspace.WorkspaceMember) domain.Member {
	return domain.Member{
		UserID:   one.GetUserId(),
		Email:    one.GetEmail(),
		Name:     one.GetName(),
		RoleID:   one.GetRoleId(),
		RoleName: one.GetRoleName(),
		Gone:     one.GetUserDeleted(),
	}
}

func inviteOf(one *invite.Invite) domain.Invite {
	held := domain.Invite{
		ID:          one.GetId(),
		WorkspaceID: one.GetWorkspaceId(),
		Email:       one.GetEmail(),
		RoleID:      one.GetRoleId(),
		RoleName:    one.GetRoleName(),
		Status:      one.GetStatus(),
		MaxUses:     one.GetMaxUses(),
		UsedCount:   one.GetUsedCount(),
	}
	if at := one.GetCreatedAt(); at != nil {
		held.CreatedAt = at.AsTime().UnixMilli()
	}
	if at := one.GetExpiresAt(); at != nil {
		held.ExpiresAt = at.AsTime().UnixMilli()
	}
	return held
}
