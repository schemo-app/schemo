package cloud

import (
	"context"
	"schemo/internal/db"
	"schemo/internal/domain"

	invitev1 "github.com/schemo-app/proto/go/invite/v1"
	rolev1 "github.com/schemo-app/proto/go/role/v1"
	workspacev1 "github.com/schemo-app/proto/go/workspace/v1"
	"google.golang.org/grpc"
)

// Members is the team of a workspace as the window sees it. Every call returns the whole team again,
// and a workspace with no account answers as a workspace with no team rather than as a failure.
type Members struct {
	store  *db.Store
	server MemberServer
	caller domain.Principal
}

func NewMembers(store *db.Store, server MemberServer, caller domain.Principal) *Members {
	return &Members{store: store, server: server, caller: caller}
}

// Team is the team of one workspace. A workspace that is not in the account answers as a workspace with no team
// rather than as a failure: it has never been anywhere and nobody can be in it.
func (s *Members) Team(ctx context.Context, workspaceID string) (Team, error) {
	workspace, err := s.store.Workspace(ctx, workspaceID)
	if err != nil {
		return Team{}, err
	}
	empty := Team{WorkspaceID: workspace.ID, Personal: workspace.Kind == domain.WorkspacePersonal}
	if workspace.RemoteID == "" {
		return empty, nil
	}

	caller, err := s.caller.Caller(ctx)
	if err != nil {
		return Team{}, err
	}
	held, err := s.of(ctx, caller, workspace.RemoteID)
	if err != nil {
		return Team{}, err
	}
	held.WorkspaceID = workspace.ID
	return held, nil
}

// Roles lists the roles of the caller's organisation, for a form that picks them before the
// workspace exists.
func (s *Members) Roles(ctx context.Context) ([]domain.Role, error) {
	caller, err := s.caller.Caller(ctx)
	if err != nil {
		return nil, err
	}
	account, err := s.server.Workspaces(ctx, caller)
	if err != nil {
		return nil, err
	}
	if account.OwnOrgID == "" {
		// Normal right after sign-in: the organisation appears with the next round.
		return nil, domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}
	return s.server.Roles(ctx, caller, account.OwnOrgID, domain.ScopeWorkspace)
}

// Invite makes an invitation link with a role; an empty address makes a link to share by hand.
func (s *Members) Invite(ctx context.Context, workspaceID, email, roleID string) (Team, error) {
	asked := domain.Invitation{Email: email, RoleID: roleID}
	workspace, account, caller, err := s.linked(ctx, workspaceID)
	if err != nil {
		return Team{}, err
	}
	if _, err := s.server.Invite(ctx, caller, account.OrgID, workspace.RemoteID, asked); err != nil {
		return Team{}, err
	}
	held, err := s.of(ctx, caller, workspace.RemoteID)
	if err != nil {
		return Team{}, err
	}
	held.WorkspaceID = workspace.ID
	return held, nil
}

// Revoke takes a link back before anybody has used it.
func (s *Members) Revoke(ctx context.Context, workspaceID, inviteID string) (Team, error) {
	workspace, account, caller, err := s.linked(ctx, workspaceID)
	if err != nil {
		return Team{}, err
	}
	if _, err := s.server.RevokeInvite(ctx, caller, account.OrgID, inviteID); err != nil {
		return Team{}, err
	}
	held, err := s.of(ctx, caller, workspace.RemoteID)
	if err != nil {
		return Team{}, err
	}
	held.WorkspaceID = workspace.ID
	return held, nil
}

// SetRole gives somebody another role here. Their place in the organisation is untouched: the two
// are separate, and a workspace's owner is not an org's.
func (s *Members) SetRole(
	ctx context.Context,
	workspaceID, userID, roleID string,
) (Team, error) {
	workspace, _, caller, err := s.linked(ctx, workspaceID)
	if err != nil {
		return Team{}, err
	}
	if _, err := s.server.ChangeRole(ctx, caller, workspace.RemoteID, userID, roleID); err != nil {
		return Team{}, err
	}
	held, err := s.of(ctx, caller, workspace.RemoteID)
	if err != nil {
		return Team{}, err
	}
	held.WorkspaceID = workspace.ID
	return held, nil
}

// Remove takes somebody out of the workspace. They stay in the organisation, and their devices stop
// syncing it — a refusal rather than a reset: there is nothing left for them to start over.
func (s *Members) Remove(ctx context.Context, workspaceID, userID string) (Team, error) {
	workspace, _, caller, err := s.linked(ctx, workspaceID)
	if err != nil {
		return Team{}, err
	}
	if err := s.server.RemoveMember(ctx, caller, workspace.RemoteID, userID); err != nil {
		return Team{}, err
	}
	held, err := s.of(ctx, caller, workspace.RemoteID)
	if err != nil {
		return Team{}, err
	}
	held.WorkspaceID = workspace.ID
	return held, nil
}

// of reads a workspace's members, invitations and roles together; invitations are filtered to this workspace.
func (s *Members) of(
	ctx context.Context,
	caller domain.Caller,
	accountID string,
) (Team, error) {
	held, err := s.workspace(ctx, caller, accountID)
	if err != nil {
		return Team{}, err
	}

	people, err := s.server.Members(ctx, caller, accountID)
	if err != nil {
		return Team{}, err
	}
	// The server does not mark "you", so the reader's row is found here and not offered for removal.
	mine, err := s.caller.Me(ctx)
	if err != nil {
		return Team{}, err
	}
	for at := range people {
		people[at].Self = people[at].UserID == mine.UserID
	}

	made, err := s.server.Invites(ctx, caller, held.OrgID)
	if err != nil {
		return Team{}, err
	}
	waiting := make([]domain.Invite, 0, len(made))
	for _, one := range made {
		if one.WorkspaceID == accountID {
			waiting = append(waiting, one)
		}
	}

	roles, err := s.server.Roles(ctx, caller, held.OrgID, domain.ScopeWorkspace)
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

// workspace reads the workspace from the account, which owns its organisation and kind.
func (s *Members) workspace(
	ctx context.Context,
	caller domain.Caller,
	accountID string,
) (domain.RemoteWorkspace, error) {
	account, err := s.server.Workspaces(ctx, caller)
	if err != nil {
		return domain.RemoteWorkspace{}, err
	}
	for _, one := range account.Workspaces {
		if one.ID == accountID {
			return one, nil
		}
	}
	// Removed or deleted in the panel; the next round marks the workspace itself.
	return domain.RemoteWorkspace{}, domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
}

// linked resolves a workspace's account id and the caller. An unlinked workspace has no team, which
// the window is told.
func (s *Members) linked(
	ctx context.Context,
	workspaceID string,
) (domain.Workspace, domain.RemoteWorkspace, domain.Caller, error) {
	workspace, err := s.store.Workspace(ctx, workspaceID)
	if err != nil {
		return domain.Workspace{}, domain.RemoteWorkspace{}, domain.Caller{}, err
	}
	if workspace.RemoteID == "" {
		return domain.Workspace{}, domain.RemoteWorkspace{}, domain.Caller{},
			domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}

	caller, err := s.caller.Caller(ctx)
	if err != nil {
		return domain.Workspace{}, domain.RemoteWorkspace{}, domain.Caller{}, err
	}
	account, err := s.workspace(ctx, caller, workspace.RemoteID)
	if err != nil {
		return domain.Workspace{}, domain.RemoteWorkspace{}, domain.Caller{}, err
	}
	return workspace, account, caller, nil
}

// The shape that exists only for the window: the team of the workspace on screen. A member row is a
// domain type and is not here.

// Team is a workspace's members and invitations; every change returns it whole from the server.
type Team struct {
	// WorkspaceID is the workspace here, which is what the window asked with.
	WorkspaceID string `json:"workspaceId"`
	// AccountID is the workspace's id in the account, empty for a workspace that has never been linked.
	AccountID string `json:"accountId,omitempty"`
	// Personal says the workspace is somebody's own: nobody may be put in it, and there is nothing for
	// the window to offer — the server refuses every put, and the window says why.
	Personal bool `json:"personal"`
	// Members are the people in it, the one reading marked among them.
	Members []domain.Member `json:"members"`
	// Invites are the links that have not been used up or taken back, for this workspace only.
	Invites []domain.Invite `json:"invites"`
	// Roles are the roles that may be handed out here: the workspace ones of the organisation the
	// workspace stands in.
	Roles []domain.Role `json:"roles"`
}

// MemberServer is the account's member API. Every method takes the caller, since the token refreshes
// behind the account.
type MemberServer interface {
	// Workspaces is the workspaces of the account. It is how the organisation a workspace stands in is read: an
	// invitation is made in it, and the roles that may be handed out are its own.
	Workspaces(ctx context.Context, caller domain.Caller) (domain.AccountWorkspaces, error)

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

	// Invites is the invitations an organisation has made; the ones for one workspace are those that
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

	// Roles is the roles of an organisation in one scope: what may be handed to somebody in a workspace.
	Roles(ctx context.Context, caller domain.Caller, orgID, scope string) ([]domain.Role, error)
}

// The team of a workspace: members, roles and invite links. The server decides every rule; refusals
// are returned worded.

// Members is the people in a workspace.
func (c *Client) Members(
	ctx context.Context,
	caller domain.Caller,
	workspaceID string,
) ([]domain.Member, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (
			*workspacev1.ListMembersResponse, error,
		) {
			client := workspacev1.NewWorkspaceMembershipServiceClient(conn)
			result := workspacev1.ListMembersResponse_builder{}.Build()
			token := ""
			for {
				page, err := client.ListMembers(ctx, workspacev1.ListMembersRequest_builder{WorkspaceId: workspaceID, PageToken: token}.Build())
				if err != nil {
					return nil, err
				}
				result.SetMembers(append(result.GetMembers(), page.GetMembers()...))

				token = page.GetNextPageToken()
				if token == "" {
					return result, nil
				}
			}
		})
	if err != nil {
		return nil, err
	}

	out := make([]domain.Member, 0, len(answer.GetMembers()))
	for _, one := range answer.GetMembers() {
		out = append(out, memberOf(one))
	}
	// Who the person reading is is not the server's to say: it answers members and not "you", and a
	// row marked as the reader is a row the window does not offer to remove from their own workspace.
	return out, nil
}

// SetMember puts somebody in a workspace at a role, or moves them to another one. Adding and changing
// are one call because they are one row — and the answer is the row as it stands afterwards.
func (c *Client) SetMember(
	ctx context.Context,
	caller domain.Caller,
	workspaceID, userID, roleID string,
) (domain.Member, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (
			*workspacev1.AddMemberResponse, error,
		) {
			return workspacev1.NewWorkspaceMembershipServiceClient(conn).AddMember(ctx,
				workspacev1.AddMemberRequest_builder{
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

// ChangeRole moves somebody to another role in a workspace only: their place in the organisation is
// untouched, because the two are separate and a workspace's owner is not an org's.
func (c *Client) ChangeRole(
	ctx context.Context,
	caller domain.Caller,
	workspaceID, userID, roleID string,
) (domain.Member, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (
			*workspacev1.ChangeMemberRoleResponse, error,
		) {
			return workspacev1.NewWorkspaceMembershipServiceClient(conn).ChangeMemberRole(ctx,
				workspacev1.ChangeMemberRoleRequest_builder{
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

// RemoveMember takes somebody out of a workspace. They stay in the organisation, and their devices stop
// syncing the workspace because they are no longer in it.
func (c *Client) RemoveMember(
	ctx context.Context,
	caller domain.Caller,
	workspaceID, userID string,
) error {
	_, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (
			*workspacev1.RemoveMemberResponse, error,
		) {
			return workspacev1.NewWorkspaceMembershipServiceClient(conn).RemoveMember(ctx,
				workspacev1.RemoveMemberRequest_builder{
					WorkspaceId:  workspaceID,
					TargetUserId: userID,
				}.Build())
		})
	return err
}

// Leave takes the caller out of a workspace. Whether they are its last manager is the server's rule.
func (c *Client) Leave(ctx context.Context, caller domain.Caller, workspaceID string) error {
	_, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (
			*workspacev1.LeaveResponse, error,
		) {
			return workspacev1.NewWorkspaceMembershipServiceClient(conn).Leave(ctx,
				workspacev1.LeaveRequest_builder{
					WorkspaceId: workspaceID,
				}.Build())
		})
	return err
}

// Roles lists an organisation's roles in one scope; the app asks for workspace roles to hand out.
func (c *Client) Roles(
	ctx context.Context,
	caller domain.Caller,
	orgID, scope string,
) ([]domain.Role, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*rolev1.ListRolesResponse, error) {
			return rolev1.NewRoleServiceClient(conn).ListRoles(ctx,
				rolev1.ListRolesRequest_builder{OrgId: orgID, Scope: roleScopeOut(scope)}.Build())
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

// Invites lists an organisation's invitations; the server answers per organisation.
func (c *Client) Invites(
	ctx context.Context,
	caller domain.Caller,
	orgID string,
) ([]domain.Invite, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*invitev1.ListInvitesResponse, error) {
			client := invitev1.NewInviteServiceClient(conn)
			result := invitev1.ListInvitesResponse_builder{}.Build()
			token := ""
			for {
				page, err := client.ListInvites(ctx, invitev1.ListInvitesRequest_builder{OrgId: orgID, PageToken: token}.Build())
				if err != nil {
					return nil, err
				}
				result.SetInvites(append(result.GetInvites(), page.GetInvites()...))

				token = page.GetNextPageToken()
				if token == "" {
					return result, nil
				}
			}
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

// Invite makes a link; the email goes out within the call, so a failed letter is not reported here.
func (c *Client) Invite(
	ctx context.Context,
	caller domain.Caller,
	orgID, workspaceID string,
	asked domain.Invitation,
) (domain.Invite, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*invitev1.CreateInviteResponse, error) {
			return invitev1.NewInviteServiceClient(conn).CreateInvite(ctx,
				invitev1.CreateInviteRequest_builder{
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
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*invitev1.RevokeInviteResponse, error) {
			return invitev1.NewInviteServiceClient(conn).RevokeInvite(ctx,
				invitev1.RevokeInviteRequest_builder{OrgId: orgID, InviteId: inviteID}.Build())
		})
	if err != nil {
		return domain.Invite{}, err
	}
	return inviteOf(answer.GetInvite()), nil
}

func memberOf(one *workspacev1.WorkspaceMember) domain.Member {
	member := one.GetMember()
	person := member.GetPerson()
	return domain.Member{UserID: person.GetUserId(), Email: person.GetEmail(), Name: person.GetName(), RoleID: member.GetRole().GetId(), RoleName: member.GetRole().GetName(), Gone: person.GetDeleted()}
}

func inviteOf(one *invitev1.Invite) domain.Invite {
	held := domain.Invite{
		ID:          one.GetId(),
		WorkspaceID: one.GetWorkspaceId(),
		Email:       one.GetEmail(),
		RoleID:      one.GetRole().GetId(),
		RoleName:    one.GetRole().GetName(),
		Status:      inviteStatusIn(one.GetStatus()),
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

func roleScopeOut(scope string) rolev1.RoleScope {
	if scope == "org" {
		return rolev1.RoleScope_ROLE_SCOPE_ORG
	}
	return rolev1.RoleScope_ROLE_SCOPE_WORKSPACE
}
func inviteStatusIn(status invitev1.InviteStatus) string {
	switch status {
	case invitev1.InviteStatus_INVITE_STATUS_PENDING:
		return "pending"
	case invitev1.InviteStatus_INVITE_STATUS_EXPIRED:
		return "expired"
	case invitev1.InviteStatus_INVITE_STATUS_EXHAUSTED:
		return "exhausted"
	case invitev1.InviteStatus_INVITE_STATUS_REVOKED:
		return "revoked"
	default:
		return ""
	}
}
