package sqlite

import (
	"context"
	"fmt"

	"schemo/internal/domain"
)

// Tabs reads the strip of a workspace: its tabs in their order, and the id of the one on screen. A
// workspace that has none answers an empty list, which is how the strip knows to start fresh.
func (s *Store) Tabs(ctx context.Context, workspaceID string) ([]domain.Tab, string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, ref, draft_id, record_id, pinned, preview, active
		   FROM tabs WHERE workspace_id = ? ORDER BY position`, workspaceID)
	if err != nil {
		return nil, "", fmt.Errorf("reading the tabs of %s: %w", workspaceID, err)
	}
	defer rows.Close()

	var (
		tabs   []domain.Tab
		active string
	)
	for rows.Next() {
		var (
			tab      domain.Tab
			kind     string
			isActive bool
		)
		if err := rows.Scan(&tab.ID, &kind, &tab.Ref, &tab.DraftID, &tab.Record, &tab.Pinned, &tab.Preview,
			&isActive); err != nil {
			return nil, "", fmt.Errorf("reading a tab of %s: %w", workspaceID, err)
		}
		tab.Kind = domain.TabKind(kind)
		if isActive {
			active = tab.ID
		}
		tabs = append(tabs, tab)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("reading the tabs of %s: %w", workspaceID, err)
	}
	return tabs, active, nil
}

// SaveTabs writes the strip whole, in one transaction: a strip is one moment, and half of it stored
// would be a strip the next launch restores wrongly.
func (s *Store) SaveTabs(
	ctx context.Context,
	workspaceID string,
	tabs []domain.Tab,
	active string,
) error {
	return s.inTx(ctx, fmt.Sprintf("writing the tabs of %s", workspaceID), func(tx *writeTx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM tabs WHERE workspace_id = ?`, workspaceID); err != nil {
			return fmt.Errorf("writing the tabs of %s: %w", workspaceID, err)
		}
		for position, tab := range tabs {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO tabs (workspace_id, id, kind, ref, draft_id, record_id, pinned, preview,
			                   position, active)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				workspaceID, tab.ID, string(tab.Kind), tab.Ref, string(tab.DraftID), tab.Record,
				tab.Pinned, tab.Preview, position, tab.ID == active); err != nil {
				return fmt.Errorf("writing tab %s of %s: %w", tab.ID, workspaceID, err)
			}
		}
		return nil
	})
}

// ClearTabs drops the strip of a workspace — what a run with the restore switch off leaves behind.
func (s *Store) ClearTabs(ctx context.Context, workspaceID string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM tabs WHERE workspace_id = ?`, workspaceID); err != nil {
		return fmt.Errorf("clearing the tabs of %s: %w", workspaceID, err)
	}
	return nil
}
