package cloud

import (
	"context"
	"schemo/internal/domain"
)

// Sessions returns all active sessions associated with the current account.
func (s *Account) Sessions(ctx context.Context) ([]domain.Session, error) {
	stored, access, err := s.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	sessions, err := s.server.Sessions(ctx, serverAddressOrDefault(stored.Server), access)
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

// EndSession terminates a specific session ID on the server and signs out if it matches local.
func (s *Account) EndSession(ctx context.Context, sessionID string) ([]domain.Session, error) {
	stored, access, err := s.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	revision := s.currentRevision()

	left, err := s.server.EndSession(ctx, serverAddressOrDefault(stored.Server), access, sessionID)
	if err != nil {
		return nil, err
	}
	if sessionID == stored.SessionID {
		s.session.Lock()
		if !s.isCurrentSession(ctx, stored, revision) {
			s.session.Unlock()
			return left, nil
		}
		if err := s.forgetAccountLocked(ctx, stored.Server); err != nil {
			s.session.Unlock()
			return nil, err
		}
		s.session.Unlock()
		if _, err := s.publishStatus(ctx); err != nil {
			return nil, err
		}
	}
	return left, nil
}

// EndOthers revokes all active sessions except the current device's session.
func (s *Account) EndOthers(ctx context.Context) ([]domain.Session, error) {
	stored, access, err := s.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	return s.server.EndOthers(ctx, serverAddressOrDefault(stored.Server), access)
}
