package cloud

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
	account, store, server, browser, notifier := newAccount(t)
	account.credentials = unavailableCredentials{}
	signedIn(t, account)
	status, err := account.Status(t.Context())
	if err != nil || !status.SignedIn || !status.SessionOnly {
		t.Fatalf("session-only sign-in: %+v, %v", status, err)
	}
	if _, token := store.saved(); token != "" {
		t.Fatal("refresh token was stored in the metadata database")
	}
	expired(account)
	if caller, err := account.Caller(t.Context()); err != nil || caller.Access != "access-2" {
		t.Fatalf("memory session cannot refresh: %+v, %v", caller, err)
	}
	restarted := NewAccount(store.db, unavailableCredentials{}, server, browser, notifier)
	status, err = restarted.Status(t.Context())
	if err != nil || status.SignedIn {
		t.Fatalf("unpersisted session survived restart: %+v, %v", status, err)
	}
}

func TestStoredCredentialRestoresAfterRestart(t *testing.T) {
	account, store, server, browser, notifier := newAccount(t)
	signedIn(t, account)
	restarted := NewAccount(store.db, store, server, browser, notifier)
	caller, err := restarted.Caller(t.Context())
	if err != nil || caller.Access != "access-2" {
		t.Fatalf("restored session: %+v, %v", caller, err)
	}
}

func TestServerChangeClearsTheOldSession(t *testing.T) {
	account, store, _, _, _ := newAccount(t)
	signedIn(t, account)
	status, err := account.SetServer(t.Context(), "https://other.example")
	_, token := store.saved()
	if err != nil || status.SignedIn || token != "" {
		t.Fatalf("server change retained the old session: %+v, %v", status, err)
	}
	if _, err := account.Caller(t.Context()); domain.CodeOf(err) != domain.CodeNotSignedIn {
		t.Fatalf("old token can be sent to the new server: %v", err)
	}
}

func TestRefreshCannotRestoreASessionAfterLogout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		account, store, server, _, _ := newAccount(t)
		signedIn(t, account)
		synctest.Wait()
		expired(account)
		release := make(chan struct{})
		remote := &heldRefresh{AccountServer: server, release: release, tokens: tokens("late-access", "late-refresh")}
		account.server = watched{AccountServer: remote, account: account}
		done := make(chan error, 1)
		go func() { _, err := account.Caller(context.Background()); done <- err }()
		synctest.Wait()
		if _, err := account.SignOut(t.Context(), false); err != nil {
			t.Fatal(err)
		}
		close(release)
		if err := <-done; domain.CodeOf(err) != domain.CodeNotSignedIn {
			t.Fatalf("old refresh returned a usable token: %v", err)
		}
		row, token := store.saved()
		if row.SignedIn() || token != "" || account.cachedAccessToken(row) != "" {
			t.Fatal("late refresh restored a signed-out session")
		}
	})
}

func TestOldSignInCannotCommitAfterANewFlow(t *testing.T) {
	account, store, _, _, _ := newAccount(t)
	oldRevision := account.currentRevision()
	account.cancelPendingSignIn()
	err := account.keepSignedInAccount(t.Context(), "https://api.example.com", domain.Tokens{
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
	account, store, _, _, _ := newAccount(t)
	if _, err := account.Begin(t.Context(), "https://api.example.com"); err != nil {
		t.Fatal(err)
	}
	account.Close()
	row, token := store.saved()
	if row.SignedIn() || token != "" {
		t.Fatal("closing persisted an unfinished session")
	}
	if _, err := account.Begin(t.Context(), "https://api.example.com"); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed service started another flow: %v", err)
	}
}
