package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"schemo/internal/domain"
)

// The account's row — the only row in the schema that is about the other side of the wire. It is
// written by the sign-in and read by every window that draws who this installation belongs to.

// Account reads the account. Nothing signed in answers ErrNotFound, which is the ordinary state of
// a local app rather than a problem with the database.
func (s *Store) Account(ctx context.Context) (domain.Account, error) {
	const query = `
		SELECT server, user_id, email, name, session_id, signed_in_at
		  FROM account WHERE id = 1`

	var (
		account    domain.Account
		signedInAt int64
	)
	err := s.db.QueryRowContext(ctx, query).Scan(&account.Server, &account.UserID, &account.Email,
		&account.Name, &account.SessionID, &signedInAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("reading the account: %w", err)
	}
	if signedInAt != 0 {
		account.SignedInAt = time.UnixMilli(signedInAt)
	}
	return account, nil
}

// Save writes account metadata. Credentials belong to the OS credential store.
func (s *Store) Save(ctx context.Context, account domain.Account) error {
	const query = `
		INSERT INTO account (id, server, user_id, email, name, session_id, signed_in_at)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			server = excluded.server, user_id = excluded.user_id, email = excluded.email,
			name = excluded.name, session_id = excluded.session_id,
			signed_in_at = excluded.signed_in_at`

	_, err := s.db.ExecContext(ctx, query, account.Server, account.UserID, account.Email, account.Name,
		account.SessionID, account.SignedInAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("saving the account: %w", err)
	}
	return nil
}

// Forget removes the account metadata row.
func (s *Store) Forget(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM account WHERE id = 1`); err != nil {
		return fmt.Errorf("forgetting the account: %w", err)
	}
	return nil
}
