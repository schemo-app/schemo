package sync

import (
	"context"
	"log"
	"time"

	"schemo/internal/domain"
)

const maxCatchUp = 500

func (u *UseCase) historyWindow(ctx context.Context) (time.Duration, int, error) {
	if u.retention == nil {
		return 0, domain.HistoryKeepCount, nil
	}
	retention, err := u.retention.Retention(ctx)
	if err != nil {
		return 0, 0, err
	}
	return retention.Age(), domain.HistoryKeepCount, nil
}

// pullHistory reads history changes until it catches up or reaches the page limit.
func (u *UseCase) pullHistory(
	ctx context.Context,
	caller domain.Caller,
	space domain.SyncSpace,
) error {
	if space.HistoryCursor == 0 {
		peek, err := u.server.PullRecords(ctx, caller, space.RemoteID, 0, 1)
		if err != nil {
			return err
		}
		if peek.Head == 0 {
			return nil
		}
		return u.snapshotHistory(ctx, caller, space)
	}

	cursor := space.HistoryCursor

	for page := range maxPages {
		answer, err := u.server.PullRecords(ctx, caller, space.RemoteID, cursor, pullLimit)
		if err != nil {
			return err
		}
		if page == 0 && answer.HasMore && answer.Head-cursor > maxCatchUp {
			return u.snapshotHistory(ctx, caller, space)
		}

		moved := max(cursor, answer.Floor, answer.Cursor)

		applied := len(answer.Changes)
		if len(answer.Changes) > 0 || moved > space.HistoryCursor {
			skipped, err := u.store.ApplyHistoryPage(ctx, space.ID, answer.Changes, moved)
			if err != nil {
				return err
			}
			if skipped > 0 {
				// A row this build cannot read is skipped so that the cursor keeps moving, as on the
				// content side, and it is lost here: the count is said out loud rather than dropped.
				log.Printf("[sync] %d history changes of %s were not applied", skipped, space.ID)
				applied -= skipped
			}
		}
		if applied > 0 {
			u.exchanged(0, applied)
			u.historyArrived()
		}

		cursor = moved
		if !answer.HasMore {
			break
		}
	}
	return nil
}

// snapshotHistory reads the newest records within the configured history window.
func (u *UseCase) snapshotHistory(
	ctx context.Context,
	caller domain.Caller,
	space domain.SyncSpace,
) error {
	age, keep, err := u.historyWindow(ctx)
	if err != nil {
		return err
	}

	var from int64
	if age > 0 {
		from = u.now().Add(-age).UnixMilli()
	}

	var (
		records []domain.Record
		head    int64
		token   string
	)

	for len(records) < keep {
		list, err := u.server.ListRecords(ctx, caller, space.RemoteID, from, 0,
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

	if err := u.store.ApplyHistorySnapshot(ctx, space.ID, records, from, head); err != nil {
		return err
	}
	if len(records) > 0 {
		u.historyArrived()
	}
	return nil
}

// Older reads a page of history older than the given timestamp.
func (u *UseCase) Older(
	ctx context.Context,
	workspaceID string,
	before int64,
	limit int,
) ([]domain.Record, error) {
	spaces, err := u.store.Spaces(ctx)
	if err != nil {
		return nil, err
	}

	var found domain.SyncSpace
	for _, space := range spaces {
		if space.ID == workspaceID {
			found = space
		}
	}

	if !found.Exchangeable() {
		return nil, nil
	}

	caller, err := u.session.Caller(ctx)
	if err != nil {
		return nil, err
	}

	list, err := u.server.ListRecords(ctx, caller, found.RemoteID, 0, before, limit, "")
	if err != nil {
		return nil, err
	}

	for at := range list.Records {
		list.Records[at].WorkspaceID = workspaceID
	}

	return list.Records, nil
}
