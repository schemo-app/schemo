package workspace

// Duplicating a subtree. A copy is written a whole level at a time: a tree row is shallow, and a
// copy of the row would have no address and no headers.

import (
	"context"
	"fmt"
	"slices"
	"strings"

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

	state, err := s.store.EnvState(ctx, workspace)
	if err != nil {
		return err
	}
	collection.Name = clip(collection.Name + suffix)
	collection.Position = position
	copied, err := s.copyCollection(ctx, workspace, collection, draftsByCollection(state))
	if err != nil {
		return err
	}
	return s.saveTree(ctx, workspace, copied)
}

// copyCollection builds the copy in memory first so a failure leaves nothing half-written, reading
// content from the store rather than the given row.
func (s *Collections) copyCollection(
	ctx context.Context,
	workspace string,
	row domain.Collection,
	environments map[string][]domain.EnvironmentDraft,
) ([]domain.Collection, error) {
	scripts, err := s.store.Scripts(ctx, workspace, row.ID)
	if err != nil {
		return nil, err
	}
	copied := domain.Collection{
		ID:              s.ids(),
		Name:            row.Name,
		Description:     row.Description,
		ParentID:        row.ParentID,
		Position:        row.Position,
		Auth:            row.Auth,
		Scripts:         scripts,
		Environments:    environments[row.ID],
		EnvironmentRoot: row.EnvironmentRoot,
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

	out := []domain.Collection{copied}
	for _, child := range row.Children {
		child.ParentID = copied.ID
		nested, err := s.copyCollection(ctx, workspace, child, environments)
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

func (s *Collections) saveTree(
	ctx context.Context,
	workspace string,
	levels []domain.Collection,
	environments ...domain.Environment,
) error {
	for _, level := range levels {
		owned, err := prepareEnvironments(s.ids, level.ID, level.Environments, nil)
		if err != nil {
			return err
		}
		environments = append(environments, owned...)
	}
	return s.store.SaveCollectionTree(ctx, workspace, levels, environments)
}

func prepareEnvironments(
	ids platform.IDGen, owner string, drafts []domain.EnvironmentDraft, siblings []domain.Environment,
) ([]domain.Environment, error) {
	base := -1
	if owner != "" && !slices.ContainsFunc(siblings, func(env domain.Environment) bool { return env.Base }) {
		base = slices.IndexFunc(drafts, func(env domain.EnvironmentDraft) bool { return env.Base && !env.Folder })
	}
	position := nextPosition(siblings, func(env domain.Environment) int { return env.Position })
	out := make([]domain.Environment, 0, len(drafts))
	names := slices.Clone(siblings)
	for i, draft := range drafts {
		name := strings.TrimSpace(draft.Name)
		if name == "" {
			return nil, domain.Refuse(domain.CodeNameEmpty, domain.ErrNotAllowed, nil)
		}
		if runes := []rune(name); len(runes) > maxEnvironmentName {
			name = string(runes[:maxEnvironmentName])
		}
		env := domain.Environment{
			ID: ids(), CollectionID: owner, Name: freeName(name, names), Color: draft.Color,
			Base: i == base, Folder: draft.Folder, Position: position + i,
			Vars: importedVariables(ids, draft.Variables),
		}
		out = append(out, env)
		names = append(names, env)
	}
	return out, nil
}
