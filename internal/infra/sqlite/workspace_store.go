package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"schemo/internal/domain"
)

// Workspaces are the scope every other table hangs off. The table is small and read whole — the
// switcher draws all of them at once — so there is no query beyond the list and the one row.

func (s *Store) Workspaces(ctx context.Context) ([]domain.Workspace, error) {
	visible, args, err := s.visible(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, kind, color, created_at, updated_at, remote_workspace_id, access_json
		   FROM workspaces WHERE `+visible+` ORDER BY position, created_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("reading workspaces: %w", err)
	}
	defer rows.Close()

	out := []domain.Workspace{}
	for rows.Next() {
		var w domain.Workspace
		var access string
		if err := rows.Scan(&w.ID, &w.Name, &w.Kind, &w.Color, &w.CreatedAt, &w.UpdatedAt,
			&w.RemoteID, &access); err != nil {
			return nil, fmt.Errorf("reading workspaces: %w", err)
		}
		w.Access = accessOf(access)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) Workspace(ctx context.Context, id string) (domain.Workspace, error) {
	visible, args, err := s.visible(ctx)
	if err != nil {
		return domain.Workspace{}, err
	}
	var w domain.Workspace
	var access string
	err = s.db.QueryRowContext(ctx,
		`SELECT id, name, kind, color, created_at, updated_at, remote_workspace_id, access_json
		   FROM workspaces WHERE id = ? AND `+visible, append([]any{id}, args...)...).
		Scan(&w.ID, &w.Name, &w.Kind, &w.Color, &w.CreatedAt, &w.UpdatedAt, &w.RemoteID, &access)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Workspace{}, fmt.Errorf("workspace %s: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("reading workspace %s: %w", id, err)
	}
	w.Access = accessOf(access)
	return w, nil
}

// accessOf reads what the account last said about a space. A column that is empty, or one this
// build cannot read, is a space nobody said anything about, and that is the person's own: the safe
// direction, since the server refuses what it must regardless.
func accessOf(raw string) domain.WorkspaceAccess {
	var held domain.WorkspaceAccess
	if raw == "" || json.Unmarshal([]byte(raw), &held) != nil {
		return domain.OwnAccess()
	}
	return held
}

// accessData is what accessOf reads back. A reading with nothing in it is no reading at all and is
// written as nothing, never as a role that may do none of it: no real answer is empty, since one
// that cannot be read is already taken as the person's own. The type is flat and every field of it
// is one JSON has, so there is nothing to fail on.
func accessData(held domain.WorkspaceAccess) string {
	if held == (domain.WorkspaceAccess{}) {
		return ""
	}
	raw, _ := json.Marshal(held)
	return string(raw)
}

// SaveWorkspace writes a workspace's own row. The position is not in the update list — it is what
// keeps the switcher's order, and a save never moves a row — and everything else is: a space is
// renamed, recoloured and decided about, and each of those is the same write.
//
// What does travel is the row itself — the name, the colour and what the space is for are what
// another device draws — so the write is queued like any other. A space is made here and not on the
// server: the server's own workspace service is how a member joins one, not how it comes to exist.
//
// What does not travel either way is the account's say about this person's place in the space; only
// the round that reads the account's list writes it.
func (s *Store) SaveWorkspace(ctx context.Context, w domain.Workspace) error {
	return s.inTx(ctx, fmt.Sprintf("saving workspace %s", w.ID), func(tx *writeTx) error {
		action, err := actionFor(ctx, tx, "workspaces", w.ID)
		if err != nil {
			return fmt.Errorf("saving workspace %s: %w", w.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workspaces (id, name, kind, color, position, created_at, updated_at, version)
			 VALUES (?, ?, ?, ?, (SELECT coalesce(max(position), -1) + 1 FROM workspaces), ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			   name = excluded.name, kind = excluded.kind, color = excluded.color,
			   updated_at = excluded.updated_at`,
			w.ID, w.Name, string(w.Kind), w.Color, w.CreatedAt, w.UpdatedAt,
			versionUnagreed); err != nil {
			return fmt.Errorf("saving workspace %s: %w", w.ID, err)
		}
		return enqueue(ctx, tx, w.ID, entityWorkspace, w.ID, action)
	})
}

// DeleteWorkspace needs one statement: every table that belongs to a workspace carries the
// foreign key, and the cascade takes history, collections, environments, drafts, script runs and
// the queue of what the space owed the server with it. The vacuum is what gives the space back —
// incremental mode only makes rows reusable, and a workspace is the one thing here big enough to
// be worth handing back.
//
// Nothing is queued: this is the space leaving **this device**, and a row marked gone would be one
// the server never took back. Ending it for the account is the panel's `workspace/v1.Delete`, the
// side that knows who else uses it — see docs/SYNC.md.
func (s *Store) DeleteWorkspace(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, id); err != nil {
		return fmt.Errorf("deleting workspace %s: %w", id, err)
	}
	if _, err := s.db.ExecContext(ctx, vacuum); err != nil {
		return fmt.Errorf("vacuuming after workspace %s: %w", id, err)
	}
	return nil
}

// FirstWorkspace names the workspace that has been there longest: the row the schema writes on a
// fresh installation, and the one every fallback points at now that any row may be deleted. An
// empty table is not a state the app has — the last workspace may not be deleted, which is what
// this error says when it somehow happened.
func (s *Store) FirstWorkspace(ctx context.Context) (string, error) {
	visible, args, err := s.visible(ctx)
	if err != nil {
		return "", err
	}
	var id string
	err = s.db.QueryRowContext(ctx,
		`SELECT id FROM workspaces WHERE `+visible+` ORDER BY position, created_at LIMIT 1`,
		args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("no workspace at all: %w", domain.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("reading the first workspace: %w", err)
	}
	return id, nil
}

// visible limits rows to local spaces and those of the account the device works as.
func (s *Store) visible(ctx context.Context) (string, []any, error) {
	key, err := s.Context(ctx)
	if err != nil {
		return "", nil, err
	}
	if key == "" {
		return "1 = 1", nil, nil
	}
	return "(remote_account = '' OR remote_account = ?)", []any{key}, nil
}

// Context is the key of the account this installation works as, empty until one has signed in.
func (s *Store) Context(ctx context.Context) (string, error) {
	key, _, err := s.Setting(ctx, domain.SettingAccountContext)
	return key, err
}

// SetContext makes an account the one this installation works as.
func (s *Store) SetContext(ctx context.Context, key string) error {
	return s.SaveSetting(ctx, domain.SettingAccountContext, key)
}

// Counts is what each space holds, counted for the two places the window draws it: the switcher's
// rows and the manager's Contents. It is a query over tables this feature does not own, which is
// why it is a reading beside the list and not a column on the row.
//
// Every row of `collections` counts, folders included: the model calls each one a collection, and a
// folder is one that sits inside another. A run is counted through the collection it belongs to —
// `collection_runs` carries no workspace of its own, and the cascade means a run of a deleted
// collection cannot be left behind.
func (s *Store) Counts(ctx context.Context) (map[string]domain.WorkspaceCounts, error) {
	out := map[string]domain.WorkspaceCounts{}
	// One grouped count per kind of thing, each written into its own field: a space with no rows of
	// a kind has no group at all, and the zero it keeps is the right answer for it.
	for _, counted := range []struct {
		query string
		into  func(*domain.WorkspaceCounts) *int
	}{
		// What a space holds is what is still there to be looked at: a deleted collection is not one
		// of its collections, and the counts are drawn beside the name in the switcher.
		{`SELECT workspace_id, count(*) FROM collections
		   WHERE deleted_at IS NULL GROUP BY workspace_id`,
			func(c *domain.WorkspaceCounts) *int { return &c.Collections }},
		{`SELECT workspace_id, count(*) FROM environments
		   WHERE deleted_at IS NULL GROUP BY workspace_id`,
			func(c *domain.WorkspaceCounts) *int { return &c.Environments }},
		{`SELECT c.workspace_id, count(*) FROM collection_runs r
		   JOIN collections c ON c.id = r.collection_id
		  WHERE c.deleted_at IS NULL
		  GROUP BY c.workspace_id`,
			func(c *domain.WorkspaceCounts) *int { return &c.Runs }},
	} {
		rows, err := s.db.QueryContext(ctx, counted.query)
		if err != nil {
			return nil, fmt.Errorf("counting what a workspace holds: %w", err)
		}
		for rows.Next() {
			var (
				id    string
				count int
			)
			if err := rows.Scan(&id, &count); err != nil {
				rows.Close()
				return nil, fmt.Errorf("counting what a workspace holds: %w", err)
			}
			held := out[id]
			*counted.into(&held) = count
			out[id] = held
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("counting what a workspace holds: %w", err)
		}
	}
	return out, nil
}

// ActiveWorkspace is the workspace the window is showing: the stored pointer, or the oldest one
// when the pointer is empty, missing, or names a space that has since been deleted. A stale pointer
// is not an error — the window has to land somewhere, and the space that has been there longest is
// the one the app is most likely to still have.
//
// "Reopen the last workspace" is not read here. It decides where the app *starts*: the use case
// settles it once at launch by pointing the pointer at the oldest space. A preference applied on
// every read would answer for the rest of the session too, and the window would snap back to the
// oldest space the moment anyone switched away from it.
func (s *Store) ActiveWorkspace(ctx context.Context) (string, error) {
	pointed, ok, err := s.Setting(ctx, domain.SettingActiveWorkspace)
	if err != nil {
		return "", err
	}
	if ok && pointed != "" {
		visible, args, err := s.visible(ctx)
		if err != nil {
			return "", err
		}
		var exists int
		err = s.db.QueryRowContext(ctx, `SELECT 1 FROM workspaces WHERE id = ? AND `+visible,
			append([]any{pointed}, args...)...).Scan(&exists)
		if err == nil {
			return pointed, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("reading workspace %s: %w", pointed, err)
		}
	}
	return s.FirstWorkspace(ctx)
}

// ReopenLast answers whether the app comes back to the space it was left in. A setting of the
// installation rather than data of a workspace, which is why it is read here beside the pointer —
// and absent means on, the way it does for every switch the app started with.
func (s *Store) ReopenLast(ctx context.Context) (bool, error) {
	raw, told, err := s.Setting(ctx, domain.SettingReopenWorkspace)
	if err != nil {
		return false, err
	}
	return !told || raw != "false", nil
}

func (s *Store) SetActiveWorkspace(ctx context.Context, id string) error {
	return s.SaveSetting(ctx, domain.SettingActiveWorkspace, id)
}

// activeEnvironment is the environment the workspace is working in. It lives on the workspace's
// own row rather than beside the app's preferences: two spaces are two working environments, and
// a switch that kept one answer would resolve variables against the other space's.
func (s *Store) activeEnvironment(ctx context.Context, workspaceID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT active_environment_id FROM workspaces WHERE id = ?`, workspaceID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("workspace %s: %w", workspaceID, domain.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("reading the environment of %s: %w", workspaceID, err)
	}
	return id, nil
}

// AwaitsFirstPull says whether the space is one of an account's that this device has not read yet:
// it is linked and its cursor has never moved.
func (s *Store) AwaitsFirstPull(ctx context.Context, workspaceID string) (bool, error) {
	var awaiting bool
	err := s.db.QueryRowContext(ctx,
		`SELECT coalesce(w.remote_workspace_id, '') <> '' AND coalesce(s.last_sync_seq, 0) = 0
		   FROM workspaces w LEFT JOIN workspace_sync s ON s.workspace_id = w.id
		  WHERE w.id = ?`, workspaceID).Scan(&awaiting)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading whether %s has been pulled: %w", workspaceID, err)
	}
	return awaiting, nil
}

func (s *Store) SetActiveEnvironment(ctx context.Context, workspaceID, envID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE workspaces SET active_environment_id = ?, updated_at = ? WHERE id = ?`,
		envID, time.Now().UnixMilli(), workspaceID)
	if err != nil {
		return fmt.Errorf("saving the environment of %s: %w", workspaceID, err)
	}
	return nil
}
