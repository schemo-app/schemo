// Package sqlite provides database access using a single connection and migration-based schema.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	// Registers the "sqlite" driver. Pure Go, which allows Windows to build with CGO_ENABLED=0.
	_ "modernc.org/sqlite"

	"schemo/internal/platform"
	"schemo/pkg/migrate"
)

// sidecarSuffixes contains temporary WAL and SHM file suffixes created alongside SQLite DB.
var sidecarSuffixes = []string{"", "-wal", "-shm"}

// vacuumPages is how much of the file a trim hands back at once, in pages. SQLite frees pages only
// at the end of the file, so a trim that names no count may move the whole of it; a page count per
// call keeps to the tail that was just emptied and leaves the rest to the next trim.
const vacuumPages = 256

// vacuum is that trim as the statement that runs. It is built once because the count is a SQL
// literal rather than a bound parameter, and PRAGMA takes no parameters at all.
var vacuum = fmt.Sprintf("PRAGMA incremental_vacuum(%d)", vacuumPages)

// querier represents an interface for running queries against either *sql.DB or *writeTx.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Store provides access to the SQLite database.
type Store struct {
	db   *sql.DB
	path string
	// wake notifies external components about workspace data changes for instant sync trigger.
	wake func(spaceID string)
}

// SetWake sets the workspace change notification callback.
func (s *Store) SetWake(wake func(spaceID string)) { s.wake = wake }

// versionUnagreed represents the initial sync version for newly created local rows.
const versionUnagreed = 0

// NewStore initializes a new Store with database configuration pragmas.
func NewStore(dataDir platform.DataDir) (*Store, error) {
	path := dataDir.DatabasePath()

	// Configure SQLite pragmas via DSN to ensure settings persist across connection pool resets.
	location := url.URL{Path: filepath.ToSlash(path)}
	dsn := "file:" + location.EscapedPath() + "?" + strings.Join([]string{
		"_pragma=journal_mode(WAL)",
		"_pragma=foreign_keys(1)",
		"_pragma=busy_timeout(5000)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=auto_vacuum(INCREMENTAL)",
	}, "&")

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	// Restrict to a single connection to eliminate SQLITE_BUSY concurrency conflicts.
	db.SetMaxOpenConns(1)
	return &Store{db: db, path: path}, nil
}

// Open verifies connection health and secures database file permissions.
func (s *Store) Open(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("connecting to %s: %w", s.path, err)
	}
	s.restrictPermissions()
	return nil
}

// Migrate applies pending schema migrations from the provided filesystem.
func (s *Store) Migrate(ctx context.Context, fsys fs.FS) (migrate.Result, error) {
	result, err := migrate.Up(ctx, s.db, fsys)
	if errors.Is(err, migrate.ErrChecksumMismatch) {
		return result, fmt.Errorf("database does not match the current pre-release schema; "+
			"back up existing data and use a fresh database for the table baseline "+
			"(see migrations/README.md): %w", err)
	}
	return result, err
}

// Close closes the underlying database connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database handle.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Path returns the filesystem path to the database file.
func (s *Store) Path() string {
	return s.path
}

// restrictPermissions limits database file permissions to the current owner (0600).
//
// A refusal is said, not returned: the folder is 0700 already, so this is the second lock and not
// the only one, and a filesystem that cannot carry a mode must not be the reason the app does not
// start. It is said at all because the file holds secret values as plain text, and this is the only
// sign that somebody else may be able to read them. A missing sidecar is not a refusal.
func (s *Store) restrictPermissions() {
	for _, suffix := range sidecarSuffixes {
		err := os.Chmod(s.path+suffix, 0o600)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("[storage] %s could not be made private: %v", s.path+suffix, err)
		}
	}
}
