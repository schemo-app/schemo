package cloud

import (
	"context"
	"log"
	"schemo/internal/domain"
	"time"
)

// Reading what other devices did. A page is applied whole with the cursor moved in the same
// transaction, so a half-applied batch cannot skip changes. The wire names a workspace by RemoteID,
// never by the local id.

// maxPages bounds one round; the next round continues from the cursor.
const maxPages = 50

// pull reads what the workspace missed, until there is nothing more or the round has had enough.
func (s *Sync) pull(ctx context.Context, caller domain.Caller, workspace domain.SyncWorkspace) error {
	cursor := workspace.Cursor
	started := false

	for page := 0; page < maxPages; page++ {
		answer, err := s.server.Pull(ctx, caller, workspace.RemoteID, cursor, pullLimit)
		if err != nil {
			return err
		}

		if answer.Resync {
			// The server cannot answer from here (a reset, or changes long gone): start over, as a
			// manual resync does. Only once per round, so a server that refuses cursor zero too does
			// not empty the outbox in a loop.
			if started {
				return domain.Refuse(domain.CodeServerRefused, nil,
					domain.Args{"reason": "resync"})
			}
			started = true
			if err := s.store.Reset(ctx, workspace.ID); err != nil {
				return err
			}
			cursor = 0
			continue
		}

		if len(answer.Changes) > 0 {
			// Map the server's workspace id onto the local one, as the live stream does.
			for at := range answer.Changes {
				answer.Changes[at].WorkspaceID = workspace.ID
			}
			// The protocol says to skip an entity this build cannot store and keep the cursor moving;
			// one foreign row must not stop this device syncing.
			skipped, err := s.store.ApplyPage(ctx, workspace.ID, answer.Changes, answer.Cursor)
			if err != nil {
				return err
			}
			if skipped > 0 {
				log.Printf("[sync] %d changes of %s were not applied", skipped, workspace.ID)
			}
			s.addProgress(0, len(answer.Changes)-skipped)
			// Something another device did is here now, and the windows have something to redraw.
			s.noteContentArrived()
		}

		cursor = answer.Cursor
		if !answer.HasMore {
			break
		}
	}
	return nil
}

const maxCatchUp = 500

func (s *Sync) historyWindow(ctx context.Context) (time.Duration, int, error) {
	if s.retention == nil {
		return 0, domain.HistoryKeepCount, nil
	}
	retention, err := s.retention.Retention(ctx)
	if err != nil {
		return 0, 0, err
	}
	return retention.Age(), domain.HistoryKeepCount, nil
}

// pullHistory reads history changes until it catches up or reaches the page limit.
func (s *Sync) pullHistory(
	ctx context.Context,
	caller domain.Caller,
	workspace domain.SyncWorkspace,
) error {
	if workspace.HistoryCursor == 0 {
		peek, err := s.server.PullRecords(ctx, caller, workspace.RemoteID, 0, 1)
		if err != nil {
			return err
		}
		if peek.Head == 0 {
			return nil
		}
		return s.snapshotHistory(ctx, caller, workspace)
	}

	cursor := workspace.HistoryCursor

	for page := range maxPages {
		answer, err := s.server.PullRecords(ctx, caller, workspace.RemoteID, cursor, pullLimit)
		if err != nil {
			return err
		}
		if page == 0 && answer.HasMore && answer.Head-cursor > maxCatchUp {
			return s.snapshotHistory(ctx, caller, workspace)
		}

		moved := max(cursor, answer.Floor, answer.Cursor)

		applied := len(answer.Changes)
		if len(answer.Changes) > 0 || moved > workspace.HistoryCursor {
			skipped, err := s.store.ApplyHistoryPage(ctx, workspace.ID, answer.Changes, moved)
			if err != nil {
				return err
			}
			if skipped > 0 {
				// A row this build cannot read is skipped so that the cursor keeps moving, as on the
				// content side, and it is lost here: the count is said out loud rather than dropped.
				log.Printf("[sync] %d history changes of %s were not applied", skipped, workspace.ID)
				applied -= skipped
			}
		}
		if applied > 0 {
			s.addProgress(0, applied)
			s.noteHistoryArrived()
		}

		cursor = moved
		if !answer.HasMore {
			break
		}
	}
	return nil
}

// snapshotHistory reads the newest records within the configured history window.
func (s *Sync) snapshotHistory(
	ctx context.Context,
	caller domain.Caller,
	workspace domain.SyncWorkspace,
) error {
	age, keep, err := s.historyWindow(ctx)
	if err != nil {
		return err
	}

	var from int64
	if age > 0 {
		from = s.now().Add(-age).UnixMilli()
	}

	var (
		records []domain.Record
		head    int64
		token   string
	)

	for len(records) < keep {
		list, err := s.server.ListRecords(ctx, caller, workspace.RemoteID, from, 0,
			keep-len(records), token)
		if err != nil {
			return err
		}
		if token == "" {
			head = list.Head
		}

		records = append(records, list.Records...)
		if token = list.Next; token == "" {
			break
		}
	}

	if err := s.store.ApplyHistorySnapshot(ctx, workspace.ID, records, from, head); err != nil {
		return err
	}
	if len(records) > 0 {
		s.noteHistoryArrived()
	}
	return nil
}

// Older reads a page of history older than the given timestamp.
//
//wails:ignore
func (s *Sync) Older(
	ctx context.Context,
	workspaceID string,
	before int64,
	limit int,
) ([]domain.Record, error) {
	workspaces, err := s.store.SyncWorkspaces(ctx)
	if err != nil {
		return nil, err
	}

	var found domain.SyncWorkspace
	for _, workspace := range workspaces {
		if workspace.ID == workspaceID {
			found = workspace
		}
	}

	if !found.Exchangeable() {
		return nil, nil
	}

	caller, err := s.session.Caller(ctx)
	if err != nil {
		return nil, err
	}

	list, err := s.server.ListRecords(ctx, caller, found.RemoteID, 0, before, limit, "")
	if err != nil {
		return nil, err
	}

	for at := range list.Records {
		list.Records[at].WorkspaceID = workspaceID
	}

	return list.Records, nil
}
