package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// writeTx keeps commit notifications local to a synchronous write callback.
type writeTx struct {
	*sql.Tx
	spaces map[string]struct{}
}

func (s *Store) inTx(ctx context.Context, what string, fn func(tx *writeTx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = tx.Rollback() }()

	write := &writeTx{Tx: tx, spaces: map[string]struct{}{}}
	if err := fn(write); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if s.wake != nil {
		for space := range write.spaces {
			s.wake(space)
		}
	}
	return nil
}
