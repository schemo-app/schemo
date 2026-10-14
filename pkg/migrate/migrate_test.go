package migrate

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	_ "modernc.org/sqlite"
)

// testDB opens an in-memory database that behaves like one database: a bare ":memory:" DSN hands
// every pooled connection its own empty database, so the pool is pinned to one connection and the
// shared cache makes it the database everything else sees. Pinning also matters for pragmas —
// foreign_keys is per-connection, so a second connection would quietly run without it.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	name := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := sql.Open("sqlite", name)
	if err != nil {
		t.Fatalf("opening in-memory database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// goodFS is the inline set most cases run against. The two files are handed over out of order
// on purpose, so that Load's sorting is exercised rather than assumed.
func goodFS() fstest.MapFS {
	return fstest.MapFS{
		"0002_second.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE good_second (id TEXT PRIMARY KEY);"),
		},
		"0001_first.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE good_first (id TEXT PRIMARY KEY);"),
		},
	}
}

func mustUp(t *testing.T, db *sql.DB, fsys fs.FS) Result {
	t.Helper()

	result, err := Up(t.Context(), db, fsys)
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	return result
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()

	var count int
	err := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count)
	if err != nil {
		t.Fatalf("looking up table %s: %v", name, err)
	}
	return count > 0
}

func ledgerVersions(t *testing.T, db *sql.DB) []int {
	t.Helper()

	rows, err := db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("reading ledger: %v", err)
	}
	defer rows.Close()

	var versions []int
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			t.Fatalf("scanning ledger: %v", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading ledger: %v", err)
	}
	return versions
}

func TestUpRejectsForeignKeyViolationsBeforeCommit(t *testing.T) {
	db := testDB(t)
	set := fstest.MapFS{
		"0001_parent.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE parent (id TEXT PRIMARY KEY);`)},
		"0002_child.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE child (id TEXT PRIMARY KEY, parent_id TEXT REFERENCES parent(id));
INSERT INTO child VALUES ('child', 'missing');`)},
	}
	if _, err := Up(t.Context(), db, set); err == nil {
		t.Fatal("migration with an orphan was accepted")
	}
	if tableExists(t, db, "child") {
		t.Fatal("failed migration committed its schema")
	}
	if got := ledgerVersions(t, db); !slices.Equal(got, []int{1}) {
		t.Fatalf("ledger = %v, want only the valid migration", got)
	}
	var enabled int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("foreign keys were not restored: enabled=%d, error=%v", enabled, err)
	}
}

func TestUpCanRebuildReferencedTables(t *testing.T) {
	db := testDB(t)
	set := fstest.MapFS{
		"0001_original.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE parent (id TEXT PRIMARY KEY);
CREATE TABLE child (id TEXT PRIMARY KEY, parent_id TEXT REFERENCES parent(id));
INSERT INTO parent VALUES ('parent');
INSERT INTO child VALUES ('child', 'parent');`)},
		"0002_rebuild.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE replacement (id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '');
INSERT INTO replacement (id) SELECT id FROM parent;
DROP TABLE parent;
ALTER TABLE replacement RENAME TO parent;`)},
	}
	mustUp(t, db, set)
	var parentID string
	if err := db.QueryRow("SELECT parent_id FROM child").Scan(&parentID); err != nil || parentID != "parent" {
		t.Fatalf("rebuild lost the relationship: parent=%q, error=%v", parentID, err)
	}
	if _, err := db.Exec("INSERT INTO child VALUES ('orphan', 'missing')"); err == nil {
		t.Fatal("foreign keys remain disabled after rebuild")
	}
}

func TestUpAppliesEverythingThenSkips(t *testing.T) {
	db := testDB(t)

	first := mustUp(t, db, goodFS())
	if len(first.Applied) != 2 {
		t.Fatalf("first run applied %d migrations, want 2", len(first.Applied))
	}
	if first.Skipped != 0 {
		t.Errorf("first run skipped %d migrations, want 0", first.Skipped)
	}
	if got := first.Applied[0].Version; got != 1 {
		t.Errorf("first applied version = %d, want 1 (ascending order)", got)
	}
	if got := first.Applied[0].Name; got != "first" {
		t.Errorf("first applied name = %q, want %q", got, "first")
	}
	if first.Applied[1].Version != 2 || first.Applied[1].Name != "second" {
		t.Errorf("second applied = %04d_%s, want 0002_second",
			first.Applied[1].Version, first.Applied[1].Name)
	}
	for _, name := range []string{"good_first", "good_second"} {
		if !tableExists(t, db, name) {
			t.Errorf("table %s missing after the first run", name)
		}
	}
	if got := ledgerVersions(t, db); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("ledger = %v, want [1 2]", got)
	}

	var checksum string
	err := db.QueryRow(`SELECT checksum FROM schema_migrations WHERE version = 1`).Scan(&checksum)
	if err != nil {
		t.Fatalf("reading checksum: %v", err)
	}
	if len(checksum) != 64 || checksum != first.Applied[0].Checksum {
		t.Errorf("recorded checksum %q does not match the loaded migration %q",
			checksum, first.Applied[0].Checksum)
	}

	second := mustUp(t, db, goodFS())
	if len(second.Applied) != 0 {
		t.Errorf("second run applied %d migrations, want 0", len(second.Applied))
	}
	if second.Skipped != 2 {
		t.Errorf("second run skipped %d migrations, want 2", second.Skipped)
	}

	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&rows); err != nil {
		t.Fatalf("counting ledger: %v", err)
	}
	if rows != 2 {
		t.Errorf("ledger has %d rows after two runs, want 2", rows)
	}
}

// TestUpChecksumMismatch edits an already-applied file behind the ledger's back and checks that
// the run refuses before touching the schema.
func TestUpChecksumMismatch(t *testing.T) {
	db := testDB(t)

	base := fstest.MapFS{
		"0001_init.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE checksum_alpha (id TEXT PRIMARY KEY);"),
		},
	}
	mustUp(t, db, base)

	// Same version 1, different SQL, plus a version 2 that has never run.
	_, err := Up(t.Context(), db, os.DirFS("testdata/migrations/changed"))
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Up on an edited file = %v, want ErrChecksumMismatch", err)
	}
	if !strings.Contains(err.Error(), "0001_init") {
		t.Errorf("error does not name the offending migration: %v", err)
	}

	// Nothing new ran: not the pending version 2, and not the edited body of version 1.
	if tableExists(t, db, "changed_beta") {
		t.Error("the edited version 1 was executed despite the mismatch")
	}
	if tableExists(t, db, "changed_more") {
		t.Error("the pending version 2 was applied on top of a mismatched ledger")
	}
	if got := ledgerVersions(t, db); !slices.Equal(got, []int{1}) {
		t.Errorf("ledger = %v, want [1]", got)
	}
}

// TestUpBrokenMigrationRollsBack checks the guarantee the per-migration transaction exists for:
// a file that fails half-way leaves nothing of itself behind, and does not take the migrations
// that already succeeded in the same run down with it.
func TestUpBrokenMigrationRollsBack(t *testing.T) {
	db := testDB(t)

	_, err := Up(t.Context(), db, os.DirFS("testdata/migrations/broken"))
	if err == nil {
		t.Fatal("Up on a set containing a syntax error returned no error")
	}
	if !strings.Contains(err.Error(), "0002_broken") {
		t.Errorf("error does not name the failing migration: %v", err)
	}

	if !tableExists(t, db, "broken_ok") {
		t.Error("the migration that succeeded earlier in the run was lost")
	}
	// 0002_broken.sql creates broken_half before it reaches the invalid statement, so the table
	// only stayed out if the transaction covering the whole file was rolled back.
	if tableExists(t, db, "broken_half") {
		t.Error("broken_half survived: the failing migration was not rolled back")
	}
	if got := ledgerVersions(t, db); !slices.Equal(got, []int{1}) {
		t.Errorf("ledger = %v, want [1]: the failed version must not be recorded", got)
	}
}

// TestUpMultiStatementFile pins the behaviour the runner silently depends on: one .sql file may
// hold several statements, and a single Exec runs all of them. Verified against modernc.org/sqlite
// v1.58.0, which prepares the whole statement list itself — so Up needs no splitter. If a driver
// change stops doing this, this test fails and Up has to grow one (naive splitting on ";" is wrong:
// a semicolon inside a string literal or trigger body is not a terminator).
func TestUpMultiStatementFile(t *testing.T) {
	db := testDB(t)

	set := fstest.MapFS{
		"0001_multi.sql": &fstest.MapFile{Data: []byte(`
CREATE TABLE multi_parent (id TEXT PRIMARY KEY);
CREATE TABLE multi_child (
  id TEXT PRIMARY KEY,
  parent_id TEXT NOT NULL REFERENCES multi_parent(id) ON DELETE CASCADE
);
CREATE INDEX multi_child_parent ON multi_child(parent_id);
INSERT INTO multi_parent (id) VALUES ('p1');
INSERT INTO multi_child (id, parent_id) VALUES ('c1', 'p1');
`)},
	}
	mustUp(t, db, set)

	for _, name := range []string{"multi_parent", "multi_child"} {
		if !tableExists(t, db, name) {
			t.Errorf("table %s missing: not every statement in the file ran", name)
		}
	}

	var index int
	err := db.QueryRow(
		`SELECT count(*) FROM sqlite_master
		 WHERE type = 'index' AND name = 'multi_child_parent'`).Scan(&index)
	if err != nil {
		t.Fatalf("looking up index: %v", err)
	}
	if index != 1 {
		t.Error("index multi_child_parent missing: the third statement did not run")
	}

	var children int
	if err := db.QueryRow(`SELECT count(*) FROM multi_child`).Scan(&children); err != nil {
		t.Fatalf("counting children: %v", err)
	}
	if children != 1 {
		t.Errorf("multi_child has %d rows, want 1: the INSERT statements did not run", children)
	}
}

// TestUpRefusesDuplicateSet checks that a version clash is caught on Load's terms and that Up
// rejects the whole set rather than picking one of the two files.
func TestUpRefusesDuplicateSet(t *testing.T) {
	db := testDB(t)

	_, err := Up(t.Context(), db, os.DirFS("testdata/migrations/duplicate"))
	if !errors.Is(err, ErrDuplicateVersion) {
		t.Fatalf("Up on a duplicate set = %v, want ErrDuplicateVersion", err)
	}
	for _, name := range []string{"duplicate_alpha", "duplicate_beta"} {
		if tableExists(t, db, name) {
			t.Errorf("table %s exists: nothing should have been applied", name)
		}
	}
	if got := ledgerVersions(t, db); len(got) != 0 {
		t.Errorf("ledger = %v, want empty", got)
	}
}

// TestLoad is the table over the filename rules and the ordering Load promises.
func TestLoad(t *testing.T) {
	tests := []struct {
		name string
		fsys fs.FS
		// want holds "%04d_%s" for each expected migration, in the order Load must return them.
		want    []string
		wantErr error
	}{
		{
			name: "empty fs",
			fsys: fstest.MapFS{},
		},
		{
			name: "nothing matches the pattern",
			fsys: os.DirFS("testdata/migrations/nonmatching"),
		},
		{
			name: "sorted ascending regardless of directory order",
			fsys: goodFS(),
			want: []string{"0001_first", "0002_second"},
		},
		{
			name:    "duplicate version",
			fsys:    os.DirFS("testdata/migrations/duplicate"),
			wantErr: ErrDuplicateVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(tt.fsys)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Load() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}

			ids := make([]string, 0, len(got))
			for _, m := range got {
				ids = append(ids, fmt.Sprintf("%04d_%s", m.Version, m.Name))
				if m.SQL == "" {
					t.Errorf("migration %04d_%s has empty SQL", m.Version, m.Name)
				}
				if len(m.Checksum) != 64 {
					t.Errorf("migration %04d_%s has checksum %q, want 64 hex chars",
						m.Version, m.Name, m.Checksum)
				}
			}
			if !slices.Equal(ids, tt.want) {
				t.Errorf("Load() = %v, want %v", ids, tt.want)
			}
		})
	}
}

// TestLoadChecksumTracksContent guards the property the mismatch check rests on: the checksum
// is a function of the file's bytes, so an edit anywhere in it is detectable.
func TestLoadChecksumTracksContent(t *testing.T) {
	original, err := Load(fstest.MapFS{
		"0001_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id TEXT PRIMARY KEY);")},
	})
	if err != nil {
		t.Fatalf("Load(original): %v", err)
	}

	edited, err := Load(fstest.MapFS{
		"0001_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id TEXT PRIMARY KEY) -- x;")},
	})
	if err != nil {
		t.Fatalf("Load(edited): %v", err)
	}

	if original[0].Checksum == edited[0].Checksum {
		t.Error("a one-character edit did not change the checksum")
	}
}
