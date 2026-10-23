package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"schemo/internal/domain"
)

// SyncWorkspaces returns all local workspaces and their sync state.
func (s *Store) SyncWorkspaces(ctx context.Context) ([]domain.SyncWorkspace, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT w.id, w.name, w.color, w.kind, w.remote_workspace_id, w.remote_account,
        w.link_pending, ifnull(y.state, 'local'),
        ifnull(y.last_sync_seq, 0), ifnull(y.last_history_seq, 0),
        ifnull(y.last_synced_at, 0), ifnull(y.error, ''), ifnull(y.attempts, 0), w.access_json
   FROM workspaces w
   LEFT JOIN workspace_sync y ON y.workspace_id = w.id
  ORDER BY w.position`)
	if err != nil {
		return nil, fmt.Errorf("reading the workspaces: %w", err)
	}
	defer rows.Close()

	out := []domain.SyncWorkspace{}
	for rows.Next() {
		var workspace domain.SyncWorkspace
		var state, failed, access string
		if err := rows.Scan(&workspace.ID, &workspace.Name, &workspace.Color, &workspace.Kind, &workspace.RemoteID,
			&workspace.RemoteAccount, &workspace.LinkPending, &state, &workspace.Cursor,
			&workspace.HistoryCursor, &workspace.SyncedAt, &failed, &workspace.Attempts, &access); err != nil {
			return nil, fmt.Errorf("reading the workspaces: %w", err)
		}
		workspace.State, workspace.Failed = domain.SyncState(state), domain.Code(failed)
		workspace.ReadOnly = !accessOf(access).Write
		out = append(out, workspace)
	}
	return out, rows.Err()
}

// Link binds a local workspace to a remote server workspace and re-queues state.
func (s *Store) Link(
	ctx context.Context,
	workspaceID string,
	remote domain.RemoteWorkspace,
	account string,
) error {
	return s.inTx(ctx, "linking the workspace", func(tx *writeTx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE workspaces
			   SET remote_workspace_id = ?, version = ?, updated_at = ?, link_pending = 0,
			       name = CASE WHEN name = '' THEN ? ELSE name END,
			       color = CASE WHEN color = '' THEN ? ELSE color END,
			       kind = ?, remote_account = ?, access_json = ?
			 WHERE id = ?`,
			remote.ID, remote.Version, time.Now().UnixMilli(), remote.Name, remote.Colour,
			kindOr(remote.Kind), account, accessData(remote.Access), workspaceID)
		if err != nil {
			return fmt.Errorf("linking workspace %s: %w", workspaceID, err)
		}
		return offerAnew(ctx, tx, workspaceID)
	})
}

// Mirror creates a local representation of a remote workspace.
func (s *Store) Mirror(ctx context.Context, remote domain.RemoteWorkspace, account string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workspaces (id, name, kind, color, position, created_at, updated_at, version,
		                        remote_workspace_id, remote_account, access_json)
		SELECT ?, ?, ?, ?, (SELECT coalesce(max(position), -1) + 1 FROM workspaces), ?, ?, ?, ?,
		       ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM workspaces WHERE remote_workspace_id = ?)`,
		newID(), remote.Name, kindOr(remote.Kind), remote.Colour, now(), now(), remote.Version,
		remote.ID, account, accessData(remote.Access), remote.ID)
	if err != nil {
		return fmt.Errorf("mirroring workspace %s: %w", remote.ID, err)
	}
	return nil
}

// Seen returns the stored user ID for an account key and whether it exists.
func (s *Store) Seen(ctx context.Context, key string) (string, bool, error) {
	return s.Setting(ctx, seenKey(key))
}

// Remember stores the server's user ID for an account key.
func (s *Store) Remember(ctx context.Context, key, userID string) error {
	return s.SaveSetting(ctx, seenKey(key), userID)
}

func seenKey(key string) string { return domain.SettingAccountUser + key }

// Rebind detaches workspaces of an account key and associates them with a new userID.
func (s *Store) Rebind(ctx context.Context, key, userID string) error {
	return s.inTx(ctx, "taking the workspaces back", func(tx *writeTx) error {
		if err := detach(ctx, tx, key); err != nil {
			return err
		}
		return saveSettingIn(ctx, tx, seenKey(key), userID)
	})
}

func detach(ctx context.Context, tx *writeTx, key string) error {
	ids, err := idsOf(ctx, tx,
		`SELECT id FROM workspaces WHERE remote_account = ? AND remote_workspace_id <> ''`, key)
	if err != nil {
		return fmt.Errorf("reading the workspaces of the account: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			UPDATE workspaces
			   SET remote_workspace_id = '', remote_account = '', link_pending = 0, access_json = ''
			 WHERE id = ?`, id); err != nil {
			return fmt.Errorf("detaching workspace %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE workspace_sync
			   SET state = 'local', last_history_seq = 0, error = ''
			 WHERE workspace_id = ?`, id); err != nil {
			return fmt.Errorf("resetting the history cursor of %s: %w", id, err)
		}
		if err := forgetVersions(ctx, tx, id); err != nil {
			return err
		}
		if err := startOver(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

// forgetVersions sets every row of a workspace back to a version nobody has agreed on. The versions
// were the old account's, and a push at one of them to a server holding nothing of the row would be
// a change of a version nobody has; at none, it is a creation.
func forgetVersions(ctx context.Context, tx *writeTx, workspaceID string) error {
	for _, statement := range []string{
		`UPDATE workspaces SET version = ? WHERE id = ?`,
		`UPDATE entities SET version = ? WHERE workspace_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, versionUnagreed, workspaceID); err != nil {
			return fmt.Errorf("forgetting the versions of %s: %w", workspaceID, err)
		}
	}
	return nil
}

// Wipe removes every workspace of an account key with all that stands in them, and forgets the key as
// the one this installation works as, leaving the work that never belonged to an account. A key
// naming nothing erases nothing and is not a failure.
func (s *Store) Wipe(ctx context.Context, key string) (int, error) {
	if key == "" {
		return 0, nil
	}
	let := 0
	err := s.inTx(ctx, "wiping an account's workspaces", func(tx *writeTx) error {
		gone, err := tx.ExecContext(ctx, `DELETE FROM workspaces WHERE remote_account = ?`, key)
		if err != nil {
			return fmt.Errorf("deleting the workspaces of %s: %w", key, err)
		}
		removed, err := gone.RowsAffected()
		if err != nil {
			return fmt.Errorf("deleting the workspaces of %s: %w", key, err)
		}
		let = int(removed)

		// The context points at workspaces that are gone; another account's sign-in sets its own, and an
		// address left here would only make the next reading of it a puzzle.
		var held string
		err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`,
			domain.SettingAccountContext).Scan(&held)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil
		case err != nil:
			return fmt.Errorf("reading the account context: %w", err)
		case held != key:
			return nil
		}
		return saveSettingIn(ctx, tx, domain.SettingAccountContext, "")
	})
	if err != nil {
		return 0, err
	}
	if let > 0 {
		if _, err := s.db.ExecContext(ctx, vacuum); err != nil {
			return 0, fmt.Errorf("vacuuming after wiping %s: %w", key, err)
		}
	}
	return let, nil
}

// Release removes fully synchronized workspaces belonging to an account key.
func (s *Store) Release(ctx context.Context, key string) (int, error) {
	ids, err := s.owedNothing(ctx, key)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := s.DeleteWorkspace(ctx, id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func (s *Store) owedNothing(ctx context.Context, key string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.id FROM workspaces w
		 WHERE w.remote_account = ? AND w.remote_workspace_id <> '' AND w.link_pending = 0
		   AND NOT EXISTS (SELECT 1 FROM sync_queue q WHERE q.workspace_id = w.id)`, key)
	if err != nil {
		return nil, fmt.Errorf("reading the workspaces of %s that owe nothing: %w", key, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("reading the workspaces of %s that owe nothing: %w", key, err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Adopt takes what the account says about a workspace this device is already paired with: what it is
// for and what this person may do in it. Both are the server's answers and arrive together. It
// reports whether the row moved, which is what the windows redraw on.
func (s *Store) Adopt(
	ctx context.Context, workspaceID string, remote domain.RemoteWorkspace,
) (bool, error) {
	kind, access := kindOr(remote.Kind), accessData(remote.Access)
	moved := false
	err := s.inTx(ctx, "keeping workspace "+workspaceID+" in step", func(tx *writeTx) error {
		done, err := tx.ExecContext(ctx,
			`UPDATE workspaces SET kind = ?, access_json = ?
			  WHERE id = ? AND (kind <> ? OR access_json <> ?)`,
			kind, access, workspaceID, kind, access)
		if err != nil {
			return err
		}
		changed, _ := done.RowsAffected()
		moved = changed > 0
		if !moved || accessOf(access).Write {
			return nil
		}
		// A role that no longer writes is a workspace nothing is pushed to: what was queued for it would
		// wait for ever, and the status line would go on saying so.
		_, err = tx.ExecContext(ctx, `DELETE FROM sync_queue WHERE workspace_id = ?`, workspaceID)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("keeping workspace %s in step: %w", workspaceID, err)
	}
	return moved, nil
}

// Discard permanently deletes a workspace and its associated entities.
func (s *Store) Discard(ctx context.Context, workspaceID string) error {
	return s.DeleteWorkspace(ctx, workspaceID)
}

// EnsureOne guarantees that at least one default workspace exists locally.
func (s *Store) EnsureOne(ctx context.Context) error {
	visible, args, err := s.visible(ctx)
	if err != nil {
		return err
	}
	var held int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM workspaces WHERE `+visible,
		args...).Scan(&held); err != nil {
		return fmt.Errorf("reading whether there are workspaces: %w", err)
	}
	if held > 0 {
		return nil
	}

	id := domain.WorkspacePersonalID
	var taken int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM workspaces WHERE id = ?`, id).
		Scan(&taken); err != nil {
		return fmt.Errorf("reading whether the born workspace is here: %w", err)
	}
	if taken > 0 {
		id = newID()
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO workspaces (id, name, kind, color, active_environment_id, position,
		                        created_at, updated_at)
		VALUES (?, '', 'personal', '', '', 0, ?, ?)`, id, now(), now())
	if err != nil {
		return fmt.Errorf("making a workspace to keep things in: %w", err)
	}
	return nil
}

// kindOr validates a workspace kind, returning WorkspacePersonal as fallback.
func kindOr(kind domain.WorkspaceKind) domain.WorkspaceKind {
	if !kind.Valid() {
		return domain.WorkspacePersonal
	}
	return kind
}

// MarkLinkPending records pending workspace remote ID and account assignment.
func (s *Store) MarkLinkPending(ctx context.Context, workspaceID, remoteID, account string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE workspaces SET link_pending = 1, remote_workspace_id = ?, remote_account = ?
		  WHERE id = ?`,
		remoteID, account, workspaceID); err != nil {
		return fmt.Errorf("naming the workspace %s will be made under: %w", workspaceID, err)
	}
	return nil
}

// Holds reports whether a workspace contains active collections or environments.
func (s *Store) Holds(ctx context.Context, workspaceID string) (bool, error) {
	var held int
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM entities WHERE workspace_id = ? AND kind = 'collection'
		                 AND deleted_at IS NULL)
		    OR EXISTS (SELECT 1 FROM entities WHERE workspace_id = ? AND kind = 'environment'
		                 AND deleted_at IS NULL)`,
		workspaceID, workspaceID).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("reading what %s holds: %w", workspaceID, err)
	}
	return held != 0, nil
}
