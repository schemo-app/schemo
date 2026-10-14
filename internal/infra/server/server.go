package server

import (
	"context"
	"slices"

	"google.golang.org/grpc"

	"github.com/schemo-app/proto/go/platform/v1"
	"github.com/schemo-app/proto/go/workspace/v1"

	"schemo/internal/domain"
)

// The two questions the engine asks before it exchanges anything: which spaces this account has,
// and what this server takes.
//
// Neither is the sync service, and both belong here: they are about the same server and the same
// account, and a device that asked them of a different client would hold two tokens to answer one
// question.

// Spaces is the spaces of the account, and the organisation the caller owns among them. An empty
// org is the caller's own spaces, which is what a device syncs: a space it has not joined is not
// one it can sync.
//
// Which of them stand in the person's own organisation is read here and not left to the engine: the
// server answers one id per call, and every space of it is theirs. A space of anybody else's — one
// they were invited into — is one this device reads and never one it fills.
func (c *Client) Spaces(ctx context.Context, caller domain.Caller) (domain.AccountSpaces, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*workspace.ListResponse, error) {
			return workspace.NewWorkspaceServiceClient(conn).List(ctx,
				workspace.ListRequest_builder{}.Build())
		})
	if err != nil {
		return domain.AccountSpaces{}, err
	}

	own := answer.GetOwnOrgId()
	out := domain.AccountSpaces{
		Spaces:   make([]domain.RemoteSpace, 0, len(answer.GetWorkspaces())),
		OwnOrgID: own,
	}
	for _, space := range answer.GetWorkspaces() {
		out.Spaces = append(out.Spaces, domain.RemoteSpace{
			ID:      space.GetId(),
			Name:    space.GetName(),
			Colour:  space.GetColour(),
			Version: int32(space.GetVersion()),
			OwnOrg:  own != "" && space.GetOrgId() == own,
			OrgID:   space.GetOrgId(),
			Kind:    kindOf(space.GetKind()),
			Access:  accessOf(space),
		})
	}
	return out, nil
}

// permWrite is the account's permission to put anything into a space, the word a push of content
// is judged by. It is the server's vocabulary, so it is read here and nowhere else.
const permWrite = "workspace:write"

// accessOf reads what the account says this person may do in a space. A row that carries no
// permission words is one this build cannot read, since every member has a role and every role has
// permissions: it is read as the person's own, and the server judges every change either way.
func accessOf(space *workspace.Workspace) domain.WorkspaceAccess {
	permissions := space.GetPermissions()
	if len(permissions) == 0 {
		return domain.WorkspaceAccess{
			RoleName: space.GetRoleName(), Update: true, Delete: true, Write: true,
		}
	}
	can := space.GetCan()
	return domain.WorkspaceAccess{
		RoleName: space.GetRoleName(),
		Update:   can.GetUpdate(),
		Delete:   can.GetDelete(),
		Write:    slices.Contains(permissions, permWrite),
	}
}

// Create makes a space in the account for one this device holds, and answers it as the server
// describes it.
//
// The organisation is named, and it is the one the person owns. A device that let the server choose
// would have its space made wherever that person was last working — a team's organisation, where
// their own month of work does not belong — and the panel's spaces and a device's would then be
// made in two different ways for no reason.
//
// What the space is for travels with it, and it is the space's own kind rather than a decision made
// here: it is what the person chose when they made it, and the server is the one that holds it.
//
// The id travels with it too, and it is this device's. It is what makes the call safe to repeat: an
// answer that never arrived leaves a space the account has and this device cannot name, and asking
// again under the same id is answered with that space rather than a second one.
func (c *Client) Create(
	ctx context.Context,
	caller domain.Caller,
	orgID, id, name, colour string,
	kind domain.WorkspaceKind,
) (domain.RemoteSpace, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*workspace.CreateResponse, error) {
			return workspace.NewWorkspaceServiceClient(conn).Create(ctx,
				workspace.CreateRequest_builder{
					OrgId:  orgID,
					Id:     id,
					Name:   name,
					Colour: colour,
					Kind:   string(kind),
				}.Build())
		})
	if err != nil {
		return domain.RemoteSpace{}, err
	}

	held := answer.GetWorkspace()
	return domain.RemoteSpace{
		ID:      held.GetId(),
		Name:    held.GetName(),
		Colour:  held.GetColour(),
		Kind:    kindOf(held.GetKind()),
		OrgID:   held.GetOrgId(),
		Version: int32(held.GetVersion()),
		OwnOrg:  true,
		Access:  accessOf(held),
	}, nil
}

// DeleteSpace ends a space in the account. The server keeps it a month in the trash and lets the
// deletion travel, which tells every device that holds the space — including one that was away when
// it happened — rather than leaving each of them to find out on its own.
//
// No name travels with it: the sheet that asked here is where the act was made deliberate, and the
// name this device draws is one a rename in flight would have made the account refuse.
func (c *Client) DeleteSpace(
	ctx context.Context,
	caller domain.Caller,
	workspaceID string,
) error {
	_, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*workspace.DeleteResponse, error) {
			return workspace.NewWorkspaceServiceClient(conn).Delete(ctx,
				workspace.DeleteRequest_builder{WorkspaceId: workspaceID}.Build())
		})
	return err
}

// SetKind changes what a space in the account is for. What counts is the server's answer: whether a
// space may become somebody's own is a question about who is in it, and only the server knows that.
// A refusal — `workspacePersonal` for a space a second person works in — travels back as it is and
// is the caller's to word.
func (c *Client) SetKind(
	ctx context.Context,
	caller domain.Caller,
	workspaceID string,
	kind domain.WorkspaceKind,
) error {
	wanted := string(kind)
	_, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*workspace.UpdateResponse, error) {
			return workspace.NewWorkspaceServiceClient(conn).Update(ctx,
				workspace.UpdateRequest_builder{
					WorkspaceId: workspaceID,
					Kind:        &wanted,
				}.Build())
		})
	return err
}

// Limits is what this server takes: the batch it accepts and the body it stores. A device that
// guessed would send a batch it has to split, or a body that comes back refused.
func (c *Client) Limits(ctx context.Context, caller domain.Caller) (domain.Limits, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*platform.GetInfoResponse, error) {
			return platform.NewPlatformServiceClient(conn).GetInfo(ctx,
				platform.GetInfoRequest_builder{}.Build())
		})
	if err != nil {
		return domain.Limits{}, err
	}

	limits := answer.GetServer().GetLimits()
	return domain.Limits{
		MaxBodyBytes:  limits.GetMaxBodyBytes(),
		MaxPushBytes:  limits.GetMaxPushBytes(),
		RetentionDays: limits.GetHistoryRetentionDays(),
	}, nil
}

// kindOf reads what a space is for from the word the server sends. A word this build does not
// know is read as a team's, which is the careful direction: a personal space may be paired with a
// device's own one and a team's never may, so a word nobody understands is one nothing offers
// work to.
func kindOf(kind string) domain.WorkspaceKind {
	if domain.WorkspaceKind(kind) == domain.WorkspacePersonal {
		return domain.WorkspacePersonal
	}
	return domain.WorkspaceTeam
}
