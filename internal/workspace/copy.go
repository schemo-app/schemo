package workspace

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
func (s *Collections) Duplicate(
	ctx context.Context,
	id string,
	suffix string,
) error {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	if collection, ok, err := s.collection(ctx, workspace, id); err != nil {
		return err
	} else if ok {
		return s.duplicateCollection(ctx, workspace, collection, suffix)
	}

	// The copy starts from the tree row and reads the request whole on the way: a copy is the request
	// and not just its name.
	tree, err := s.store.Collections(ctx, workspace)
	if err != nil {
		return err
	}
	node, ok := findNode(tree, id)
	if !ok {
		return fmt.Errorf("node %s: %w", id, domain.ErrNotFound)
	}
	position, err := s.store.NextPosition(ctx, workspace, node.CollectionID)
	if err != nil {
		return err
	}

	copied, err := s.copyNode(ctx, node, node.CollectionID, node.Name+suffix, position)
	if err != nil {
		return err
	}
	if err := s.store.SaveNode(ctx, copied); err != nil {
		return err
	}
	return nil
}

// The tree is read with its request fields left out, so every request is read again on the way in
// — a copy that lost its headers would be worse than no copy at all.
func (s *Collections) duplicateCollection(
	ctx context.Context,
	workspace string,
	collection domain.Collection,
	suffix string,
) error {
	position, err := s.store.NextPosition(ctx, workspace, collection.ParentID)
	if err != nil {
		return err
	}

	copied, err := s.copyCollection(ctx, collection, collection.ParentID, position,
		clip(collection.Name+suffix))
	if err != nil {
		return err
	}
	return s.saveTree(ctx, workspace, copied)
}

// copyCollection builds the copy in memory first so a failure leaves nothing half-written, reading
// content from the store rather than the given row.
func (s *Collections) copyCollection(
	ctx context.Context,
	row domain.Collection,
	parentID string,
	position int64,
	name string,
) ([]pendingLevel, error) {
	copied := domain.Collection{
		ID:          s.ids(),
		Name:        name,
		Description: row.Description,
		ParentID:    parentID,
		Position:    position,
		Auth:        row.Auth,
		// The variables are the copy's own, for the reason the rows are: a variable is addressed by its
		// id, and one id naming a variable in two collections is one variable in two places.
		Variables: copyVariables(s.ids, row.Variables),
		Items:     []domain.CollectionNode{},
		Children:  []domain.Collection{},
	}

	for _, node := range row.Items {
		child, err := s.copyNode(ctx, node, copied.ID, node.Name, node.Position)
		if err != nil {
			return nil, err
		}
		copied.Items = append(copied.Items, child)
	}

	// The copy is a second level, so what the original ran around its requests goes with it — read at
	// the end, because SaveScripts writes to the row the copy does not have yet.
	out := []pendingLevel{{collection: copied, from: row.ID}}
	for _, child := range row.Children {
		nested, err := s.copyCollection(ctx, child, copied.ID, child.Position, child.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, nested...)
	}
	return out, nil
}

func (s *Collections) copyNode(
	ctx context.Context,
	row domain.CollectionNode,
	collectionID string,
	name string,
	position int64,
) (domain.CollectionNode, error) {
	node, err := s.store.Node(ctx, row.ID)
	if err != nil {
		return domain.CollectionNode{}, err
	}
	copied := node
	copied.ID = s.ids()
	copied.CollectionID = collectionID
	copied.Name = name
	copied.Position = position
	// The rows are the copy's own: a row is addressed by its id, and one id naming a row in two
	// requests is one row in two places. The copy is a second thing, rows and all.
	copied.Params = copyRows(s.ids, node.Params)
	copied.Headers = copyRows(s.ids, node.Headers)
	copied.PathParams = copyRows(s.ids, node.PathParams)
	// The skip list names jar cookies, so it is copied as is, not re-minted.
	copied.Form = copyFormRows(s.ids, node.Form)
	return copied, nil
}

// minted gives each copied row a new id, copying the rows out rather than touching the caller's slice.
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

// pendingLevel is a level to write, with its scripts beside it, since SaveScripts needs the row to exist.
type pendingLevel struct {
	collection domain.Collection
	from       string
	scripts    *domain.Scripts
}

// saveTree writes built levels parents first: a row's parent has to exist before it does. The list
// comes pre-order, so the order it is given in is the order it writes in.
func (s *Collections) saveTree(
	ctx context.Context,
	workspace string,
	pending []pendingLevel,
) error {
	for _, level := range pending {
		if err := s.store.SaveCollection(ctx, workspace, level.collection); err != nil {
			return err
		}
		if err := s.saveScripts(ctx, workspace, level); err != nil {
			return err
		}
		for _, node := range level.collection.Items {
			if err := s.store.SaveNode(ctx, node); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Collections) saveScripts(
	ctx context.Context,
	workspace string,
	level pendingLevel,
) error {
	if level.scripts != nil {
		return s.store.SaveScripts(ctx, workspace, level.collection.ID, level.scripts)
	}
	if level.from == "" {
		return nil
	}
	scripts, err := s.store.Scripts(ctx, workspace, level.from)
	if err != nil {
		return err
	}
	if scripts == nil {
		return nil
	}
	return s.store.SaveScripts(ctx, workspace, level.collection.ID, scripts)
}
