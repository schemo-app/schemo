package cloud

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"schemo/internal/domain"
)

type heldRefresh struct {
	AccountServer
	release <-chan struct{}
	calls   atomic.Int32
	tokens  domain.Tokens
}

func (s *heldRefresh) Refresh(ctx context.Context, _, _ string) (domain.Tokens, error) {
	s.calls.Add(1)
	select {
	case <-s.release:
		return s.tokens, nil
	case <-ctx.Done():
		return domain.Tokens{}, ctx.Err()
	}
}

func TestConcurrentCallersShareOneRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		account, store, server, _, _ := newAccount(t)
		signedIn(t, account)
		synctest.Wait()
		expired(account)
		release := make(chan struct{})
		remote := &heldRefresh{AccountServer: server, release: release, tokens: domain.Tokens{
			Access: "shared-access", Refresh: "shared-refresh", ExpiresAt: account.now().Add(time.Hour),
		}}
		account.server = watched{AccountServer: remote, account: account}
		const callers = 8
		results := make([]domain.Caller, callers)
		errors := make([]error, callers)
		var group sync.WaitGroup
		for i := range callers {
			group.Go(func() {
				results[i], errors[i] = account.Caller(context.Background())
			})
		}
		synctest.Wait()
		calls := remote.calls.Load()
		close(release)
		group.Wait()
		if calls != 1 {
			t.Fatalf("concurrent callers submitted %d refreshes", calls)
		}
		for i, result := range results {
			if errors[i] != nil || result.Access != "shared-access" {
				t.Fatalf("caller %d: %+v, %v", i, result, errors[i])
			}
		}
		_, saved := store.saved()
		if saved != "shared-refresh" {
			t.Fatalf("rotated refresh token was not persisted: %q", saved)
		}
		if _, err := account.Caller(context.Background()); err != nil || remote.calls.Load() != 1 {
			t.Fatalf("cached access token triggered another refresh: %v", err)
		}
	})
}

func TestACancelledCallerStopsWaitingForASharedRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		account, _, server, _, _ := newAccount(t)
		signedIn(t, account)
		synctest.Wait()
		expired(account)
		release := make(chan struct{})
		remote := &heldRefresh{AccountServer: server, release: release, tokens: domain.Tokens{
			Access: "shared-access", Refresh: "shared-refresh", ExpiresAt: account.now().Add(time.Hour),
		}}
		account.server = watched{AccountServer: remote, account: account}
		var firstErr, cancelledErr error
		var group sync.WaitGroup
		group.Go(func() { _, firstErr = account.Caller(context.Background()) })
		synctest.Wait()
		ctx, cancel := context.WithCancel(context.Background())
		group.Go(func() { _, cancelledErr = account.Caller(ctx) })
		synctest.Wait()
		cancel()
		synctest.Wait()
		stopped := errors.Is(cancelledErr, context.Canceled)
		close(release)
		group.Wait()
		if !stopped || firstErr != nil || remote.calls.Load() != 1 {
			t.Fatalf("cancelled %v, first error %v, calls %d", stopped, firstErr, remote.calls.Load())
		}
	})
}
