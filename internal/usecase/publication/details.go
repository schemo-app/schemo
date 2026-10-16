package publication

import (
	"context"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
	"schemo/internal/domain"
)

func (u *UseCase) Details(ctx context.Context, collectionID string) (domain.PublicationDetails, error) {
	page, known, err := u.store.Publication(ctx, collectionID)
	if err != nil {
		return domain.PublicationDetails{}, err
	}
	if !known {
		return domain.PublicationDetails{}, domain.ErrNotFound
	}
	caller, err := u.caller.Caller(ctx)
	if err != nil {
		return domain.PublicationDetails{}, err
	}
	return u.server.Details(ctx, caller, page.ID)
}

func (u *UseCase) Restore(ctx context.Context, workspace, collectionID string, revision int) (domain.Publication, error) {
	owner, err := u.pageOf(ctx, workspace, collectionID)
	if err != nil {
		return domain.Publication{}, err
	}
	if !owner.Held || !owner.Page.Runnable() {
		return domain.Publication{}, domain.ErrNotFound
	}
	caller, err := u.caller.Caller(ctx)
	if err != nil {
		return domain.Publication{}, err
	}
	page, document, err := u.server.Restore(ctx, caller, owner.Page.ID, revision)
	if err != nil {
		return domain.Publication{}, err
	}
	raw, err := expand(document)
	if err != nil {
		return domain.Publication{}, err
	}
	digest, err := Digest(raw)
	if err != nil {
		return domain.Publication{}, err
	}
	if digest != page.ContentHash {
		return domain.Publication{}, notAPage()
	}
	snapshot, err := snapshotv1.UnmarshalSnapshot(raw)
	if err != nil {
		return domain.Publication{}, notAPage()
	}
	outline, err := outlineOf(snapshot)
	if err != nil {
		return domain.Publication{}, err
	}
	page.WorkspaceID = workspace
	if err := u.store.SavePublication(ctx, page); err != nil {
		return domain.Publication{}, err
	}
	options := domain.PublicationOptions{Docs: snapshot.Options.Docs, Examples: snapshot.Options.Examples}
	if err := u.store.SavePublished(ctx, page.CollectionID, outline, storedChanges(snapshot.Changes), options); err != nil {
		return domain.Publication{}, err
	}
	return page, nil
}
