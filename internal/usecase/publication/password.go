package publication

import (
	"context"
	"errors"

	"schemo/internal/domain"
)

func (u *UseCase) passwordOwner(ctx context.Context, collection string) (domain.Account, domain.Publication, error) {
	account, err := u.caller.Me(ctx)
	if err != nil {
		return domain.Account{}, domain.Publication{}, err
	}
	page, known, err := u.store.Publication(ctx, collection)
	if err != nil {
		return domain.Account{}, domain.Publication{}, err
	}
	if !account.SignedIn() || !known || !page.CanManage || !page.Runnable() {
		return domain.Account{}, domain.Publication{}, domain.ErrNotAllowed
	}
	return account, page, nil
}

// Password explicitly reads a password saved on this device, outside normal publication DTOs.
func (u *UseCase) Password(ctx context.Context, collection string) (string, error) {
	account, page, err := u.passwordOwner(ctx, collection)
	if err != nil {
		return "", err
	}
	if page.Visibility != domain.VisibilityPassword {
		return "", nil
	}
	password, err := u.passwords.PublicationPassword(account, page.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return "", nil
	}
	return password, err
}

// RememberPassword saves a successfully published password without a database fallback.
func (u *UseCase) RememberPassword(ctx context.Context, collection, password, sessionID string) error {
	account, page, err := u.passwordOwner(ctx, collection)
	if err != nil {
		return err
	}
	if account.SessionID != sessionID {
		return domain.ErrNotAllowed
	}
	if page.Visibility != domain.VisibilityPassword {
		return u.passwords.DeletePublicationPassword(account, page.ID)
	}
	if password == "" {
		return nil
	}
	if err := passwordFits(password); err != nil {
		return err
	}
	return u.passwords.SetPublicationPassword(account, page.ID, password)
}
