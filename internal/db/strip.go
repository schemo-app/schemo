package db

import (
	"context"
	"errors"

	"schemo/internal/domain"
)

// kindStrip is the window's strip of one workspace. Like a draft it never travels.
const kindStrip domain.EntityType = "strip"

func stripID(workspaceID string) string { return "strip/" + workspaceID }

// Strip reads the strip of a workspace. One that was never saved is empty.
func (s *Store) Strip(ctx context.Context, workspaceID string) (domain.Strip, error) {
	out := domain.Strip{Workspace: workspaceID, Tabs: []domain.Tab{}}
	row, err := getEntity(ctx, s.db, kindStrip, stripID(workspaceID))
	if errors.Is(err, domain.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	held, err := decodeData[domain.Strip](row)
	if err != nil {
		return out, err
	}
	held.Workspace = workspaceID
	held.Tabs = domain.OrEmpty(held.Tabs)
	return held, nil
}

// SaveStrip writes the strip of a workspace whole.
func (s *Store) SaveStrip(ctx context.Context, strip domain.Strip) error {
	data, err := encodeData(strip)
	if err != nil {
		return err
	}
	return s.inTx(ctx, "saving the tabs", func(tx *writeTx) error {
		return putEntity(ctx, tx, entity{Kind: kindStrip, ID: stripID(strip.Workspace),
			Workspace: strip.Workspace, Data: data})
	})
}

// DraftIDs is every draft a workspace keeps.
func (s *Store) DraftIDs(ctx context.Context, workspaceID string) ([]domain.DraftID, error) {
	rows, err := listEntities(ctx, s.db, workspaceID, kinds(kindDraft), "")
	if err != nil {
		return nil, err
	}
	out := make([]domain.DraftID, 0, len(rows))
	prefix := len(draftKey(workspaceID, ""))
	for _, row := range rows {
		out = append(out, domain.DraftID(row.ID[prefix:]))
	}
	return out, nil
}
