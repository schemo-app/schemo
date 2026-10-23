package db

import (
	"context"
	"database/sql"
	"fmt"

	"schemo/internal/domain"
)

// writeTx keeps commit notifications local to a synchronous write callback.
type writeTx struct {
	*sql.Tx
	workspaces map[string]struct{}
	changed    map[domain.Changed]struct{}
}

// touched records that a kind of entity changed in a workspace, to be announced after the commit.
func (tx *writeTx) touched(kind domain.EntityType, workspace string) {
	tx.changed[domain.Changed{Kind: string(kind), Workspace: workspace}] = struct{}{}
}

func (s *Store) inTx(ctx context.Context, what string, fn func(tx *writeTx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer func() { _ = tx.Rollback() }()

	write := &writeTx{Tx: tx, workspaces: map[string]struct{}{}, changed: map[domain.Changed]struct{}{}}
	if err := fn(write); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if s.wake != nil {
		for workspace := range write.workspaces {
			s.wake(workspace)
		}
	}
	if s.notify != nil {
		for changed := range write.changed {
			s.notify(changed)
		}
	}
	return nil
}
