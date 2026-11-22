package workspace

import (
	"context"
	"fmt"
	"slices"

	"schemo/internal/db"
	"schemo/internal/domain"
)

// MoveVariable moves a collection's value between its own variables and environment blocks.
// A matching target name is replaced. Secret values stay in Go throughout the operation.
func (s *Environments) MoveVariable(ctx context.Context, collectionID, variableID, from, into string) (bool, error) {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return false, err
	}
	tree, err := s.store.Collections(ctx, workspace)
	if err != nil {
		return false, err
	}
	collection, ok := findCollection(tree, collectionID)
	if !ok || collection.ParentID != "" {
		return false, collectionNotFound(collectionID)
	}
	for _, id := range []string{from, into} {
		if id == "" {
			continue
		}
		env, ok := state.Find(id)
		if !ok {
			return false, environmentNotFound(id)
		}
		if env.Readonly {
			return false, domain.Refuse(domain.CodeEnvironmentReadOnly, domain.ErrNotAllowed, nil)
		}
	}
	variables := func(id string) []domain.Variable {
		if id == "" {
			return collection.Variables
		}
		env, _ := state.Find(id)
		return env.Block(collectionID)
	}
	source := slices.Clone(variables(from))
	at := slices.IndexFunc(source, func(variable domain.Variable) bool { return variable.ID == variableID })
	if at < 0 {
		return false, fmt.Errorf("variable %s: %w", variableID, domain.ErrNotFound)
	}
	if from == into {
		return false, nil
	}
	variable := source[at]
	if into == "" && variable.Kind == domain.VariableSecret {
		return false, domain.Refuse(domain.CodeVariableSecret, domain.ErrNotAllowed, nil)
	}
	target := slices.Clone(variables(into))
	replaced := variable.Name != "" && slices.ContainsFunc(target, func(held domain.Variable) bool {
		return held.Name == variable.Name
	})
	target = slices.DeleteFunc(target, func(held domain.Variable) bool {
		return variable.Name != "" && held.Name == variable.Name
	})
	// New identity prevents a source deletion from tombstoning the destination during sync.
	variable.ID, variable.HasValue = s.ids(), false
	target = append(target, variable)
	source = slices.Delete(source, at, at+1)
	changes := db.ScopeChanges{}
	for _, change := range []struct {
		environment string
		vars        []domain.Variable
	}{{from, source}, {into, target}} {
		for index := range change.vars {
			change.vars[index].Position = index + 1
		}
		if change.environment == "" {
			collection.Variables = change.vars
			changes.Collection = &collection
			continue
		}
		changes.Scopes = append(changes.Scopes, db.ScopeWrite{
			Scope: domain.EnvScope{Environment: change.environment, Collection: collectionID},
			Vars:  change.vars,
		})
	}
	return replaced, s.store.SaveScopes(ctx, workspace, changes)
}
