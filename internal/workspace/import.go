package workspace

// A collection going out as a file and coming back as one. The tree a list draws is shallow, so
// what leaves is read whole first — an export of what the window holds would be an export of names.

import (
	"context"
	"fmt"
	"strings"

	"schemo/internal/domain"
)

// Import writes the collections a file held into the tree, at the end of the level named by
// parentID, or of the top level when it is empty. The whole slice arrives in one call because it
// came from one file: ids are minted here because a file has none, and every collection keeps its
// name, order and everything it came with, nested collections included.
//
//wails:ignore
func (s *Collections) Import(
	ctx context.Context,
	collections []domain.Collection,
	parentID string,
) ([]domain.Collection, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if parentID != "" {
		if _, ok, err := s.collection(ctx, workspace, parentID); err != nil {
			return nil, err
		} else if !ok {
			return nil, fmt.Errorf("collection %s: %w", parentID, domain.ErrNotFound)
		}
	}
	position, err := s.store.NextPosition(ctx, workspace, parentID)
	if err != nil {
		return nil, err
	}

	// Everything is built before any of it is written: an import that failed halfway would leave a
	// tree with half a file in it.
	pending := []pendingLevel{}
	for index, collection := range collections {
		name, err := importName(collection.Name)
		if err != nil {
			return nil, err
		}
		imported := domain.Collection{
			ID:          s.ids(),
			Name:        name,
			Description: strings.TrimSpace(collection.Description),
			ParentID:    parentID,
			Position:    position + int64(index),
			Auth:        collection.Auth,
			Scripts:     collection.Scripts,
			// A file carries what a level answers for `{{tokens}}` and writes no ids with them: the ids are
			// minted here, the way every other id of an import is.
			Variables: withVariablesIDs(s.ids, collection.Variables),
		}
		pending = append(pending, s.adopt(imported, collection.Items, collection.Children)...)
	}
	if err := s.saveTree(ctx, workspace, pending); err != nil {
		return nil, err
	}
	return s.store.Collections(ctx, workspace)
}

// importName keeps a file's name inside the ceiling rather than refusing it: a name longer than a
// person would type is the file's doing, and refusing the whole document over one line is not what
// the person asked for.
func importName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", domain.Refuse(domain.CodeNameEmpty, domain.ErrNotAllowed, nil)
	}
	return clip(name), nil
}

// adopt gives an imported subtree what a file does not write: ids, and where it lives. Everything
// else (names, addresses, rows) is the file's, and the level's own code travels beside it because
// SaveScripts can only write to a row that exists.
func (s *Collections) adopt(
	collection domain.Collection,
	items []domain.CollectionNode,
	children []domain.Collection,
) []pendingLevel {
	adopted := collection
	adopted.Items = []domain.CollectionNode{}
	adopted.Children = []domain.Collection{}
	adopted.Scripts = nil

	for _, node := range items {
		node.ID = s.ids()
		node.CollectionID = adopted.ID
		node.Params = withIDs(s.ids, node.Params)
		node.Headers = withIDs(s.ids, node.Headers)
		node.PathParams = withIDs(s.ids, node.PathParams)
		node.Form = withFormIDs(s.ids, node.Form)
		adopted.Items = append(adopted.Items, node)
	}

	out := []pendingLevel{{collection: adopted, scripts: collection.Scripts}}
	for _, child := range children {
		nested := domain.Collection{
			ID:          s.ids(),
			Name:        clip(strings.TrimSpace(child.Name)),
			Description: child.Description,
			ParentID:    adopted.ID,
			Position:    child.Position,
			Auth:        child.Auth,
			Scripts:     child.Scripts,
			Variables:   withVariablesIDs(s.ids, child.Variables),
		}
		out = append(out, s.adopt(nested, child.Items, child.Children)...)
	}
	return out
}

// Full is a collection with every request read whole, or a single request when the id names one:
// what an export writes down. The tree the window draws carries the method and nothing else, since
// two hundred rows have no business carrying two hundred bodies, so an export reads them again.
//
//wails:ignore
func (s *Collections) Full(ctx context.Context, id string) (domain.Collection, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.Collection{}, err
	}
	return s.fullOf(ctx, workspace, id)
}

// FullOf is Full for a level of a named workspace, which the window may not be looking at: a copy of a
// collection out of another workspace reads it there and writes it here.
//
//wails:ignore
func (s *Collections) FullOf(ctx context.Context, workspace, id string) (domain.Collection, error) {
	return s.fullOf(ctx, workspace, id)
}

func (s *Collections) fullOf(ctx context.Context, workspace, id string) (domain.Collection, error) {
	collection, ok, err := s.collection(ctx, workspace, id)
	if err != nil {
		return domain.Collection{}, err
	}
	if !ok {
		node, err := s.store.Node(ctx, id)
		if err != nil {
			return domain.Collection{}, err
		}
		return domain.Collection{Name: node.Name, Items: []domain.CollectionNode{node}}, nil
	}
	return s.fullCollection(ctx, workspace, collection)
}

func (s *Collections) fullCollection(
	ctx context.Context,
	workspace string,
	collection domain.Collection,
) (domain.Collection, error) {
	items, err := s.fullNodes(ctx, collection.Items)
	if err != nil {
		return domain.Collection{}, err
	}
	children := []domain.Collection{}
	for _, child := range collection.Children {
		full, err := s.fullCollection(ctx, workspace, child)
		if err != nil {
			return domain.Collection{}, err
		}
		children = append(children, full)
	}

	// The code of a level hangs off the row rather than off this read of it: the tree carries names
	// and methods, and an export that dropped the code would drop what the level does around its
	// requests.
	scripts, err := s.store.Scripts(ctx, workspace, collection.ID)
	if err != nil {
		return domain.Collection{}, err
	}
	return domain.Collection{
		ID:          collection.ID,
		Name:        collection.Name,
		Description: collection.Description,
		Position:    collection.Position,
		Auth:        collection.Auth,
		// What the level answers for `{{tokens}}` travels with it: a collection handed to somebody else
		// has to mean the same thing on the other side, which is the whole reason they exist.
		Variables: collection.Variables,
		Scripts:   scripts,
		Items:     items,
		Children:  children,
	}, nil
}

func (s *Collections) fullNodes(
	ctx context.Context,
	rows []domain.CollectionNode,
) ([]domain.CollectionNode, error) {
	out := make([]domain.CollectionNode, 0, len(rows))
	for _, row := range rows {
		node, err := s.store.Node(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		node.Position = row.Position
		out = append(out, node)
	}
	return out, nil
}

// Requests is every request under a node, read whole, with the authorization and variables its
// levels answer, in the order the tree draws them. An empty nodeID means the whole collection.
//
//wails:ignore
func (s *Collections) Requests(
	ctx context.Context,
	collectionID, nodeID string,
) ([]domain.ResolvedRequest, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	tree, err := s.store.Collections(ctx, workspace)
	if err != nil {
		return nil, err
	}
	requests, err := runnablesUnder(tree, collectionID, nodeID)
	if err != nil {
		return nil, err
	}

	rows := make([]domain.CollectionNode, 0, len(requests))
	for _, item := range requests {
		rows = append(rows, item.node)
	}
	nodes, err := s.fullNodes(ctx, rows)
	if err != nil {
		return nil, err
	}

	out := make([]domain.ResolvedRequest, 0, len(nodes))
	for index, node := range nodes {
		out = append(out, domain.ResolvedRequest{
			Node:      node,
			Auth:      requests[index].auth,
			Variables: requests[index].variables,
		})
	}
	return out, nil
}
