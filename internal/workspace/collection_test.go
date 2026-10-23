package workspace

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"schemo/internal/db"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// ws is the workspace the tests below call the store in directly — the one the app is born with,
// since none of them is about a second workspace.
const ws = domain.WorkspacePersonalID

func newCollections(t *testing.T) (*Collections, *db.Store) {
	store := dbtest.New(t)
	return NewCollections(store, domain.WorkspaceScope(ws), platform.NewIDGen()), store
}

// addWorkspace makes a second workspace for the tests about a workspace the window is not showing.
func addWorkspace(t *testing.T, store *db.Store, id string) string {
	t.Helper()
	if err := store.SaveWorkspace(t.Context(), domain.NewWorkspace(id, id, domain.WorkspacePersonal, "", 0)); err != nil {
		t.Fatal(err)
	}
	return id
}

// after answers the tree a change left behind, for the tests that look at it.
func (s *Collections) after(err error) ([]domain.Collection, error) {
	if err != nil {
		return nil, err
	}
	return s.Tree(context.Background())
}

func (s *Collections) collectionAndTree(c domain.Collection, err error) (domain.Collection, []domain.Collection, error) {
	tree, err := s.after(err)
	return c, tree, err
}

func (s *Collections) nodeAndTree(n domain.CollectionNode, err error) (domain.CollectionNode, []domain.Collection, error) {
	tree, err := s.after(err)
	return n, tree, err
}

func only(t *testing.T, tree []domain.Collection) domain.Collection {
	t.Helper()
	if len(tree) != 1 {
		t.Fatalf("tree = %d collections, want 1", len(tree))
	}
	return tree[0]
}

func TestCreateCollectionAppends(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, first, err := collections.collectionAndTree(collections.CreateCollection(ctx, "  Users  ", " test ", ""))
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	_, tree, err := collections.collectionAndTree(collections.CreateCollection(ctx, "Orders", "", ""))
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	if len(tree) != 2 {
		t.Fatalf("tree = %d collections, want 2", len(tree))
	}
	if tree[0].Name != "Users" || tree[0].Description != "test" {
		t.Errorf("first = %+v, want the name trimmed", tree[0])
	}
	if tree[1].Position != 1 {
		t.Errorf("second position = %d, want the end of the list", tree[1].Position)
	}
	if first[0].ID == tree[1].ID {
		t.Error("two collections share an id")
	}
}

func TestCreateCollectionRejectsAnEmptyName(t *testing.T) {
	collections, _ := newCollections(t)

	if _, err := collections.CreateCollection(context.Background(), "   ", "", ""); !errors.Is(err,
		domain.ErrNotAllowed) {
		t.Fatalf("empty name = %v, want ErrNotAllowed", err)
	}
}

func TestCreateNodeLandsAtTheEndOfItsLevel(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, err := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	id := only(t, tree).ID

	if _, err = collections.CreateNode(ctx, RequestDraft{CollectionID: id, Name: "First"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	// Dropped in at the top of the level, so the request that was there is now second.
	nestedID := nestCollection(t, collections, "Nested", id)

	_, tree, err = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: id, Name: "Second", Method: "post"}))
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	collection := only(t, tree)
	level := collection.Level()
	if len(level) != 3 {
		t.Fatalf("level = %+v, want two requests and the collection between them", level)
	}
	// The requests of a collection and the collections inside it are one number line: the new request
	// lands after the collection that was dropped in, not at the end of a group of its own.
	if level[0].Collection == nil || level[0].Collection.ID != nestedID {
		t.Errorf("level[0] = %+v, want the nested collection at 0", level[0])
	}
	if level[0].Collection.Position != 0 {
		t.Errorf("level[0] = %+v, want it numbered 0", level[0])
	}
	if level[1].Node == nil || level[1].Node.Name != "First" || level[1].Node.Position != 1 {
		t.Errorf("level[1] = %+v, want the first request pushed to 1", level[1])
	}
	if level[1].Node.Method != "GET" {
		t.Errorf("method = %q, want the default one", level[1].Node.Method)
	}
	if level[2].Node == nil || level[2].Node.Position != 2 || level[2].Node.Method != "POST" {
		t.Errorf("level[2] = %+v, want the new request at 2 with the method as typed", level[2])
	}
}

// nestCollection creates a collection and drops it at the top of the one named, which is how a
// collection comes to sit inside another: the tree has no gesture that makes one there.
func nestCollection(t *testing.T, collections *Collections, name string, parentID string) string {
	t.Helper()
	return nestCollectionAt(t, collections, name, parentID, 0)
}

// nestCollectionAt is the same drop at a place of its own, which is what a fixture needs when it is
// the order of the level that a test is about.
func nestCollectionAt(t *testing.T, collections *Collections, name string, parentID string, at int64) string {
	t.Helper()
	ctx := context.Background()

	// The row that came back is the one that appeared: the answer carries it now, so nothing here has
	// to guess which of the last two it is.
	made, err := collections.CreateCollection(ctx, name, "", "")
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	if err := collections.Move(ctx, made.ID, parentID, at); err != nil {
		t.Fatalf("MoveCollection: %v", err)
	}
	return made.ID
}

// A request saved from a composer is written whole: the row that comes back is the one that
// appeared, with everything the request already had, so nothing depends on finding it again by
// name.
func TestCreateNodeTakesAWholeRequest(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	_, tree, err := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID

	created, tree, err := collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{
		CollectionID: collectionID,
		Name:         "Saved",
		Method:       "patch",
		URL:          "https://api.example.com/users/1?page=2",
		Headers: []domain.Row{
			{ID: "h1", Name: "Accept", Value: "application/vnd.api+json", Enabled: true},
		},
		Body: `{"data": 1}`,
		Auth: bearerRef("{{token}}"),
	}))
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	if created.ID == "" || created.Name != "Saved" {
		t.Fatalf("created = %+v, want the row that appeared", created)
	}
	if len(only(t, tree).Items) != 1 || only(t, tree).Items[0].ID != created.ID {
		t.Errorf("tree = %+v, want the created row in it", only(t, tree).Items)
	}

	stored, err := store.Node(ctx, created.ID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if stored.Method != "PATCH" || stored.URL != "https://api.example.com/users/1?page=2" {
		t.Errorf("stored = %+v, want the request it was given", stored)
	}
	if len(stored.Headers) != 1 || stored.Body != `{"data": 1}` {
		t.Errorf("stored = %+v, want its headers and body", stored)
	}
	if stored.Auth == nil || stored.Auth.Answer("token") != "{{token}}" {
		t.Errorf("auth = %+v, want the choice the composer made", stored.Auth)
	}
}

func TestRenameReachesBothKinds(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	_, tree, _ = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: collectionID, Name: "Request"}))
	requestID := only(t, tree).Items[0].ID

	tree, err := collections.after(collections.Update(ctx, collectionID, patchName("Renamed")))
	if err != nil {
		t.Fatalf("Rename collection: %v", err)
	}
	if only(t, tree).Name != "Renamed" {
		t.Errorf("collection = %+v, want the new name", only(t, tree))
	}

	tree, err = collections.after(collections.Update(ctx, requestID, patchName("  Also  ")))
	if err != nil {
		t.Fatalf("Rename request: %v", err)
	}
	if only(t, tree).Items[0].Name != "Also" {
		t.Errorf("request = %+v, want the name trimmed", only(t, tree).Items[0])
	}
}

// Renaming a collection inside another one is the same gesture, and it leaves the row where it is:
// a rename saves the row it read, and where a row sits is not part of what a rename edits.
func TestRenameKeepsANestedCollectionNested(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)

	tree, err := collections.after(collections.Update(ctx, nestedID, patchName("Renamed")))
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	renamed, ok := findCollection(tree, nestedID)
	if !ok {
		t.Fatalf("the nested collection left the tree: %+v", tree)
	}
	if renamed.Name != "Renamed" || renamed.ParentID != collectionID {
		t.Errorf("nested = %+v, want it renamed and still inside the collection", renamed)
	}
}

// What a level is for is written the same way every level answers it, and an empty answer is a real
// one: the header draws a placeholder for it rather than a gap.
func TestDescribeReachesBothKinds(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "old description", ""))
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)

	tree, err := collections.after(collections.Update(ctx, collectionID, patchDescription("  Catalog endpoints  ")))
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got := only(t, tree).Description; got != "Catalog endpoints" {
		t.Errorf("description = %q, want it trimmed", got)
	}

	tree, err = collections.after(collections.Update(ctx, nestedID, patchDescription("Admin only")))
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if nested, ok := findCollection(tree, nestedID); !ok || nested.Description != "Admin only" {
		t.Errorf("nested = %+v, want the description it was given", nested)
	}

	// Empty is allowed and means what it says — unlike a name, which cannot be empty.
	tree, err = collections.after(collections.Update(ctx, collectionID, patchDescription("   ")))
	if err != nil {
		t.Fatalf("Describe(empty): %v", err)
	}
	if got := only(t, tree).Description; got != "" {
		t.Errorf("description = %q, want it cleared", got)
	}

	// What is too long to be a description is refused, and the row keeps the one it had.
	refused := strings.Repeat("é", maxDescriptionLength+1)
	if err := collections.Update(ctx, collectionID, patchDescription(refused)); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("a description of %d runes = %v, want ErrNotAllowed", len(refused), err)
	}
	if err := collections.Update(ctx, "not-found", patchDescription("something")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("describing nothing = %v, want ErrNotFound", err)
	}
}

// copySuffix is what the window sends: a word, in whatever language it is in.
const copySuffix = " (copy)"

func TestDuplicateCopiesTheSubtree(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)
	// The tree is read again: the row looked up on the next line is the one this call adds.
	_, tree, _ = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: nestedID, Name: "Inside"}))
	requestID := findIn(t, tree, nestedID).Items[0].ID

	// A request inside a collection is more than the tree row the copy starts from, and a copy that
	// forgot it would be an empty collection.
	if err := collections.SaveNode(ctx, domain.CollectionNode{
		ID: requestID, Name: "Inside", Method: "PATCH", URL: "https://api.example.com/users/1",
		Headers: []domain.Row{{ID: "h1", Name: "X-Test", Value: "1", Enabled: true}},
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	tree, err := collections.after(collections.Duplicate(ctx, nestedID, copySuffix))
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	// The copy lands in the level the original is in: a copy of a nested collection is not a reason to
	// hoist it to the top.
	level := only(t, tree).Children
	if len(level) != 2 {
		t.Fatalf("children = %d, want the copy beside the original", len(level))
	}
	if level[1].Name != "Nested"+copySuffix || level[1].Position != 1 {
		t.Errorf("copy = %+v, want it named and placed next to the original", level[1])
	}
	if level[1].ParentID != collectionID {
		t.Errorf("copy = %+v, want it inside the collection the original is in", level[1])
	}
	if len(level[1].Items) != 1 {
		t.Fatalf("copy = %+v, want its own request", level[1])
	}
	copiedID := level[1].Items[0].ID
	if copiedID == requestID {
		t.Error("the copy kept the original's id")
	}
	copied, err := store.Node(ctx, copiedID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if copied.CollectionID != level[1].ID {
		t.Errorf("copied request = %+v, want it inside the copy", copied)
	}
	if copied.URL != "https://api.example.com/users/1" || len(copied.Headers) != 1 {
		t.Errorf("copied request = %+v, want the request's own fields", copied)
	}
	if copied.Method != "PATCH" {
		t.Errorf("copied method = %q, want the original's", copied.Method)
	}
}

// findIn is a collection of the tree by id, which is how a test reaches one that is not at the top.
func findIn(t *testing.T, tree []domain.Collection, id string) domain.Collection {
	t.Helper()
	collection, ok := findCollection(tree, id)
	if !ok {
		t.Fatalf("collection %s is not in the tree", id)
	}
	return collection
}

// A copy of a request is the request, not its name: the tree row a duplicate starts from carries
// the method and nothing else, and taking that for the content is how a copy comes out empty.
func TestDuplicateCopiesTheRequestItself(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	_, tree, _ = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: collectionID, Name: "Request"}))
	requestID := only(t, tree).Items[0].ID

	bearer := bearerRef("{{token}}")
	if err := collections.SaveNode(ctx, domain.CollectionNode{
		ID: requestID, Name: "Request", Method: "PATCH", URL: "https://api.example.com/users/1?page=2",
		Description: "about the user",
		Params:      []domain.Row{{ID: "p1", Name: "page", Value: "2", Enabled: true}},
		Headers: []domain.Row{
			{ID: "h1", Name: "Accept", Value: "application/vnd.api+json", Enabled: true},
		},
		Body: `{"data": {"type": "users"}}`,
		Auth: bearer,
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	// A copy runs the same code the original did: its scripts come with it, the way its rows do.
	if err := store.SaveScripts(ctx, ws, requestID,
		&domain.Scripts{Post: "console.log('own');"}); err != nil {
		t.Fatalf("SaveScripts: %v", err)
	}

	tree, err := collections.after(collections.Duplicate(ctx, requestID, copySuffix))
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	copiedID := only(t, tree).Items[1].ID
	copied, err := store.Node(ctx, copiedID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}

	if copied.Name != "Request"+copySuffix || copied.Position != 1 {
		t.Errorf("copy = %+v, want it named and placed beside the original", copied)
	}
	if copied.URL != "https://api.example.com/users/1?page=2" || copied.Method != "PATCH" ||
		copied.Body != `{"data": {"type": "users"}}` {
		t.Errorf("copy = %+v, want the request it was copied from", copied)
	}
	if len(copied.Params) != 1 || len(copied.Headers) != 1 {
		t.Errorf("copy = %+v, want its rows", copied)
	}
	if copied.Description != "about the user" {
		t.Errorf("description = %q, want it carried over", copied.Description)
	}
	if copied.Auth == nil || copied.Auth.Answer("token") != "{{token}}" {
		t.Errorf("auth = %+v, want it carried over", copied.Auth)
	}
	// The copy is a second thing: its own rows, so editing one does not edit the other.
	if copied.Params[0].ID == "p1" || copied.Headers[0].ID == "h1" {
		t.Errorf("copy = %+v, want rows of its own", copied)
	}
	if copied.Scripts == nil || copied.Scripts.Post != "console.log('own');" {
		t.Errorf("copy scripts = %+v, want the original's code with it", copied.Scripts)
	}
}

// The card edits a request — its address, its rows, its body — and knows nothing about its scripts:
// a save that dropped them would take the code off a request every time it was touched.
func TestSavingARequestKeepsItsScripts(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	_, tree, _ = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: collectionID, Name: "Request"}))
	requestID := only(t, tree).Items[0].ID

	if err := store.SaveScripts(ctx, ws, requestID,
		&domain.Scripts{Post: "console.log('own');"}); err != nil {
		t.Fatalf("SaveScripts: %v", err)
	}
	if err := collections.SaveNode(ctx, domain.CollectionNode{
		ID: requestID, Name: "Renamed", Method: "GET", URL: "https://api.example.com/users",
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	stored, err := store.Node(ctx, requestID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if stored.Scripts == nil || stored.Scripts.Post != "console.log('own');" {
		t.Errorf("node scripts = %+v, want them where the card left them", stored.Scripts)
	}
}

// A copy behaves the way the original did, so it takes what the collection runs around its requests
// with it. The tree carries no scripts — they belong to the level, not to the row — so a duplicate
// that only copied the row would lose them without saying so.
func TestDuplicateCopiesTheCollectionScripts(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	if err := store.SaveScripts(ctx, ws, collectionID,
		&domain.Scripts{Pre: "console.log('running');"}); err != nil {
		t.Fatalf("SaveScripts: %v", err)
	}

	tree, err := collections.after(collections.Duplicate(ctx, collectionID, copySuffix))
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	copied, err := store.Scripts(ctx, ws, tree[1].ID)
	if err != nil {
		t.Fatalf("Scripts: %v", err)
	}
	if copied == nil || copied.Pre != "console.log('running');" {
		t.Errorf("copied scripts = %+v, want the original's", copied)
	}
	if original, err := store.Scripts(ctx, ws, collectionID); err != nil || original == nil {
		t.Errorf("the original's scripts = %+v, %v, want them where they were", original, err)
	}
}

func TestDuplicateCopiesACollection(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "description", ""))
	collectionID := only(t, tree).ID
	_, tree, _ = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: collectionID, Name: "First"}))
	nestedID := nestCollection(t, collections, "Nested", collectionID)
	// A request is a tree row too, and a copy that took the row for the content would lose the
	// request behind it — the same trap the request duplicate had.
	if err := collections.SaveNode(ctx, domain.CollectionNode{
		ID: only(t,
			tree).Items[0].ID, Name: "First", Method: "GET", URL: "https://api.example.com/first",
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	tree, err := collections.after(collections.Duplicate(ctx, collectionID, copySuffix))
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("tree = %d collections, want 2", len(tree))
	}
	copied := tree[1]
	// The copy lands at the end of the level the original is in, which holds the original alone.
	if copied.Name != "Collection"+copySuffix || copied.Description != "description" ||
		copied.Position != 1 {
		t.Errorf("copy = %+v, want it named and placed after the original", copied)
	}
	if len(copied.Items) != 1 || len(copied.Children) != 1 {
		t.Fatalf("copy = %+v, want its own level: a request and the collection inside it", copied)
	}
	// Everything has to move into the new collection: a row left behind would be visible in both.
	for _, node := range copied.Items {
		if node.CollectionID != copied.ID {
			t.Errorf("request %s still lives in %s", node.ID, node.CollectionID)
		}
	}
	if copied.Children[0].ParentID != copied.ID || copied.Children[0].ID == nestedID {
		t.Errorf("nested copy = %+v, want a collection of its own inside the copy", copied.Children[0])
	}
	if len(copied.Children[0].Items) != 0 {
		t.Errorf("nested copy = %+v, want the original's own level copied with it", copied.Children[0])
	}
	if copied.Items[0].ID == tree[0].Items[0].ID {
		t.Error("the copy kept the original's request id")
	}
	copiedFirst, err := store.Node(ctx, copied.Items[0].ID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if copiedFirst.Name != "First" || copiedFirst.URL != "https://api.example.com/first" {
		t.Errorf("copied request = %+v, want the request it was copied from", copiedFirst)
	}
}

// A copy answers for the names the original answered for, and with ids of its own: a variable is
// addressed by its id, and one id naming a variable in two collections names two things.
func TestDuplicateCopiesTheVariablesOfEveryLevel(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)
	saveVariables(t, collections, collectionID, variable("baseUrl", "https://api.example.com"))
	saveVariables(t, collections, nestedID, variable("version", "v2"))

	tree, err := collections.after(collections.Duplicate(ctx, collectionID, copySuffix))
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("tree = %d collections, want the copy beside the original", len(tree))
	}
	copied := tree[1]

	if len(copied.Variables) != 1 || copied.Variables[0].Name != "baseUrl" ||
		copied.Variables[0].Value != "https://api.example.com" {
		t.Fatalf("copy = %+v, want the original's own answer", copied.Variables)
	}
	if copied.Variables[0].ID == tree[0].Variables[0].ID {
		t.Error("the copy kept the original's variable id")
	}

	if len(copied.Children) != 1 || len(copied.Children[0].Variables) != 1 {
		t.Fatalf("nested copy = %+v, want the level inside it copied whole", copied.Children)
	}
	if v := copied.Children[0].Variables[0]; v.Name != "version" || v.Value != "v2" {
		t.Errorf("nested variable = %+v, want the one the folder answered", v)
	}
}

func TestDuplicateClipsTheNameAtTheCeiling(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	long := ""
	for len([]rune(long)) < maxCollectionName {
		long += "é"
	}
	_, tree, err := collections.collectionAndTree(collections.CreateCollection(ctx, long, "", ""))
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	tree, err = collections.after(collections.Duplicate(ctx, only(t, tree).ID, copySuffix))
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	if got := len([]rune(tree[1].Name)); got > maxCollectionName {
		t.Errorf("name is %d runes, want at most %d", got, maxCollectionName)
	}
}

func TestDeleteTakesTheWholeSubtree(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)
	// The node has to exist for the cascade below to have something to take: what the tree looks like
	// after this call is what Delete reads.
	if _, err := collections.CreateNode(ctx, RequestDraft{CollectionID: nestedID, Name: "Inside"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	// A collection inside another is a row of its own, so removing it is what takes its requests with
	// it — through both cascades: the nesting one, and the collection the request belongs to.
	tree, err := collections.after(collections.Delete(ctx, nestedID))
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(only(t, tree).Children) != 0 {
		t.Errorf("children = %+v, want it and its request gone", only(t, tree).Children)
	}

	tree, err = collections.after(collections.Delete(ctx, collectionID))
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(tree) != 0 {
		t.Errorf("tree = %+v, want nothing left", tree)
	}
}

func TestSaveNodeKeepsWhereItLives(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)
	_, tree, _ = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: nestedID, Name: "Request"}))
	requestID := findIn(t, tree, nestedID).Items[0].ID

	before, err := store.Node(ctx, requestID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}

	// The card knows nothing about where the request lives, so it sends what it has and the rest must
	// survive: a save that moved a request out of its collection would be a lost tree.
	if err := collections.SaveNode(ctx, domain.CollectionNode{
		ID: requestID, Name: "  Renamed  ", Method: "delete", URL: "https://api.example.com/users",
		Description: "  about users  ", CollectionID: "",
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	after, err := store.Node(ctx, requestID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if after.Name != "Renamed" || after.Description != "about users" {
		t.Errorf("request = %+v, want the edited name trimmed", after)
	}
	if after.Method != "DELETE" {
		t.Errorf("method = %q, want it upper-cased", after.Method)
	}
	if after.CollectionID != before.CollectionID || after.Position != before.Position ||
		after.CreatedAt != before.CreatedAt {
		t.Errorf("request = %+v, want the place it had (%+v)", after, before)
	}
	if after.CollectionID != nestedID {
		t.Errorf("collection = %q, want it still in the one inside the collection", after.CollectionID)
	}
}

func TestSaveNodeRejectsAnEmptyName(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	_, tree, _ = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: collectionID, Name: "Request"}))
	requestID := only(t, tree).Items[0].ID

	err := collections.SaveNode(ctx, domain.CollectionNode{ID: requestID, Name: "   "})
	if !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("empty name = %v, want ErrNotAllowed", err)
	}
}

func TestMoveNodeTakesTheDropIndex(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, err := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID
	for _, name := range []string{"First", "Second", "Third"} {
		if _, tree, err = collections.nodeAndTree(collections.CreateNode(ctx,
			RequestDraft{CollectionID: collectionID, Name: name})); err != nil {
			t.Fatalf("CreateNode %s: %v", name, err)
		}
	}
	second := only(t, tree).Items[1].ID

	// Dropped after the row that was behind it: the index counts the level as it looks, the moving row
	// included, so "after the second" is 2 — and the row must not be counted twice.
	if tree, err = collections.after(collections.Move(ctx, second, collectionID, 2)); err != nil {
		t.Fatalf("MoveNode: %v", err)
	}
	order := []string{}
	for _, node := range only(t, tree).Items {
		order = append(order, node.Name)
	}
	if !slices.Equal(order, []string{"First", "Second", "Third"}) {
		t.Errorf("level = %v, want the request where it already stood", order)
	}

	// And to the front, which is the other end of the same index.
	if tree, err = collections.after(collections.Move(ctx, second, collectionID, 0)); err != nil {
		t.Fatalf("MoveNode: %v", err)
	}
	order = []string{}
	for _, node := range only(t, tree).Items {
		order = append(order, node.Name)
	}
	if !slices.Equal(order, []string{"Second", "First", "Third"}) {
		t.Errorf("level = %v, want the dropped request first", order)
	}
	// The rows around it close up rather than keeping the numbers they had.
	for i, node := range only(t, tree).Items {
		if node.Position != int64(i) {
			t.Errorf("%s is at %d, want %d", node.Name, node.Position, i)
		}
	}
}

func TestMoveNodeBetweenCollections(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)
	_, tree, _ = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: collectionID, Name: "Outside"}))
	requestID := only(t, tree).Items[0].ID

	if err := collections.Move(ctx, requestID, nestedID, 0); err != nil {
		t.Fatalf("MoveNode: %v", err)
	}

	moved, err := store.Node(ctx, requestID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if moved.CollectionID != nestedID {
		t.Errorf("collection = %q, want the one it was dropped into", moved.CollectionID)
	}
	tree, err = collections.Tree(ctx)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(only(t, tree).Items) != 0 || len(findIn(t, tree, nestedID).Items) != 1 {
		t.Errorf("tree = %+v, want the request inside the nested collection only", only(t, tree))
	}
}

// A collection dropped into itself or into something it holds would be a ring, and a ring is a tree
// nothing can be drawn from: the refusal is what keeps the tree a tree.
func TestMoveCollectionRefusesARing(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)

	for _, parent := range []string{collectionID, nestedID} {
		err := collections.Move(ctx, collectionID, parent, 0)
		if !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("moving %s into %s = %v, want ErrNotAllowed", collectionID, parent, err)
		}
		// The window words the refusal from the code: "it was refused" says nothing about a ring.
		if code := domain.CodeOf(err); code != domain.CodeIntoItself {
			t.Errorf("code = %q, want %q", code, domain.CodeIntoItself)
		}
	}

	// And nothing moved: a refused drop leaves the tree as it was.
	tree, err := collections.Tree(ctx)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(tree) != 1 || len(only(t, tree).Children) != 1 {
		t.Errorf("tree = %+v, want it untouched", tree)
	}
	if any := findIn(t, tree, nestedID); any.ParentID != collectionID {
		t.Errorf("nested = %+v, want it still inside the collection", any)
	}
}

func TestMoveCollectionReturnsToTheTopLevel(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)

	tree, err := collections.after(collections.Move(ctx, nestedID, "", 0))
	if err != nil {
		t.Fatalf("MoveCollection: %v", err)
	}
	if len(tree) != 2 || tree[0].ID != nestedID || tree[0].ParentID != "" {
		t.Errorf("tree = %+v, want it back out at the top", tree)
	}
	if len(findIn(t, tree, collectionID).Children) != 0 {
		t.Errorf("tree = %+v, want nothing left inside", tree)
	}
}

// A collection made elsewhere becomes a collection here: its name, its order, and everything inside
// it — with ids minted for everything a file does not write.
func TestImportTakesAWholeCollection(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	tree, err := collections.Import(ctx, []domain.Collection{{
		Name: "  Imported  ",
		Items: []domain.CollectionNode{
			{Name: "Outside", Position: 1, Method: "GET", URL: "https://api.example.com/out?page=2",
				Params: []domain.Row{{Name: "page", Value: "2", Enabled: true}}},
		},
		Children: []domain.Collection{
			{
				Name: "Nested", Position: 0,
				Items: []domain.CollectionNode{
					{Name: "Inside", Method: "post", URL: "https://api.example.com/inside",
						Headers: []domain.Row{{Name: "Accept", Value: "application/json", Enabled: true}}},
				},
			},
		},
	}}, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	collection := only(t, tree)
	if collection.Name != "Imported" || collection.Position != 0 {
		t.Errorf("collection = %+v, want the file's name, trimmed", collection)
	}
	if len(collection.Items) != 1 || len(collection.Children) != 1 {
		t.Fatalf("imported = %+v, want the request and the collection inside it", collection)
	}
	nested := collection.Children[0]
	if nested.Name != "Nested" || len(nested.Items) != 1 || nested.Items[0].Name != "Inside" {
		t.Fatalf("nested = %+v, want the file's tree", nested)
	}
	if nested.ParentID != collection.ID {
		t.Errorf("nested = %+v, want the collection it was imported into", nested)
	}
	for _, node := range []domain.CollectionNode{collection.Items[0], nested.Items[0]} {
		if node.ID == "" {
			t.Errorf("request = %+v, want an id minted for it", node)
		}
	}
	// A request belongs to the collection that holds it: the one from inside the file's folder belongs
	// to the collection that folder became.
	if nested.Items[0].CollectionID != nested.ID {
		t.Errorf("the request inside = %+v, want it in the nested collection", nested.Items[0])
	}
	if collection.Items[0].CollectionID != collection.ID {
		t.Errorf("the request at the top = %+v, want it in the imported collection", collection.Items[0])
	}

	// What the tree carries is a row; what was stored is the request.
	stored, err := store.Node(ctx, collection.Items[0].ID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if stored.URL != "https://api.example.com/out?page=2" || stored.Method != "GET" {
		t.Errorf("stored = %+v, want the request the file had", stored)
	}
	if len(stored.Params) != 1 || stored.Params[0].ID == "" {
		t.Errorf("params = %+v, want the row with an id the window can address", stored.Params)
	}
	if stored.Position != 1 {
		t.Errorf("stored = %+v, want its place in the imported order", stored)
	}
}

// The tree's menu imports into a collection, not only beside one: a file named a level lands at the
// end of that level, and a name that is no collection at all is refused rather than quietly put at
// the top.
func TestImportLandsInTheNamedCollection(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, err := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	parent := only(t, tree).ID

	if _, err = collections.Import(ctx, []domain.Collection{{Name: "From file"}}, parent); err != nil {
		t.Fatalf("Import: %v", err)
	}

	tree, err = collections.store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("tree = %d collections at the top, want the one that was already there", len(tree))
	}
	inside := findIn(t, tree, parent)
	if len(inside.Children) != 1 || inside.Children[0].Name != "From file" {
		t.Errorf("children = %+v, want the import inside %s", inside.Children, parent)
	}

	// An import aimed at a collection that is not there is refused; no level is made up for it.
	nowhere := []domain.Collection{{Name: "From file"}}
	if _, err = collections.Import(ctx, nowhere, "not-found"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("import into nothing = %v, want ErrNotFound", err)
	}
}

// A file carries what a level answers for `{{tokens}}`, and an import keeps it: a collection that
// lost them would answer `{{baseUrl}}` with nothing on the other side of the move.
func TestImportKeepsTheVariablesOfEveryLevel(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	tree, err := collections.Import(ctx, []domain.Collection{{
		Name: "Imported",
		Variables: []domain.Variable{
			{Name: "baseUrl", Value: "https://api.example.com", Kind: domain.VariableText, Enabled: true},
		},
		Children: []domain.Collection{{
			Name: "Nested",
			Variables: []domain.Variable{
				{Name: "version", Value: "v2", Kind: domain.VariableText, Enabled: true},
			},
		}},
	}}, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	collection := only(t, tree)
	if len(collection.Variables) != 1 || collection.Variables[0].Value != "https://api.example.com" {
		t.Fatalf("level = %+v, want the file's own answer", collection.Variables)
	}
	// A file writes no ids with them, and a variable the window cannot address is one it cannot edit.
	if collection.Variables[0].ID == "" {
		t.Error("the imported variable has no id")
	}
	if len(collection.Children) != 1 || len(collection.Children[0].Variables) != 1 {
		t.Fatalf("nested = %+v, want the folder's own answer", collection.Children)
	}
	if v := collection.Children[0].Variables[0]; v.Name != "version" || v.Value != "v2" || v.ID == "" {
		t.Errorf("nested variable = %+v, want the one the file carried", v)
	}
}

// An export writes down what a tree row does not carry, and what a level answers for `{{tokens}}`
// is one of those: a collection handed on has to mean the same thing on the other side.
func TestFullReadsTheVariablesWhole(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)
	saveVariables(t, collections, collectionID, variable("baseUrl", "https://api.example.com"))
	saveVariables(t, collections, nestedID, variable("version", "v2"))

	full, err := collections.Full(ctx, collectionID)
	if err != nil {
		t.Fatalf("Full: %v", err)
	}
	if len(full.Variables) != 1 || full.Variables[0].Name != "baseUrl" {
		t.Errorf("level = %+v, want the collection's own answer", full.Variables)
	}
	if len(full.Children) != 1 || len(full.Children[0].Variables) != 1 {
		t.Fatalf("nested = %+v, want the folder read whole too", full.Children)
	}
	if v := full.Children[0].Variables[0]; v.Name != "version" || v.Value != "v2" {
		t.Errorf("nested variable = %+v, want the one the folder answers", v)
	}
}

// An export writes down what a tree row does not carry: Full reads the nodes whole, and a request
// on its own is the collection an export of one request makes.
func TestFullReadsTheRequestsWhole(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, err := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "description", ""))
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID
	_, tree, err = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{
		CollectionID: collectionID, Name: "Request", Method: "GET",
		URL: "https://api.example.com/users", Body: `{"a": 1}`,
		Headers: []domain.Row{{Name: "Accept", Value: "application/vnd.api+json", Enabled: true}},
	}))
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	requestID := only(t, tree).Items[0].ID

	full, err := collections.Full(ctx, collectionID)
	if err != nil {
		t.Fatalf("Full: %v", err)
	}
	if full.Name != "Collection" || full.Description != "description" || len(full.Items) != 1 {
		t.Fatalf("full = %+v, want the collection and its request", full)
	}
	if full.Items[0].Body != `{"a": 1}` || len(full.Items[0].Headers) != 1 {
		t.Errorf("request = %+v, want it read whole", full.Items[0])
	}

	single, err := collections.Full(ctx, requestID)
	if err != nil {
		t.Fatalf("Full: %v", err)
	}
	if single.Name != "Request" || len(single.Items) != 1 ||
		single.Items[0].URL != "https://api.example.com/users" {
		t.Errorf("single = %+v, want a collection of the one request", single)
	}
}

// An export of a collection carries the collections inside it: what a file writes as folders is the
// levels of the tree, and one left out would be a file that is not the collection.
func TestFullReadsNestedCollectionsWhole(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)
	if _, err := collections.CreateNode(ctx, RequestDraft{
		CollectionID: nestedID, Name: "Inside", Method: "GET", URL: "https://api.example.com/inside",
		Body: `{"b": 2}`,
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	full, err := collections.Full(ctx, collectionID)
	if err != nil {
		t.Fatalf("Full: %v", err)
	}
	if len(full.Children) != 1 || full.Children[0].Name != "Nested" {
		t.Fatalf("full = %+v, want the collection and the one inside it", full)
	}
	if len(full.Children[0].Items) != 1 || full.Children[0].Items[0].Body != `{"b": 2}` {
		t.Errorf("nested = %+v, want its request read whole", full.Children[0])
	}
}

// A collection is read with the levels it sits inside of, itself first: what asks for it is a
// reader of one folder of a page, and the answer is the chain from that folder to the root
// of its workspace.
func TestPathOfReadsTheLevelsACollectionSitsInsideOf(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	root := only(t, tree).ID
	middle := nestCollection(t, collections, "middle", root)
	leaf := nestCollection(t, collections, "Leaf", middle)

	path, err := collections.PathOf(ctx, domain.WorkspacePersonalID, leaf)
	if err != nil {
		t.Fatalf("PathOf: %v", err)
	}

	want := []domain.Collection{
		{ID: leaf, Name: "Leaf"},
		{ID: middle, Name: "middle"},
		{ID: root, Name: "Collection"},
	}
	if !slices.EqualFunc(path, want, func(a, b domain.Collection) bool {
		return a.ID == b.ID && a.Name == b.Name
	}) {
		t.Errorf("path = %+v, want the collection and the levels above it", path)
	}
}

// A request and a collection are named in the same id workspace, and only one of them has a path: an id
// the tree never meets is answered with nothing rather than with a refusal nobody asked for.
func TestPathOfAnswersNothingForAnIDTheTreeDoesNotHold(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, _ := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	node, err := collections.CreateNode(ctx, RequestDraft{
		CollectionID: only(t, tree).ID, Name: "Request", Method: "GET",
		URL: "https://api.example.com/v1",
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	path, err := collections.PathOf(ctx, domain.WorkspacePersonalID, node.ID)
	if err != nil {
		t.Fatalf("PathOf: %v", err)
	}
	if len(path) != 0 {
		t.Errorf("path = %+v, want nothing for a request", path)
	}
}

func TestNodeInAMissingTree(t *testing.T) {
	collections, _ := newCollections(t)

	if _, err := collections.Node(context.Background(), "not-found"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing node = %v, want ErrNotFound", err)
	}
}

func findInTree(t *testing.T, tree []domain.Collection, name string) domain.CollectionNode {
	t.Helper()
	walk := func(nodes []domain.CollectionNode) (domain.CollectionNode, bool) {
		for _, node := range nodes {
			if node.Name == name {
				return node, true
			}
		}
		return domain.CollectionNode{}, false
	}
	var inCollection func([]domain.Collection) (domain.CollectionNode, bool)
	inCollection = func(collections []domain.Collection) (domain.CollectionNode, bool) {
		for _, collection := range collections {
			if found, ok := walk(collection.Items); ok {
				return found, true
			}
			if found, ok := inCollection(collection.Children); ok {
				return found, true
			}
		}
		return domain.CollectionNode{}, false
	}
	if found, ok := inCollection(tree); ok {
		return found
	}
	t.Fatalf("no request named %s in the tree", name)
	return domain.CollectionNode{}
}

func patchName(name string) CollectionPatch { return CollectionPatch{Name: &name} }

func patchDescription(text string) CollectionPatch { return CollectionPatch{Description: &text} }

func patchAuth(auth domain.Auth) CollectionPatch { return CollectionPatch{Auth: &auth} }

func patchVariables(variables []domain.Variable) CollectionPatch {
	return CollectionPatch{Variables: &variables}
}
