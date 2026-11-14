package script

import (
	"context"
	"errors"
	"schemo/internal/domain"
)

// Chain is what runs around a request, outermost first; levels with nothing to run are left out.
func (s *Scripts) Chain(ctx context.Context, nodeID string) ([]Level, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return s.chain(ctx, workspace, nodeID)
}

func (s *Scripts) chain(ctx context.Context, workspace, nodeID string) ([]Level, error) {
	// An empty id is a request nothing was composed around: one that came out of a link in a response,
	// or out of the browser. Nothing is above it because there is no it.
	if nodeID == "" {
		return []Level{}, nil
	}

	tree, err := s.store.Collections(ctx, workspace)
	if err != nil {
		return nil, err
	}
	for _, collection := range tree {
		chain, ok, err := s.pathTo(ctx, workspace, collection, nodeID)
		if err != nil {
			return nil, err
		}
		if ok {
			return chain, nil
		}
	}

	// A request in no tree, or under a level since deleted, runs only its own code.
	scripts, err := s.store.Scripts(ctx, workspace, nodeID)
	if errors.Is(err, domain.ErrNotFound) {
		return []Level{}, nil
	}
	if err != nil {
		return nil, err
	}
	if scripts.Empty() {
		return []Level{}, nil
	}
	return []Level{{NodeID: nodeID, Kind: KindDraft, Scripts: *scripts}}, nil
}

// pathTo walks down from a collection to id, giving the chain's order; id being the collection is a
// chain of one.
func (s *Scripts) pathTo(
	ctx context.Context,
	workspace string,
	from domain.Collection,
	id string,
) ([]Level, bool, error) {
	own, err := s.levelOf(ctx, workspace, from.ID, from.Name, KindCollection)
	if err != nil {
		return nil, false, err
	}
	if from.ID == id {
		return levelsOf(own), true, nil
	}

	for _, entry := range from.Level() {
		if entry.Collection != nil {
			below, ok, err := s.pathTo(ctx, workspace, *entry.Collection, id)
			if err != nil {
				return nil, false, err
			}
			if ok {
				return append(levelsOf(own), below...), true, nil
			}
			continue
		}
		if entry.Node.ID != id {
			continue
		}

		last, err := s.levelOf(ctx, workspace, entry.Node.ID, entry.Node.Name, KindRequest)
		if err != nil {
			return nil, false, err
		}
		return levelsOf(own, last), true, nil
	}
	return nil, false, nil
}

// levelOf is one step of a chain, or nothing when the level has no code of its own to run: a level
// that runs nothing is not a step, it is a collection on the way.
func (s *Scripts) levelOf(
	ctx context.Context,
	workspace, id string,
	name string,
	kind string,
) (*Level, error) {
	scripts, err := s.store.Scripts(ctx, workspace, id)
	if err != nil {
		return nil, err
	}
	if scripts.Empty() {
		return nil, nil
	}
	return &Level{NodeID: id, Name: name, Kind: kind, Scripts: *scripts}, nil
}

func levelsOf(levels ...*Level) []Level {
	out := []Level{}
	for _, level := range levels {
		if level != nil {
			out = append(out, *level)
		}
	}
	return out
}

// Level is one step of a chain: a collection, a collection inside one, or a request, and what it
// runs.
type Level struct {
	NodeID  string         `json:"nodeId"`
	Name    string         `json:"name"`
	Kind    string         `json:"kind"`
	Scripts domain.Scripts `json:"scripts"`
}
