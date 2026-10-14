package record

import (
	"context"

	"schemo/internal/domain"
)

// UndoCookieWrites takes back what one answer wrote into the jar. The record is what knows what
// that answer changed — the rows it replaced, the rows it deleted — and the jar is what knows how
// to put them back, so the two meet here rather than in either of them.
//
// Undoing twice is not an error: the second one puts back what the first already did, and a row
// that is already the way the write found it is the state the write asked for.
func (u *UseCase) UndoCookieWrites(ctx context.Context, id string) error {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	rec, err := u.store.Record(ctx, id)
	if err != nil {
		return err
	}
	if len(rec.CookieWrites) == 0 || u.jar == nil {
		return nil
	}
	if err := u.jar.Undo(ctx, workspace, rec.CookieWrites); err != nil {
		return err
	}
	// The record stops claiming what has been taken back. A write that never happened — a blocked
	// domain, an answer this app did not send — stays: it is a fact about that answer, and there is
	// nothing about it to undo.
	kept := make([]domain.CookieWrite, 0, len(rec.CookieWrites))
	for _, write := range rec.CookieWrites {
		if write.State == domain.CookieNotSaved {
			kept = append(kept, write)
		}
	}
	if len(kept) == len(rec.CookieWrites) {
		return nil
	}
	return u.store.SaveCookieWrites(ctx, workspace, id, kept)
}
