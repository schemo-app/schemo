package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"schemo/internal/domain"
)

// EnvState reads one workspace's environments, its globals and which one is active. Globals are
// `variables` rows with no scope, so one pair of queries fills the whole screen.
func (s *Store) EnvState(ctx context.Context, workspaceID string) (domain.EnvState, error) {
	state := domain.EnvState{Environments: []domain.Environment{}, Globals: []domain.Variable{}}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, coalesce(color, ''), readonly, position FROM environments
		  WHERE workspace_id = ? AND deleted_at IS NULL ORDER BY position, name`, workspaceID)
	if err != nil {
		return state, fmt.Errorf("reading environments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var env domain.Environment
		if err := rows.Scan(&env.ID, &env.Name, &env.Color, &env.Readonly, &env.Position); err != nil {
			return state, fmt.Errorf("reading environments: %w", err)
		}
		env.Vars = []domain.Variable{}
		state.Environments = append(state.Environments, env)
	}
	if err := rows.Err(); err != nil {
		return state, fmt.Errorf("reading environments: %w", err)
	}

	vars, err := s.variables(ctx, workspaceID)
	if err != nil {
		return state, err
	}
	for i := range state.Environments {
		for _, v := range vars {
			if v.scope == state.Environments[i].ID {
				state.Environments[i].Vars = append(state.Environments[i].Vars, v.variable)
			}
		}
	}
	for _, v := range vars {
		if v.scope == "" {
			state.Globals = append(state.Globals, v.variable)
		}
	}

	active, err := s.activeEnvironment(ctx, workspaceID)
	if err != nil {
		return state, err
	}
	state.ActiveID = active
	return state, nil
}

// scope is the environment the variable belongs to; empty means the globals.
type scopedVariable struct {
	scope    string
	variable domain.Variable
}

func (s *Store) variables(ctx context.Context, workspaceID string) ([]scopedVariable, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ifnull(scope_id, ''), name, value, kind, enabled, position
		   FROM variables WHERE workspace_id = ? AND deleted_at IS NULL
		  ORDER BY position, name`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("reading variables: %w", err)
	}
	defer rows.Close()

	var out []scopedVariable
	for rows.Next() {
		var (
			scope string
			v     domain.Variable
		)
		if err := rows.Scan(&v.ID, &scope, &v.Name, &v.Value, &v.Kind, &v.Enabled,
			&v.Position); err != nil {
			return nil, fmt.Errorf("reading variables: %w", err)
		}
		v.HasValue = v.Value != ""
		out = append(out, scopedVariable{scope: scope, variable: v})
	}
	return out, rows.Err()
}

// SaveEnvironment writes the environment's own row; its variables are saved one by one.
func (s *Store) SaveEnvironment(
	ctx context.Context,
	workspaceID string,
	env domain.Environment,
) error {
	now := time.Now().UnixMilli()
	return s.inTx(ctx, fmt.Sprintf("saving environment %s", env.ID), func(tx *writeTx) error {
		action, err := actionFor(ctx, tx, "environments", env.ID)
		if err != nil {
			return fmt.Errorf("saving environment %s: %w", env.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO environments (id, workspace_id, name, color, readonly, position, created_at,
			                           updated_at, version)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			   name = excluded.name, color = excluded.color, readonly = excluded.readonly,
			   position = excluded.position, updated_at = excluded.updated_at`,
			env.ID, workspaceID, env.Name, nullIfEmpty(env.Color), env.Readonly,
			env.Position, now, now, versionUnagreed); err != nil {
			return fmt.Errorf("saving environment %s: %w", env.ID, err)
		}
		return enqueue(ctx, tx, workspaceID, entityEnvironment, env.ID, action)
	})
}

// DeleteEnvironment marks the environment gone and its variables with it, in one transaction. The
// variables have no foreign key to lean on — scope_id is a plain column, so that globals can share
// the table — which is why this is not a single statement.
//
// A variable is a row of its own on the other side, so each one carries its own marker: leaving
// them alive under an environment that is not would be variables that come back the next time the
// space is pulled.
func (s *Store) DeleteEnvironment(ctx context.Context, workspaceID, id string) error {
	now := time.Now().UnixMilli()
	return s.inTx(ctx, fmt.Sprintf("deleting environment %s", id), func(tx *writeTx) error {
		// Who goes is read before the marker is written over the thing that says who was alive.
		variables, err := idsOf(ctx, tx,
			`SELECT id FROM variables
			  WHERE workspace_id = ? AND scope_kind = 'environment' AND scope_id = ?
			    AND deleted_at IS NULL ORDER BY id`, workspaceID, id)
		if err != nil {
			return fmt.Errorf("deleting variables of %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE variables SET deleted_at = ?
			  WHERE workspace_id = ? AND scope_kind = 'environment' AND scope_id = ?
			    AND deleted_at IS NULL`,
			now, workspaceID, id); err != nil {
			return fmt.Errorf("deleting variables of %s: %w", id, err)
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE environments SET deleted_at = ?
			  WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL`,
			now, workspaceID, id)
		if err != nil {
			return fmt.Errorf("deleting environment %s: %w", id, err)
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("deleting environment %s: %w", id, err)
		}
		if changed == 0 {
			return nil
		}
		if err := enqueue(ctx, tx, workspaceID, entityEnvironment, id, actionDelete); err != nil {
			return err
		}
		return queueAll(ctx, tx, workspaceID, entityVariable, actionDelete, variables)
	})
}

// SaveVariable writes a variable into an environment or into the globals. A name that already
// exists in the same scope is refused by the unique index and reported as a conflict, not as a
// database error.
func (s *Store) SaveVariable(
	ctx context.Context,
	workspaceID string,
	scope domain.EnvScope,
	v domain.Variable,
) error {
	kind, scopeID := "globals", any(nil)
	if scope.Environment != "" {
		kind, scopeID = "environment", scope.Environment
	}

	now := time.Now().UnixMilli()
	return s.inTx(ctx, fmt.Sprintf("saving variable %s", v.ID), func(tx *writeTx) error {
		action, err := actionFor(ctx, tx, "variables", v.ID)
		if err != nil {
			return fmt.Errorf("saving variable %s: %w", v.ID, err)
		}
		// Every column is written on the update too: a scope left out here would be a variable
		// answering for an environment it had been moved out of.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO variables (id, workspace_id, scope_kind, scope_id, name, value, kind, enabled,
			                        position, created_at, updated_at, version)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			   name = excluded.name, value = excluded.value, kind = excluded.kind,
			   enabled = excluded.enabled, position = excluded.position,
			   updated_at = excluded.updated_at, scope_kind = excluded.scope_kind,
			   scope_id = excluded.scope_id`,
			v.ID, workspaceID, kind, scopeID, v.Name, v.Value, string(v.Kind), v.Enabled,
			v.Position, now, now, versionUnagreed); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("variable %q: %w", v.Name, domain.ErrConflict)
			}
			return fmt.Errorf("saving variable %s: %w", v.ID, err)
		}
		return enqueue(ctx, tx, workspaceID, entityVariable, v.ID, action)
	})
}

// DeleteVariable marks one variable gone: it is a row of its own, deleted by a person, and the
// other side hears about it the same way it hears about any other change.
func (s *Store) DeleteVariable(ctx context.Context, workspaceID, id string) error {
	return s.inTx(ctx, fmt.Sprintf("deleting variable %s", id), func(tx *writeTx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE variables SET deleted_at = ?
			  WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL`,
			time.Now().UnixMilli(), workspaceID, id)
		if err != nil {
			return fmt.Errorf("deleting variable %s: %w", id, err)
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("deleting variable %s: %w", id, err)
		}
		if changed == 0 {
			return nil
		}
		return enqueue(ctx, tx, workspaceID, entityVariable, id, actionDelete)
	})
}

// VariableValue reads one variable's value, which is what the reveal button and the send path ask
// for; a snapshot never carries a secret's value.
func (s *Store) VariableValue(ctx context.Context, workspaceID, id string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM variables WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL`,
		workspaceID, id).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("reading variable %s: %w", id, err)
	}
	return value, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
