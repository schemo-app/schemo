package workspace

// What updating a collection from its API description reads and writes. Each call names the
// workspace, so an update lands where it was reviewed even if the window has moved on.

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"schemo/internal/db"
	"schemo/internal/domain"
)

// Open is the workspace an update is reviewed in.
//
//wails:ignore
func (s *Collections) Open(ctx context.Context) (string, error) {
	return s.scope.ActiveWorkspace(ctx)
}

// WholeLevel reads a collection or folder with every request whole, and names the top-level
// collection whose environment values answer for it.
//
//wails:ignore
func (s *Collections) WholeLevel(ctx context.Context, workspace, id string) (domain.Collection, string, error) {
	tree, err := s.store.Collections(ctx, workspace)
	if err != nil {
		return domain.Collection{}, "", err
	}
	level, ok := findCollection(tree, id)
	if !ok {
		return domain.Collection{}, "", collectionNotFound(id)
	}
	full, err := s.fullCollection(ctx, workspace, level)
	return full, rootOf(tree, id), err
}

// SpecLink is where this device reads a collection's description; false when it keeps no such place.
//
//wails:ignore
func (s *Collections) SpecLink(ctx context.Context, workspace, id string) (domain.SpecLink, bool, error) {
	return s.store.SpecLink(ctx, workspace, id)
}

// SpecSnapshot is what a collection's description said at the last update; nil when none is kept.
//
//wails:ignore
func (s *Collections) SpecSnapshot(ctx context.Context, id string) (json.RawMessage, error) {
	return s.store.SpecSnapshot(ctx, id)
}

// WriteUpdate writes an update from a description and answers with the tree to draw.
//
//wails:ignore
func (s *Collections) WriteUpdate(
	ctx context.Context,
	workspace string,
	update domain.TreeUpdate,
) ([]domain.Collection, error) {
	for index := range update.Levels {
		update.Levels[index].Name = clip(strings.TrimSpace(update.Levels[index].Name))
	}
	for index := range update.Patches {
		patch := &update.Patches[index]
		patch.Name = clip(strings.TrimSpace(patch.Name))
		patch.Variables = numberedVariables(withVariablesIDs(s.ids, patch.Variables))
	}
	for index := range update.Requests {
		node := &update.Requests[index]
		node.Name = clip(strings.TrimSpace(node.Name))
		node.Params = withIDs(s.ids, node.Params)
		node.Headers = withIDs(s.ids, node.Headers)
		node.PathParams = withIDs(s.ids, node.PathParams)
		node.Form = withFormIDs(s.ids, node.Form)
	}
	if err := s.store.SaveTree(ctx, workspace, update); err != nil {
		return nil, err
	}
	return s.store.Collections(ctx, workspace)
}

func numberedVariables(variables []domain.Variable) []domain.Variable {
	for index := range variables {
		variables[index].Position = index + 1
	}
	return variables
}

// StateOf is State of a named workspace, secrets withheld.
//
//wails:ignore
func (s *Environments) StateOf(ctx context.Context, workspace string) (domain.EnvState, error) {
	return s.InWorkspace(workspace).State(ctx)
}

// Held is every environment of a workspace with its values, secrets included, so that writing a
// merged list back keeps what it did not change.
//
//wails:ignore
func (s *Environments) Held(ctx context.Context, workspace string) ([]domain.Environment, error) {
	_, state, err := s.InWorkspace(workspace).load(ctx)
	return state.Environments, err
}

// WriteBlocks sets what each named environment holds for one collection, making an environment
// that is not there. A read-only one is left alone.
//
//wails:ignore
func (s *Environments) WriteBlocks(
	ctx context.Context,
	workspace, collection string,
	drafts []domain.EnvironmentDraft,
) error {
	scoped := s.InWorkspace(workspace)
	for _, draft := range drafts {
		_, state, err := scoped.load(ctx)
		if err != nil {
			return err
		}
		name := strings.TrimSpace(draft.Name)
		if runes := []rune(name); len(runes) > maxEnvironmentName {
			name = string(runes[:maxEnvironmentName])
		}
		environment, found := findByName(state.Environments, name)
		if found && environment.Readonly {
			continue
		}
		var made *domain.Environment
		if !found {
			environment = domain.Environment{ID: s.ids(), Name: name, Color: draft.Color,
				Position: nextEnvironmentPosition(state.Environments)}
			made = &environment
		}
		var values []domain.Variable
		for _, block := range draft.Blocks {
			values = append(values, block.Variables...)
		}
		values = slices.Clone(values)
		for index := range values {
			if values[index].ID == "" {
				values[index].ID = s.ids()
				values[index].Kind = kindFor(values[index].Kind == domain.VariableSecret)
			}
		}
		writes := []db.ScopeWrite{{
			Scope: domain.EnvScope{Environment: environment.ID, Collection: collection},
			Vars:  numberedVariables(values),
		}}
		if err := s.store.SaveScopes(ctx, workspace, db.ScopeChanges{Environment: made, Scopes: writes}); err != nil {
			return err
		}
	}
	return nil
}
