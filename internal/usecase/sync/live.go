package sync

import (
	"context"
	"time"

	"schemo/internal/domain"
)

const reconnect = 5 * time.Second

type stream struct {
	cancel context.CancelFunc
	spaces map[string]struct{}
}

// watch updates the live stream to follow the current set of exchangeable spaces.
func (u *UseCase) watch(ctx context.Context, spaces []domain.SyncSpace) {
	wanted := map[string]struct{}{}
	for _, space := range spaces {
		if space.Exchangeable() {
			wanted[space.ID] = struct{}{}
		}
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	if u.stream != nil && sameSet(u.stream.spaces, wanted) {
		return
	}
	if u.stream != nil {
		u.stream.cancel()
		u.stream = nil
	}
	if len(wanted) == 0 {
		return
	}

	streamCtx, cancel := context.WithCancel(ctx)
	held := &stream{cancel: cancel, spaces: wanted}
	u.stream = held
	go u.keepLive(streamCtx, held)
}

func sameSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if _, ok := b[id]; !ok {
			return false
		}
	}
	return true
}

// keepLive maintains the live stream and reopens it when it ends.
func (u *UseCase) keepLive(ctx context.Context, held *stream) {
	defer u.unwatch(held)

	wait := u.till
	for {
		caller, err := u.session.Caller(ctx)
		if err != nil {
			return
		}

		spaces, err := u.followed(ctx)
		if err != nil || len(spaces) == 0 {
			return
		}

		opened := u.now()
		err = u.live(ctx, caller, spaces)
		lived := u.now().Sub(opened)
		u.finish(err)
		if ctx.Err() != nil {
			return
		}
		if domain.CodeOf(err) == domain.CodeNotSignedIn {
			return
		}
		if err == nil {
			u.Wake()
		}

		wait = reopenWait(wait, u.till, lived, err)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// healthyStream is how long a stream must hold before its end stops counting as a failure.
const healthyStream = 30 * time.Second

// reopenWait doubles while connections keep failing and starts over after a healthy one.
func reopenWait(previous, base, lived time.Duration, err error) time.Duration {
	if err == nil || lived >= healthyStream {
		return base
	}
	return min(2*previous, maxBackoff)
}

// live follows the given spaces until the stream ends.
func (u *UseCase) live(ctx context.Context, caller domain.Caller, spaces []domain.SyncSpace) error {
	cursors := make([]domain.SpaceCursor, 0, len(spaces))
	local := make(map[string]string, len(spaces))

	for _, space := range spaces {
		cursors = append(cursors, domain.SpaceCursor{
			SpaceID: space.RemoteID,
			Content: space.Cursor,
			History: space.HistoryCursor,
		})
		local[space.RemoteID] = space.ID
	}

	return u.server.Connect(ctx, caller, cursors, func(signal domain.Signal) error {
		if signal.SpacesChanged {
			u.Wake()
			return nil
		}
		if id, ok := local[signal.SpaceID]; ok && u.behind(ctx, id, signal.Poke) {
			u.WakeSpace(id)
		}
		return nil
	})
}

// behind reports whether a poke is beyond the local cursors.
func (u *UseCase) behind(ctx context.Context, spaceID string, poke domain.Poke) bool {
	spaces, err := u.followed(ctx)
	if err != nil {
		return true
	}

	for _, space := range spaces {
		if space.ID == spaceID {
			return poke.Content > space.Cursor || poke.History > space.HistoryCursor
		}
	}
	return false
}

// followed returns exchangeable spaces that still belong to the account.
func (u *UseCase) followed(ctx context.Context) ([]domain.SyncSpace, error) {
	spaces, err := u.store.Spaces(ctx)
	if err != nil {
		return nil, err
	}

	u.mu.Lock()
	known, seen, mine := u.known, u.seen, u.mine
	u.mu.Unlock()

	out := make([]domain.SyncSpace, 0, len(spaces))
	for _, space := range spaces {
		if space.Exchangeable() &&
			!notInAccount(known, seen, mine, space) &&
			!foreign(space, mine) {
			out = append(out, space)
		}
	}
	return out, nil
}

// unwatch clears the current stream if it is still the active one.
func (u *UseCase) unwatch(held *stream) {
	u.mu.Lock()
	if u.stream == held {
		u.stream = nil
	}
	u.mu.Unlock()
}
