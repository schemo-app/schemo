package db

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"schemo/internal/domain"
)

var expectedTables = []string{
	"workspaces", "workspace_sync", "entities", "settings",
	"records", "record_bodies", "script_runs", "collection_runs", "collection_run_results",
	"sync_queue", "publications",
}

func tableExists(t *testing.T, store *Store, name string) bool {
	t.Helper()
	var count int
	err := store.DB().QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count)
	if err != nil {
		t.Fatalf("looking up table %s: %v", name, err)
	}
	return count > 0
}

func TestEmbeddedSchemaApplies(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if moved, err := store.Migrate(ctx); err != nil || moved != "" {
		t.Fatalf("Migrate = %q, %v", moved, err)
	}
	for _, name := range expectedTables {
		if !tableExists(t, store, name) {
			t.Errorf("table %s is missing", name)
		}
	}
	if moved, err := store.Migrate(ctx); err != nil || moved != "" {
		t.Fatalf("second Migrate = %q, %v", moved, err)
	}
}

func TestADatabaseOfAnotherSchemaIsMovedAside(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx,
		`CREATE TABLE legacy (id TEXT); INSERT INTO legacy VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}
	moved, err := store.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !strings.HasPrefix(moved, store.Path()+".old-") {
		t.Fatalf("moved to %q", moved)
	}
	if _, err := os.Stat(moved); err != nil {
		t.Fatalf("the old database is not where it was moved: %v", err)
	}
	if tableExists(t, store, "legacy") || !tableExists(t, store, "entities") {
		t.Fatal("the fresh database is not the new schema")
	}
}

// TestEmbeddedSchemaEnforcesForeignKeys verifies foreign key enforcement and CASCADE constraints.
func TestEmbeddedSchemaEnforcesForeignKeys(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	var enabled int
	if err := store.DB().QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		t.Fatalf("reading the foreign_keys pragma: %v", err)
	}
	if enabled != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1", enabled)
	}

	_, err := store.DB().ExecContext(ctx,
		`INSERT INTO record_bodies (record_id, side) VALUES (?, ?)`, "no-such-record", "request")
	if err == nil {
		t.Fatal("a record_bodies row with no parent record was accepted: foreign keys are off")
	}

	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO records (id, workspace_id, source, method, url,
			started_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"rec-1", "personal", "manual", "GET", "https://example.test/", 1); err != nil {
		t.Fatalf("inserting a record: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO record_bodies (record_id, side) VALUES ('rec-1', 'response')`); err != nil {
		t.Fatalf("inserting a body for it: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM records WHERE id = ?`, "rec-1"); err != nil {
		t.Fatalf("deleting the record: %v", err)
	}

	var bodies int
	if err := store.DB().QueryRow(`SELECT count(*) FROM record_bodies`).Scan(&bodies); err != nil {
		t.Fatalf("counting bodies: %v", err)
	}
	if bodies != 0 {
		t.Errorf("%d bodies survived their record: ON DELETE CASCADE is not firing", bodies)
	}
}

// TestEmbeddedSchemaCascadesAWorkspace tests foreign key deletion cascades from workspace removal.
func TestEmbeddedSchemaCascadesAWorkspace(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	const ws = "team-1"
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO workspaces (id, kind, position, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		ws, "team", 1, 1, 1); err != nil {
		t.Fatalf("inserting a workspace: %v", err)
	}

	seed := []struct {
		what string
		sql  string
		args []any
	}{
		{"a record", `INSERT INTO records (id, workspace_id, source, method, url, started_at)
			VALUES ('rec-2', ?, 'browser', 'GET', 'https://example.test/', 2)`, []any{ws}},
		{"a record body", `INSERT INTO record_bodies (record_id, side)
			VALUES ('rec-2', 'response')`, nil},
		{"a script run", `INSERT INTO script_runs (id, workspace_id, scope, ok, created_at)
			VALUES ('run-2', ?, 'pre', 1, 2)`, []any{ws}},
		{"an entity", `INSERT INTO entities (id, workspace_id, kind, created_at, updated_at)
			VALUES ('col-2', ?, 'collection', 2, 2)`, []any{ws}},
		{"an entry the workspace owes the server", `INSERT INTO sync_queue
			(id, workspace_id, entity_type, entity_id, action, operation_id, created_at)
			VALUES ('q-2', ?, 'collection', 'col-2', 'create', 'op-2', 2)`, []any{ws}},
		{"where the workspace stands", `INSERT INTO workspace_sync (workspace_id, state, last_sync_seq)
			VALUES (?, 'synced', 7)`, []any{ws}},
	}
	for _, s := range seed {
		if _, err := store.DB().ExecContext(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("inserting %s: %v", s.what, err)
		}
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, ws); err != nil {
		t.Fatalf("deleting the workspace: %v", err)
	}
	for _, table := range []string{"records", "record_bodies", "script_runs", "entities"} {
		var rows int
		if err := store.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&rows); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if rows != 0 {
			t.Errorf("%d rows survived in %s", rows, table)
		}
	}
	for _, table := range []string{"sync_queue", "workspace_sync"} {
		var rows int
		if err := store.DB().QueryRow(`SELECT count(*) FROM `+table+` WHERE workspace_id = ?`, ws).
			Scan(&rows); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		if rows != 0 {
			t.Errorf("%d rows survived in %s", rows, table)
		}
	}
}

// TestAFreshDatabaseOwesTheServerItsWorkspace verifies initial sync queue population after migration.
func TestAFreshDatabaseOwesTheServerItsWorkspace(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var waiting int
	row := store.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue
		  WHERE workspace_id = 'personal' AND entity_type = 'workspace' AND status = 'pending'`)
	if err := row.Scan(&waiting); err != nil {
		t.Fatalf("counting the entries the fresh database owes: %v", err)
	}
	if waiting != 1 {
		t.Errorf("%d entries are waiting on a fresh database, want the one it starts with", waiting)
	}
}

// TestTheOutboxHoldsOneWaitingEntryPerRow tests unique pending entries per row in sync queue.
func TestTheOutboxHoldsOneWaitingEntryPerRow(t *testing.T) {
	ctx := context.Background()
	store := newMigratedStore(t)

	queue := func() {
		t.Helper()
		err := store.inTx(ctx, "queueing a change", func(tx *writeTx) error {
			return enqueue(ctx, tx, "personal", domain.EntityCollection, "col-1",
				domain.ActionUpdate)
		})
		if err != nil {
			t.Fatalf("queueing a change: %v", err)
		}
	}
	entries := func() int {
		t.Helper()
		var held int
		if err := store.DB().QueryRowContext(ctx,
			`SELECT count(*) FROM sync_queue WHERE entity_id = 'col-1'`).Scan(&held); err != nil {
			t.Fatalf("counting the entries: %v", err)
		}
		return held
	}
	theOneWaiting := func() (status, reason string, retries int, retryAt bool) {
		t.Helper()
		var next sql.NullInt64
		if err := store.DB().QueryRowContext(ctx,
			`SELECT status, reason, retry_count, next_retry_at FROM sync_queue
			  WHERE entity_id = 'col-1' AND status <> 'sending'`).
			Scan(&status, &reason, &retries, &next); err != nil {
			t.Fatalf("reading the queue: %v", err)
		}
		return status, reason, retries, next.Valid
	}

	queue()

	queue()
	if held := entries(); held != 1 {
		t.Errorf("%d entries are waiting for one row, want one: the second edit upserts the first",
			held)
	}

	if _, err := store.DB().ExecContext(ctx,
		`UPDATE sync_queue SET status = 'parked', reason = 'tooLarge', retry_count = 3,
		        next_retry_at = 1
		  WHERE entity_id = 'col-1'`); err != nil {
		t.Fatalf("parking an entry: %v", err)
	}
	queue()
	status, reason, retries, retryAt := theOneWaiting()
	if status != "pending" || reason != "" || retries != 0 || retryAt {
		t.Errorf("the row changed and its entry is %q/%q after %d tries, at %v — want it waiting",
			status, reason, retries, retryAt)
	}
	if held := entries(); held != 1 {
		t.Errorf("%d entries for one row after an edit to a parked one, want one", held)
	}

	if _, err := store.DB().ExecContext(ctx,
		`UPDATE sync_queue SET status = 'sending' WHERE entity_id = 'col-1'`); err != nil {
		t.Fatalf("marking an entry in flight: %v", err)
	}
	queue()
	if held := entries(); held != 2 {
		t.Errorf("%d entries for one row, want two: the one in flight and the one waiting", held)
	}
}
