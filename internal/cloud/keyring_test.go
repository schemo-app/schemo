package cloud

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

func TestPublicationPasswordsSurviveRestartAndStayWithinTheirAccount(t *testing.T) {
	keyring.MockInit()
	store := NewKeyring(platform.DataDir(t.TempDir()))
	account := domain.Account{Server: "https://cloud.example", UserID: "user", SessionID: "session"}
	if err := store.SetPublicationPassword(account, "page", "saved-password"); err != nil {
		t.Fatal(err)
	}
	restarted := NewKeyring(platform.DataDir(store.installation))
	account.SessionID = "next-session"
	if value, err := restarted.PublicationPassword(account, "page"); err != nil || value != "saved-password" {
		t.Fatalf("credential did not survive restart and sign-in: %v", err)
	}
	for _, other := range []domain.Account{
		{Server: "https://other.example", UserID: account.UserID},
		{Server: account.Server, UserID: "other"},
	} {
		if _, err := store.PublicationPassword(other, "page"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("credential crossed an account boundary: %v", err)
		}
	}
	if _, err := NewKeyring(platform.DataDir(t.TempDir())).PublicationPassword(account, "page"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("credential crossed an installation boundary: %v", err)
	}
	if _, err := store.PublicationPassword(account, "other-page"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("credential crossed a publication boundary: %v", err)
	}
	if err := store.DeletePublicationPassword(account, "page"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublicationPassword(account, "page"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("credential survived deletion")
	}
}

func TestCredentialsAreScopedToInstallationServerAccountAndSession(t *testing.T) {
	store := NewKeyring(platform.DataDir(t.TempDir()))
	account := domain.Account{Server: "https://cloud.example", UserID: "user", SessionID: "session"}
	original := store.key(account)
	for _, changed := range []domain.Account{
		{Server: "https://other.example", UserID: account.UserID, SessionID: account.SessionID},
		{Server: account.Server, UserID: "other", SessionID: account.SessionID},
		{Server: account.Server, UserID: account.UserID, SessionID: "other"},
	} {
		if store.key(changed) == original {
			t.Fatal("credentials can cross account boundaries")
		}
	}
	if NewKeyring(platform.DataDir(t.TempDir())).key(account) == original {
		t.Fatal("installations share a credential")
	}
	if NewKeyring(platform.DataDir(store.installation)).key(account) != original {
		t.Fatal("credential identity does not survive restart")
	}
}
