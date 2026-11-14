package cloud

import (
	"context"
	"errors"
	"schemo/internal/domain"
	"time"
)

// SignOut ends the session and tells the server. With `wipe` it also deletes the account's workspaces
// here; work that never belonged to an account stays.
func (s *Account) SignOut(ctx context.Context, wipe bool) (AccountStatus, error) {
	s.cancelPendingSignIn()
	revision := s.currentRevision()
	stored, err := s.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return AccountStatus{}, err
	}

	if stored.SignedIn() {
		if access := s.cachedAccessToken(stored); access != "" {
			revoke, cancel := context.WithTimeout(ctx, revokeTimeout)
			_ = s.server.SignOut(revoke, serverAddressOrDefault(stored.Server), access)
			cancel()
		}
	}

	if err := s.forgetHere(ctx, stored, revision, wipe); err != nil {
		return AccountStatus{}, err
	}
	return s.publishStatus(ctx)
}

// Delete purges the account on the server and, only if that succeeds, wipes its workspaces here.
// It cannot be undone.
func (s *Account) Delete(ctx context.Context) (AccountStatus, error) {
	stored, access, err := s.requireSignedIn(ctx)
	if err != nil {
		return AccountStatus{}, err
	}
	revision := s.currentRevision()
	if err := s.server.DeleteAccount(ctx, serverAddressOrDefault(stored.Server), access); err != nil {
		return AccountStatus{}, err
	}
	if err := s.forgetHere(ctx, stored, revision, true); err != nil {
		return AccountStatus{}, err
	}
	return s.publishStatus(ctx)
}

// revokeTimeout bounds the courtesy revoke on sign-out; signing out never waits on the server.
const revokeTimeout = 3 * time.Second

// forgetHere drops the account from this device, unless another sign-in replaced the session
// meanwhile. With wipe its workspaces go first, since their key lives in the account row.
func (s *Account) forgetHere(ctx context.Context, stored domain.Account, revision uint64, wipe bool) error {
	s.session.Lock()
	defer s.session.Unlock()
	if s.revision != revision || (stored.SignedIn() && !s.isCurrentSession(ctx, stored, revision)) {
		return nil
	}
	if wipe {
		if err := s.eraseLocalAccount(ctx, stored); err != nil {
			return err
		}
	}
	return s.forgetAccountLocked(ctx, stored.Server)
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
