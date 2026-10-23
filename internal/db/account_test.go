package db

import (
	"context"
	"errors"
	"testing"

	"schemo/internal/domain"
)

// A database that cannot be read is not an account that is not there, and the two have to answer
// differently: "no account" is the ordinary state of a local app, while a failure has to reach the
// caller that would otherwise decide the user is not signed in and sign them out for good.
func TestAccountTellsAFailureFromNoAccount(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if _, err := store.Account(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("with nothing signed in = %v, want ErrNotFound", err)
	}

	// The database goes away under the call, which is the shortest way to a failure that
	// must not be mistaken for "not signed in".
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := store.Account(ctx); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Errorf("on a database that is gone = %v, want a failure and not ErrNotFound", err)
	}
}

func TestAccountSchemaDoesNotStoreCredentials(t *testing.T) {
	store := newMigratedStore(t)
	var count int
	if err := store.DB().QueryRowContext(t.Context(),
		`SELECT count(*) FROM pragma_table_info('account') WHERE name = 'refresh_token'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("account schema still has a plaintext credential column")
	}
}
