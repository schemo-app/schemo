package workspace

import (
	"context"
	"fmt"
	"schemo/internal/db"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"strconv"
	"strings"
)

// Collections is the saved requests: the tree and what inherits from what.
type Collections struct {
	store *db.Store
	scope domain.Scope
	ids   platform.IDGen
}

func NewCollections(store *db.Store, scope domain.Scope, ids platform.IDGen) *Collections {
	return &Collections{store: store, scope: scope, ids: ids}
}

// InWorkspace returns an independently scoped operation handler; it does not switch the UI.
//
//wails:ignore
func (s *Collections) InWorkspace(workspace string) *Collections {
	return NewCollections(s.store, domain.WorkspaceScope(workspace), s.ids)
}

// Tree is every collection with its nodes, which is what the list draws and what a run walks.
func (s *Collections) Tree(ctx context.Context) ([]domain.Collection, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return s.store.Collections(ctx, workspace)
}

// TreeOf is the tree of one named workspace, for a reader that is not the window: the MCP server reads
// workspaces the window is not showing, and switching the app there to read them would move the window
// under somebody's cursor.
//
//wails:ignore
func (s *Collections) TreeOf(ctx context.Context, workspace string) ([]domain.Collection, error) {
	return s.store.Collections(ctx, workspace)
}

// WorkspaceOf is which workspace a collection belongs to, asked by a reader that reached the id from
// outside: the MCP server checks the answer against what an agent may reach before it reads or
// sends anything of it.
//
//wails:ignore
func (s *Collections) WorkspaceOf(ctx context.Context, collectionID string) (string, error) {
	return s.store.WorkspaceOf(ctx, collectionID)
}

// PathOf is the collections from the asked one up to the root of its workspace, the asked one first and
// each said by its name alone: a page is a whole tree, so the page a folder sits inside of is found
// by walking up. An id the tree never meets returns nothing rather than a refusal.
//
//wails:ignore
func (s *Collections) PathOf(ctx context.Context, workspace, id string) ([]domain.Collection, error) {
	tree, err := s.store.Collections(ctx, workspace)
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
func (s *Collections) Node(ctx context.Context, id string) (domain.CollectionNode, error) {
	return s.store.Node(ctx, id)
}

// Contents reads the requests a collection holds, as its page draws them: a name, a method, an
// address and the folder each sits in. It covers everything inside the collection, not the level
// alone: the table is what running the collection would send, and a run reaches the whole subtree.
func (s *Collections) Contents(ctx context.Context, collectionID string) ([]domain.LevelRow, error) {
	return s.store.ContentRows(ctx, collectionID)
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
// addressed in the same id workspace, and a caller naming an id does not say which of the two it named.
func (s *Collections) collection(
	ctx context.Context,
	workspace, id string,
) (domain.Collection, bool, error) {
	tree, err := s.store.Collections(ctx, workspace)
	if err != nil {
		return domain.Collection{}, false, err
	}
	collection, ok := findCollection(tree, id)
	return collection, ok, nil
}

func collectionNotFound(id string) error {
	return fmt.Errorf("collection %s: %w", id, domain.ErrNotFound)
}

// requireCollection refuses an id that names no collection of the workspace.
func (s *Collections) requireCollection(ctx context.Context, workspace, id string) error {
	_, ok, err := s.collection(ctx, workspace, id)
	if err != nil {
		return err
	}
	if !ok {
		return collectionNotFound(id)
	}
	return nil
}

// CreateCollection adds an empty collection at the end of a level. The name is given rather than
// invented: the window asks for it in the tree. The level is the top of the tree when parentID is
// empty, and the collection it names otherwise; a folder inside a folder is a collection inside
// one. The created row comes back with the tree for the reason CreateNode's does: the id is Go's to
// mint, and a lookup by name would find the older row of the same name (a folder's name is not
// unique, even within one level).
func (s *Collections) CreateCollection(
	ctx context.Context,
	name string,
	description string,
	parentID string,
) (domain.Collection, error) {
	name, err := validName(name)
	if err != nil {
		return domain.Collection{}, err
	}
	description, err = validDescription(description)
	if err != nil {
		return domain.Collection{}, err
	}

	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.Collection{}, err
	}
	// A parent that is not there is a refusal rather than a silent insert at the top of the tree:
	// the window asked for a row in a level it was looking at.
	if parentID != "" {
		tree, err := s.store.Collections(ctx, workspace)
		if err != nil {
			return domain.Collection{}, err
		}
		if _, ok := findCollection(tree, parentID); !ok {
			return domain.Collection{}, collectionNotFound(parentID)
		}
	}

	position, err := s.store.NextPosition(ctx, workspace, parentID)
	if err != nil {
		return domain.Collection{}, err
	}
	created := domain.Collection{
		ID:          s.ids(),
		Name:        name,
		Description: strings.TrimSpace(description),
		Position:    position,
		ParentID:    parentID,
		Items:       []domain.CollectionNode{},
		Children:    []domain.Collection{},
	}
	if err := s.store.SaveCollection(ctx, workspace, created); err != nil {
		return domain.Collection{}, err
	}
	return created, nil
}

// CreateNode adds a request to the end of a collection's level.
//
// It returns the row that appeared and the tree it appeared in: the window needs the id Go minted,
// and looking it up by name afterwards would find the older row of the same name.
func (s *Collections) CreateNode(
	ctx context.Context,
	in RequestDraft,
) (domain.CollectionNode, error) {
	name, err := validName(in.Name)
	if err != nil {
		return domain.CollectionNode{}, err
	}
	description, err := validDescription(in.Description)
	if err != nil {
		return domain.CollectionNode{}, err
	}
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.CollectionNode{}, err
	}
	if err := s.requireCollection(ctx, workspace, in.CollectionID); err != nil {
		return domain.CollectionNode{}, err
	}

	position, err := s.store.NextPosition(ctx, workspace, in.CollectionID)
	if err != nil {
		return domain.CollectionNode{}, err
	}

	node := domain.CollectionNode{
		ID:            s.ids(),
		CollectionID:  in.CollectionID,
		Name:          name,
		Description:   description,
		Position:      position,
		Method:        defaultMethod(in.Method),
		URL:           strings.TrimSpace(in.URL),
		Params:        domain.OrEmpty(in.Params),
		Headers:       domain.OrEmpty(in.Headers),
		PathParams:    domain.OrEmpty(in.PathParams),
		Body:          in.Body,
		BodyKind:      domain.KindOf(in.BodyKind),
		Form:          withFormIDs(s.ids, in.Form),
		BodyFile:      in.BodyFile,
		EnvironmentID: in.EnvironmentID,
		// The auth is stored the way the tree keeps one: "None" with nothing behind it is a level
		// nobody has answered at, and storing it as a value would make a request saved from the
		// command line stop inheriting. That is not what "None" means there, where nothing is above
		// it and the chip offers no "Inherit" to pick instead.
		Auth: storedAuth(in.Auth),
	}
	if err := s.store.SaveNode(ctx, node); err != nil {
		return domain.CollectionNode{}, err
	}
	return node, nil
}

// Move puts a collection or request where it was dropped. The position counts the level the way
// the tree draws it (requests and collections are one list); an empty parent is the top level,
// which only a collection can take.
func (s *Collections) Move(ctx context.Context, id, parentID string, position int64) error {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	if _, ok, err := s.collection(ctx, workspace, id); err != nil {
		return err
	} else if ok {
		return s.moveCollection(ctx, workspace, id, parentID, position)
	}
	return s.moveNode(ctx, workspace, id, parentID, position)
}

func (s *Collections) moveNode(ctx context.Context, workspace, id, collectionID string, position int64) error {
	if _, err := s.store.Node(ctx, id); err != nil {
		return err
	}
	if err := s.requireCollection(ctx, workspace, collectionID); err != nil {
		return err
	}

	if err := s.store.MoveNode(ctx, workspace, id, collectionID, position); err != nil {
		return err
	}
	return nil
}

// A collection dropped into itself or one of its children is refused: the tree would be a ring.
func (s *Collections) moveCollection(ctx context.Context, workspace, id, parentID string, position int64) error {
	tree, err := s.store.Collections(ctx, workspace)
	if err != nil {
		return err
	}
	moved, ok := findCollection(tree, id)
	if !ok {
		return collectionNotFound(id)
	}
	if parentID != "" {
		if _, ok := findCollection(tree, parentID); !ok {
			return collectionNotFound(parentID)
		}
		if _, inside := findCollection(moved.Children, parentID); parentID == id || inside {
			return domain.Refuse(domain.CodeIntoItself, domain.ErrNotAllowed, nil)
		}
	}

	if err := s.store.MoveCollection(ctx, workspace, id, parentID, position); err != nil {
		return err
	}
	return nil
}

// Update changes the fields the patch carries on a collection or request, validated together and
// saved once. Variables belong to collections only.
func (s *Collections) Update(ctx context.Context, id string, patch CollectionPatch) error {
	var name, description string
	var variables []domain.Variable
	var err error
	if patch.Name != nil {
		if name, err = validName(*patch.Name); err != nil {
			return err
		}
	}
	if patch.Description != nil {
		if description, err = validDescription(*patch.Description); err != nil {
			return err
		}
	}
	if patch.Variables != nil {
		if variables, err = s.cleanVariables(*patch.Variables); err != nil {
			return err
		}
	}
	var auth *domain.Auth
	if patch.Auth != nil {
		// "None" over a filled-in scheme is an answer; Stored keeps that apart from "inherit".
		auth = patch.Auth.Normalized().Stored()
	}
	return s.editRow(ctx, id,
		func(collection *domain.Collection) {
			if patch.Name != nil {
				collection.Name = name
			}
			if patch.Description != nil {
				collection.Description = description
			}
			if patch.Auth != nil {
				collection.Auth = auth
			}
			if patch.Variables != nil {
				collection.Variables = variables
			}
		},
		func(node *domain.CollectionNode) error {
			if patch.Variables != nil {
				return fmt.Errorf("collection %s: %w", id, domain.ErrNotFound)
			}
			if patch.Name != nil {
				node.Name = name
			}
			if patch.Description != nil {
				node.Description = description
			}
			if patch.Auth != nil {
				node.Auth = auth
			}
			return nil
		})
}

// editRow changes the collection the id names or, when it names none, the request of that id: the
// two share one id workspace, and a caller does not say which it means. Either way it returns the whole
// tree.
func (s *Collections) editRow(
	ctx context.Context,
	id string,
	editCollection func(*domain.Collection),
	editNode func(*domain.CollectionNode) error,
) error {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	collection, ok, err := s.collection(ctx, workspace, id)
	if err != nil {
		return err
	}
	if ok {
		editCollection(&collection)
		if err := s.store.SaveCollection(ctx, workspace, collection); err != nil {
			return err
		}
		return nil
	}

	node, err := s.store.Node(ctx, id)
	if err != nil {
		return err
	}
	if err := editNode(&node); err != nil {
		return err
	}
	if err := s.store.SaveNode(ctx, node); err != nil {
		return err
	}
	return nil
}

// Delete removes a collection or a node. What was inside goes with it through the schema's cascade
// — one statement, so a half-deleted tree is not a state that exists.
func (s *Collections) Delete(ctx context.Context, id string) error {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	if _, ok, err := s.collection(ctx, workspace, id); err != nil {
		return err
	} else if ok {
		if err := s.store.DeleteCollection(ctx, workspace, id); err != nil {
			return err
		}
		return nil
	}

	if _, err := s.store.Node(ctx, id); err != nil {
		return err
	}
	if err := s.store.DeleteNode(ctx, id); err != nil {
		return err
	}
	return nil
}

// SaveNode writes what the card was editing back into the tree. The node is re-read first: the
// parts the card does not own (where it sits, when it was made) belong to the tree and stay as they
// are.
//
//wails:ignore
func (s *Collections) SaveNode(
	ctx context.Context,
	edited domain.CollectionNode,
) error {
	stored, err := s.store.Node(ctx, edited.ID)
	if err != nil {
		return err
	}

	stored.Name, err = validName(edited.Name)
	if err != nil {
		return err
	}
	// The same ceiling the description editor applies: two paths writing one field is two chances for
	// one of them to let through what the other refuses.
	stored.Description, err = validDescription(edited.Description)
	if err != nil {
		return err
	}
	stored.Method = defaultMethod(edited.Method)
	stored.URL = edited.URL
	stored.Params = domain.OrEmpty(edited.Params)
	stored.Headers = domain.OrEmpty(edited.Headers)
	stored.PathParams = domain.OrEmpty(edited.PathParams)
	stored.Body = edited.Body
	// What the body was composed as travels with the text: a form and a file are fields of their
	// own in the model, and a save that wrote the text without them would turn a multipart request
	// back into raw text the next time it was opened.
	stored.BodyKind = domain.KindOf(edited.BodyKind)
	stored.Form = domain.OrEmpty(edited.Form)
	stored.BodyFile = edited.BodyFile
	stored.Auth = edited.Auth
	stored.EnvironmentID = edited.EnvironmentID
	if err := s.store.SaveNode(ctx, stored); err != nil {
		return err
	}
	return nil
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

// What a name and a description may be. Both are refused with the code the window words the
// sentence from, so an empty name and an overlong one are not the same news.

// maxCollectionName is the same ceiling the environments screen uses, so a name that is too long means
// the same thing wherever it is typed.
const maxCollectionName = 120

// maxDescriptionLength accommodates the complete Markdown document, including public sections.
const maxDescriptionLength = 8000

func validName(name string) (string, error) {
	return domain.CleanName(name, maxCollectionName)
}

// validDescription trims what was typed and keeps it within the ceiling. An empty description is a
// real answer (the header shows a placeholder), so it is not refused the way an empty name is.
func validDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	if len([]rune(description)) > maxDescriptionLength {
		return "", domain.Refuse(domain.CodeDescriptionTooLong, domain.ErrNotAllowed,
			domain.Args{"max": strconv.Itoa(maxDescriptionLength)})
	}
	return description, nil
}

// clip keeps a name inside the ceiling. Duplicating at the limit is a thing a user does, and
// refusing it would be the app's problem, not theirs.
func clip(name string) string {
	runes := []rune(name)
	if len(runes) <= maxCollectionName {
		return name
	}
	return string(runes[:maxCollectionName])
}

// CollectionPatch is the fields of a collection or request to change; nil leaves one as it is.
type CollectionPatch struct {
	Name        *string            `json:"name,omitempty"`
	Description *string            `json:"description,omitempty"`
	Auth        *domain.Auth       `json:"auth,omitempty"`
	Variables   *[]domain.Variable `json:"variables,omitempty"`
}

// RequestDraft is what the tree asks for when a row is created: where it goes and what is already
// known about the request. A request made by hand arrives with its method only and the next card
// fills in the rest, while one saved from the command line is a whole request; building it empty
// first would leave a node with no address if the second step failed.
type RequestDraft struct {
	CollectionID string `json:"collectionId"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`

	Method     string           `json:"method,omitempty"`
	URL        string           `json:"url,omitempty"`
	Params     []domain.Row     `json:"params,omitempty"`
	Headers    []domain.Row     `json:"headers,omitempty"`
	PathParams []domain.Row     `json:"pathParams,omitempty"`
	Body       string           `json:"body,omitempty"`
	BodyKind   domain.BodyKind  `json:"bodyKind,omitempty"`
	Form       []domain.FormRow `json:"form,omitempty"`
	BodyFile   string           `json:"bodyFile,omitempty"`
	Auth       *domain.Auth     `json:"auth,omitempty"`
	// EnvironmentID carries a command-line request's pin into the node "Save" makes of it — a
	// request that was pointed at a particular environment stays pointed at it once saved.
	EnvironmentID string `json:"environmentId,omitempty"`
}
