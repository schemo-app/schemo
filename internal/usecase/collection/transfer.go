package collection

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
// came from one file: ids are minted here because a file has none, and every collection keeps the
// name, the order and everything it came with, nested collections included.
func (u *UseCase) Import(
	ctx context.Context,
	collections []domain.Collection,
	parentID string,
) ([]domain.Collection, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if parentID != "" {
		if _, ok, err := u.collection(ctx, workspace, parentID); err != nil {
			return nil, err
		} else if !ok {
			return nil, fmt.Errorf("collection %s: %w", parentID, domain.ErrNotFound)
		}
	}
	position, err := u.store.NextPosition(ctx, workspace, parentID)
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
			ID:          u.ids(),
			Name:        name,
			Description: strings.TrimSpace(collection.Description),
			ParentID:    parentID,
			Position:    position + int64(index),
			Auth:        collection.Auth,
			Scripts:     collection.Scripts,
			// A file carries what a level answers for `{{tokens}}` and writes no ids with them: the ids are
			// minted here, the way every other id of an import is.
			Variables: withVariablesIDs(u.ids, collection.Variables),
		}
		pending = append(pending, u.adopt(imported, collection.Items, collection.Children)...)
	}
	return u.saveTree(ctx, workspace, pending)
}

// importName keeps a file's name inside the ceiling rather than refusing it: a name longer than a
// person would ever type is the file's own doing, and refusing the whole document over one line of
// it is not what the person asked for.
func importName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", domain.Refuse(domain.CodeNameEmpty, domain.ErrNotAllowed, nil)
	}
	return clip(name), nil
}

// adopt gives an imported subtree what a file does not write: ids, and where it lives. Everything
// else — the names, the addresses, the rows — is the file's, and the level's own code travels
// beside it because SaveScripts can only write to a row that exists.
func (u *UseCase) adopt(
	collection domain.Collection,
	items []domain.CollectionNode,
	children []domain.Collection,
) []pendingLevel {
	adopted := collection
	adopted.Items = []domain.CollectionNode{}
	adopted.Children = []domain.Collection{}
	adopted.Scripts = nil

	for _, node := range items {
		node.ID = u.ids()
		node.CollectionID = adopted.ID
		node.Params = withIDs(u.ids, node.Params)
		node.Headers = withIDs(u.ids, node.Headers)
		node.PathParams = withIDs(u.ids, node.PathParams)
		node.Form = withFormIDs(u.ids, node.Form)
		adopted.Items = append(adopted.Items, node)
	}

	out := []pendingLevel{{collection: adopted, scripts: collection.Scripts}}
	for _, child := range children {
		nested := domain.Collection{
			ID:          u.ids(),
			Name:        clip(strings.TrimSpace(child.Name)),
			Description: child.Description,
			ParentID:    adopted.ID,
			Position:    child.Position,
			Auth:        child.Auth,
			Scripts:     child.Scripts,
			Variables:   withVariablesIDs(u.ids, child.Variables),
		}
		out = append(out, u.adopt(nested, child.Items, child.Children)...)
	}
	return out
}

// Full is a collection with every request read whole, or a single request when the id names one:
// what an export writes down. The tree the window draws carries the method and nothing else — a
// list of two hundred rows has no business carrying two hundred bodies — so an export reads them
// again.
func (u *UseCase) Full(ctx context.Context, id string) (domain.Collection, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.Collection{}, err
	}
	return u.fullOf(ctx, workspace, id)
}

// FullOf is Full for a level of a named space, which the window may not be looking at: a copy of a
// collection out of another space reads it there and writes it here.
func (u *UseCase) FullOf(ctx context.Context, workspace, id string) (domain.Collection, error) {
	return u.fullOf(ctx, workspace, id)
}

func (u *UseCase) fullOf(ctx context.Context, workspace, id string) (domain.Collection, error) {
	collection, ok, err := u.collection(ctx, workspace, id)
	if err != nil {
		return domain.Collection{}, err
	}
	if !ok {
		node, err := u.store.Node(ctx, id)
		if err != nil {
			return domain.Collection{}, err
		}
		return domain.Collection{Name: node.Name, Items: []domain.CollectionNode{node}}, nil
	}
	return u.fullCollection(ctx, workspace, collection)
}

func (u *UseCase) fullCollection(
	ctx context.Context,
	workspace string,
	collection domain.Collection,
) (domain.Collection, error) {
	items, err := u.fullNodes(ctx, collection.Items)
	if err != nil {
		return domain.Collection{}, err
	}
	children := []domain.Collection{}
	for _, child := range collection.Children {
		full, err := u.fullCollection(ctx, workspace, child)
		if err != nil {
			return domain.Collection{}, err
		}
		children = append(children, full)
	}

	// The code of a level hangs off the row rather than off this read of it: the tree carries names
	// and methods, and an export that dropped the code would drop what the level does around its
	// requests.
	scripts, err := u.store.Scripts(ctx, workspace, collection.ID)
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

func (u *UseCase) fullNodes(
	ctx context.Context,
	rows []domain.CollectionNode,
) ([]domain.CollectionNode, error) {
	out := make([]domain.CollectionNode, 0, len(rows))
	for _, row := range rows {
		node, err := u.store.Node(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		node.Position = row.Position
		out = append(out, node)
	}
	return out, nil
}

// RequestsOf is every request under a collection, read whole and with the authorization and the
// variables its levels answer — the same walk a run makes, kept for what copies a whole collection
// out as a file.
func (u *UseCase) RequestsOf(
	ctx context.Context,
	collectionID string,
) ([]domain.ResolvedRequest, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	tree, err := u.store.Collections(ctx, workspace)
	if err != nil {
		return nil, err
	}
	requests, err := requestsUnder(tree, collectionID, "")
	if err != nil {
		return nil, err
	}

	rows := make([]domain.CollectionNode, 0, len(requests))
	for _, item := range requests {
		rows = append(rows, item.node)
	}
	nodes, err := u.fullNodes(ctx, rows)
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
