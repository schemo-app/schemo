package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/schemo-app/proto/secret"

	"schemo/internal/domain"
)

// ApplyPage applies a page of remote changes and advances the sync cursor in a single transaction.
func (s *Store) ApplyPage(
	ctx context.Context,
	workspaceID string,
	changes []domain.SyncChange,
	cursor int64,
) (int, error) {
	skipped := 0

	err := s.inTx(ctx, "applying what the server sent", func(tx *writeTx) error {
		left, err := unsettledRows(ctx, tx, workspaceID)
		if err != nil {
			return err
		}
		for _, change := range changes {
			if left.holds(workspaceID, change.Entity) {
				continue
			}
			if _, err := tx.ExecContext(ctx, `SAVEPOINT row`); err != nil {
				return fmt.Errorf("applying %s: %w", change.Entity.ID, err)
			}
			if err := applyEntity(ctx, tx, workspaceID, change.Entity); err != nil {
				if _, err := tx.ExecContext(ctx, `ROLLBACK TO row`); err != nil {
					return fmt.Errorf("applying %s: %w", change.Entity.ID, err)
				}
				skipped++
			}
			if _, err := tx.ExecContext(ctx, `RELEASE row`); err != nil {
				return fmt.Errorf("applying %s: %w", change.Entity.ID, err)
			}
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workspace_sync (workspace_id, last_sync_seq) VALUES (?, ?)
			 ON CONFLICT(workspace_id) DO UPDATE
			   SET last_sync_seq = max(workspace_sync.last_sync_seq, excluded.last_sync_seq)`,
			workspaceID, cursor); err != nil {
			return fmt.Errorf("moving the cursor of %s: %w", workspaceID, err)
		}
		return nil
	})
	return skipped, err
}

// unsettled is the rows of a workspace that the device holds a change of the server has not settled.
type unsettled map[[2]string]struct{}

// holds reports whether the row a pulled entity is about is one of them. The workspace's own row is
// queued under its local id, which is the one a page is applied under.
func (u unsettled) holds(workspaceID string, entity domain.SyncEntity) bool {
	id := entity.ID
	if entity.Kind == domain.EntityWorkspace {
		id = workspaceID
	}
	_, held := u[[2]string{string(entity.Kind), id}]
	return held
}

// unsettledRows reads the rows a pull must leave alone: those with an edit queued. Parked, re-offered
// and read-only workspace entries do not count.
func unsettledRows(ctx context.Context, tx *writeTx, workspaceID string) (unsettled, error) {
	var access string
	err := tx.QueryRowContext(ctx,
		`SELECT access_json FROM workspaces WHERE id = ?`, workspaceID).Scan(&access)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("reading the access to %s: %w", workspaceID, err)
	}
	held := unsettled{}
	if err == nil && !accessOf(access).Write {
		return held, nil
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT entity_type, entity_id FROM sync_queue
		  WHERE workspace_id = ? AND status <> 'parked' AND offered = 0
		    AND entity_type NOT IN (?, ?)`, workspaceID, entityRecord, entityRun)
	if err != nil {
		return nil, fmt.Errorf("reading what %s still owes: %w", workspaceID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id string
		if err := rows.Scan(&kind, &id); err != nil {
			return nil, fmt.Errorf("reading what %s still owes: %w", workspaceID, err)
		}
		held[[2]string{kind, id}] = struct{}{}
	}
	return held, rows.Err()
}

// applyEntity writes one entity the server sent.
func applyEntity(ctx context.Context, tx *writeTx, workspaceID string, in domain.SyncEntity) error {
	switch in.Kind {
	case domain.EntityWorkspace:
		return applyWorkspace(ctx, tx, workspaceID, in)
	case domain.EntityRecord:
		return nil
	}
	spec, synced := syncKinds[in.Kind]
	if !synced {
		return fmt.Errorf("applying a %s is not a thing this device does", in.Kind)
	}
	if in.Deleted {
		_, err := tx.ExecContext(ctx,
			`UPDATE entities SET version = ?, deleted_at = ?, updated_at = ? WHERE id = ?`,
			in.Version, now(), in.UpdatedAt, in.ID)
		if err != nil {
			return fmt.Errorf("deleting %s %s: %w", in.Kind, in.ID, err)
		}
		tx.touched(in.Kind, workspaceID)
		return nil
	}
	row := entity{Kind: in.Kind, ID: in.ID, Workspace: workspaceID, Parent: in.Parent,
		Name: in.Name, Position: in.Position, Data: in.Data,
		Version: int64(in.Version), CreatedAt: in.CreatedAt, UpdatedAt: in.UpdatedAt}
	row.Key = keyOf(row)
	if len(row.Data) == 0 {
		row.Data = []byte("{}")
	}
	if len(in.Withheld) > 0 {
		data, err := secret.Restore(row.Data, in.Withheld, heldData(ctx, tx, row))
		if err != nil {
			return fmt.Errorf("reading %s %s: %w", in.Kind, in.ID, err)
		}
		row.Data = data
	}
	if spec.parentRequired && row.Parent == "" {
		return fmt.Errorf("applying %s %s: missing parent", in.Kind, in.ID)
	}
	// Editing a parent moves it later in the server log, possibly onto a later page.
	// Keep its children now so advancing the cursor cannot lose them.
	return putRemote(ctx, tx, row)
}

func applyWorkspace(
	ctx context.Context,
	tx *writeTx,
	workspaceID string,
	entity domain.SyncEntity,
) error {
	if entity.Deleted {
		return nil
	}

	if entity.Name == "" && len(entity.Data) == 0 {
		_, err := tx.ExecContext(ctx,
			`UPDATE workspaces SET version = ?, updated_at = ? WHERE id = ?`,
			entity.Version, entity.UpdatedAt, workspaceID)
		if err != nil {
			return fmt.Errorf("moving the workspace to the server's version: %w", err)
		}
		return nil
	}
	var data struct {
		Color string `json:"color"`
	}
	if len(entity.Data) > 0 {
		if err := json.Unmarshal(entity.Data, &data); err != nil {
			return fmt.Errorf("reading the workspace the server sent: %w", err)
		}
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE workspaces SET name = ?, color = ?, version = ?, updated_at = ? WHERE id = ?`,
		entity.Name, data.Color, entity.Version, entity.UpdatedAt, workspaceID)
	if err != nil {
		return fmt.Errorf("writing the workspace the server sent: %w", err)
	}
	return nil
}

func now() int64 { return time.Now().UnixMilli() }
