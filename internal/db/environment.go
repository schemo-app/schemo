package db

import (
	"context"

	"schemo/internal/domain"
)

func environmentRow(workspace string, env domain.Environment) (entity, error) {
	env.Vars, env.Collections = nil, nil
	data, err := encodeData(env)
	if err != nil {
		return entity{}, err
	}
	return entity{Kind: domain.EntityEnvironment, ID: env.ID, Workspace: workspace,
		Position: int64(env.Position), Name: env.Name, Data: data}, nil
}

func environmentFrom(e entity) (domain.Environment, error) {
	env, err := decodeData[domain.Environment](e)
	env.ID, env.Name, env.Position = e.ID, e.Name, int(e.Position)
	env.Vars, env.Collections = []domain.Variable{}, map[string][]domain.Variable{}
	return env, err
}

// storedVariable is a variable row's data: a block's row says which collection it is for.
type storedVariable struct {
	domain.Variable
	Collection string `json:"collection,omitempty"`
}

// variableRow files a variable under its environment, or under no parent for the globals.
func variableRow(workspace string, scope domain.EnvScope, v domain.Variable) (entity, error) {
	v.HasValue = false
	data, err := encodeData(storedVariable{Variable: v, Collection: blockOf(scope)})
	if err != nil {
		return entity{}, err
	}
	key := ""
	if v.Name != "" {
		// Unnamed rows are drafts and never collide.
		key = variableKey(scope.Environment, blockOf(scope), v.Name)
	}
	return entity{Kind: domain.EntityVariable, ID: v.ID, Workspace: workspace, Parent: scope.Environment,
		Position: int64(v.Position), Name: v.Name, Key: key, Data: data}, nil
}

// variableKey keeps a name unique within one set: the globals, an environment, or one block.
func variableKey(environment, collection, name string) string {
	return naturalKey(environment, collection, name)
}

func variableFrom(e entity) (domain.Variable, string, error) {
	stored, err := decodeData[storedVariable](e)
	v := stored.Variable
	v.ID, v.Name, v.Position, v.HasValue = e.ID, e.Name, int(e.Position), v.Value != ""
	return v, stored.Collection, err
}

// blockOf is the collection a scope narrows to; the globals have no blocks.
func blockOf(scope domain.EnvScope) string {
	if scope.Environment == "" {
		return ""
	}
	return scope.Collection
}

func (s *Store) EnvState(ctx context.Context, workspaceID string) (domain.EnvState, error) {
	state := domain.EnvState{Environments: []domain.Environment{}, Globals: []domain.Variable{}}
	rows, err := listEntities(ctx, s.db, workspaceID,
		kinds(domain.EntityEnvironment, domain.EntityVariable), "")
	if err != nil {
		return state, err
	}
	vars := map[string][]domain.Variable{}
	blocks := map[string]map[string][]domain.Variable{}
	for _, row := range rows {
		if row.Kind == domain.EntityVariable {
			v, collection, err := variableFrom(row)
			if err != nil {
				return state, err
			}
			if collection != "" && row.Parent != "" {
				if blocks[row.Parent] == nil {
					blocks[row.Parent] = map[string][]domain.Variable{}
				}
				blocks[row.Parent][collection] = append(blocks[row.Parent][collection], v)
				continue
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
		if held := blocks[state.Environments[i].ID]; held != nil {
			state.Environments[i].Collections = held
		}
	}
	if held := vars[""]; held != nil {
		state.Globals = held
	}
	state.ActiveID, err = s.activeEnvironment(ctx, workspaceID)
	return state, err
}

func (s *Store) SaveEnvironment(ctx context.Context, workspaceID string, env domain.Environment) error {
	return s.SaveEnvironments(ctx, workspaceID, []domain.Environment{env})
}

// SaveEnvironments writes environments as given, without their variables: a new order is one
// write, not one per row.
func (s *Store) SaveEnvironments(ctx context.Context, workspaceID string, envs []domain.Environment) error {
	return s.inTx(ctx, "saving environments", func(tx *writeTx) error {
		for _, env := range envs {
			row, err := environmentRow(workspaceID, env)
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

// DeleteEnvironment deletes an environment with its variables, blocks included.
func (s *Store) DeleteEnvironment(ctx context.Context, workspaceID, id string) error {
	return s.inTx(ctx, "deleting environment "+id, func(tx *writeTx) error {
		return deleteEnvironment(ctx, tx, workspaceID, id)
	})
}

func deleteEnvironment(ctx context.Context, tx *writeTx, workspaceID, id string) error {
	env, err := getEntity(ctx, tx, domain.EntityEnvironment, id)
	if err != nil {
		return nil
	}
	vars, err := listEntities(ctx, tx, workspaceID, kinds(domain.EntityVariable), `parent_id = ?`, id)
	if err != nil {
		return err
	}
	return deleteEntities(ctx, tx, workspaceID, append([]entity{env}, vars...))
}

// SaveScope writes an environment (nil for the globals) and the whole variable list of its scope in
// one transaction: variables missing from the list are deleted, unchanged ones are left alone.
func (s *Store) SaveScope(ctx context.Context, workspaceID string, env *domain.Environment,
	scope domain.EnvScope, vars []domain.Variable) error {
	return s.SaveScopes(ctx, workspaceID, ScopeChanges{
		Environment: env,
		Scopes:      []ScopeWrite{{Scope: scope, Vars: vars}},
	})
}

// ScopeWrite is one scope's whole list, for writes that change several scopes at once.
type ScopeWrite struct {
	Scope domain.EnvScope
	Vars  []domain.Variable
}

// ScopeChanges keeps a variable operation's metadata, lists and deletion in one transaction.
type ScopeChanges struct {
	Environment *domain.Environment
	Collection  *domain.Collection
	Scopes      []ScopeWrite
	DeleteEnv   string
}

// SaveScopes applies a variable operation atomically, including its environment or collection.
func (s *Store) SaveScopes(ctx context.Context, workspaceID string, changes ScopeChanges) error {
	return s.inTx(ctx, "saving variables", func(tx *writeTx) error {
		if changes.Environment != nil {
			row, err := environmentRow(workspaceID, *changes.Environment)
			if err != nil {
				return err
			}
			if err := putEntity(ctx, tx, row); err != nil {
				return err
			}
		}
		if changes.Collection != nil {
			row, err := collectionRow(workspaceID, *changes.Collection)
			if err != nil {
				return err
			}
			if err := putEntity(ctx, tx, row); err != nil {
				return err
			}
		}
		for _, write := range changes.Scopes {
			if err := saveScope(ctx, tx, workspaceID, write.Scope, write.Vars); err != nil {
				return err
			}
		}
		if changes.DeleteEnv == "" {
			return nil
		}
		return deleteEnvironment(ctx, tx, workspaceID, changes.DeleteEnv)
	})
}

func saveScope(ctx context.Context, tx *writeTx, workspaceID string, scope domain.EnvScope, vars []domain.Variable) error {
	kept := map[string]bool{}
	for _, v := range vars {
		kept[v.ID] = true
	}
	held, err := scopeRows(ctx, tx, workspaceID, scope)
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

// scopeRows are the variable rows of one scope: the environment's own, one of its blocks, or the
// globals.
func scopeRows(ctx context.Context, q querier, workspaceID string, scope domain.EnvScope) ([]entity, error) {
	rows, err := listEntities(ctx, q, workspaceID, kinds(domain.EntityVariable), `parent_id = ?`,
		scope.Environment)
	if err != nil {
		return nil, err
	}
	want := blockOf(scope)
	out := []entity{}
	for _, row := range rows {
		_, collection, err := variableFrom(row)
		if err != nil {
			return nil, err
		}
		if collection == want {
			out = append(out, row)
		}
	}
	return out, nil
}

// blockRows are the variable rows the environments hold for some collections.
func blockRows(ctx context.Context, q querier, workspaceID string, collections map[string]bool) ([]entity, error) {
	rows, err := listEntities(ctx, q, workspaceID, kinds(domain.EntityVariable), `parent_id <> ''`)
	if err != nil {
		return nil, err
	}
	out := []entity{}
	for _, row := range rows {
		_, collection, err := variableFrom(row)
		if err != nil {
			return nil, err
		}
		if collection != "" && collections[collection] {
			out = append(out, row)
		}
	}
	return out, nil
}

// CopyBlocks gives a copied collection what every environment holds for the original, under new
// ids: a variable is addressed by its id, and one id in two blocks is one variable in two places.
func (s *Store) CopyBlocks(ctx context.Context, workspaceID, from, to string, ids func() string) error {
	return s.inTx(ctx, "copying the environment values of "+from, func(tx *writeTx) error {
		rows, err := blockRows(ctx, tx, workspaceID, map[string]bool{from: true})
		if err != nil {
			return err
		}
		for _, row := range rows {
			v, _, err := variableFrom(row)
			if err != nil {
				return err
			}
			v.ID = ids()
			copied, err := variableRow(workspaceID, domain.EnvScope{Environment: row.Parent, Collection: to}, v)
			if err != nil {
				return err
			}
			if err := putEntity(ctx, tx, copied); err != nil {
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
	v, _, err := variableFrom(row)
	return v.Value, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
