package workspace

import (
	"context"
	"fmt"

	"schemo/internal/domain"
)

// Above reads the same inherited values for the editor that Requests reads for a run.
//
//wails:ignore
func (s *Collections) Above(ctx context.Context, id domain.DraftID) (Above, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return Above{}, err
	}
	tree, err := s.store.Collections(ctx, workspace)
	if err != nil {
		return Above{}, err
	}
	state, err := s.store.EnvState(ctx, workspace)
	if err != nil {
		return Above{}, err
	}
	_, above, _ := newInheritance(state).findRequest(tree, string(id), Above{})
	return above, nil
}

type inheritance struct {
	active  map[string]string
	bases   map[string][]domain.Variable
	folders map[string][]domain.Variable
}

func newInheritance(state domain.EnvState) inheritance {
	out := inheritance{
		active:  state.CollectionActive,
		bases:   map[string][]domain.Variable{},
		folders: map[string][]domain.Variable{},
	}
	for _, environment := range state.CollectionEnvironments {
		switch {
		case environment.Folder:
			out.folders[environment.CollectionID] = append(out.folders[environment.CollectionID], environment.Vars...)
		case environment.Base:
			out.bases[environment.CollectionID] = environment.Vars
		}
	}
	return out
}

func (in inheritance) enter(level domain.Collection, above Above) Above {
	above.Auth = effectiveAuth(level.Auth, above.Auth)
	active := in.active[level.ID]
	boundary := level.ParentID == "" || level.EnvironmentRoot || active != ""
	source := domain.ScopeFolder
	if boundary {
		// A separately imported collection keeps its own variable scope even under another API.
		above.Variables = nil
		above.EnvironmentID = active
		if above.EnvironmentID == "" && level.EnvironmentRoot && len(level.Variables) > 0 {
			above.EnvironmentID = domain.EnvironmentBase
		}
		source = ""
	}
	above.Variables = appendLevel(above.Variables, level.Variables, source)
	if boundary {
		above.Variables = appendLevel(above.Variables, in.bases[level.ID], source)
	}
	above.Variables = appendLevel(above.Variables, in.folders[level.ID], domain.ScopeFolder)
	return above
}

func (in inheritance) findRequest(
	levels []domain.Collection, id string, above Above,
) (domain.CollectionNode, Above, bool) {
	for _, level := range levels {
		at := in.enter(level, above)
		for _, node := range level.Items {
			if node.ID == id {
				at.Auth = effectiveAuth(node.Auth, at.Auth)
				return node, at, true
			}
		}
		if node, inherited, ok := in.findRequest(level.Children, id, at); ok {
			return node, inherited, true
		}
	}
	return domain.CollectionNode{}, Above{}, false
}

func (in inheritance) findLevel(
	levels []domain.Collection, id string, above Above,
) (domain.Collection, Above, bool) {
	for _, level := range levels {
		if level.ID == id {
			return level, above, true
		}
		if found, inherited, ok := in.findLevel(level.Children, id, in.enter(level, above)); ok {
			return found, inherited, true
		}
	}
	return domain.Collection{}, Above{}, false
}

type runnable struct {
	node  domain.CollectionNode
	above Above
}

func (in inheritance) requests(tree []domain.Collection, collectionID, nodeID string) ([]runnable, error) {
	level, above, ok := in.findLevel(tree, collectionID, Above{})
	if !ok {
		return nil, collectionNotFound(collectionID)
	}
	if nodeID == "" || nodeID == collectionID {
		return in.walk(level, above), nil
	}
	if nested, inherited, ok := in.findLevel(level.Children, nodeID, in.enter(level, above)); ok {
		return in.walk(nested, inherited), nil
	}
	node, inherited, ok := in.findRequest([]domain.Collection{level}, nodeID, above)
	if !ok {
		return nil, fmt.Errorf("node %s: %w", nodeID, domain.ErrNotFound)
	}
	return []runnable{{node: node, above: inherited}}, nil
}

func (in inheritance) walk(level domain.Collection, above Above) []runnable {
	at := in.enter(level, above)
	out := []runnable{}
	for _, entry := range level.Level() {
		if entry.Collection != nil {
			out = append(out, in.walk(*entry.Collection, at)...)
			continue
		}
		resolved := at
		resolved.Auth = effectiveAuth(entry.Node.Auth, at.Auth)
		out = append(out, runnable{node: *entry.Node, above: resolved})
	}
	return out
}

// Both walks go through here so "None" means the same in a run and the editor.
func effectiveAuth(level *domain.Auth, inherited *domain.Auth) *domain.Auth {
	if level.Answered() {
		return level
	}
	return inherited
}

// An unanswered level inherits, while an explicit "None" suppresses inherited auth.
func storedAuth(auth *domain.Auth) *domain.Auth {
	if auth == nil {
		return nil
	}
	return auth.Stored()
}

func appendLevel(above, level []domain.Variable, source domain.VarScope) []domain.Variable {
	if len(level) == 0 {
		return above
	}
	out := make([]domain.Variable, 0, len(above)+len(level))
	out = append(out, above...)
	for _, variable := range level {
		variable.Source = source
		out = append(out, variable)
	}
	return out
}

func findCollection(tree []domain.Collection, id string) (domain.Collection, bool) {
	for _, collection := range tree {
		if collection.ID == id {
			return collection, true
		}
		if found, ok := findCollection(collection.Children, id); ok {
			return found, true
		}
	}
	return domain.Collection{}, false
}
