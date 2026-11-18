package db

import (
	"context"
	"fmt"

	"schemo/internal/domain"
)

func environmentRow(workspace string, env domain.Environment) (entity, error) {
	parent := env.CollectionID
	env.CollectionID = ""
	env.Vars = nil
	data, err := encodeData(env)
	if err != nil {
		return entity{}, err
	}
	return entity{Kind: domain.EntityEnvironment, ID: env.ID, Workspace: workspace,
		Parent: parent, Position: int64(env.Position), Name: env.Name, Data: data}, nil
}

func environmentFrom(e entity) (domain.Environment, error) {
	env, err := decodeData[domain.Environment](e)
	env.ID, env.Name, env.Position, env.Vars = e.ID, e.Name, int(e.Position), []domain.Variable{}
	env.CollectionID = e.Parent
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
		if env.CollectionID == "" {
			state.Environments = append(state.Environments, env)
		} else {
			state.CollectionEnvironments = append(state.CollectionEnvironments, env)
		}
	}
	for _, environments := range [][]domain.Environment{state.Environments, state.CollectionEnvironments} {
		for i := range environments {
			if held := vars[environments[i].ID]; held != nil {
				environments[i].Vars = held
			}
		}
	}
	if held := vars[""]; held != nil {
		state.Globals = held
	}
	state.ActiveID, err = s.activeEnvironment(ctx, workspaceID)
	if err != nil {
		return state, err
	}
	state.CollectionActive, err = s.collectionSelections(ctx, workspaceID, state)
	return state, err
}

const collectionEnvironmentSetting = "collection_environment/"

func (s *Store) collectionSelections(
	ctx context.Context, workspaceID string, state domain.EnvState,
) (map[string]string, error) {
	active := map[string]string{}
	for _, env := range state.CollectionEnvironments {
		if env.Folder {
			continue
		}
		if env.Base {
			active[env.CollectionID] = env.ID
		} else if active[env.CollectionID] == "" {
			active[env.CollectionID] = domain.EnvironmentBase
		}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, s.value
		  FROM entities e JOIN settings s ON s.key = ? || e.id
		 WHERE e.workspace_id = ? AND e.kind = ? AND e.deleted_at IS NULL`,
		collectionEnvironmentSetting, workspaceID, domain.EntityCollection)
	if err != nil {
		return nil, fmt.Errorf("reading collection environment selections: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var collection, selection string
		if err := rows.Scan(&collection, &selection); err != nil {
			return nil, err
		}
		switch selection {
		case domain.EnvironmentBase:
			if active[collection] == "" {
				active[collection] = selection
			}
		case domain.EnvironmentWindow:
			active[collection] = selection
		default:
			if env, ok := state.Find(selection); ok && !env.Folder &&
				(env.CollectionID == "" || env.CollectionID == collection) {
				active[collection] = selection
			} else if active[collection] == "" {
				active[collection] = domain.EnvironmentBase
			}
		}
	}
	return active, rows.Err()
}

// SelectCollectionEnvironment selects one of the collection's own environments. An empty id restores its default.
func (s *Store) SelectCollectionEnvironment(ctx context.Context, workspaceID, collectionID, id string) error {
	return s.inTx(ctx, "selecting a collection environment", func(tx *writeTx) error {
		collection, err := getEntity(ctx, tx, domain.EntityCollection, collectionID)
		if err != nil {
			return err
		}
		if collection.Workspace != workspaceID {
			return domain.ErrNotFound
		}
		if id == "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, collectionEnvironmentSetting+collectionID); err != nil {
				return fmt.Errorf("clearing collection environment: %w", err)
			}
			tx.touched(domain.EntityEnvironment, workspaceID)
			return nil
		}
		if id != domain.EnvironmentBase {
			row, err := getEntity(ctx, tx, domain.EntityEnvironment, id)
			if err != nil {
				return err
			}
			if row.Workspace != workspaceID || row.Parent != collectionID {
				return domain.ErrNotFound
			}
			env, err := environmentFrom(row)
			if err != nil {
				return err
			}
			if env.Folder {
				return domain.ErrNotAllowed
			}
		}
		if err := saveSettingIn(ctx, tx, collectionEnvironmentSetting+collectionID, id); err != nil {
			return err
		}
		tx.touched(domain.EntityEnvironment, workspaceID)
		return nil
	})
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
		if err != nil || env.Workspace != workspaceID {
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
		return saveScope(ctx, tx, workspaceID, env, scope, vars)
	})
}

func saveScope(ctx context.Context, tx *writeTx, workspaceID string, env *domain.Environment,
	scope domain.EnvScope, vars []domain.Variable) error {
	if env != nil && env.CollectionID != "" {
		parent, err := getEntity(ctx, tx, domain.EntityCollection, env.CollectionID)
		if err != nil {
			return err
		}
		if parent.Workspace != workspaceID {
			return domain.ErrNotFound
		}
	}
	if env != nil {
		row, err := environmentRow(workspaceID, *env)
		if err != nil {
			return err
		}
		if err := putEntity(ctx, tx, row); err != nil {
			return err
		}
	}
	if env == nil && scope.Environment != "" {
		parent, err := getEntity(ctx, tx, domain.EntityEnvironment, scope.Environment)
		if err != nil {
			return err
		}
		if parent.Workspace != workspaceID {
			return domain.ErrNotFound
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
