package request

import (
	"context"
	"time"

	"schemo/internal/domain"
)

type attempt struct {
	id        string
	workspace string
	started   int64
	cancel    context.CancelFunc
	done      chan struct{}
	stream    *Stream
	message   messageResolver
}

type messageResolver func(string) (value, masked string)

func (s *History) startAttempt(ctx context.Context, workspace string, in SendInput) (context.Context, *attempt, error) {
	s.attemptMu.Lock()
	defer s.attemptMu.Unlock()
	if s.stopping {
		return nil, nil, domain.Refuse(domain.CodeNotAllowed, domain.ErrNotAllowed, nil)
	}
	ctx, cancel := context.WithCancel(ctx)
	active := &attempt{
		id: s.ids(), workspace: workspace, started: time.Now().UnixMilli(),
		cancel: cancel, done: make(chan struct{}), message: in.message,
	}
	if in.Protocol.Live() {
		active.stream = &Stream{limit: defaultMaxBodyBytes}
	}
	s.attempts[active.id] = active
	return ctx, active, nil
}

func (s *History) finishAttempt(active *attempt) {
	active.cancel()
	s.attemptMu.Lock()
	defer s.attemptMu.Unlock()
	delete(s.attempts, active.id)
	close(active.done)
}

func (s *History) findAttempt(id string) *attempt {
	s.attemptMu.Lock()
	defer s.attemptMu.Unlock()
	return s.attempts[id]
}

func (s *History) activeAttempts() []*attempt {
	s.attemptMu.Lock()
	defer s.attemptMu.Unlock()
	return s.attemptSnapshot()
}

func (s *History) attemptSnapshot() []*attempt {
	active := make([]*attempt, 0, len(s.attempts))
	for _, one := range s.attempts {
		active = append(active, one)
	}
	return active
}

// StopAttempts stops admission, cancels requests and waits for their records to finish writing.
//
//wails:ignore
func (s *History) StopAttempts(ctx context.Context) error {
	s.attemptMu.Lock()
	s.stopping = true
	active := s.attemptSnapshot()
	s.attemptMu.Unlock()
	for _, one := range active {
		one.cancel()
	}
	for _, one := range active {
		select {
		case <-one.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
