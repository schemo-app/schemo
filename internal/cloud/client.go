// Package cloud is everything that talks to the Schemo server: sign-in and the account, the plan,
// workspace members, publications, sync, the gRPC client and the OS keyring.
package cloud

import (
	"context"
	"crypto/tls"
	"io"
	"net/url"
	"schemo/internal/domain"
	"slices"
	"sync"
	"time"

	platformv1 "github.com/schemo-app/proto/go/platform/v1"
	syncv1 "github.com/schemo-app/proto/go/sync/v1"
	workspacev1 "github.com/schemo-app/proto/go/workspace/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// The two questions the engine asks before it exchanges anything: which workspaces this account has,
// and what this server takes.
//
// Neither is the sync service, but both belong here: they are about the same server and account,
// and another client would need a second token to answer one question.

// Workspaces is the workspaces of the account, and the organisation the caller owns among them. An empty
// org is the caller's own workspaces, which is what a device syncs: a workspace it has not joined is not
// one it can sync.
//
// Which workspaces stand in the person's own organisation is read here, not left to the engine: the
// server answers one id per call and every workspace of it is theirs. A workspace of anybody else's, one
// they were invited into, is read but never filled.
func (c *Client) Workspaces(ctx context.Context, caller domain.Caller) (domain.AccountWorkspaces, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*workspacev1.ListResponse, error) {
			client := workspacev1.NewWorkspaceServiceClient(conn)
			result := workspacev1.ListResponse_builder{}.Build()
			token := ""
			for {
				page, err := client.List(ctx, workspacev1.ListRequest_builder{PageToken: token}.Build())
				if err != nil {
					return nil, err
				}
				result.SetWorkspaces(append(result.GetWorkspaces(), page.GetWorkspaces()...))
				result.SetOwnOrgId(page.GetOwnOrgId())
				token = page.GetNextPageToken()
				if token == "" {
					return result, nil
				}
			}
		})
	if err != nil {
		return domain.AccountWorkspaces{}, err
	}

	own := answer.GetOwnOrgId()
	out := domain.AccountWorkspaces{
		Workspaces: make([]domain.RemoteWorkspace, 0, len(answer.GetWorkspaces())),
		OwnOrgID:   own,
	}
	for _, workspace := range answer.GetWorkspaces() {
		out.Workspaces = append(out.Workspaces, domain.RemoteWorkspace{
			ID:      workspace.GetId(),
			Name:    workspace.GetName(),
			Colour:  workspace.GetColor(),
			Version: int32(workspace.GetVersion()),
			OwnOrg:  own != "" && workspace.GetOrgId() == own,
			OrgID:   workspace.GetOrgId(),
			Kind:    kindOf(workspace.GetKind()),
			Access:  accessOf(workspace),
		})
	}
	return out, nil
}

// permWrite is the account's permission to put anything into a workspace, the word a push of content
// is judged by. It is the server's vocabulary, so it is read here and nowhere else.
const permWrite = "workspace:write"

// accessOf reads the permissions and actions granted by the server.
func accessOf(workspace *workspacev1.Workspace) domain.WorkspaceAccess {
	permissions := workspace.GetPermissions()
	can := workspace.GetCan()
	return domain.WorkspaceAccess{
		RoleName: workspace.GetRole().GetName(),
		Update:   can.GetUpdate(),
		Delete:   can.GetDelete(),
		Write:    slices.Contains(permissions, permWrite),
	}
}

// Create makes a workspace in the account for one this device holds, and returns it as the server
// describes it.
//
// The organisation named is the one the person owns. Letting the server choose would put the workspace
// wherever the person last worked, such as a team's organisation, and the panel and devices would
// create workspaces differently.
//
// What the workspace is for travels with it: it is the kind the person chose, held by the server. The
// id is this device's and makes the call safe to repeat: if an answer never arrived, asking again
// under the same id returns that workspace instead of a second one.
func (c *Client) Create(
	ctx context.Context,
	caller domain.Caller,
	orgID, id, name, colour string,
	kind domain.WorkspaceKind,
) (domain.RemoteWorkspace, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*workspacev1.CreateResponse, error) {
			return workspacev1.NewWorkspaceServiceClient(conn).Create(ctx,
				workspacev1.CreateRequest_builder{
					OrgId: orgID,
					Id:    id,
					Name:  name,
					Color: colour,
					Kind:  workspaceKindOut(kind),
				}.Build())
		})
	if err != nil {
		return domain.RemoteWorkspace{}, err
	}

	held := answer.GetWorkspace()
	return domain.RemoteWorkspace{
		ID:      held.GetId(),
		Name:    held.GetName(),
		Colour:  held.GetColor(),
		Kind:    kindOf(held.GetKind()),
		OrgID:   held.GetOrgId(),
		Version: int32(held.GetVersion()),
		OwnOrg:  true,
		Access:  accessOf(held),
	}, nil
}

// DeleteWorkspace ends a workspace in the account. The server keeps it a month in the trash and lets the
// deletion travel, so every device holding the workspace learns of it, including one that was away.
//
// No name travels with it: the sheet that asked is where the act was made deliberate, and the name
// this device draws could be one a rename in flight would make the account refuse.
func (c *Client) DeleteWorkspace(
	ctx context.Context,
	caller domain.Caller,
	workspaceID string,
) error {
	_, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*workspacev1.DeleteResponse, error) {
			return workspacev1.NewWorkspaceServiceClient(conn).Delete(ctx,
				workspacev1.DeleteRequest_builder{WorkspaceId: workspaceID}.Build())
		})
	return err
}

// SetKind changes what a workspace in the account is for. The server's answer decides: whether a workspace
// may become somebody's own depends on who is in it. A refusal such as `workspacePersonal` for a
// workspace a second person works in travels back as it is and is the caller's to word.
func (c *Client) SetKind(
	ctx context.Context,
	caller domain.Caller,
	workspaceID string,
	kind domain.WorkspaceKind,
) error {
	wanted := workspaceKindOut(kind)
	_, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*workspacev1.UpdateResponse, error) {
			return workspacev1.NewWorkspaceServiceClient(conn).Update(ctx,
				workspacev1.UpdateRequest_builder{
					WorkspaceId: workspaceID,
					Kind:        &wanted,
				}.Build())
		})
	return err
}

// Limits is what this server takes: the batch it accepts and the body it stores. A device that
// guessed would send a batch it has to split, or a body that comes back refused.
func (c *Client) Limits(ctx context.Context, caller domain.Caller) (domain.Limits, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*platformv1.GetInfoResponse, error) {
			return platformv1.NewPlatformServiceClient(conn).GetInfo(ctx,
				platformv1.GetInfoRequest_builder{}.Build())
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

// kindOf reads what a workspace is for from the word the server sends. An unknown word is read as a
// team's, the careful direction: a personal workspace may be paired with a device's own one and a
// team's never, so nothing offers work to a word nobody understands.
func kindOf(kind workspacev1.WorkspaceKind) domain.WorkspaceKind {
	if kind == workspacev1.WorkspaceKind_WORKSPACE_KIND_PERSONAL {
		return domain.WorkspacePersonal
	}
	return domain.WorkspaceTeam
}

func workspaceKindOut(kind domain.WorkspaceKind) workspacev1.WorkspaceKind {
	if kind == domain.WorkspacePersonal {
		return workspacev1.WorkspaceKind_WORKSPACE_KIND_PERSONAL
	}
	return workspacev1.WorkspaceKind_WORKSPACE_KIND_TEAM
}

// Refuser notifies account management when an authentication session is invalidated by server.
type Refuser interface {
	Refused(ctx context.Context, sessionID string)
}

// Client executes server RPC requests with automatic context timeouts and error conversion.
type Client struct {
	dialer  *Dialer
	session Refuser
}

// NewClient creates an initialized server Client instance.
func NewClient(dialer *Dialer, session Refuser) *Client {
	return &Client{dialer: dialer, session: session}
}

// callTimeout defines the maximum duration allowed for a single RPC execution (2 minutes).
const callTimeout = 2 * time.Minute

// callServer wraps gRPC invocation with connection acquisition, timeout context, and error handling.
func callServer[T any](
	c *Client,
	ctx context.Context,
	caller domain.Caller,
	do func(context.Context, *grpc.ClientConn) (T, error),
) (T, error) {
	var nothing T

	conn, err := c.dialer.Client(caller.Address)
	if err != nil {
		return nothing, err
	}

	timed, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	answer, err := do(Authorized(timed, caller.Access), conn)
	if err != nil {
		return nothing, c.refusal(ctx, caller, err)
	}
	return answer, nil
}

// refusal processes gRPC errors into domain errors and triggers session refusal callbacks.
func (c *Client) refusal(ctx context.Context, caller domain.Caller, err error) error {
	held := FromStatus(err)
	if domain.CodeOf(held) == domain.CodeNotSignedIn && c.session != nil {
		c.session.Refused(ctx, caller.SessionID)
	}
	return held
}

// Connect opens a stream to receive real-time sync signals for the provided workspace cursors.
// It blocks until the context cancels, the stream closes, or the callback returns an error.
func (c *Client) Connect(
	ctx context.Context,
	caller domain.Caller,
	cursors []domain.WorkspaceCursor,
	on func(domain.Signal) error,
) error {
	conn, err := c.dialer.Client(caller.Address)
	if err != nil {
		return err
	}

	held := make([]*syncv1.WorkspaceCursor, 0, len(cursors))
	for _, one := range cursors {
		held = append(held, syncv1.WorkspaceCursor_builder{
			WorkspaceId: one.WorkspaceID, SyncSeq: one.Content, HistorySeq: one.History,
		}.Build())
	}
	stream, err := syncv1.NewSyncServiceClient(conn).Connect(
		Authorized(ctx, caller.Access),
		syncv1.ConnectRequest_builder{Workspaces: held}.Build())
	if err != nil {
		return c.refusal(ctx, caller, err)
	}

	for {
		answer, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return c.refusal(ctx, caller, err)
		}

		// Ignore heartbeat messages used to keep the idle connection alive.
		var signal domain.Signal
		switch {
		case answer.GetPoke() != nil:
			poke := answer.GetPoke()
			signal.Poke = domain.Poke{
				WorkspaceID: poke.GetWorkspaceId(), Content: poke.GetSyncSeq(),
				History: poke.GetHistorySeq(),
			}
		case answer.GetWorkspacesChanged() != nil:
			signal.WorkspacesChanged = true
		default:
			continue
		}
		if err := on(signal); err != nil {
			return err
		}
	}
}

// Keepalive parameters for idle connection health checks.
const (
	pingEvery = 30 * time.Second
	pingWait  = 10 * time.Second
)

// maxRecvBytes is what one answer may weigh. gRPC stops reading at 4 MiB, but the heaviest answer
// asked for is a record with both bodies, each up to 8 MiB on the server; at the default such a
// call fails with a refusal about the call itself.
const maxRecvBytes = 32 << 20

// Dialer manages reusable gRPC client connections keyed by server host address.
type Dialer struct {
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

// NewDialer creates an initialized gRPC Dialer instance.
func NewDialer() *Dialer {
	return &Dialer{conns: map[string]*grpc.ClientConn{}}
}

// Client returns an existing gRPC client connection for address or initializes a new one.
func (d *Dialer) Client(address string) (*grpc.ClientConn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if conn, ok := d.conns[address]; ok {
		return conn, nil
	}

	parsed, err := URL(address)
	if err != nil {
		return nil, err
	}

	options := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time: pingEvery, Timeout: pingWait, PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.UseCompressor(gzip.Name),
			grpc.MaxCallRecvMsgSize(maxRecvBytes),
		),
	}
	if parsed.Scheme == "https" {
		options = append(options, grpc.WithTransportCredentials(
			credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})))
	} else {
		options = append(options, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(parsed.Host, options...)
	if err != nil {
		return nil, domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	d.conns[address] = conn
	return conn, nil
}

// Close closes all cached gRPC connections and resets internal connection map.
func (d *Dialer) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for address, conn := range d.conns {
		_ = conn.Close()
		delete(d.conns, address)
	}
}

// Authorized attaches a Bearer authorization token to outgoing gRPC metadata context.
func Authorized(ctx context.Context, access string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+access)
}

// FromStatus converts gRPC status errors into domain application refusal errors.
func FromStatus(err error) error {
	if err == nil {
		return nil
	}
	if domain.CodeOf(err) != "" {
		return err
	}

	held := status.Convert(err)
	switch held.Code() {
	case codes.OK:
		return nil
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	}

	switch reason(held) {
	case "unauthorized", "sessionGone":
		return domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
	case "notFound":
		return domain.Refuse(domain.CodeNotFound, domain.ErrNotFound, nil)
	case "workspacePersonal":
		return domain.Refuse(domain.CodeWorkspacePersonal, domain.ErrNotAllowed, nil)
	case "workspaceUnknown", "notAMember":
		return domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	case "memberExists":
		return domain.Refuse(domain.CodeMemberExists, domain.ErrConflict, nil)
	case "deviceDenied", "deviceExpired":
		return domain.Refuse(domain.CodeSignInFailed, domain.ErrNotAllowed, nil)
	case "pushTooLarge":
		return domain.Refuse(domain.CodeServerRefused, nil,
			domain.Args{"reason": domain.ArgTooLarge})
	case "featureRequired":
		// A plan is the server's to know and this is its answer, not a fault: the window draws the
		// control as one the plan does not carry, and the feature's name travels so it can be named
		// in a sentence.
		return domain.Refuse(domain.CodeFeatureRequired, domain.ErrNotAllowed, named(held))
	case "publishConfirmRequired", "publishVisibilityRefused", "snapshotUnreadable",
		"snapshotVersion", "snapshotTooLarge", "snapshotChanged", "nameTaken",
		"askedTooMany":
		// The server refuses publishing for reasons the window has no sentence for, each about the
		// document rather than this device: the server's reason goes along and the window shows it
		// as the server's answer.
		return domain.Refuse(domain.CodeServerRefused, nil, named(held))
	}
	return domain.Refuse(domain.CodeServerRefused, nil, nil)
}

// named is the values a refusal carries: the server's own arguments with the reason it gave kept
// among them. A window names the reason when it has nothing better; the rest are whatever the
// refusal was measured against.
func named(held *status.Status) domain.Args {
	for _, detail := range held.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok {
			continue
		}
		args := domain.Args{}
		for name, value := range info.GetMetadata() {
			args[name] = value
		}
		args["reason"] = info.GetReason()
		return args
	}
	return nil
}

// reason extracts error detail reason string from gRPC ErrorInfo if present.
func reason(held *status.Status) string {
	for _, detail := range held.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}
	return ""
}

// URL accepts verified HTTPS or explicit HTTP loopback connections.
func URL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && domain.LoopbackHost(u.Hostname())) {
		return nil, domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	return u, nil
}

// Endpoint prevents remote providers and redirects from downgrading to local HTTP.
func Endpoint(origin, destination string) error {
	from, err := URL(origin)
	if err != nil {
		return err
	}
	to, err := URL(destination)
	if err != nil {
		return err
	}
	if to.Scheme == "http" && (from.Scheme == "https" || !domain.LoopbackHost(from.Hostname())) {
		return domain.Refuse(domain.CodeServerRefused, nil, nil)
	}
	return nil
}
