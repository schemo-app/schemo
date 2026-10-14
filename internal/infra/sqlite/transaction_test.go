package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueuedChangesNotifyOnlyAfterSuccessfulCommit(t *testing.T) {
	for _, scenario := range []string{"commit", "callback failure", "commit failure"} {
		t.Run(scenario, func(t *testing.T) {
			store := newMigratedStore(t)
			ctx := t.Context()
			if _, err := store.DB().ExecContext(ctx, `CREATE TABLE deferred_guard (
				workspace_id TEXT REFERENCES workspaces(id) DEFERRABLE INITIALLY DEFERRED)`); err != nil {
				t.Fatal(err)
			}
			woken, visible := 0, 0
			var readError error
			store.SetWake(func(spaceID string) {
				woken++
				if spaceID != ws {
					t.Errorf("notified workspace %q, want %q", spaceID, ws)
				}
				readContext, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				readError = store.DB().QueryRowContext(readContext,
					`SELECT count(*) FROM sync_queue WHERE entity_id IN ('first', 'second')`).Scan(&visible)
			})
			refusal := errors.New("stop after queueing")
			err := store.inTx(ctx, "write with notifications", func(tx *writeTx) error {
				for _, id := range []string{"first", "second"} {
					if err := enqueue(ctx, tx, ws, entityCollection, id, actionCreate); err != nil {
						return err
					}
				}
				switch scenario {
				case "callback failure":
					return refusal
				case "commit failure":
					_, err := tx.ExecContext(ctx, `INSERT INTO deferred_guard VALUES ('missing')`)
					return err
				}
				return nil
			})
			if scenario == "commit" {
				if err != nil || woken != 1 || visible != 2 || readError != nil {
					t.Fatalf("commit error=%v, wakes=%d, committed rows=%d, read error=%v", err, woken, visible, readError)
				}
				return
			}
			if err == nil || woken != 0 || len(queued(t, store, ws)) != 0 {
				t.Fatalf("failed transaction left changes or notifications: error=%v, wakes=%d", err, woken)
			}
			if scenario == "callback failure" && !errors.Is(err, refusal) {
				t.Fatalf("callback error lost its identity: %v", err)
			}
		})
	}
}
