// Package channel manages gRPC connection lifecycle, authorization context, and status handling.
package channel

import (
	"context"
	"crypto/tls"
	"sync"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"schemo/internal/domain"
)

// Keepalive parameters for idle connection health checks.
const (
	pingEvery = 30 * time.Second
	pingWait  = 10 * time.Second
)

// maxRecvBytes is what one answer may weigh. gRPC stops reading at 4 MiB, and the heaviest answer
// this client asks for is a record with both of its bodies, each of which the server takes up to
// 8 MiB: read at the default, such a call comes back as a refusal about the call itself.
const maxRecvBytes = 32 << 20

// Dialer manages reusable gRPC client connections keyed by server host address.
type Dialer struct {
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

// New creates an initialized gRPC Dialer instance.
func New() *Dialer {
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
		// A plan is the server's to know, and this is its answer rather than a fault: the window
		// draws the control as one its plan does not carry, and the feature's own name travels here
		// so that it can be named in a sentence.
		return domain.Refuse(domain.CodeFeatureRequired, domain.ErrNotAllowed, named(held))
	case "publishConfirmRequired", "publishVisibilityRefused", "snapshotUnreadable",
		"snapshotVersion", "snapshotTooLarge", "snapshotChanged", "nameTaken",
		"askedTooMany":
		// Publishing is refused by the server for reasons the window has no sentence of its own
		// for, and each of them is about the document rather than about this device: what the
		// server said the refusal was goes along, and the window shows it as the server's answer.
		return domain.Refuse(domain.CodeServerRefused, nil, named(held))
	}
	return domain.Refuse(domain.CodeServerRefused, nil, nil)
}

// named is the values a refusal carries: the server's own arguments, with the reason it gave kept
// among them. The reason is what a window names when it has nothing better to say, and the rest are
// whatever the refusal was measured against.
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
