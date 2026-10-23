package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"schemo/internal/domain"
)

// Constants mapping domain entity types and action constants to local package aliases.
const (
	entityWorkspace   = domain.EntityWorkspace
	entityCollection  = domain.EntityCollection
	entityRequest     = domain.EntityRequest
	entityEnvironment = domain.EntityEnvironment
	entityVariable    = domain.EntityVariable
	entityCookie      = domain.EntityCookie
	entityJarDomain   = domain.EntityJarDomain
	entityRecord      = domain.EntityRecord
	entityRun         = domain.EntityRun

	actionCreate = domain.ActionCreate
	actionUpdate = domain.ActionUpdate
	actionDelete = domain.ActionDelete
)

// enqueue inserts or updates an outbox sync queue entry within an active database transaction.
func enqueue(
	ctx context.Context,
	tx *writeTx,
	workspaceID string,
	entityType domain.EntityType,
	entityID string,
	action domain.Action,
) error {
	workspace, err := rightsIn(ctx, tx, workspaceID)
	if err != nil {
		return err
	}
	switch {
	case domain.ThroughHistory(entityType):
		// History travels from the moment a workspace is linked: a link that uploaded a year of
		// captured traffic would upload gigabytes. A workspace this person may not write in has no door
		// for it, since a queue nothing drains only grows.
		if !workspace.linked || !workspace.access.Write {
			return nil
		}
	case !workspace.mayChange(entityType):
		// The one place every write of shared content passes: a workspace this person may only read
		// refuses it, and the row it was written to goes back with the transaction.
		return domain.Refuse(domain.CodeWorkspaceReadOnly, domain.ErrNotAllowed, nil)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO sync_queue (id, workspace_id, entity_type, entity_id, action, operation_id,
                         created_at)
		 VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, lower(hex(randomblob(16))), ?)
		 ON CONFLICT(workspace_id, entity_type, entity_id) WHERE status <> 'sending'
		 DO UPDATE SET
		   action = CASE
		     WHEN sync_queue.action = 'delete' OR excluded.action = 'delete' THEN 'delete'
		     WHEN sync_queue.action = 'create' OR excluded.action = 'create' THEN 'create'
		     ELSE 'update' END,
		   operation_id = excluded.operation_id,
		   status = 'pending',
		   reason = '',
		   retry_count = 0,
		   next_retry_at = NULL,
		   offered = 0`,
		workspaceID, entityType, entityID, action, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("queueing %s %s: %w", entityType, entityID, err)
	}
	tx.workspaces[workspaceID] = struct{}{}
	return nil
}

// workspaceRights is what this device knows of a workspace's place in an account and of the right to
// write in it. A row that is not there is the person's own, and so is any workspace nobody described.
type workspaceRights struct {
	linked bool
	access domain.WorkspaceAccess
}

// mayChange reports whether a row of the given kind may be written: the workspace's own row is judged
// by the right to change the workspace, everything else by the right to write in it.
func (r workspaceRights) mayChange(kind domain.EntityType) bool {
	if kind == domain.EntityWorkspace {
		return r.access.Update
	}
	return r.access.Write
}

func rightsIn(ctx context.Context, tx *writeTx, workspaceID string) (workspaceRights, error) {
	var remote, access string
	err := tx.QueryRowContext(ctx,
		`SELECT remote_workspace_id, access_json FROM workspaces WHERE id = ?`, workspaceID).
		Scan(&remote, &access)
	if errors.Is(err, sql.ErrNoRows) {
		return workspaceRights{access: domain.OwnAccess()}, nil
	}
	if err != nil {
		return workspaceRights{}, fmt.Errorf("reading the link of %s: %w", workspaceID, err)
	}
	return workspaceRights{linked: remote != "", access: accessOf(access)}, nil
}

// actionFor determines whether a target row exists to select between create and update actions.
func actionFor(ctx context.Context, tx *writeTx, table, id string) (domain.Action, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ?`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return actionCreate, nil
	}
	if err != nil {
		return "", fmt.Errorf("reading whether %s is already here: %w", id, err)
	}
	return actionUpdate, nil
}

// queueAll enqueues sync outbox entries for multiple entity IDs under a single transaction.
func queueAll(
	ctx context.Context,
	tx *writeTx,
	workspaceID string,
	entityType domain.EntityType,
	action domain.Action,
	ids []string,
) error {
	for _, id := range ids {
		if err := enqueue(ctx, tx, workspaceID, entityType, id, action); err != nil {
			return err
		}
	}
	return nil
}

// idsOf executes a single-column SQL query and returns the results as a slice of ID strings.
func idsOf(ctx context.Context, tx *writeTx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
