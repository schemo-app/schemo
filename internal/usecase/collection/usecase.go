// Package collection owns the saved requests: the tree they live in, what inherits from what, and
// the runs that walk it.
package collection

import (
	"context"
	"sync/atomic"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

type UseCase struct {
	store       Store
	scope       domain.Scope
	sender      Sender
	assertions  Assertions
	environment Environment
	notifier    domain.Notifier
	ids         platform.IDGen

	// Only one run at a time: two of them would write their rows into the same overview.
	running atomic.Bool
	stopped atomic.Bool
}

func NewUseCase(
	store Store,
	scope domain.Scope,
	sender Sender,
	assertions Assertions,
	environment Environment,
	notifier domain.Notifier,
	ids platform.IDGen,
) *UseCase {
	return &UseCase{
		store:       store,
		scope:       scope,
		sender:      sender,
		assertions:  assertions,
		environment: environment,
		notifier:    notifier,
		ids:         ids,
	}
}

// Tree is every collection with its nodes, which is what the list draws and what a run walks.
func (u *UseCase) Tree(ctx context.Context) ([]domain.Collection, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return u.store.Collections(ctx, workspace)
}

// TreeOf is the tree of one named space, for a reader that is not the window: the MCP server reads
// spaces the window is not showing, and switching the app there to read them would be a window
// moving under somebody's cursor.
func (u *UseCase) TreeOf(ctx context.Context, workspace string) ([]domain.Collection, error) {
	return u.store.Collections(ctx, workspace)
}

// SpaceOf is which space a collection belongs to, asked by a reader that reached the id from
// outside: the MCP server checks the answer against what an agent may reach before it reads or
// sends anything of it.
func (u *UseCase) SpaceOf(ctx context.Context, collectionID string) (string, error) {
	return u.store.SpaceOf(ctx, collectionID)
}

// PathOf is the collections from the asked one up to the root of its space, the asked one first and
// each said by its name alone: a page is a whole tree, so the page a folder sits inside of is found
// by walking up from it. An id the tree never meets answers with nothing rather than a refusal.
func (u *UseCase) PathOf(ctx context.Context, workspace, id string) ([]domain.Collection, error) {
	tree, err := u.store.Collections(ctx, workspace)
	if err != nil {
		return nil, err
	}
	return pathTo(tree, id), nil
}

// pathTo is the way down to a collection, turned around as it unwinds: the collection comes first,
// then the levels it was found inside of, outwards. Nothing at all for an id it never meets.
func pathTo(levels []domain.Collection, id string) []domain.Collection {
	for _, level := range levels {
		if level.ID == id {
			return []domain.Collection{{ID: level.ID, Name: level.Name}}
		}
		if below := pathTo(level.Children, id); below != nil {
			return append(below, domain.Collection{ID: level.ID, Name: level.Name})
		}
	}
	return nil
}

// Node reads one node whole: everything opening a request needs, and the tree deliberately left
// out.
func (u *UseCase) Node(ctx context.Context, id string) (domain.CollectionNode, error) {
	return u.store.Node(ctx, id)
}

// Contents reads the requests a collection holds, as its page draws them: a name, a method, an
// address and the folder each of them sits in. Everything inside the collection, and not the level
// alone — the page's table is what running the collection would send, and a run reaches the whole
// subtree, so a report that listed only the top level would be a report of another run.
func (u *UseCase) Contents(ctx context.Context, collectionID string) ([]domain.LevelRow, error) {
	return u.store.ContentRows(ctx, collectionID)
}

// findNode looks a request up in the tree, collections inside collections included: the tree is
// small enough to walk, and walking it is what says whether the id names a request at all.
func findNode(tree []domain.Collection, id string) (domain.CollectionNode, bool) {
	for _, collection := range tree {
		for _, node := range collection.Items {
			if node.ID == id {
				return node, true
			}
		}
		if node, ok := findNode(collection.Children, id); ok {
			return node, true
		}
	}
	return domain.CollectionNode{}, false
}

// collection finds a collection by id and says whether it found one rather than failing: nodes are
// addressed in the same id space, and a caller naming an id does not say which of the two it named.
func (u *UseCase) collection(
	ctx context.Context,
	workspace, id string,
) (domain.Collection, bool, error) {
	tree, err := u.store.Collections(ctx, workspace)
	if err != nil {
		return domain.Collection{}, false, err
	}
	collection, ok := findCollection(tree, id)
	return collection, ok, nil
}
