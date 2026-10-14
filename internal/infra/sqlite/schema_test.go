package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"schemo/internal/domain"
	"schemo/migrations"
	"schemo/pkg/migrate"
)

// expectedTables contains all database tables required by the application.
var expectedTables = []string{
	"workspaces",
	"environments", "variables",
	"records", "record_bodies",
	"collections", "collection_nodes",
	"script_runs",
	"collection_runs", "collection_run_results",
	"settings", "drafts", "tabs",
	"jar_cookies", "jar_domains",
	"sync_queue", "workspace_sync",
	"publications",
	"account", "plan",
	"schema_migrations",
}

// syncedTables lists tables replicated across devices alongside their sync envelope fields.
var syncedTables = []struct {
	table    string
	version  bool
	deletion bool
}{
	{"workspaces", true, false},
	{"collections", true, true},
	{"collection_nodes", true, true},
	{"environments", true, true},
	{"variables", true, true},
	{"jar_cookies", true, true},
	{"jar_domains", true, true},
	{"records", false, true},
	{"record_bodies", false, false},
	{"collection_runs", false, false},
	{"collection_run_results", false, false},
	{"script_runs", false, false},
}

// tableExists checks if a table with the given name exists in sqlite_master.
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

// TestEmbeddedSchemaApplies verifies that all embedded migration files execute successfully.
func TestEmbeddedSchemaApplies(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}

	result, err := store.Migrate(ctx, migrations.FS)
	if err != nil {
		t.Fatalf("Migrate(migrations.FS): %v", err)
	}

	embedded, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("listing the embedded migrations: %v", err)
	}
	if len(result.Applied) != len(embedded) {
		t.Errorf("applied %d migrations, want %d — every embedded one",
			len(result.Applied), len(embedded))
	}
	if result.Skipped != 0 {
		t.Errorf("skipped %d migrations on a fresh database, want 0", result.Skipped)
	}

	for _, name := range expectedTables {
		if !tableExists(t, store, name) {
			t.Errorf("table %s is missing after migrating", name)
		}
	}

	again, err := store.Migrate(ctx, migrations.FS)
	if err != nil {
		t.Fatalf("second Migrate(migrations.FS): %v", err)
	}
	if len(again.Applied) != 0 || again.Skipped != len(result.Applied) {
		t.Errorf("second run applied %d and skipped %d, want 0 and %d",
			len(again.Applied), again.Skipped, len(result.Applied))
	}
}

func TestPreviousBaselineIsRefusedWithoutChangingData(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	previous := fstest.MapFS{
		"0001_workspaces.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE legacy_content (id TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO legacy_content VALUES ('local', 'unsynced');`)},
	}
	if _, err := store.Migrate(ctx, previous); err != nil {
		t.Fatal(err)
	}
	_, err := store.Migrate(ctx, migrations.FS)
	if !errors.Is(err, migrate.ErrChecksumMismatch) || !strings.Contains(err.Error(), "pre-release schema") {
		t.Fatalf("expected a clear baseline refusal, got %v", err)
	}
	var value string
	if err := store.DB().QueryRow(`SELECT value FROM legacy_content WHERE id = 'local'`).Scan(&value); err != nil || value != "unsynced" {
		t.Fatalf("existing data changed: value=%q, error=%v", value, err)
	}
	if tableExists(t, store, "workspaces") {
		t.Fatal("new schema was partially applied to the old database")
	}
}

func TestBaselineCreatesOneTablePerMigration(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	loaded, err := migrate.Load(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{}
	for _, migration := range loaded {
		before := applicationTableCount(t, store)
		name := fmt.Sprintf("%04d_%s.sql", migration.Version, migration.Name)
		files[name] = &fstest.MapFile{Data: []byte(migration.SQL)}
		if _, err := store.Migrate(ctx, files); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if after := applicationTableCount(t, store); after != before+1 || !tableExists(t, store, migration.Name) {
			t.Fatalf("%s did not create exactly its own table", name)
		}
	}
}

func applicationTableCount(t *testing.T, store *Store) int {
	t.Helper()
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> 'schema_migrations'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
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
		{"an environment", `INSERT INTO environments
			(id, workspace_id, name, position, created_at, updated_at)
			VALUES ('env-2', ?, 'Stage', 0, 2, 2)`, []any{ws}},
		{"a variable", `INSERT INTO variables
			(id, workspace_id, scope_kind, name, kind, position, created_at, updated_at)
			VALUES ('var-2', ?, 'globals', 'host', 'text', 0, 2, 2)`, []any{ws}},
		{"a collection", `INSERT INTO collections
			(id, workspace_id, name, position, created_at, updated_at)
			VALUES ('col-2', ?, 'Team', 0, 2, 2)`, []any{ws}},
		{"a node in it", `INSERT INTO collection_nodes
			(id, collection_id, name, position, created_at, updated_at)
			VALUES ('node-2', 'col-2', 'List', 0, 2, 2)`, nil},
		{"the draft", `INSERT INTO drafts (workspace_id, id, updated_at) VALUES (?, 'command-line', 2)`,
			[]any{ws}},
		{"a tab", `INSERT INTO tabs (workspace_id, id, kind)
			VALUES (?, 't-2', 'request')`, []any{ws}},
		{"a jar cookie", `INSERT INTO jar_cookies
			(id, workspace_id, domain, name, value, origin, written_at)
			VALUES ('jar-2', ?, 'example.test', 'session', 'abc', 'response', 2)`, []any{ws}},
		{"a blocked domain", `INSERT INTO jar_domains (id, workspace_id, domain, blocked, written_at)
			VALUES ('jd-2', ?, 'example.test', 1, 2)`, []any{ws}},
		{"an entry the space owes the server", `INSERT INTO sync_queue
			(id, workspace_id, entity_type, entity_id, action, operation_id, created_at)
			VALUES ('q-2', ?, 'collection', 'col-2', 'create', 'op-2', 2)`, []any{ws}},
		{"where the space stands", `INSERT INTO workspace_sync (workspace_id, state, last_sync_seq)
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

	for _, counted := range []struct {
		table string
		query string
	}{
		{"records", `SELECT count(*) FROM records`},
		{"record_bodies", `SELECT count(*) FROM record_bodies`},
		{"script_runs", `SELECT count(*) FROM script_runs`},
		{"environments", `SELECT count(*) FROM environments`},
		{"variables", `SELECT count(*) FROM variables`},
		{"collections", `SELECT count(*) FROM collections`},
		{"collection_nodes", `SELECT count(*) FROM collection_nodes`},
		{"drafts", `SELECT count(*) FROM drafts`},
		{"tabs", `SELECT count(*) FROM tabs`},
		{"jar_cookies", `SELECT count(*) FROM jar_cookies`},
		{"jar_domains", `SELECT count(*) FROM jar_domains`},
		{"sync_queue", `SELECT count(*) FROM sync_queue WHERE workspace_id = ?`},
		{"workspace_sync", `SELECT count(*) FROM workspace_sync WHERE workspace_id = ?`},
	} {
		var rows int
		if err := store.DB().QueryRow(counted.query, ws).Scan(&rows); err != nil {
			t.Fatalf("counting %s: %v", counted.table, err)
		}
		if rows != 0 {
			t.Errorf("%d rows survived in %s: ON DELETE CASCADE is not firing", rows, counted.table)
		}
	}

	var kept int
	personal := `SELECT count(*) FROM workspaces WHERE id = 'personal'`
	if err := store.DB().QueryRow(personal).Scan(&kept); err != nil {
		t.Fatalf("counting workspaces: %v", err)
	}
	if kept != 1 {
		t.Errorf("the default workspace is gone: %d rows", kept)
	}
}

// TestEverySyncedTableCarriesItsEnvelope validates required sync metadata columns on target tables.
func TestEverySyncedTableCarriesItsEnvelope(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Migrate(ctx, migrations.FS); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, table := range syncedTables {
		columns := columnsOf(t, store, table.table)
		if columns["version"] != table.version {
			t.Errorf("%s: version present = %v, want %v", table.table, columns["version"], table.version)
		}
		if columns["deleted_at"] != table.deletion {
			t.Errorf("%s: deleted_at present = %v, want %v",
				table.table, columns["deleted_at"], table.deletion)
		}
		if table.version && !columns["updated_at"] && !columns["written_at"] {
			t.Errorf("%s carries no clock at all", table.table)
		}
	}
}

// TestAFreshDatabaseOwesTheServerItsSpace verifies initial sync queue population after migration.
func TestAFreshDatabaseOwesTheServerItsSpace(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Migrate(ctx, migrations.FS); err != nil {
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

// columnsOf inspects database schema pragma and returns column names present in a table.
func columnsOf(t *testing.T, store *Store, table string) map[string]bool {
	t.Helper()
	rows, err := store.DB().Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("reading the columns of %s: %v", table, err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("reading a column of %s: %v", table, err)
		}
		out[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the columns of %s: %v", table, err)
	}
	return out
}
