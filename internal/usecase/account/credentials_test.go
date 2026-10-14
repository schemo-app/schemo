package account

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"schemo/internal/domain"
)

type unavailableCredentials struct{}

func (unavailableCredentials) Get(domain.Account) (string, error) {
	return "", errors.New("unavailable")
}
func (unavailableCredentials) Set(domain.Account, string) error { return errors.New("unavailable") }
func (unavailableCredentials) Delete(domain.Account) error      { return errors.New("unavailable") }

func TestUnavailableSecureStorageKeepsOnlyAnInMemorySession(t *testing.T) {
	uc, store, server, browser, notifier, data := newUseCaseWithData()
	uc.credentials = unavailableCredentials{}
	signedIn(t, uc)
	status, err := uc.Status(t.Context())
	if err != nil || !status.SignedIn || !status.SessionOnly {
		t.Fatalf("session-only sign-in: %+v, %v", status, err)
	}
	if _, token := store.saved(); token != "" {
		t.Fatal("refresh token was stored in the metadata database")
	}
	expired(uc)
	if caller, err := uc.Caller(t.Context()); err != nil || caller.Access != "access-2" {
		t.Fatalf("memory session cannot refresh: %+v, %v", caller, err)
	}
	restarted := NewUseCase(store, unavailableCredentials{}, server, browser, data, notifier)
	status, err = restarted.Status(t.Context())
	if err != nil || status.SignedIn {
		t.Fatalf("unpersisted session survived restart: %+v, %v", status, err)
	}
}

func TestStoredCredentialRestoresAfterRestart(t *testing.T) {
	uc, store, server, browser, notifier, data := newUseCaseWithData()
	signedIn(t, uc)
	restarted := NewUseCase(store, store, server, browser, data, notifier)
	caller, err := restarted.Caller(t.Context())
	if err != nil || caller.Access != "access-2" {
		t.Fatalf("restored session: %+v, %v", caller, err)
	}
}

func TestServerChangeClearsTheOldSession(t *testing.T) {
	uc, store, _, _, _ := newUseCase()
	signedIn(t, uc)
	status, err := uc.SetServer(t.Context(), "https://other.example")
	_, token := store.saved()
	if err != nil || status.SignedIn || token != "" {
		t.Fatalf("server change retained the old session: %+v, %v", status, err)
	}
	if _, err := uc.Caller(t.Context()); domain.CodeOf(err) != domain.CodeNotSignedIn {
		t.Fatalf("old token can be sent to the new server: %v", err)
	}
}

func TestRefreshCannotRestoreASessionAfterLogout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		uc, store, server, _, _ := newUseCase()
		signedIn(t, uc)
		synctest.Wait()
		expired(uc)
		release := make(chan struct{})
		remote := &heldRefresh{Server: server, release: release, tokens: tokens("late-access", "late-refresh")}
		uc.server = watched{Server: remote, uc: uc}
		done := make(chan error, 1)
		go func() { _, err := uc.Caller(context.Background()); done <- err }()
		synctest.Wait()
		if _, err := uc.SignOut(t.Context(), false); err != nil {
			t.Fatal(err)
		}
		close(release)
		if err := <-done; domain.CodeOf(err) != domain.CodeNotSignedIn {
			t.Fatalf("old refresh returned a usable token: %v", err)
		}
		row, token := store.saved()
		if row.SignedIn() || token != "" || uc.cachedToken(row) != "" {
			t.Fatal("late refresh restored a signed-out session")
		}
	})
}

func TestOldSignInCannotCommitAfterANewFlow(t *testing.T) {
	uc, store, _, _, _ := newUseCase()
	oldRevision := uc.revisionNow()
	uc.stop()
	err := uc.remember(t.Context(), "https://api.example.com", domain.Tokens{
		Access: "late", Refresh: "late-refresh", ExpiresAt: time.Now().Add(time.Hour),
	}, oldRevision)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("old flow was not rejected: %v", err)
	}
	row, token := store.saved()
	if row.SignedIn() || token != "" {
		t.Fatal("old flow saved a credential")
	}
}

func TestCloseCancelsAndJoinsPendingSignIn(t *testing.T) {
	uc, store, _, _, _ := newUseCase()
	if _, err := uc.Begin(t.Context(), "https://api.example.com"); err != nil {
		t.Fatal(err)
	}
	uc.Close()
	row, token := store.saved()
	if row.SignedIn() || token != "" {
		t.Fatal("closing persisted an unfinished session")
	}
	if _, err := uc.Begin(t.Context(), "https://api.example.com"); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed use case started another flow: %v", err)
	}
}
