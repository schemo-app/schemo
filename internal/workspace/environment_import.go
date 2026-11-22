package workspace

import (
	"context"
	"slices"
	"strings"

	"schemo/internal/db"
	"schemo/internal/domain"
)

// ImportVariables adds a file's variables to an environment, one of its blocks or the globals.
// Only names the set does not hold are added; a value already there is never replaced, so the same
// file imported twice adds nothing the second time.
func (s *Environments) ImportVariables(ctx context.Context, scope domain.EnvScope, vars []domain.Variable) (domain.VarImport, error) {
	workspace, state, err := s.writable(ctx, scope)
	if err != nil {
		return domain.VarImport{}, err
	}
	report := domain.VarImport{}
	merged := s.addMissing(state.Variables(scope), vars, &report)
	if report.Added == 0 {
		return report, nil
	}
	return report, s.saveScope(ctx, workspace, scope, merged)
}

// ImportDrafts writes the environments a document described. An environment is found by its name
// alone — case and surrounding spaces aside — and made when there is none; nothing is guessed and
// nothing renamed. A block goes to the collection the document's index names in collections. A
// name the set already holds keeps its value. A read-only environment is not written into: its
// values go to a new one named with the suffix the window gives.
//
//wails:ignore
func (s *Environments) ImportDrafts(ctx context.Context, drafts []domain.EnvironmentDraft,
	collections []string, readonlySuffix string) (domain.EnvImport, error) {
	report := domain.EnvImport{Created: []string{}}
	for _, draft := range drafts {
		if err := s.importDraft(ctx, draft, collections, readonlySuffix, &report); err != nil {
			return domain.EnvImport{}, err
		}
	}
	state, err := s.State(ctx)
	report.State = state
	return report, err
}

func (s *Environments) importDraft(ctx context.Context, draft domain.EnvironmentDraft,
	collections []string, readonlySuffix string, report *domain.EnvImport) error {
	name := strings.TrimSpace(draft.Name)
	if name == "" {
		return nil
	}
	if runes := []rune(name); len(runes) > maxEnvironmentName {
		name = string(runes[:maxEnvironmentName])
	}
	workspace, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	env, found := findByName(state.Environments, name)
	if found && env.Readonly {
		name = strings.TrimSpace(name + " " + strings.TrimSpace(readonlySuffix))
		env, found = findByName(state.Environments, name)
		if found && env.Readonly {
			name, found = freeName(name, state.Environments), false
		}
	}
	vars := domain.VarImport{}
	writes := []db.ScopeWrite{}
	var made *domain.Environment
	if !found {
		env = domain.Environment{ID: s.ids(), Name: name, Color: draft.Color,
			Position: nextEnvironmentPosition(state.Environments)}
		made = &env
		report.Created = append(report.Created, name)
	}
	if shared := s.addMissing(env.Vars, draft.Variables, &vars); made != nil || len(shared) != len(env.Vars) {
		writes = append(writes, db.ScopeWrite{Scope: domain.EnvScope{Environment: env.ID}, Vars: shared})
	}
	blocks := map[string][]domain.Variable{}
	for _, block := range draft.Blocks {
		if block.Collection < 0 || block.Collection >= len(collections) || collections[block.Collection] == "" {
			continue
		}
		collection := collections[block.Collection]
		held := env.Block(collection)
		if pending, ok := blocks[collection]; ok {
			held = pending
		}
		if merged := s.addMissing(held, block.Variables, &vars); len(merged) != len(held) {
			blocks[collection] = merged
		}
	}
	for collection, merged := range blocks {
		writes = append(writes, db.ScopeWrite{
			Scope: domain.EnvScope{Environment: env.ID, Collection: collection}, Vars: merged})
	}
	report.Added += vars.Added
	report.Kept += vars.Kept
	return s.store.SaveScopes(ctx, workspace, db.ScopeChanges{Environment: made, Scopes: writes})
}

// addMissing appends the incoming names the list does not hold, with ids minted here, and counts
// what it did. A name listed twice in the incoming list is added once, with its last value.
func (s *Environments) addMissing(held, incoming []domain.Variable, report *domain.VarImport) []domain.Variable {
	out := slices.Clone(held)
	have := map[string]bool{}
	for _, v := range held {
		have[v.Name] = true
	}
	added := map[string]int{}
	for _, one := range incoming {
		name := strings.TrimSpace(one.Name)
		if name == "" || !domain.IsVariableName(name) {
			continue
		}
		if have[name] {
			if report != nil {
				report.Kept++
			}
			continue
		}
		v := domain.Variable{ID: s.ids(), Name: name, Value: one.Value,
			Kind: kindFor(one.Kind == domain.VariableSecret), Enabled: one.Enabled}
		if at, seen := added[name]; seen {
			v.ID = out[at].ID
			out[at] = v
			continue
		}
		added[name] = len(out)
		out = append(out, v)
		if report != nil {
			report.Added++
		}
	}
	for i := range out {
		out[i].Position = i + 1
	}
	return out
}
