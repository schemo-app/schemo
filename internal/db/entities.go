package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// kindDraft is a request being composed in a tab. It never travels.
const kindDraft domain.EntityType = "draft"

// entity is one row of the entities table. Everything but the columns lives in Data.
type entity struct {
	Kind      domain.EntityType
	ID        string
	Workspace string
	Parent    string
	Position  int64
	Name      string
	Key       string
	Data      []byte
	Version   int64
	CreatedAt int64
	UpdatedAt int64
	Deleted   bool
}

// keepsParent lists the kinds whose parent is changed only by a move, never by a save: a rename
// saves the row it read, and that row may be older than a move made since.
var keepsParent = map[domain.EntityType]bool{
	domain.EntityCollection: true,
	domain.EntityRequest:    true,
}

func synced(kind domain.EntityType) bool { return kind != kindDraft && kind != kindStrip }

// naturalKey joins the parts of a key that must be unique among live rows of one kind.
func naturalKey(parts ...string) string { return strings.Join(parts, "\x1f") }

func encodeData(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encoding: %w", err)
	}
	return data, nil
}

func decodeData[T any](e entity) (T, error) {
	var v T
	if err := json.Unmarshal(e.Data, &v); err != nil {
		return v, fmt.Errorf("reading %s %s: %w", e.Kind, e.ID, err)
	}
	return v, nil
}

// putEntity writes a row made on this device and owes the server the change.
func putEntity(ctx context.Context, tx *writeTx, e entity) error {
	if held, err := getEntity(ctx, tx, e.Kind, e.ID); err == nil && sameEntity(held, e) {
		// A save that changes nothing owes the server nothing and tells nobody.
		return nil
	}
	action, err := actionFor(ctx, tx, "entities", e.ID)
	if err != nil {
		return fmt.Errorf("saving %s %s: %w", e.Kind, e.ID, err)
	}
	if e.UpdatedAt == 0 {
		e.UpdatedAt = time.Now().UnixMilli()
	}
	if e.CreatedAt == 0 {
		e.CreatedAt = e.UpdatedAt
	}
	parent := "parent_id = excluded.parent_id,"
	if keepsParent[e.Kind] {
		parent = ""
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO entities (id, workspace_id, kind, parent_id, position, name, key, data, version,
		                      created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET `+parent+`
		  position = excluded.position, name = excluded.name, key = excluded.key,
		  data = excluded.data, updated_at = excluded.updated_at, deleted_at = NULL`,
		e.ID, e.Workspace, e.Kind, e.Parent, e.Position, e.Name, e.Key, string(e.Data),
		versionUnagreed, e.CreatedAt, e.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%s %q: %w", e.Kind, e.Name, domain.ErrConflict)
		}
		return fmt.Errorf("saving %s %s: %w", e.Kind, e.ID, err)
	}
	tx.touched(e.Kind, e.Workspace)
	if !synced(e.Kind) {
		return nil
	}
	return enqueue(ctx, tx, e.Workspace, e.Kind, e.ID, action)
}

// sameEntity reports whether a save would leave a live row as it is.
func sameEntity(held, e entity) bool {
	return !held.Deleted && held.Workspace == e.Workspace &&
		(held.Parent == e.Parent || keepsParent[e.Kind]) && held.Position == e.Position &&
		held.Name == e.Name && held.Key == e.Key && string(held.Data) == string(e.Data)
}

// putRemote writes a row the server sent. It owes nothing back.
func putRemote(ctx context.Context, tx *writeTx, e entity) error {
	// A live row of this device with the same natural key but another id is the same thing made
	// twice; the server's copy wins.
	if e.Key != "" {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM entities WHERE workspace_id = ? AND kind = ? AND key = ? AND id <> ?`,
			e.Workspace, e.Kind, e.Key, e.ID); err != nil {
			return fmt.Errorf("making room for %s %s: %w", e.Kind, e.ID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO entities (id, workspace_id, kind, parent_id, position, name, key, data, version,
		                      created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  parent_id = excluded.parent_id, position = excluded.position, name = excluded.name,
		  key = excluded.key, data = excluded.data, version = excluded.version,
		  updated_at = excluded.updated_at, deleted_at = NULL`,
		e.ID, e.Workspace, e.Kind, e.Parent, e.Position, e.Name, e.Key, string(e.Data),
		e.Version, e.CreatedAt, e.UpdatedAt); err != nil {
		return fmt.Errorf("writing %s %s: %w", e.Kind, e.ID, err)
	}
	tx.touched(e.Kind, e.Workspace)
	return nil
}

// deleteEntities marks live rows gone and owes the server one deletion per row, in the order given.
func deleteEntities(ctx context.Context, tx *writeTx, workspace string, rows []entity) error {
	now := time.Now().UnixMilli()
	for _, row := range rows {
		res, err := tx.ExecContext(ctx,
			`UPDATE entities SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
			now, now, row.ID)
		if err != nil {
			return fmt.Errorf("deleting %s %s: %w", row.Kind, row.ID, err)
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("deleting %s %s: %w", row.Kind, row.ID, err)
		}
		if changed == 0 {
			continue
		}
		tx.touched(row.Kind, workspace)
		if !synced(row.Kind) {
			continue
		}
		if err := enqueue(ctx, tx, workspace, row.Kind, row.ID, actionDelete); err != nil {
			return err
		}
	}
	return nil
}

const entityColumns = `kind, id, workspace_id, parent_id, position, name, key, data, version,
	created_at, updated_at, deleted_at IS NOT NULL`

func scanEntity(row interface{ Scan(...any) error }) (entity, error) {
	var (
		e    entity
		data string
	)
	err := row.Scan(&e.Kind, &e.ID, &e.Workspace, &e.Parent, &e.Position, &e.Name, &e.Key, &data,
		&e.Version, &e.CreatedAt, &e.UpdatedAt, &e.Deleted)
	e.Data = []byte(data)
	return e, err
}

// getEntity reads one live row of a kind.
func getEntity(ctx context.Context, q querier, kind domain.EntityType, id string) (entity, error) {
	e, err := scanEntity(q.QueryRowContext(ctx,
		`SELECT `+entityColumns+` FROM entities WHERE id = ? AND kind = ? AND deleted_at IS NULL`,
		id, kind))
	if errors.Is(err, sql.ErrNoRows) {
		return entity{}, fmt.Errorf("%s %s: %w", kind, id, domain.ErrNotFound)
	}
	if err != nil {
		return entity{}, fmt.Errorf("reading %s %s: %w", kind, id, err)
	}
	return e, nil
}

// anyEntity reads a row whether or not it is deleted: what the sync engine sends.
func anyEntity(ctx context.Context, q querier, id string) (entity, error) {
	return scanEntity(q.QueryRowContext(ctx, `SELECT `+entityColumns+` FROM entities WHERE id = ?`, id))
}

// listEntities reads the live rows of some kinds in a workspace, ordered by parent and position.
// extra narrows the rows with a condition on the columns.
func listEntities(
	ctx context.Context,
	q querier,
	workspace string,
	kinds []domain.EntityType,
	extra string,
	args ...any,
) ([]entity, error) {
	query := `SELECT ` + entityColumns + ` FROM entities
		WHERE workspace_id = ? AND deleted_at IS NULL AND kind IN (` + placeholders(len(kinds)) + `)`
	all := []any{workspace}
	for _, kind := range kinds {
		all = append(all, kind)
	}
	if extra != "" {
		query += ` AND (` + extra + `)`
		all = append(all, args...)
	}
	rows, err := q.QueryContext(ctx, query+` ORDER BY parent_id, position, created_at, id`, all...)
	if err != nil {
		return nil, fmt.Errorf("listing %v: %w", kinds, err)
	}
	defer rows.Close()
	out := []entity{}
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, fmt.Errorf("listing %v: %w", kinds, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func kinds(k ...domain.EntityType) []domain.EntityType { return k }

var newID = platform.NewIDGen()
