package collection

import (
	"context"
	"fmt"
	"strings"

	"schemo/internal/domain"
)

// CreateCollection adds an empty collection at the end of a level. The name is given rather than
// invented: the window asks for it in the tree, in the row the user is looking at. The level is the
// top of the tree when parentID is empty, and the collection it names otherwise — a folder inside a
// folder is a collection inside one, and the only thing that ever separated them was this argument.
// The created row comes back with the tree for the reason CreateNode's does: the id is Go's to
// mint, and a window that looked the row up by name would find the older one of the same name — a
// folder's name is not unique, and not even within one level.
func (u *UseCase) CreateCollection(
	ctx context.Context,
	name string,
	description string,
	parentID string,
) (domain.Collection, []domain.Collection, error) {
	name, err := validName(name)
	if err != nil {
		return domain.Collection{}, nil, err
	}

	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.Collection{}, nil, err
	}
	// A parent that is not there is a refusal rather than a silent insert at the top of the tree:
	// the window asked for a row in a level it was looking at.
	if parentID != "" {
		tree, err := u.store.Collections(ctx, workspace)
		if err != nil {
			return domain.Collection{}, nil, err
		}
		if _, ok := findCollection(tree, parentID); !ok {
			return domain.Collection{}, nil, fmt.Errorf("collection %s: %w", parentID,
				domain.ErrNotFound)
		}
	}

	position, err := u.store.NextPosition(ctx, workspace, parentID)
	if err != nil {
		return domain.Collection{}, nil, err
	}
	created := domain.Collection{
		ID:          u.ids(),
		Name:        name,
		Description: strings.TrimSpace(description),
		Position:    position,
		ParentID:    parentID,
		Items:       []domain.CollectionNode{},
		Children:    []domain.Collection{},
	}
	if err := u.store.SaveCollection(ctx, workspace, created); err != nil {
		return domain.Collection{}, nil, err
	}
	tree, err := u.Tree(ctx)
	if err != nil {
		return domain.Collection{}, nil, err
	}
	return created, tree, nil
}

// CreateNode adds a request to the end of a collection's level.
//
// It answers with the row that appeared and the tree it appeared in: the window needs the id Go
// minted, and looking it up by name afterwards would find the older row of the same name.
func (u *UseCase) CreateNode(
	ctx context.Context,
	in NodeDraft,
) (domain.CollectionNode, []domain.Collection, error) {
	name, err := validName(in.Name)
	if err != nil {
		return domain.CollectionNode{}, nil, err
	}
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.CollectionNode{}, nil, err
	}
	if _, ok, err := u.collection(ctx, workspace, in.CollectionID); err != nil {
		return domain.CollectionNode{}, nil, err
	} else if !ok {
		return domain.CollectionNode{}, nil, fmt.Errorf("collection %s: %w", in.CollectionID,
			domain.ErrNotFound)
	}

	position, err := u.store.NextPosition(ctx, workspace, in.CollectionID)
	if err != nil {
		return domain.CollectionNode{}, nil, err
	}

	node := domain.CollectionNode{
		ID:            u.ids(),
		CollectionID:  in.CollectionID,
		Name:          name,
		Position:      position,
		Method:        defaultMethod(in.Method),
		URL:           strings.TrimSpace(in.URL),
		Params:        domain.OrEmpty(in.Params),
		Headers:       domain.OrEmpty(in.Headers),
		PathParams:    domain.OrEmpty(in.PathParams),
		Body:          in.Body,
		BodyKind:      domain.KindOf(in.BodyKind),
		Form:          withFormIDs(u.ids, in.Form),
		BodyFile:      in.BodyFile,
		EnvironmentID: in.EnvironmentID,
		// The auth is stored the way the tree keeps one: «None» with nothing behind it is a level
		// nobody has answered anything at, and storing it as a value would make a request saved from
		// the command line stop inheriting — which is not what «None» means there, where nothing is
		// above it and the chip offers no «Inherit» to pick instead.
		Auth: storedAuth(in.Auth),
	}
	if err := u.store.SaveNode(ctx, node); err != nil {
		return domain.CollectionNode{}, nil, err
	}
	tree, err := u.store.Collections(ctx, workspace)
	if err != nil {
		return domain.CollectionNode{}, nil, err
	}
	return node, tree, nil
}

// MoveNode puts a request where it was dropped: another collection, or another place in its own.
// The position counts the level the way the tree draws it — a collection's requests and the
// collections inside it are one list — so the window can name the row it was dropped after.
func (u *UseCase) MoveNode(
	ctx context.Context,
	id string,
	collectionID string,
	position int64,
) ([]domain.Collection, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := u.store.Node(ctx, id); err != nil {
		return nil, err
	}
	if _, ok, err := u.collection(ctx, workspace, collectionID); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("collection %s: %w", collectionID, domain.ErrNotFound)
	}

	if err := u.store.MoveNode(ctx, workspace, id, collectionID, position); err != nil {
		return nil, err
	}
	return u.store.Collections(ctx, workspace)
}

// MoveCollection puts a collection inside another, or back at the top level when the parent is
// empty.
//
// A collection dropped into itself or into one of its own children is refused rather than stored:
// the tree would be a ring, and a ring is a tree nothing can be drawn from.
func (u *UseCase) MoveCollection(
	ctx context.Context,
	id string,
	parentID string,
	position int64,
) ([]domain.Collection, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	tree, err := u.store.Collections(ctx, workspace)
	if err != nil {
		return nil, err
	}
	moved, ok := findCollection(tree, id)
	if !ok {
		return nil, fmt.Errorf("collection %s: %w", id, domain.ErrNotFound)
	}
	if parentID != "" {
		if _, ok := findCollection(tree, parentID); !ok {
			return nil, fmt.Errorf("collection %s: %w", parentID, domain.ErrNotFound)
		}
		if _, inside := findCollection(moved.Children, parentID); parentID == id || inside {
			return nil, domain.Refuse(domain.CodeIntoItself, domain.ErrNotAllowed, nil)
		}
	}

	if err := u.store.MoveCollection(ctx, workspace, id, parentID, position); err != nil {
		return nil, err
	}
	return u.store.Collections(ctx, workspace)
}

// Rename is the one edit a tree row takes: the name. What a request is made of is edited in its own
// card and saved from there, so both a collection and a node answer to the same gesture.
func (u *UseCase) Rename(ctx context.Context, id string, name string) ([]domain.Collection, error) {
	name, err := validName(name)
	if err != nil {
		return nil, err
	}

	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	collection, ok, err := u.collection(ctx, workspace, id)
	if err != nil {
		return nil, err
	}
	if ok {
		collection.Name = name
		if err := u.store.SaveCollection(ctx, workspace, collection); err != nil {
			return nil, err
		}
		return u.store.Collections(ctx, workspace)
	}

	node, err := u.store.Node(ctx, id)
	if err != nil {
		return nil, err
	}
	node.Name = name
	if err := u.store.SaveNode(ctx, node); err != nil {
		return nil, err
	}
	return u.store.Collections(ctx, workspace)
}

// Describe is the other edit a tree row takes: what the level is for. A folder answers it too — a
// collection's description is what the overview draws above its tabs, and a folder has the same
// line in the same place — and both answer with the whole tree, because the tree is where the
// header reads it from.
func (u *UseCase) Describe(
	ctx context.Context,
	id string,
	description string,
) ([]domain.Collection, error) {
	description, err := validDescription(description)
	if err != nil {
		return nil, err
	}

	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if collection, ok, err := u.collection(ctx, workspace, id); err != nil {
		return nil, err
	} else if ok {
		collection.Description = description
		if err := u.store.SaveCollection(ctx, workspace, collection); err != nil {
			return nil, err
		}
		return u.store.Collections(ctx, workspace)
	}

	node, err := u.store.Node(ctx, id)
	if err != nil {
		return nil, err
	}
	node.Description = description
	if err := u.store.SaveNode(ctx, node); err != nil {
		return nil, err
	}
	return u.store.Collections(ctx, workspace)
}

// Delete removes a collection or a node. What was inside goes with it through the schema's cascade
// — one statement, so a half-deleted tree is not a state that exists.
func (u *UseCase) Delete(ctx context.Context, id string) ([]domain.Collection, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok, err := u.collection(ctx, workspace, id); err != nil {
		return nil, err
	} else if ok {
		if err := u.store.DeleteCollection(ctx, workspace, id); err != nil {
			return nil, err
		}
		return u.store.Collections(ctx, workspace)
	}

	if _, err := u.store.Node(ctx, id); err != nil {
		return nil, err
	}
	if err := u.store.DeleteNode(ctx, id); err != nil {
		return nil, err
	}
	return u.store.Collections(ctx, workspace)
}

// SaveNode writes what the card was editing back into the tree. The node is re-read first: the
// parts the card does not own — where it sits, when it was made — belong to the tree and stay as
// they are.
func (u *UseCase) SaveNode(
	ctx context.Context,
	edited domain.CollectionNode,
) ([]domain.Collection, error) {
	stored, err := u.store.Node(ctx, edited.ID)
	if err != nil {
		return nil, err
	}

	stored.Name, err = validName(edited.Name)
	if err != nil {
		return nil, err
	}
	// The same ceiling the description editor applies: two paths writing one field is two chances for
	// one of them to let through what the other refuses.
	stored.Description, err = validDescription(edited.Description)
	if err != nil {
		return nil, err
	}
	stored.Method = defaultMethod(edited.Method)
	stored.URL = edited.URL
	stored.Params = domain.OrEmpty(edited.Params)
	stored.Headers = domain.OrEmpty(edited.Headers)
	stored.PathParams = domain.OrEmpty(edited.PathParams)
	stored.Body = edited.Body
	// What the body was composed as travels with the text: a form and a file are fields of their own
	// in the model, and a save that wrote the text without them would turn a multipart request back
	// into raw text the next time it was opened.
	stored.BodyKind = domain.KindOf(edited.BodyKind)
	stored.Form = domain.OrEmpty(edited.Form)
	stored.BodyFile = edited.BodyFile
	stored.Auth = edited.Auth
	stored.EnvironmentID = edited.EnvironmentID
	if err := u.store.SaveNode(ctx, stored); err != nil {
		return nil, err
	}
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return u.store.Collections(ctx, workspace)
}

// defaultMethod is what a request gets when the tree did not name one — the tree offers GET first,
// and a method is never empty on a request that has been sent.
func defaultMethod(method string) string {
	method = strings.TrimSpace(strings.ToUpper(method))
	if method == "" {
		return "GET"
	}
	return method
}
