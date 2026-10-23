package cloud

import (
	"context"
	"errors"
	"schemo/internal/domain"
	"time"
)

// SignOut revokes the local session and attempts to notify the remote server. With `wipe` it also
// deletes what this device holds of the account: the workspaces that carry its key, with everything in
// them. Work that never belonged to an account is left alone.
func (s *Account) SignOut(ctx context.Context, wipe bool) (AccountStatus, error) {
	s.cancelPendingSignIn()
	revision := s.currentRevision()
	stored, err := s.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return AccountStatus{}, err
	}

	if stored.SignedIn() {
		if access := s.cachedAccessToken(stored); access != "" {
			revoke, cancel := context.WithTimeout(ctx, 3*time.Second)
			_ = s.server.SignOut(revoke, serverAddressOrDefault(stored.Server), access)
			cancel()
		}
	}

	s.session.Lock()
	if s.revision != revision || (stored.SignedIn() && !s.isCurrentSession(ctx, stored, revision)) {
		s.session.Unlock()
		return s.publishStatus(ctx)
	}
	// The workspaces go before the account row does: the key they are found by lives in that row.
	if wipe {
		if err := s.eraseLocalAccount(ctx, stored); err != nil {
			s.session.Unlock()
			return AccountStatus{}, err
		}
	}

	if err := s.forgetAccountLocked(ctx, stored.Server); err != nil {
		s.session.Unlock()
		return AccountStatus{}, err
	}
	s.session.Unlock()
	return s.publishStatus(ctx)
}

// Delete purges the account and forgets it here. The server is asked first and its answer decides
// whether anything happened: a refusal leaves this device exactly as it was, still signed in. Only
// an account that is really gone takes this device's copy with it.
//
// It cannot be undone and takes the same as a signing out that wipes: the workspaces tagged with the
// account's key, with everything inside them. Keeping them would keep somebody's work for an
// account that no longer exists.
func (s *Account) Delete(ctx context.Context) (AccountStatus, error) {
	stored, access, err := s.requireSignedIn(ctx)
	if err != nil {
		return AccountStatus{}, err
	}
	revision := s.currentRevision()
	if err := s.server.DeleteAccount(ctx, serverAddressOrDefault(stored.Server), access); err != nil {
		return AccountStatus{}, err
	}
	s.session.Lock()
	if !s.isCurrentSession(ctx, stored, revision) {
		s.session.Unlock()
		return s.publishStatus(ctx)
	}

	// The workspaces go before the account row does: the key they are found by lives in that row.
	if err := s.eraseLocalAccount(ctx, stored); err != nil {
		s.session.Unlock()
		return AccountStatus{}, err
	}
	if err := s.forgetAccountLocked(ctx, stored.Server); err != nil {
		s.session.Unlock()
		return AccountStatus{}, err
	}
	s.session.Unlock()
	return s.publishStatus(ctx)
}

// eraseLocalAccount removes the account's own workspaces. A device that was never signed in has none,
// and an account without an address is not one to find them by.
func (s *Account) eraseLocalAccount(ctx context.Context, stored domain.Account) error {
	if stored.Email == "" {
		return nil
	}
	_, err := s.store.Wipe(ctx, domain.AccountKey(serverAddressOrDefault(stored.Server), stored.Email))
	return err
}
