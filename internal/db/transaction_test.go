package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"schemo/internal/domain"
)

func TestRemoteDeletionsNotifyTheOpenWorkspace(t *testing.T) {
	t.Parallel()
	store := newMigratedStore(t)
	ctx := t.Context()
	workspace := linked(t, store)
	page := []domain.SyncChange{
		{Entity: domain.SyncEntity{Kind: domain.EntityCollection, ID: "collection", Data: []byte(`{}`)}},
		{Entity: domain.SyncEntity{Kind: domain.EntityRequest, ID: "request", Parent: "collection", Data: []byte(`{}`)}},
	}
	if skipped, err := store.ApplyPage(ctx, workspace.ID, page, 2); err != nil || skipped != 0 {
		t.Fatalf("ApplyPage(create) = %d skipped, %v, want both rows saved", skipped, err)
	}
	notices := map[domain.Changed]bool{}
	store.SetNotify(func(change domain.Changed) { notices[change] = true })
	for i := range page {
		page[i].Entity.Deleted = true
	}
	if skipped, err := store.ApplyPage(ctx, workspace.ID, page, 4); err != nil || skipped != 0 {
		t.Fatalf("ApplyPage(delete) = %d skipped, %v, want both rows deleted", skipped, err)
	}
	for _, kind := range []domain.EntityType{domain.EntityCollection, domain.EntityRequest} {
		if !notices[domain.Changed{Kind: string(kind), Workspace: workspace.ID}] {
			t.Errorf("ApplyPage(delete %s) did not notify the workspace", kind)
		}
	}
	tree, err := store.Collections(ctx, workspace.ID)
	if err != nil || len(tree) != 0 {
		t.Errorf("Collections(after deletion) = %+v, %v, want an empty tree", tree, err)
	}
}

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
			store.SetWake(func(workspaceID string) {
				woken++
				if workspaceID != ws {
					t.Errorf("notified workspace %q, want %q", workspaceID, ws)
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
