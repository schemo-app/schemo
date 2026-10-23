package db

import (
	"context"

	"schemo/internal/domain"
)

func environmentRow(workspace string, env domain.Environment) (entity, error) {
	env.Vars = nil
	data, err := encodeData(env)
	if err != nil {
		return entity{}, err
	}
	return entity{Kind: domain.EntityEnvironment, ID: env.ID, Workspace: workspace,
		Position: int64(env.Position), Name: env.Name, Data: data}, nil
}

func environmentFrom(e entity) (domain.Environment, error) {
	env, err := decodeData[domain.Environment](e)
	env.ID, env.Name, env.Position, env.Vars = e.ID, e.Name, int(e.Position), []domain.Variable{}
	return env, err
}

// variableRow files a variable under its environment, or under no parent for the globals.
func variableRow(workspace string, scope domain.EnvScope, v domain.Variable) (entity, error) {
	v.HasValue = false
	data, err := encodeData(v)
	if err != nil {
		return entity{}, err
	}
	key := ""
	if v.Name != "" {
		// A row still being named is not a name yet; two of them do not collide.
		key = naturalKey(scope.Environment, v.Name)
	}
	return entity{Kind: domain.EntityVariable, ID: v.ID, Workspace: workspace, Parent: scope.Environment,
		Position: int64(v.Position), Name: v.Name, Key: key, Data: data}, nil
}

func variableFrom(e entity) (domain.Variable, error) {
	v, err := decodeData[domain.Variable](e)
	v.ID, v.Name, v.Position, v.HasValue = e.ID, e.Name, int(e.Position), v.Value != ""
	return v, err
}

func (s *Store) EnvState(ctx context.Context, workspaceID string) (domain.EnvState, error) {
	state := domain.EnvState{Environments: []domain.Environment{}, Globals: []domain.Variable{}}
	rows, err := listEntities(ctx, s.db, workspaceID,
		kinds(domain.EntityEnvironment, domain.EntityVariable), "")
	if err != nil {
		return state, err
	}
	vars := map[string][]domain.Variable{}
	for _, row := range rows {
		if row.Kind == domain.EntityVariable {
			v, err := variableFrom(row)
			if err != nil {
				return state, err
			}
			vars[row.Parent] = append(vars[row.Parent], v)
			continue
		}
		env, err := environmentFrom(row)
		if err != nil {
			return state, err
		}
		state.Environments = append(state.Environments, env)
	}
	for i := range state.Environments {
		if held := vars[state.Environments[i].ID]; held != nil {
			state.Environments[i].Vars = held
		}
	}
	if held := vars[""]; held != nil {
		state.Globals = held
	}
	state.ActiveID, err = s.activeEnvironment(ctx, workspaceID)
	return state, err
}

func (s *Store) SaveEnvironment(ctx context.Context, workspaceID string, env domain.Environment) error {
	row, err := environmentRow(workspaceID, env)
	if err != nil {
		return err
	}
	return s.inTx(ctx, "saving environment "+env.ID, func(tx *writeTx) error {
		return putEntity(ctx, tx, row)
	})
}

// DeleteEnvironment deletes an environment with its variables.
func (s *Store) DeleteEnvironment(ctx context.Context, workspaceID, id string) error {
	return s.inTx(ctx, "deleting environment "+id, func(tx *writeTx) error {
		env, err := getEntity(ctx, tx, domain.EntityEnvironment, id)
		if err != nil {
			return nil
		}
		vars, err := listEntities(ctx, tx, workspaceID, kinds(domain.EntityVariable), `parent_id = ?`, id)
		if err != nil {
			return err
		}
		return deleteEntities(ctx, tx, workspaceID, append([]entity{env}, vars...))
	})
}

// SaveScope writes an environment (nil for the globals) and the whole variable list of its scope in
// one transaction: variables missing from the list are deleted, unchanged ones are left alone.
func (s *Store) SaveScope(ctx context.Context, workspaceID string, env *domain.Environment,
	scope domain.EnvScope, vars []domain.Variable) error {
	return s.inTx(ctx, "saving variables", func(tx *writeTx) error {
		if env != nil {
			row, err := environmentRow(workspaceID, *env)
			if err != nil {
				return err
			}
			if err := putEntity(ctx, tx, row); err != nil {
				return err
			}
		}
		kept := map[string]bool{}
		for _, v := range vars {
			kept[v.ID] = true
		}
		held, err := listEntities(ctx, tx, workspaceID, kinds(domain.EntityVariable), `parent_id = ?`,
			scope.Environment)
		if err != nil {
			return err
		}
		gone := []entity{}
		for _, row := range held {
			if !kept[row.ID] {
				gone = append(gone, row)
			}
		}
		if err := deleteEntities(ctx, tx, workspaceID, gone); err != nil {
			return err
		}
		for _, v := range vars {
			row, err := variableRow(workspaceID, scope, v)
			if err != nil {
				return err
			}
			if err := putEntity(ctx, tx, row); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) SaveVariable(ctx context.Context, workspaceID string, scope domain.EnvScope,
	v domain.Variable) error {
	row, err := variableRow(workspaceID, scope, v)
	if err != nil {
		return err
	}
	return s.inTx(ctx, "saving variable "+v.ID, func(tx *writeTx) error {
		return putEntity(ctx, tx, row)
	})
}

func (s *Store) DeleteVariable(ctx context.Context, workspaceID, id string) error {
	return s.inTx(ctx, "deleting variable "+id, func(tx *writeTx) error {
		row, err := getEntity(ctx, tx, domain.EntityVariable, id)
		if err != nil || row.Workspace != workspaceID {
			return nil
		}
		return deleteEntities(ctx, tx, workspaceID, []entity{row})
	})
}

func (s *Store) VariableValue(ctx context.Context, workspaceID, id string) (string, error) {
	row, err := getEntity(ctx, s.db, domain.EntityVariable, id)
	if err == nil && row.Workspace != workspaceID {
		err = domain.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	v, err := variableFrom(row)
	return v.Value, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
