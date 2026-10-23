package db

import (
	"context"
	"errors"

	"schemo/internal/domain"
)

// storedDraft is a draft as it is kept. Scripts are saved apart from the draft's own fields, by
// SaveScripts, so a draft save keeps whatever scripts are already there.
type storedDraft struct {
	domain.Draft
	Scripts *domain.Scripts `json:"scripts,omitempty"`
}

// Draft ids are only unique within a workspace.
func draftKey(workspaceID string, id domain.DraftID) string { return workspaceID + "/" + string(id) }

func (s *Store) draft(ctx context.Context, q querier, workspaceID string, id domain.DraftID) (storedDraft, error) {
	row, err := getEntity(ctx, q, kindDraft, draftKey(workspaceID, id))
	if err != nil {
		return storedDraft{}, err
	}
	held, err := decodeData[storedDraft](row)
	held.ID = id
	held.BodyKind = domain.KindOf(held.BodyKind)
	return held, err
}

func (s *Store) Draft(ctx context.Context, workspaceID string, id domain.DraftID) (domain.Draft, error) {
	held, err := s.draft(ctx, s.db, workspaceID, id)
	return held.Draft, err
}

func (s *Store) saveDraft(ctx context.Context, tx *writeTx, workspaceID string, held storedDraft) error {
	held.Params, held.Headers = domain.OrEmpty(held.Params), domain.OrEmpty(held.Headers)
	held.PathParams, held.Form = domain.OrEmpty(held.PathParams), domain.OrEmpty(held.Form)
	held.BodyKind = domain.KindOf(held.BodyKind)
	data, err := encodeData(held)
	if err != nil {
		return err
	}
	return putEntity(ctx, tx, entity{Kind: kindDraft, ID: draftKey(workspaceID, held.ID),
		Workspace: workspaceID, Data: data})
}

func (s *Store) SaveDraft(ctx context.Context, workspaceID string, draft domain.Draft) error {
	return s.inTx(ctx, "saving draft "+string(draft.ID), func(tx *writeTx) error {
		held, err := s.draft(ctx, tx, workspaceID, draft.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		return s.saveDraft(ctx, tx, workspaceID, storedDraft{Draft: draft, Scripts: held.Scripts})
	})
}

func (s *Store) DeleteDraft(ctx context.Context, workspaceID string, id domain.DraftID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM entities WHERE id = ?`, draftKey(workspaceID, id))
	return err
}
