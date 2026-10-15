package sqlite

import (
	"context"
	"errors"
	"slices"
	"testing"

	"schemo/internal/domain"
)

func TestCollectionsReadsTheTreeNested(t *testing.T) {
	store := newMigratedStore(t)
	seedTree(t, store)

	tree, err := store.Collections(context.Background(), ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("tree = %d collections, want 1", len(tree))
	}
	collection := tree[0]
	if collection.Name != "Users" || collection.Description != "test" {
		t.Errorf("collection = %+v, want the saved one", collection)
	}
	if collection.Auth == nil || collection.Auth.Answer("token") != "{{token}}" {
		t.Errorf("collection auth = %+v, want what everything inside inherits", collection.Auth)
	}
	// A collection holds requests and collections, and they are two lists here: the window merges
	// them by position, which is the one number line they share.
	if len(collection.Items) != 1 || collection.Items[0].ID != "r-2" {
		t.Fatalf("top level = %+v, want only the request that belongs to it", collection.Items)
	}
	if len(collection.Children) != 1 || collection.Children[0].ID != "f-1" {
		t.Fatalf("children = %+v, want the collection inside it", collection.Children)
	}
	if inner := collection.Children[0]; len(inner.Items) != 1 || inner.Items[0].ID != "r-1" {
		t.Fatalf("nested = %+v, want its own request", collection.Children[0])
	}

	// The order the tree draws them in is the order the two lists merge into.
	level := collection.Level()
	if len(level) != 2 || level[0].Collection == nil || level[0].Collection.ID != "f-1" {
		t.Errorf("level = %+v, want the nested collection first", level)
	}
	if level[1].Node == nil || level[1].Node.ID != "r-2" {
		t.Errorf("level = %+v, want the request after it", level)
	}

	// A tree row carries the method because that is what it draws, and nothing heavier: two hundred
	// bodies is not what a list loads.
	row := collection.Items[0]
	if row.Method != "PATCH" {
		t.Errorf("method = %q, want the row to carry it", row.Method)
	}
	if row.URL != "" || row.Body != "" || len(row.Headers) != 0 || len(row.Params) != 0 {
		t.Errorf("row = %+v, want a tree row with no request payload", row)
	}
}

// The page's read carries the address a tree row leaves out, and it carries every request inside
// the collection rather than the level's own: the table is what running the collection would send,
// and a run reaches the folders. The order is the walk's — a folder and the requests beside it are
// one numbered sequence.
func TestContentRowsWalkTheWholeCollectionInTreeOrder(t *testing.T) {
	store := newMigratedStore(t)
	seedTree(t, store)

	rows, err := store.ContentRows(context.Background(), "col-1")
	if err != nil {
		t.Fatalf("ContentRows: %v", err)
	}
	// The fixture numbers the folder first and this level's own request second, so the folder's
	// request comes first: a read that listed the level and then the folders would have it the other
	// way round, and a report would draw a run in an order it never ran in.
	if len(rows) != 2 || rows[0].ID != "r-1" || rows[1].ID != "r-2" {
		t.Fatalf("rows = %+v, want the folder's request then this level's own", rows)
	}
	if rows[0].URL != "https://api.example.com/users" || rows[0].Method != "GET" {
		t.Errorf("row = %+v, want the address and the method the tree leaves out", rows[0])
	}

	// A folder is asked for as a level of its own: what is under it, and nothing beside it.
	nested, err := store.ContentRows(context.Background(), "f-1")
	if err != nil {
		t.Fatalf("ContentRows(nested): %v", err)
	}
	if len(nested) != 1 || nested[0].ID != "r-1" || nested[0].Name != "List" {
		t.Errorf("rows = %+v, want the nested collection's own request", nested)
	}
}

func TestCollectionsKeepsTheirOwnOrder(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	for _, c := range []domain.Collection{
		{ID: "col-b", Name: "Second", Position: 1},
		{ID: "col-a", Name: "First", Position: 0},
	} {
		if err := store.SaveCollection(ctx, ws, c); err != nil {
			t.Fatalf("SaveCollection: %v", err)
		}
	}

	tree, err := store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(tree) != 2 || tree[0].ID != "col-a" || tree[1].ID != "col-b" {
		t.Errorf("tree = %+v, want the position order, not the insert order", tree)
	}
}

func TestCollectionsDoNotMixNodes(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	if err := store.SaveCollection(ctx, ws,
		domain.Collection{ID: "col-2", Name: "Orders", Position: 1}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	if err := store.SaveNode(ctx,
		sampleNode("r-3", "col-2", 0, "Orders", "GET", "https://api.example.com/orders")); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	tree, err := store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(tree[1].Items) != 1 || tree[1].Items[0].ID != "r-3" {
		t.Errorf("second collection = %+v, want only its own request", tree[1].Items)
	}
	if len(tree[0].Items) != 1 || len(tree[0].Children) != 1 {
		t.Errorf("first collection = %+v, want only its own level", tree[0])
	}
}

func TestNodeRoundTrip(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	node, err := store.Node(ctx, "r-1")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node.Name != "List" || node.Method != "GET" || node.URL != "https://api.example.com/users" {
		t.Errorf("node = %+v, want the saved request", node)
	}
	if node.CollectionID != "f-1" || node.Position != 0 {
		t.Errorf("node = %+v, want its place in the tree", node)
	}
	if len(node.Params) != 1 || node.Params[0].Value != "2" {
		t.Errorf("params = %+v, want the row with its id", node.Params)
	}
	if len(node.Headers) != 1 || node.Headers[0].Name != "Accept" {
		t.Errorf("headers = %+v", node.Headers)
	}
	if len(node.PathParams) != 1 || node.PathParams[0].Value != "42" ||
		node.PathParams[0].Description != "which user" {
		t.Errorf("path params = %+v, want the value and the description kept", node.PathParams)
	}
	if node.Body != `{"data": {"type": "users"}}` {
		t.Errorf("body = %q", node.Body)
	}
	if node.Auth != nil {
		t.Errorf("auth = %+v, want nil for a node that inherits", node.Auth)
	}
}

// The saved answers beside a request ride the node whole — they are the one thing on a request a
// document carried in rather than a person typing — and a save must not lose them.
func TestNodeRoundTripKeepsTheSavedExamples(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	node := sampleNode("r-6", "col-1", 2, "List", "GET", "https://api.example.com/products")
	node.Responses = []domain.ResponseExample{
		{Name: "The list", Status: 200, Body: `{"data":[]}`,
			Headers: []domain.HeaderPair{{Name: "Content-Type", Value: "application/json"}}},
		{Name: "Not here", Status: 404},
	}
	if err := store.SaveNode(ctx, node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	back, err := store.Node(ctx, "r-6")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if len(back.Responses) != 2 || back.Responses[0].Name != "The list" ||
		back.Responses[0].Status != 200 || back.Responses[0].Body != `{"data":[]}` ||
		len(back.Responses[0].Headers) != 1 || back.Responses[1].Status != 404 {
		t.Errorf("responses = %+v, want the saved answers back", back.Responses)
	}
}

// A saved request carries its format and what its body is made of, so a card gets the same five
// formats the command line has.
func TestNodeRoundTripKeepsTheBodyFormat(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	node := sampleNode("r-5", "col-1", 2, "Upload", "POST", "https://api.example.com/upload")
	node.Body = ""
	node.BodyKind = domain.BodyForm
	node.Form = []domain.FormRow{
		{ID: "f-1", Name: "title", Value: "Coffee grinder", Enabled: true},
		{ID: "f-2", Name: "photo", Src: "/tmp/logo.png", File: true, Enabled: true},
	}
	if err := store.SaveNode(ctx, node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	read, err := store.Node(ctx, "r-5")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if read.BodyKind != domain.BodyForm {
		t.Errorf("bodyKind = %q, want the saved one", read.BodyKind)
	}
	if len(read.Form) != 2 || read.Form[1].Src != "/tmp/logo.png" || !read.Form[1].File {
		t.Errorf("form = %+v, want both rows and the file's path", read.Form)
	}

	// The format survives a second save: body_kind and body_file are in the upsert's update set.
	node.BodyFile = "/tmp/other.bin"
	node.BodyKind = domain.BodyBinary
	if err := store.SaveNode(ctx, node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	again, err := store.Node(ctx, "r-5")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if again.BodyKind != domain.BodyBinary || again.BodyFile != "/tmp/other.bin" {
		t.Errorf("node = %+v, want the second save's format and path", again)
	}
}

func TestNodeAuthRemembersExplicitNothing(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	// An empty auth is an answer, not an absence: without it a collection could not say "no auth for
	// anything in here", which is the whole point of the NULL column.
	none := &domain.Auth{Type: domain.AuthNone}
	node := sampleNode("r-4", "col-1", 2, "No authorization", "GET", "https://api.example.com/open")
	node.Auth = none
	if err := store.SaveNode(ctx, node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	read, err := store.Node(ctx, "r-4")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if read.Auth == nil || read.Auth.Type != domain.AuthNone {
		t.Errorf("auth = %+v, want the explicit none back", read.Auth)
	}
}

func TestNodeInAMissingRow(t *testing.T) {
	store := newMigratedStore(t)

	if _, err := store.Node(context.Background(), "not-found"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing node = %v, want ErrNotFound", err)
	}
}

func TestSaveNodeUpdatesInPlace(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	before, err := store.Node(ctx, "r-2")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	before.Name = "One user"
	before.Headers = []domain.Row{{ID: "h", Name: "If-None-Match", Value: `"v1"`, Enabled: true}}
	if err := store.SaveNode(ctx, before); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	after, err := store.Node(ctx, "r-2")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if after.Name != "One user" || len(after.Headers) != 1 ||
		after.Headers[0].Name != "If-None-Match" {
		t.Errorf("node = %+v, want the edit", after)
	}
	if after.CreatedAt != before.CreatedAt || after.Position != before.Position {
		t.Errorf("node = %+v, want created_at and position untouched by an edit", after)
	}

	tree, err := store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if tree[0].Items[0].Name != "One user" {
		t.Errorf("tree = %+v, want the new name in the row", tree[0].Items[0])
	}
}

// A card's pin on an environment of its own survives the round trip like the rest of its request,
// and a card that never touched the pin reads back as unpinned rather than as any particular
// string.
func TestNodeRoundTripKeepsTheEnvironmentPin(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	unpinned, err := store.Node(ctx, "r-1")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if unpinned.EnvironmentID != "" {
		t.Errorf("environmentID = %q, want unpinned for a node that never set one",
			unpinned.EnvironmentID)
	}

	pinned := unpinned
	pinned.EnvironmentID = "env-1"
	if err := store.SaveNode(ctx, pinned); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	after, err := store.Node(ctx, "r-1")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if after.EnvironmentID != "env-1" {
		t.Errorf("environmentID = %q, want the pin it was saved with", after.EnvironmentID)
	}
}

func TestNextPositionCountsTheWholeLevel(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	// The nested collection sits at 0 and the request at 1, one sequence: the next row of that level
	// is the third of them, whichever kind it is.
	next, err := store.NextPosition(ctx, ws, "col-1")
	if err != nil {
		t.Fatalf("NextPosition: %v", err)
	}
	if next != 2 {
		t.Errorf("next at the top level = %d, want one past the last child", next)
	}

	next, err = store.NextPosition(ctx, ws, "f-1")
	if err != nil {
		t.Fatalf("NextPosition: %v", err)
	}
	if next != 1 {
		t.Errorf("next inside the nested collection = %d, want its own count", next)
	}

	next, err = store.NextPosition(ctx, ws, "col-2")
	if err != nil {
		t.Fatalf("NextPosition: %v", err)
	}
	if next != 0 {
		t.Errorf("next in an empty collection = %d, want 0", next)
	}

	// The empty id is the top level: the collections that have no parent.
	if next, err = store.NextPosition(ctx, ws, ""); err != nil {
		t.Fatalf("NextPosition: %v", err)
	} else if next != 1 {
		t.Errorf("next among the top-level collections = %d, want one past them", next)
	}
}

// TestNextPositionIsNotAMixOfTheTwoTables pins the half of the shared number line that is easy to
// get wrong: a nested collection counts towards the level's next position, so a new request lands
// after it rather than on top of its number.
func TestNextPositionIsNotAMixOfTheTwoTables(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	if err := store.SaveCollection(ctx, ws, nested("f-2", "col-1", 5, "Second")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	next, err := store.NextPosition(ctx, ws, "col-1")
	if err != nil {
		t.Fatalf("NextPosition: %v", err)
	}
	if next != 6 {
		t.Errorf("next = %d, want one past the nested collection at 5", next)
	}
}

func TestDeleteTakesTheNestedTreeWithIt(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	// A nested collection is a row of its own, so removing it is what takes its requests with it —
	// through both cascades: the nesting one, and the collection the request belongs to.
	if err := store.DeleteCollection(ctx, ws, "f-1"); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	if _, err := store.Node(ctx, "r-1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a request of a deleted nested collection = %v, want it gone too", err)
	}

	node, err := store.Node(ctx, "r-2")
	if err != nil {
		t.Fatalf("Node: %v, want the sibling of the deleted collection", err)
	}
	if node.Name != "One" {
		t.Errorf("node = %+v, want the request at the top untouched", node)
	}

	if err := store.DeleteCollection(ctx, ws, "col-1"); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	if _, err := store.Node(ctx, "r-2"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a request of a deleted collection = %v, want it gone too", err)
	}
	tree, err := store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(tree) != 0 {
		t.Errorf("tree = %+v, want nothing left", tree)
	}
}

// TestMoveNodeChangesItsCollection is the case SaveNode cannot do: its upsert rewrites the row's
// fields but never where the row lives, so a move has to be an update of its own.
func TestMoveNodeChangesItsCollection(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	if err := store.MoveNode(ctx, ws, "r-2", "f-1", 1); err != nil {
		t.Fatalf("MoveNode: %v", err)
	}

	moved, err := store.Node(ctx, "r-2")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if moved.CollectionID != "f-1" || moved.Position != 1 {
		t.Errorf("moved = %+v, want it inside the nested collection, after the request there", moved)
	}

	tree, err := store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	level := tree[0].Children[0]
	if len(level.Items) != 2 || level.Items[0].ID != "r-1" || level.Items[1].ID != "r-2" {
		t.Errorf("nested = %+v, want both requests in the dropped order", level.Items)
	}
	// The level it left is renumbered, not left with a hole where the request was.
	if len(tree[0].Items) != 0 {
		t.Errorf("the level it left = %+v, want nothing in it", tree[0].Items)
	}
}

// TestMoveNodePutsARowBetweenItsNeighbours is the drop the window draws as a line: the row goes
// where the index says, and the rows around it close up.
func TestMoveNodePutsARowBetweenItsNeighbours(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	for _, node := range []domain.CollectionNode{
		sampleNode("r-3", "col-1", 2, "Two", "GET", "https://api.example.com/2"),
		sampleNode("r-4", "col-1", 3, "Three", "GET", "https://api.example.com/3"),
	} {
		if err := store.SaveNode(ctx, node); err != nil {
			t.Fatalf("SaveNode %s: %v", node.ID, err)
		}
	}
	// The level is now: f-1 (0), r-2 (1), r-3 (2), r-4 (3).

	if err := store.MoveNode(ctx, ws, "r-4", "col-1", 1); err != nil {
		t.Fatalf("MoveNode: %v", err)
	}

	tree, err := store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	order := []string{}
	for _, entry := range tree[0].Level() {
		if entry.Collection != nil {
			order = append(order, entry.Collection.ID)
			continue
		}
		order = append(order, entry.Node.ID)
	}
	want := []string{"f-1", "r-4", "r-2", "r-3"}
	if !slices.Equal(order, want) {
		t.Errorf("level = %v, want %v", order, want)
	}
}

// TestMoveCollectionNestsAndComesBack covers both ends of a collection's move: into another, and
// back out to the top level, where it lands among the collections that have no parent.
func TestMoveCollectionNestsAndComesBack(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	if err := store.SaveCollection(ctx, ws,
		domain.Collection{ID: "col-2", Name: "Orders", Position: 1}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	if err := store.MoveCollection(ctx, ws, "col-2", "f-1", 0); err != nil {
		t.Fatalf("MoveCollection: %v", err)
	}

	tree, err := store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(tree) != 1 || tree[0].ID != "col-1" {
		t.Fatalf("top level = %+v, want only the collection it was moved into", tree)
	}
	nested := tree[0].Children[0]
	if len(nested.Children) != 1 || nested.Children[0].ID != "col-2" {
		t.Fatalf("nested = %+v, want the collection that was moved in", nested.Children)
	}
	if inner := nested.Children[0]; len(inner.Items) != 0 || inner.Name != "Orders" {
		t.Errorf("moved = %+v, want its own name and its own (empty) level", inner)
	}

	if err := store.MoveCollection(ctx, ws, "col-2", "", 0); err != nil {
		t.Fatalf("MoveCollection back out: %v", err)
	}
	tree, err = store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(tree) != 2 || tree[0].ID != "col-2" || tree[0].ParentID != "" {
		t.Errorf("top level = %+v, want it back out at the top, first", tree)
	}
	if len(tree[1].Children[0].Children) != 0 {
		t.Errorf("nested = %+v, want nothing left inside", tree[1].Children[0].Children)
	}
}

func TestRunRoundTrip(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	if _, found, err := store.LastRun(ctx, "col-1", ""); err != nil || found {
		t.Fatalf("a run that never happened = %v, %v, want nothing", err, found)
	}

	run := domain.CollectionRun{
		ID: "run-1", CollectionID: "col-1", NodeID: "f-1", Environment: "Local · dev",
		StartedAt: 1_700_000_000_000, Results: []domain.CollectionRunResult{},
	}
	if err := store.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	created := 200
	for _, result := range []domain.CollectionRunResult{
		{NodeID: "r-1", Position: 0, Status: &created, OK: true, DurationUs: 12_345, RecordID: "rec-1",
			AssertionsPassed: 1, AssertionsTotal: 2},
		{NodeID: "r-2", Position: 1, OK: false, Error: "server did not respond"},
	} {
		if err := store.AppendRunResult(ctx, run.ID, result); err != nil {
			t.Fatalf("AppendRunResult: %v", err)
		}
	}

	run.Passed, run.Failed, run.DurationUs = 1, 1, 90_000
	run.FinishedAt = 1_700_000_000_500
	if err := store.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	last, found, err := store.LastRun(ctx, "col-1", "f-1")
	if err != nil || !found {
		t.Fatalf("LastRun = %v, %v", err, found)
	}
	if last.Passed != 1 || last.Failed != 1 || last.DurationUs != 90_000 || last.FinishedAt == 0 {
		t.Errorf("run = %+v, want the counters and the time it was closed with", last)
	}
	// The environment the run went out under travels with it: the page that draws the run is opened
	// under whatever environment the window is on by then, and the run does not change with it.
	if last.Environment != "Local · dev" {
		t.Errorf("run environment = %q, want the one it was started under", last.Environment)
	}
	if len(last.Results) != 2 {
		t.Fatalf("run kept %d results, want 2", len(last.Results))
	}
	if first := last.Results[0]; first.Status == nil || *first.Status != 200 || !first.OK ||
		first.DurationUs != 12_345 {
		t.Errorf("first result = %+v, want the 200 with its microseconds", first)
	}
	// What the scripts asserted is read while the row is made and kept with it: the report hangs off a
	// record, and the row must still be able to say what was asserted once that record is gone.
	if first := last.Results[0]; first.AssertionsPassed != 1 || first.AssertionsTotal != 2 {
		t.Errorf("first result = %+v, want the one of two assertions it carried", first)
	}
	if second := last.Results[1]; second.Status != nil || second.OK ||
		second.Error != "server did not respond" {
		t.Errorf("second result = %+v, want no status and the reason", second)
	}
	if second := last.Results[1]; second.AssertionsTotal != 0 {
		t.Errorf("second result = %+v, want nothing asserted about a request that never went out", second)
	}
	// The row names the record it produced: it is what a click on it opens, and without it the row can
	// only say the status and the time.
	if last.Results[0].RecordID != "rec-1" {
		t.Errorf("first result = %+v, want the record it produced", last.Results[0])
	}
	if last.Results[1].RecordID != "" {
		t.Errorf("second result = %+v, want no record for a request that never got an answer",
			last.Results[1])
	}

	// The results hang off the run: dropping it takes them with it, so a deleted collection does not
	// leave orphan rows behind.
	if err := store.DeleteCollection(ctx, ws, "col-1"); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	if _, found, err := store.LastRun(ctx, "col-1", "f-1"); err != nil || found {
		t.Errorf("a run of a deleted collection = %v, %v, want nothing", err, found)
	}
}

func TestLastRunIsTheNewestOfThatNode(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	// A run of a collection inside another and a run of the whole collection are two different things:
	// the overview of one must not draw the other's summary.
	for _, run := range []domain.CollectionRun{
		{ID: "run-old", CollectionID: "col-1", NodeID: "", StartedAt: 100, Passed: 1},
		{ID: "run-folder", CollectionID: "col-1", NodeID: "f-1", StartedAt: 200, Failed: 1},
		{ID: "run-new", CollectionID: "col-1", NodeID: "", StartedAt: 300, Passed: 2},
	} {
		if err := store.SaveRun(ctx, run); err != nil {
			t.Fatalf("SaveRun %s: %v", run.ID, err)
		}
	}

	whole, _, err := store.LastRun(ctx, "col-1", "")
	if err != nil {
		t.Fatalf("LastRun: %v", err)
	}
	if whole.ID != "run-new" {
		t.Errorf("the collection's last run is %s, want the newest of its own", whole.ID)
	}

	nested, found, err := store.LastRun(ctx, "col-1", "f-1")
	if err != nil || !found {
		t.Fatalf("LastRun = %v, %v", err, found)
	}
	if nested.ID != "run-folder" {
		t.Errorf("the nested collection's last run is %s, want its own", nested.ID)
	}
}

func TestNodesSurviveACollectionRename(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	tree, err := store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	collection := tree[0]
	collection.Name = "Users API"
	if err := store.SaveCollection(ctx, ws, collection); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	tree, err = store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if tree[0].Name != "Users API" {
		t.Errorf("collection = %+v, want the new name", tree[0])
	}
	if tree[0].Auth == nil {
		t.Error("the rename dropped the collection's auth")
	}
	if len(tree[0].Items) != 1 || len(tree[0].Children) != 1 || len(tree[0].Children[0].Items) != 1 {
		t.Errorf("tree = %+v, want everything where it was", tree[0])
	}
	// A rename saves the row it read, and the row's parent is not part of what a rename edits: the
	// upsert leaves the placement alone, so a nested collection stays nested.
	if tree[0].Children[0].ParentID != "col-1" {
		t.Errorf("nested = %+v, want it still inside the collection", tree[0].Children[0])
	}
}

// A refusal a row carried is read back with it: the page draws the words of a code, and a run
// opened a week later has to say what the window said while it was going. A row the network failed
// keeps its error and no code at all — that one belongs to the machine.
func TestARunRowKeepsTheRefusalItCarried(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	run := domain.CollectionRun{
		ID: "run-1", CollectionID: "col-1", NodeID: "f-1",
		StartedAt: 1_700_000_000_000, Results: []domain.CollectionRunResult{},
	}
	if err := store.SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	refusal := &domain.Failure{
		Code: domain.CodeVariableMissing,
		Args: domain.Args{"n": "1", "names": "var3"},
	}
	for _, result := range []domain.CollectionRunResult{
		{NodeID: "r-1", Position: 0, OK: false, Error: refusal.Error(), Failure: refusal},
		{NodeID: "r-2", Position: 1, OK: false, Error: "server did not respond"},
	} {
		if err := store.AppendRunResult(ctx, run.ID, result); err != nil {
			t.Fatalf("AppendRunResult: %v", err)
		}
	}

	last, found, err := store.LastRun(ctx, "col-1", "f-1")
	if err != nil || !found {
		t.Fatalf("LastRun = %v, %v", err, found)
	}
	if len(last.Results) != 2 {
		t.Fatalf("run kept %d results, want 2", len(last.Results))
	}
	stored := last.Results[0].Failure
	if stored == nil || stored.Code != domain.CodeVariableMissing {
		t.Fatalf("first result = %+v, want the refusal it was written with", last.Results[0])
	}
	if stored.Args["names"] != "var3" || stored.Args["n"] != "1" {
		t.Errorf("args = %+v, want the values the sentence interpolates", stored.Args)
	}
	if last.Results[1].Failure != nil {
		t.Errorf("second result = %+v, want no code for a failure of the network's",
			last.Results[1].Failure)
	}
}

// A row written by something other than this app — an import, a statement run by hand — fills none
// of the columns it has nothing to say about, and the schema allows that. Opening the request must
// not be where that is discovered: the node reads with no rows of its own rather than failing.
func TestNodeReadsARowWithNullColumns(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveCollection(ctx, ws, nested("col-1", "", 0, "Collection")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO collection_nodes
		(id, collection_id, name, position, created_at, updated_at)
		VALUES ('r-1', 'col-1', 'List', 0, 1, 1)`); err != nil {
		t.Fatalf("inserting the row: %v", err)
	}

	node, err := store.Node(ctx, "r-1")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if node.Name != "List" || node.CollectionID != "col-1" {
		t.Errorf("node = %+v, want the row that was written", node)
	}
	if len(node.Params) != 0 || len(node.Headers) != 0 {
		t.Errorf("node = %+v, want no rows of any kind", node)
	}
}

// A deleted collection takes its tree off every screen at once: the panel's rows, the page under
// them, the requests opened by id and the numbering of what is left. The rows stay where they are,
// marked, because a deletion is one of the things the other side has to be told.
func TestADeletedCollectionLeavesEveryScreen(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	if err := store.DeleteCollection(ctx, ws, "col-1"); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}

	tree, err := store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(tree) != 0 {
		t.Errorf("the tree still holds %d levels: %+v", len(tree), tree)
	}
	// A request of the deleted tree is not a request anymore, whichever way it is asked for.
	for _, id := range []string{"r-1", "r-2"} {
		if _, err := store.Node(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("node %s = %v, want not found", id, err)
		}
	}
	if rows, err := store.ContentRows(ctx, "col-1"); err != nil || len(rows) != 0 {
		t.Errorf("the requests of a deleted collection = %+v, %v, want nothing", rows, err)
	}
	// And the numbering starts over: a request added to the empty level is the first of it.
	next, err := store.NextPosition(ctx, ws, "col-1")
	if err != nil {
		t.Fatalf("NextPosition: %v", err)
	}
	if next != 0 {
		t.Errorf("next position = %d, want the deleted rows not to count", next)
	}

	var marked int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM collection_nodes WHERE deleted_at IS NOT NULL`,
	).Scan(&marked); err != nil {
		t.Fatalf("counting the marked nodes: %v", err)
	}
	if marked != 2 {
		t.Errorf("%d nodes are marked, want the two the collection took with it", marked)
	}
}
