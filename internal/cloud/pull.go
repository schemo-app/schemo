package cloud

import (
	"context"
	"log"
	"schemo/internal/domain"
	"time"
)

// Reading what the other devices did. The cursor is the server's own number and never a clock, and
// the page it comes back with is applied whole, in one transaction, with the cursor moved inside
// it: a batch half applied with the cursor already past it would lose whatever came between.
//
// There is no subscription here. A stream is for a device somebody is watching (another device's
// edit arrives within the second rather than the interval) and is the window's business, which is
// why the engine's own round is a pull and nothing more.
//
// The workspace is named on the wire by the id the server knows it by, RemoteID, never by the id it has
// here. They are ids of two different rows, and a call naming the local one asks about a workspace that
// does not exist there: the server answers "not found". Every request in this package is named the
// same way; what this device keeps is named the other.

// maxPages bounds one round's reading. A workspace with a year of history makes a first pull of
// thousands of rows, and a round that never ended would hold the loop for as long as the workspace is
// long. What is left over is read by the next round, from the cursor it has reached.
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
			// The server cannot answer from where this device stands: the workspace was reset there, or
			// this device is so far behind that the changes it missed are gone. Starting over is
			// the only road left, and it is the one a person's own resync takes.
			//
			// Once per round: a server that says the same thing from a cursor of zero is one this
			// device cannot get anywhere with, and a loop that cleared its queue every time round
			// would empty the outbox for nothing.
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
			// The page arrives named by the server's own id and lands in this device's own workspace,
			// so each change is renamed before it is applied: what the store receives is the workspace
			// as this device knows it, the same way the live half does it.
			for at := range answer.Changes {
				answer.Changes[at].WorkspaceID = workspace.ID
			}
			// A row the store could not write is dropped rather than allowed to stop the page: the
			// contract says to skip an entity this build does not understand and keep the cursor
			// moving, and a device that stopped syncing over somebody else's row would stop its own
			// work too. This is the one thing here that is lost quietly, and it is lost on the side
			// that did not make it.
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
