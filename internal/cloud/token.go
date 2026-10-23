package cloud

import (
	"context"
	"errors"
	"log"
	"schemo/internal/domain"
	"time"
)

// Caller provides access tokens and metadata required for authorized server requests.
//
//wails:ignore
func (s *Account) Caller(ctx context.Context) (domain.Caller, error) {
	stored, access, err := s.requireSignedIn(ctx)
	if err != nil {
		return domain.Caller{}, err
	}
	return domain.Caller{
		Address:   serverAddressOrDefault(stored.Server),
		Access:    access,
		SessionID: stored.SessionID,
	}, nil
}

// Refused handles server-side token rejection, forcing a refresh or ending the session.
//
//wails:ignore
func (s *Account) Refused(ctx context.Context, sessionID string) {
	s.session.Lock()
	stored, err := s.store.Account(ctx)
	if err != nil || sessionID == "" || stored.SessionID != sessionID {
		s.session.Unlock()
		return
	}
	revision := s.revision

	s.forgetTokens()
	s.session.Unlock()
	if _, err := s.accessTokenFor(ctx, stored); err != nil {
		if domain.CodeOf(err) == domain.CodeNotSignedIn {
			// A rejected refresh clears the session and advances its revision once.
			s.finishSignIn(revision+1, &domain.Failure{Code: domain.CodeSessionEnded})
		}
	}
}

// Me returns local account details without triggering a remote API call.
//
//wails:ignore
func (s *Account) Me(ctx context.Context) (domain.Account, error) {
	stored, err := s.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.Account{}, err
	}
	return stored, nil
}

func (s *Account) requireSignedIn(ctx context.Context) (domain.Account, string, error) {
	stored, err := s.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.Account{}, "", err
	}
	if !stored.SignedIn() {
		return domain.Account{}, "", domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
	}

	access, err := s.accessTokenFor(ctx, stored)
	if err != nil {
		return domain.Account{}, "", err
	}
	return stored, access, nil
}

func (s *Account) accessTokenFor(ctx context.Context, stored domain.Account) (string, error) {
	if access := s.cachedAccessToken(stored); access != "" {
		return access, nil
	}
	key := serverAddressOrDefault(stored.Server) + ":" + stored.SessionID
	result := s.refresh.DoChan(key, func() (any, error) {
		return s.renewAccessToken(ctx, stored)
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return "", result.Err
		}
		return result.Val.(string), nil
	}
}

func (s *Account) renewAccessToken(ctx context.Context, stored domain.Account) (string, error) {
	if access := s.cachedAccessToken(stored); access != "" {
		return access, nil
	}

	s.session.Lock()
	revision := s.revision
	if !s.isCurrentSession(ctx, stored, revision) {
		s.session.Unlock()
		return "", domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
	}
	refresh, err := s.refreshTokenOf(stored)
	s.session.Unlock()
	if err != nil {
		return "", domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
	}

	tokens, err := s.server.Refresh(ctx, serverAddressOrDefault(stored.Server), refresh)
	s.session.Lock()
	if !s.isCurrentSession(ctx, stored, revision) {
		s.session.Unlock()
		return "", domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
	}
	if err != nil {
		switch domain.CodeOf(err) {
		case domain.CodeServerRefused, domain.CodeNotSignedIn:
			_ = s.forgetAccountLocked(ctx, stored.Server)
			s.session.Unlock()
			return "", domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
		}
		s.session.Unlock()
		return "", err
	}

	if tokens.Refresh != "" && tokens.Refresh != refresh {
		s.storeRefreshToken(stored, tokens.Refresh)
	}
	s.mu.Lock()
	s.access, s.expiresAt, s.accessKey = tokens.Access, tokens.ExpiresAt, sessionKey(stored)
	s.mu.Unlock()
	s.session.Unlock()
	return tokens.Access, nil
}

func (s *Account) cachedAccessToken(stored domain.Account) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accessKey == sessionKey(stored) && s.access != "" && s.now().Add(time.Minute).Before(s.expiresAt) {
		return s.access
	}
	return ""
}

func (s *Account) forgetAccountLocked(ctx context.Context, server string) error {
	s.cancelPendingSignInLocked()
	stored, err := s.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	s.forgetTokens()
	s.credentialKey, s.refreshToken, s.sessionOnly = "", "", false
	if stored.SignedIn() {
		if err := s.credentials.Delete(stored); err != nil {
			log.Print("[account] could not remove the system credential; local session is being cleared")
		}
	}
	if err := s.store.Forget(ctx); err != nil {
		return err
	}
	if server == "" {
		return nil
	}
	return s.store.Save(ctx, domain.Account{Server: server})
}

func (s *Account) forgetTokens() {
	s.mu.Lock()
	s.access, s.expiresAt, s.accessKey = "", time.Time{}, ""
	s.mu.Unlock()
}
