package credentials

import (
	"testing"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

func TestCredentialsAreScopedToInstallationServerAccountAndSession(t *testing.T) {
	store := New(platform.DataDir(t.TempDir()))
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
	if New(platform.DataDir(t.TempDir())).key(account) == original {
		t.Fatal("installations share a credential")
	}
	if New(platform.DataDir(store.installation)).key(account) != original {
		t.Fatal("credential identity does not survive restart")
	}
}
