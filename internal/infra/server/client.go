// Package server provides gRPC transport wrapper methods for engine synchronization RPCs.
package server

import (
	"context"
	"time"

	"google.golang.org/grpc"

	"schemo/internal/domain"
	"schemo/internal/infra/channel"
)

// Session notifies account management when an authentication session is invalidated by server.
type Session interface {
	Refused(ctx context.Context, sessionID string)
}

// Client executes server RPC requests with automatic context timeouts and error conversion.
type Client struct {
	dialer  *channel.Dialer
	session Session
}

// New creates an initialized server Client instance.
func New(dialer *channel.Dialer, session Session) *Client {
	return &Client{dialer: dialer, session: session}
}

// callTimeout defines the maximum duration allowed for a single RPC execution (2 minutes).
const callTimeout = 2 * time.Minute

// call wraps gRPC invocation with connection acquisition, timeout context, and error handling.
func call[T any](
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

	answer, err := do(channel.Authorized(timed, caller.Access), conn)
	if err != nil {
		return nothing, c.refusal(ctx, caller, err)
	}
	return answer, nil
}

// refusal processes gRPC errors into domain errors and triggers session refusal callbacks.
func (c *Client) refusal(ctx context.Context, caller domain.Caller, err error) error {
	held := channel.FromStatus(err)
	if domain.CodeOf(held) == domain.CodeNotSignedIn && c.session != nil {
		c.session.Refused(ctx, caller.SessionID)
	}
	return held
}
