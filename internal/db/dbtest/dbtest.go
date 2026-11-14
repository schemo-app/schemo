// Package dbtest gives tests a migrated SQLite store in a temp dir: the rules that matter live in SQL,
// so tests use the real store.
package dbtest

import (
	"testing"

	"schemo/internal/db"
	"schemo/internal/platform"
)

func New(t testing.TB) *db.Store {
	t.Helper()
	store, err := db.NewStore(platform.DataDir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return store
}
