package server

import (
	"context"
	"io"

	"github.com/schemo-app/proto/go/sync/v1"

	"schemo/internal/domain"
	"schemo/internal/infra/channel"
)

// Connect opens a stream to receive real-time sync signals for the provided workspace cursors.
// It blocks until the context cancels, the stream closes, or the callback returns an error.
func (c *Client) Connect(
	ctx context.Context,
	caller domain.Caller,
	cursors []domain.SpaceCursor,
	on func(domain.Signal) error,
) error {
	conn, err := c.dialer.Client(caller.Address)
	if err != nil {
		return err
	}

	held := make([]*sync.SpaceCursor, 0, len(cursors))
	for _, one := range cursors {
		held = append(held, sync.SpaceCursor_builder{
			WorkspaceId: one.SpaceID, SyncSeq: one.Content, HistorySeq: one.History,
		}.Build())
	}
	stream, err := sync.NewSyncServiceClient(conn).Connect(
		channel.Authorized(ctx, caller.Access),
		sync.ConnectRequest_builder{Spaces: held}.Build())
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
				SpaceID: poke.GetWorkspaceId(), Content: poke.GetSyncSeq(),
				History: poke.GetHistorySeq(),
			}
		case answer.GetSpacesChanged() != nil:
			signal.SpacesChanged = true
		default:
			continue
		}
		if err := on(signal); err != nil {
			return err
		}
	}
}
