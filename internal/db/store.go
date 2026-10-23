// Package db is the SQLite store: one connection, the schema, transactions, entities and sync.
package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Registers the "sqlite" driver. Pure Go, which allows Windows to build with CGO_ENABLED=0.
	_ "modernc.org/sqlite"

	"schemo/internal/platform"

	"schemo/internal/domain"
)

// sidecarSuffixes contains temporary WAL and SHM file suffixes created alongside SQLite DB.
var sidecarSuffixes = []string{"", "-wal", "-shm"}

// vacuumPages is how many pages a trim hands back at once. SQLite frees pages only at the end of
// the file, so a trim naming no count may move the whole file; a page count per call keeps to the
// tail just emptied.
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
	dsn  string
	// wake notifies external components about workspace data changes for instant sync trigger.
	wake func(workspaceID string)
	// notify announces committed entity changes; see SetNotify.
	notify func(domain.Changed)
}

// SetWake sets the workspace change notification callback.
func (s *Store) SetWake(wake func(workspaceID string)) { s.wake = wake }

// SetNotify is told, after each commit, every kind of entity the transaction changed and in which
// workspace. It is how a change reaches the window whatever made it: the window, MCP, sync or the
// extension.
func (s *Store) SetNotify(notify func(domain.Changed)) { s.notify = notify }

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
	return &Store{db: db, path: path, dsn: dsn}, nil
}

// Open verifies connection health and secures database file permissions.
func (s *Store) Open(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("connecting to %s: %w", s.path, err)
	}
	s.restrictPermissions()
	return nil
}

//go:embed schema.sql
var schema string

// schemaVersion is bumped whenever schema.sql changes. Until the first public release there are no
// upgrades: a database of another version is moved aside and a fresh one is created.
const schemaVersion = 2

// Migrate creates the schema in a fresh database. It reports the path an incompatible database was
// moved to, if it had to move one.
func (s *Store) Migrate(ctx context.Context) (string, error) {
	var version, tables int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return "", fmt.Errorf("reading the schema version: %w", err)
	}
	if version == schemaVersion {
		return "", nil
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_schema WHERE type = 'table'`).Scan(&tables); err != nil {
		return "", fmt.Errorf("reading the schema: %w", err)
	}
	moved := ""
	if tables > 0 {
		var err error
		if moved, err = s.moveAside(ctx); err != nil {
			return "", err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return moved, fmt.Errorf("creating the schema: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return moved, fmt.Errorf("creating the schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return moved, fmt.Errorf("creating the schema: %w", err)
	}
	return moved, tx.Commit()
}

// moveAside renames a database of another schema next to itself and reopens an empty one.
func (s *Store) moveAside(ctx context.Context) (string, error) {
	if err := s.db.Close(); err != nil {
		return "", fmt.Errorf("closing the old database: %w", err)
	}
	backup := fmt.Sprintf("%s.old-%d", s.path, time.Now().Unix())
	for _, suffix := range sidecarSuffixes {
		if err := os.Rename(s.path+suffix, backup+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("moving the old database aside: %w", err)
		}
	}
	db, err := sql.Open("sqlite", s.dsn)
	if err != nil {
		return "", fmt.Errorf("reopening %s: %w", s.path, err)
	}
	db.SetMaxOpenConns(1)
	s.db = db
	return backup, s.Open(ctx)
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
// A refusal is reported, not returned: the folder is already 0700 and a filesystem that cannot
// carry a mode must not stop the app from starting. It is reported because the file holds secret
// values as plain text. A missing sidecar is not a refusal.
func (s *Store) restrictPermissions() {
	for _, suffix := range sidecarSuffixes {
		err := os.Chmod(s.path+suffix, 0o600)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("[storage] %s could not be made private: %v", s.path+suffix, err)
		}
	}
}
