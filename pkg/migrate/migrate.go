// Package migrate applies SQL migrations to a SQLite database.
package migrate

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// ErrChecksumMismatch reports that an applied migration was modified.
var ErrChecksumMismatch = errors.New("migration checksum mismatch")

// ErrDuplicateVersion reports that multiple migrations use the same version.
var ErrDuplicateVersion = errors.New("duplicate migration version")

const ledgerDDL = `CREATE TABLE IF NOT EXISTS schema_migrations (
    version      INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL,
    checksum     TEXT    NOT NULL,
    applied_at   INTEGER NOT NULL,
    execution_ms INTEGER NOT NULL
)`

// migrationName matches NNNN_name.sql migration files.
var migrationName = regexp.MustCompile(`^([0-9]{4})_(.+)\.sql$`)

// Migration is a SQL migration loaded from a fs.FS.
type Migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string
}

// Result reports the migrations applied and skipped by Up.
type Result struct {
	Applied []Migration
	Skipped int
}

// Load reads and sorts migrations by version.
func Load(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("listing migrations: %w", err)
	}

	out := make([]Migration, 0, len(entries))
	seen := make(map[int]string)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}

		version, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("parsing version of %s: %w", entry.Name(), err)
		}

		if previous, duplicate := seen[version]; duplicate {
			return nil, fmt.Errorf(
				"%w: version %04d is claimed by both %s and %s",
				ErrDuplicateVersion, version, previous, entry.Name(),
			)
		}
		seen[version] = entry.Name()

		body, err := fs.ReadFile(fsys, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", entry.Name(), err)
		}

		normalizedBody := bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
		sum := sha256.Sum256(normalizedBody)

		out = append(out, Migration{
			Version:  version,
			Name:     match[2],
			SQL:      string(body),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}

	slices.SortFunc(out, func(a, b Migration) int { return cmp.Compare(a.Version, b.Version) })

	return out, nil
}

// Up applies all pending migrations in version order.
func Up(ctx context.Context, db *sql.DB, fsys fs.FS) (Result, error) {
	connection, err := db.Conn(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquiring migration connection: %w", err)
	}
	defer func() { _ = connection.Close() }()

	if _, err := connection.ExecContext(ctx, ledgerDDL); err != nil {
		return Result{}, fmt.Errorf("creating schema_migrations: %w", err)
	}

	migrations, err := Load(fsys)
	if err != nil {
		return Result{}, err
	}

	recorded, err := appliedChecksums(ctx, connection)
	if err != nil {
		return Result{}, err
	}

	var result Result
	pending := make([]Migration, 0, len(migrations))

	for _, m := range migrations {
		checksum, applied := recorded[m.Version]
		if !applied {
			pending = append(pending, m)
			continue
		}

		if checksum != m.Checksum {
			return Result{}, fmt.Errorf(
				"applying migration %04d_%s: %w",
				m.Version, m.Name, ErrChecksumMismatch,
			)
		}

		result.Skipped++
	}

	for _, m := range pending {
		if err := apply(ctx, connection, m); err != nil {
			return Result{}, fmt.Errorf(
				"applying migration %04d_%s: %w",
				m.Version, m.Name, err,
			)
		}
		result.Applied = append(result.Applied, m)
	}

	return result, nil
}

func appliedChecksums(ctx context.Context, db *sql.Conn) (map[int]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("reading schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]string)

	for rows.Next() {
		var (
			version  int
			checksum string
		)

		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("scanning schema_migrations: %w", err)
		}

		applied[version] = checksum
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading schema_migrations: %w", err)
	}

	return applied, nil
}

// apply executes a migration and records it in the same transaction.
func apply(ctx context.Context, db *sql.Conn, m Migration) (err error) {
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF;`); err != nil {
		return fmt.Errorf("disabling foreign keys: %w", err)
	}
	defer func() {
		// Restore connection-local enforcement even when the migration's context was canceled.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, back := db.ExecContext(cleanup, `PRAGMA foreign_keys = ON;`); back != nil {
			_ = db.Raw(func(any) error { return driver.ErrBadConn })
			err = errors.Join(err, fmt.Errorf("enabling foreign keys: %w", back))
		}
	}()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	started := time.Now()

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("executing: %w", err)
	}
	var invalid bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM pragma_foreign_key_check)`).Scan(&invalid); err != nil {
		return fmt.Errorf("checking foreign keys: %w", err)
	}
	if invalid {
		return errors.New("migration violates foreign key constraints")
	}

	elapsed := time.Since(started).Milliseconds()

	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, checksum, applied_at, execution_ms)
		 VALUES (?, ?, ?, ?, ?)`,
		m.Version, m.Name, m.Checksum, time.Now().UnixMilli(), elapsed,
	)
	if err != nil {
		return fmt.Errorf("recording: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing: %w", err)
	}

	return nil
}
