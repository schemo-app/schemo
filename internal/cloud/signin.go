package cloud

import (
	"context"
	"errors"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Begin initiates the browser-based auth flow and starts background code polling.
func (s *Account) Begin(ctx context.Context, address string) (AccountStatus, error) {
	to := serverAddressOrDefault(address)
	s.session.Lock()
	if s.closed {
		s.session.Unlock()
		return AccountStatus{}, context.Canceled
	}
	s.cancelPendingSignInLocked()
	revision := s.revision
	s.session.Unlock()

	device := s.thisDevice()
	challenge, err := s.server.Authorize(ctx, to, device)
	if err != nil {
		return AccountStatus{}, err
	}

	if err := s.browser.OpenURL(challenge.URL); err != nil {
		return AccountStatus{}, err
	}

	stored, err := s.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return AccountStatus{}, err
	}
	s.session.Lock()
	if s.closed || s.revision != revision {
		s.session.Unlock()
		return AccountStatus{}, context.Canceled
	}
	if stored.SignedIn() {
		_ = s.credentials.Delete(stored)
	}
	s.credentialKey, s.refreshToken, s.sessionOnly = "", "", false
	s.forgetTokens()
	stored = domain.Account{Server: normalizedAddress(address)}
	if err := s.store.Save(ctx, stored); err != nil {
		s.session.Unlock()
		return AccountStatus{}, err
	}

	watching, stop := context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Lock()
	s.waiting, s.challenge, s.cancel = true, challenge, stop
	s.mu.Unlock()
	s.flows.Go(func() { s.pollSignIn(watching, to, challenge, revision) })
	s.session.Unlock()

	state, err := s.Status(ctx)
	if err != nil {
		return AccountStatus{}, err
	}
	s.notifier.Publish(TopicAccountChanged, state)
	return state, nil
}

// Cancel aborts the active sign-in polling process locally.
func (s *Account) Cancel(ctx context.Context) (AccountStatus, error) {
	s.cancelPendingSignIn()
	return s.publishStatus(ctx)
}

func (s *Account) pollSignIn(ctx context.Context, address string, challenge domain.Challenge, revision uint64) {
	tokens, err := s.server.Tokens(ctx, address, challenge)
	if err == nil {
		err = s.keepSignedInAccount(ctx, address, tokens, revision)
	}

	var failure *domain.Failure
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		failure = domain.FailureOf(err, domain.CodeSignInFailed)
	}
	s.finishSignIn(revision, failure)
}

func (s *Account) keepSignedInAccount(ctx context.Context, address string, tokens domain.Tokens, revision uint64) error {
	stored, err := s.server.Me(ctx, address, tokens.Access)
	if err != nil {
		return err
	}

	stored.Server = ""
	if address != platform.DefaultServer {
		stored.Server = address
	}
	stored.SignedInAt = s.now()

	s.session.Lock()
	defer s.session.Unlock()
	if ctx.Err() != nil || s.revision != revision {
		return context.Canceled
	}
	return s.saveSignedInAccount(ctx, stored, tokens)
}

func (s *Account) finishSignIn(revision uint64, failure *domain.Failure) {
	s.session.Lock()
	defer s.session.Unlock()
	if s.revision != revision {
		return
	}
	s.mu.Lock()
	s.waiting, s.challenge, s.cancel = false, domain.Challenge{}, nil
	s.mu.Unlock()

	state, err := s.readStatus(context.Background())
	if err != nil {
		return
	}
	state.Failure = failure
	s.notifier.Publish(TopicAccountChanged, state)
}
