package collection

// Duplicating a subtree. A copy is written a whole level at a time: a tree row is shallow, and a
// copy of the row would have no address and no headers.

import (
	"context"
	"fmt"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Duplicate copies a collection or a node into the same place, under a name that says what it is.
// Ids are minted anew: the copy is a second thing, not the same thing twice.
func (u *UseCase) Duplicate(
	ctx context.Context,
	id string,
	suffix string,
) ([]domain.Collection, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if collection, ok, err := u.collection(ctx, workspace, id); err != nil {
		return nil, err
	} else if ok {
		return u.duplicateCollection(ctx, workspace, collection, suffix)
	}

	// The copy starts from the tree row and reads the request whole on the way: a copy is the request
	// and not just its name.
	tree, err := u.store.Collections(ctx, workspace)
	if err != nil {
		return nil, err
	}
	node, ok := findNode(tree, id)
	if !ok {
		return nil, fmt.Errorf("node %s: %w", id, domain.ErrNotFound)
	}
	position, err := u.store.NextPosition(ctx, workspace, node.CollectionID)
	if err != nil {
		return nil, err
	}

	copied, err := u.copyNode(ctx, node, node.CollectionID, node.Name+suffix, position)
	if err != nil {
		return nil, err
	}
	if err := u.store.SaveNode(ctx, copied); err != nil {
		return nil, err
	}
	return u.store.Collections(ctx, workspace)
}

// The tree is read with its request fields left out, so every request is read again on the way in
// — a copy that lost its headers would be worse than no copy at all.
func (u *UseCase) duplicateCollection(
	ctx context.Context,
	workspace string,
	collection domain.Collection,
	suffix string,
) ([]domain.Collection, error) {
	position, err := u.store.NextPosition(ctx, workspace, collection.ParentID)
	if err != nil {
		return nil, err
	}

	copied, err := u.copyCollection(ctx, collection, collection.ParentID, position,
		clip(collection.Name+suffix))
	if err != nil {
		return nil, err
	}
	return u.saveTree(ctx, workspace, copied)
}

// Built in memory before anything is written: a duplicate that failed halfway would leave a
// collection with half a tree in it. The row it is given is not what the store holds — taking the
// row for the content is how a duplicate once came out as an empty request with the right name.
func (u *UseCase) copyCollection(
	ctx context.Context,
	row domain.Collection,
	parentID string,
	position int64,
	name string,
) ([]pendingLevel, error) {
	copied := domain.Collection{
		ID:          u.ids(),
		Name:        name,
		Description: row.Description,
		ParentID:    parentID,
		Position:    position,
		Auth:        row.Auth,
		// The variables are the copy's own, for the reason the rows are: a variable is addressed by its
		// id, and one id naming a variable in two collections is one variable in two places.
		Variables: copyVariables(u.ids, row.Variables),
		Items:     []domain.CollectionNode{},
		Children:  []domain.Collection{},
	}

	for _, node := range row.Items {
		child, err := u.copyNode(ctx, node, copied.ID, node.Name, node.Position)
		if err != nil {
			return nil, err
		}
		copied.Items = append(copied.Items, child)
	}

	// The copy is a second level, so what the original ran around its requests goes with it — read at
	// the end, because SaveScripts writes to the row the copy does not have yet.
	out := []pendingLevel{{collection: copied, from: row.ID}}
	for _, child := range row.Children {
		nested, err := u.copyCollection(ctx, child, copied.ID, child.Position, child.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, nested...)
	}
	return out, nil
}

func (u *UseCase) copyNode(
	ctx context.Context,
	row domain.CollectionNode,
	collectionID string,
	name string,
	position int64,
) (domain.CollectionNode, error) {
	node, err := u.store.Node(ctx, row.ID)
	if err != nil {
		return domain.CollectionNode{}, err
	}
	copied := node
	copied.ID = u.ids()
	copied.CollectionID = collectionID
	copied.Name = name
	copied.Position = position
	// The rows are the copy's own: a row is addressed by its id, and one id naming a row in two
	// requests is one row in two places. The copy is a second thing, rows and all.
	copied.Params = copyRows(u.ids, node.Params)
	copied.Headers = copyRows(u.ids, node.Headers)
	copied.PathParams = copyRows(u.ids, node.PathParams)
	// The skip list is copied as it stands and not minted: it names jar cookies, and a copy of a
	// request turning off the same cookies is the copy saying the same thing, not a second set of
	// cookies. The list is the copy's own so that one of them can change its mind.
	copied.Form = copyFormRows(u.ids, node.Form)
	return copied, nil
}

// minted gives every row an id of its own. Both halves of the tree need it: a row is edited by id,
// so a copy that kept the original's would be an edit away from changing both.
//
// The row is copied out of the list before it is written to, and the list itself is never touched:
// it belongs to the caller, and a copy that renamed the rows it was copied from would be the very
// thing the new ids are for.
func minted[T any](ids platform.IDGen, rows []T, id func(*T) *string) []T {
	out := make([]T, 0, len(rows))
	for _, row := range rows {
		*id(&row) = ids()
		out = append(out, row)
	}
	return out
}

// keptAsIs is minted for rows that may already have ids — a file writes none at all, and a row the
// window cannot address is a row it cannot edit. An id that is already there stays.
func keptAsIs[T any](ids platform.IDGen, rows []T, id func(*T) *string) []T {
	out := make([]T, 0, len(rows))
	for _, row := range rows {
		if *id(&row) == "" {
			*id(&row) = ids()
		}
		out = append(out, row)
	}
	return out
}

func withIDs(ids platform.IDGen, rows []domain.Row) []domain.Row {
	return keptAsIs(ids, rows, func(row *domain.Row) *string { return &row.ID })
}

func withFormIDs(ids platform.IDGen, rows []domain.FormRow) []domain.FormRow {
	return keptAsIs(ids, rows, func(row *domain.FormRow) *string { return &row.ID })
}

func withVariablesIDs(ids platform.IDGen, variables []domain.Variable) []domain.Variable {
	return keptAsIs(ids, variables, func(v *domain.Variable) *string { return &v.ID })
}

func copyRows(ids platform.IDGen, rows []domain.Row) []domain.Row {
	return minted(ids, rows, func(row *domain.Row) *string { return &row.ID })
}

func copyFormRows(ids platform.IDGen, rows []domain.FormRow) []domain.FormRow {
	return minted(ids, rows, func(row *domain.FormRow) *string { return &row.ID })
}

func copyVariables(ids platform.IDGen, variables []domain.Variable) []domain.Variable {
	return minted(ids, variables, func(v *domain.Variable) *string { return &v.ID })
}

// A level waiting to be written. Scripts belong to the level, not to the row, and SaveScripts only
// writes to a row that exists — so they travel beside it: in hand when a file brought them, or read
// from `from`, the original of a duplicate (empty for an import).
type pendingLevel struct {
	collection domain.Collection
	from       string
	scripts    *domain.Scripts
}

// saveTree writes built levels parents first: a row's parent has to exist before it does. The list
// comes pre-order, so the order it is given in is the order it writes in.
func (u *UseCase) saveTree(
	ctx context.Context,
	workspace string,
	pending []pendingLevel,
) ([]domain.Collection, error) {
	for _, level := range pending {
		if err := u.store.SaveCollection(ctx, workspace, level.collection); err != nil {
			return nil, err
		}
		if err := u.saveScripts(ctx, workspace, level); err != nil {
			return nil, err
		}
		for _, node := range level.collection.Items {
			if err := u.store.SaveNode(ctx, node); err != nil {
				return nil, err
			}
		}
	}
	return u.store.Collections(ctx, workspace)
}

func (u *UseCase) saveScripts(
	ctx context.Context,
	workspace string,
	level pendingLevel,
) error {
	if level.scripts != nil {
		return u.store.SaveScripts(ctx, workspace, level.collection.ID, level.scripts)
	}
	if level.from == "" {
		return nil
	}
	scripts, err := u.store.Scripts(ctx, workspace, level.from)
	if err != nil {
		return err
	}
	if scripts == nil {
		return nil
	}
	return u.store.SaveScripts(ctx, workspace, level.collection.ID, scripts)
}
