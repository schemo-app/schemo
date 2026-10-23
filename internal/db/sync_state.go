package db

import (
	"context"
	"database/sql"
	"fmt"

	"schemo/internal/domain"
)

// RecordRound updates workspace_sync record after completing a sync round.
func (s *Store) RecordRound(
	ctx context.Context,
	workspaceID string,
	state domain.SyncState,
	at int64,
	failed domain.Code,
	attempts int,
) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workspace_sync (workspace_id, state, last_synced_at, error, attempts)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(workspace_id) DO UPDATE SET
		   state = excluded.state,
		   last_synced_at = CASE WHEN excluded.last_synced_at = 0
		                         THEN workspace_sync.last_synced_at
		                         ELSE excluded.last_synced_at END,
		   error = excluded.error, attempts = excluded.attempts`,
		workspaceID, string(state), at, string(failed), attempts)
	if err != nil {
		return fmt.Errorf("recording where %s stands: %w", workspaceID, err)
	}
	return nil
}

// SetState updates workspace sync state column in workspace_sync.
func (s *Store) SetState(ctx context.Context, workspaceID string, state domain.SyncState) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workspace_sync (workspace_id, state) VALUES (?, ?)
		 ON CONFLICT(workspace_id) DO UPDATE SET state = excluded.state`, workspaceID, string(state))
	if err != nil {
		return fmt.Errorf("recording the state of %s: %w", workspaceID, err)
	}
	return nil
}

// Reset clears workspace queue state and triggers full re-synchronization.
func (s *Store) Reset(ctx context.Context, workspaceID string) error {
	return s.inTx(ctx, "starting the workspace over", func(tx *writeTx) error {
		return offerAnew(ctx, tx, workspaceID)
	})
}

// offerAnew resets queue state and enqueues all local workspace entities. A workspace this person may
// not write in is started over and offered nothing, since the round does not push there and the
// queue would wait for ever. A role given write access later is read as an access change, which the
// server answers with a resync that comes here again.
func offerAnew(ctx context.Context, tx *writeTx, workspaceID string) error {
	if err := startOver(ctx, tx, workspaceID); err != nil {
		return err
	}
	var raw string
	err := tx.QueryRowContext(ctx,
		`SELECT access_json FROM workspaces WHERE id = ?`, workspaceID).Scan(&raw)
	if err != nil {
		return fmt.Errorf("reading the access to %s: %w", workspaceID, err)
	}
	if !accessOf(raw).Write {
		return nil
	}
	if err := offerEverything(ctx, tx, workspaceID); err != nil {
		return err
	}
	// What was just offered is the whole workspace as it stands, not somebody's unsettled work, so a pull
	// may still rewrite those rows: the read from the beginning is how a role change reaches them.
	if _, err := tx.ExecContext(ctx,
		`UPDATE sync_queue SET offered = 1 WHERE workspace_id = ? AND entity_type NOT IN (?, ?)`,
		workspaceID, entityRecord, entityRun); err != nil {
		return fmt.Errorf("marking what was offered anew in %s: %w", workspaceID, err)
	}
	return nil
}

// offerEverything enqueues creation/deletion actions for workspace entities.
func offerEverything(ctx context.Context, tx *writeTx, workspaceID string) error {
	for _, kind := range domain.EntityTypes {
		if kind == domain.EntityWorkspace || domain.ThroughHistory(kind) {
			continue
		}
		action := actionCreate
		if kind == domain.EntityCookie || kind == domain.EntityJarDomain {
			action = actionUpdate
		}
		for _, fate := range []struct {
			where  string
			action domain.Action
		}{
			{` AND deleted_at IS NULL`, action},
			{` AND deleted_at IS NOT NULL`, actionDelete},
		} {
			ids, err := idsOf(ctx, tx, `SELECT id FROM entities WHERE workspace_id = ? AND kind = ?`+
				fate.where+` ORDER BY parent_id, position, id`, workspaceID, kind)
			if err != nil {
				return fmt.Errorf("offering the %s rows of %s: %w", kind, workspaceID, err)
			}
			if err := queueAll(ctx, tx, workspaceID, kind, fate.action, ids); err != nil {
				return err
			}
		}
	}
	return enqueue(ctx, tx, workspaceID, entityWorkspace, workspaceID, actionUpdate)
}

// forget removes entry from sync_queue by ID.
func (s *Store) forget(ctx context.Context, q execer, queueID string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM sync_queue WHERE id = ?`, queueID); err != nil {
		return fmt.Errorf("dropping an entry from the queue: %w", err)
	}
	return nil
}

// startOver clears sync_queue entries and resets the workspace sync sequence.
//
// Records and runs are not the queue of a workspace's contents: they are this device's own history,
// were not made again by starting over, and the server will not offer them back. They stay.
func startOver(ctx context.Context, tx *writeTx, workspaceID string) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sync_queue WHERE workspace_id = ? AND entity_type NOT IN (?, ?)`,
		workspaceID, entityRecord, entityRun); err != nil {
		return fmt.Errorf("clearing the queue of %s: %w", workspaceID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE workspace_sync SET last_sync_seq = 0, attempts = 0 WHERE workspace_id = ?`,
		workspaceID); err != nil {
		return fmt.Errorf("resetting the cursor of %s: %w", workspaceID, err)
	}
	return nil
}

// agreed records updated server version for local entities.
func agreed(
	ctx context.Context,
	tx *writeTx,
	workspaceID string,
	kind domain.EntityType,
	entityID string,
	version int32,
) error {
	if domain.ThroughHistory(kind) {
		// A record and a run carry no version: history is a log, and what it says about one of them
		// is whether it was filed and not which version of it won.
		return nil
	}
	if kind == domain.EntityWorkspace {
		_, err := tx.ExecContext(ctx, `UPDATE workspaces SET version = ? WHERE id = ?`,
			version, workspaceID)
		if err != nil {
			return fmt.Errorf("recording the version of %s: %w", workspaceID, err)
		}
		return nil
	}

	_, err := tx.ExecContext(ctx, `UPDATE entities SET version = ? WHERE id = ?`, version, entityID)
	if err != nil {
		return fmt.Errorf("recording the version of %s: %w", entityID, err)
	}
	return nil
}

// execer defines DB execution interface for *sql.DB and *writeTx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// nullIfZero64 returns nil if value is zero, else value.
func nullIfZero64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}
