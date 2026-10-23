package cloud

import (
	"context"
	"maps"
	"time"

	"schemo/internal/domain"
)

const reconnect = 5 * time.Second

type stream struct {
	cancel     context.CancelFunc
	workspaces map[string]struct{}
}

// watch updates the live stream to follow the current set of exchangeable workspaces.
func (s *Sync) watch(ctx context.Context, workspaces []domain.SyncWorkspace) {
	wanted := map[string]struct{}{}
	for _, workspace := range workspaces {
		if workspace.Exchangeable() {
			wanted[workspace.ID] = struct{}{}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stream != nil && maps.Equal(s.stream.workspaces, wanted) {
		return
	}
	if s.stream != nil {
		s.stream.cancel()
		s.stream = nil
	}
	if len(wanted) == 0 {
		return
	}

	streamCtx, cancel := context.WithCancel(ctx)
	held := &stream{cancel: cancel, workspaces: wanted}
	s.stream = held
	go s.keepLive(streamCtx, held)
}

// keepLive maintains the live stream and reopens it when it ends.
func (s *Sync) keepLive(ctx context.Context, held *stream) {
	defer s.unwatch(held)

	wait := s.till
	for {
		caller, err := s.session.Caller(ctx)
		if err != nil {
			return
		}

		workspaces, err := s.followedWorkspaces(ctx)
		if err != nil || len(workspaces) == 0 {
			return
		}

		opened := s.now()
		err = s.followWorkspaces(ctx, caller, workspaces)
		lived := s.now().Sub(opened)
		s.finishRound(err)
		if ctx.Err() != nil {
			return
		}
		if domain.CodeOf(err) == domain.CodeNotSignedIn {
			return
		}
		if err == nil {
			s.Wake()
		}

		wait = reopenDelay(wait, s.till, lived, err)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// healthyStream is how long a stream must hold before its end stops counting as a failure.
const healthyStream = 30 * time.Second

// reopenDelay doubles while connections keep failing and starts over after a healthy one.
func reopenDelay(previous, base, lived time.Duration, err error) time.Duration {
	if err == nil || lived >= healthyStream {
		return base
	}
	return min(2*previous, maxBackoff)
}

// followWorkspaces follows the given workspaces until the stream ends.
func (s *Sync) followWorkspaces(ctx context.Context, caller domain.Caller, workspaces []domain.SyncWorkspace) error {
	cursors := make([]domain.WorkspaceCursor, 0, len(workspaces))
	local := make(map[string]string, len(workspaces))

	for _, workspace := range workspaces {
		cursors = append(cursors, domain.WorkspaceCursor{
			WorkspaceID: workspace.RemoteID,
			Content:     workspace.Cursor,
			History:     workspace.HistoryCursor,
		})
		local[workspace.RemoteID] = workspace.ID
	}

	return s.server.Connect(ctx, caller, cursors, func(signal domain.Signal) error {
		if signal.WorkspacesChanged {
			s.Wake()
			return nil
		}
		if id, ok := local[signal.WorkspaceID]; ok && s.isPokeAhead(ctx, id, signal.Poke) {
			s.WakeWorkspace(id)
		}
		return nil
	})
}

// isPokeAhead reports whether a poke is beyond the local cursors.
func (s *Sync) isPokeAhead(ctx context.Context, workspaceID string, poke domain.Poke) bool {
	workspaces, err := s.followedWorkspaces(ctx)
	if err != nil {
		return true
	}

	for _, workspace := range workspaces {
		if workspace.ID == workspaceID {
			return poke.Content > workspace.Cursor || poke.History > workspace.HistoryCursor
		}
	}
	return false
}

// followedWorkspaces returns exchangeable workspaces that still belong to the account.
func (s *Sync) followedWorkspaces(ctx context.Context) ([]domain.SyncWorkspace, error) {
	workspaces, err := s.store.SyncWorkspaces(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	known, seen, mine := s.known, s.seen, s.mine
	s.mu.Unlock()

	out := make([]domain.SyncWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.Exchangeable() &&
			!isMissingFromAccount(known, seen, mine, workspace) &&
			!isForeign(workspace, mine) {
			out = append(out, workspace)
		}
	}
	return out, nil
}

// unwatch clears the current stream if it is still the active one.
func (s *Sync) unwatch(held *stream) {
	s.mu.Lock()
	if s.stream == held {
		s.stream = nil
	}
	s.mu.Unlock()
}
