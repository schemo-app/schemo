package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"schemo/internal/platform"
	"schemo/migrations"
)

func TestDatabasePathTreatsURICharactersAsFilenameData(t *testing.T) {
	dir := platform.DataDir(filepath.Join(t.TempDir(), "private %# data"))
	if err := dir.Create(); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(t.Context(), "CREATE TABLE path_fixture (value TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir.DatabasePath()); err != nil {
		t.Fatalf("SQLite opened a different path: %v", err)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(tempDataDir(t))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// The pragmas live in the DSN precisely because a pooled connection would silently lose them.
// This asserts the connection the pool actually hands out, which is the one that matters.
func TestOpenAppliesPragmas(t *testing.T) {
	store := newTestStore(t)
	if err := store.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}

	var foreignKeys int
	if err := store.DB().QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("reading foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1 — cascades would be silently off", foreignKeys)
	}

	var journalMode string
	if err := store.DB().QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("reading journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	var autoVacuum int
	if err := store.DB().QueryRow("PRAGMA auto_vacuum").Scan(&autoVacuum); err != nil {
		t.Fatalf("reading auto_vacuum: %v", err)
	}
	// 2 is INCREMENTAL. It can only be set before the first table exists, so getting it wrong
	// once would mean a full VACUUM every time retention frees rows.
	if autoVacuum != 2 {
		t.Errorf("auto_vacuum = %d, want 2 (incremental)", autoVacuum)
	}
}

func TestOpenCreatesFileAndIsRepeatable(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.Open(ctx); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := store.Migrate(ctx, migrations.FS); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// The retry button on the failure screen calls the same pair again.
	if err := store.Open(ctx); err != nil {
		t.Fatalf("second Open: %v", err)
	}
	res, err := store.Migrate(ctx, migrations.FS)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if len(res.Applied) != 0 {
		t.Errorf("second Migrate applied %d migration(s), want 0", len(res.Applied))
	}

	if filepath.Base(store.Path()) != "app.db" {
		t.Errorf("database path = %q, want it to end in app.db", store.Path())
	}
}

func TestNewStoreDoesNoIO(t *testing.T) {
	// A directory that does not exist: NewStore must still succeed, because the failure belongs
	// on the failure screen rather than in the dependency graph.
	dir := filepath.Join(t.TempDir(), "missing", "deeper")
	store, err := NewStore(platform.DataDir(dir))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Open(context.Background()); err == nil {
		t.Error("Open on a missing directory succeeded, want an error the UI can show")
	}
}
