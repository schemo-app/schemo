package record

import (
	"context"
	"log"
	"time"

	"schemo/internal/domain"
)

const (
	// cacheAge is the maximum retention duration for cached remote records.
	cacheAge = 14 * 24 * time.Hour
	// cacheCount limits the maximum number of cached remote records preserved.
	cacheCount = 1000
)

// pruneEvery specifies the interval (in saves) for executing background retention pruning.
const pruneEvery = 20

// Prune applies retention count and age rules to the active workspace.
func (u *UseCase) Prune(ctx context.Context) (int, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return 0, err
	}
	return u.prune(ctx, workspace)
}

func (u *UseCase) prune(ctx context.Context, workspace string) (int, error) {
	opts := domain.PruneOptions{
		MaxCount: domain.HistoryKeepCount, CacheAge: cacheAge, CacheCount: cacheCount,
	}
	if u.retention != nil {
		retention, err := u.retention.Retention(ctx)
		if err != nil {
			return 0, err
		}
		opts.MaxAge = retention.Age()
	}
	return u.store.Prune(ctx, workspace, opts)
}

// offer hands a saved record to the server's queue. The record is already kept and the request it
// records is already answered, so a queue that cannot be written is logged and not reported.
func (u *UseCase) offer(ctx context.Context, workspace, id string) {
	if err := u.store.QueueRecord(ctx, workspace, id); err != nil {
		log.Printf("[record] offering %s: %v", id, err)
	}
}

// save stores a record and periodically triggers background pruning based on save frequency.
func (u *UseCase) save(ctx context.Context, workspace string, rec domain.Record) error {
	if err := u.store.SaveRecord(ctx, workspace, rec); err != nil {
		return err
	}

	if u.sincePrune.Add(1) < pruneEvery {
		return nil
	}
	u.sincePrune.Store(0)
	if _, err := u.prune(ctx, workspace); err != nil {
		// Log retention errors without failing the save operation itself.
		log.Printf("[record] retention: %v", err)
	}
	return nil
}
