package db

import (
	"context"
	"strconv"
	"testing"

	"schemo/internal/domain"
)

// An addressed request is drawn by its address, and the name it was given is the last step of the
// way down to it — which is what keeps the name findable and what puts a name match on something
// visible.
func TestFindRequestsNamesTheWayDown(t *testing.T) {
	store := newMigratedStore(t)
	seedTree(t, store)

	hits, err := store.FindRequests(context.Background(), ws)
	if err != nil {
		t.Fatalf("FindRequests: %v", err)
	}

	deep := findHit(t, hits, "r-1")
	if deep.Title != "https://api.example.com/users" {
		t.Errorf("title = %q, want the address", deep.Title)
	}
	if deep.Badge != "GET" {
		t.Errorf("badge = %q, want the method", deep.Badge)
	}
	want := []string{"Users", "Admin", "List"}
	if len(deep.Path) != len(want) {
		t.Fatalf("trail = %v, want %v", deep.Path, want)
	}
	for i := range want {
		if deep.Path[i] != want[i] {
			t.Fatalf("trail = %v, want %v", deep.Path, want)
		}
	}
	if deep.Open.Target != domain.TargetRequest || deep.Open.ID != "r-1" {
		t.Errorf("open = %+v, want the request itself", deep.Open)
	}
}

// A request made a moment ago is named and not yet addressed: a row titled by an empty address says
// nothing but its method, so the name is the identity until an address can stand in for it.
func TestFindRequestsDrawsAnUnaddressedRequestByName(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveCollection(ctx, ws,
		domain.Collection{ID: "col-1", Name: "234", Position: 0}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	if err := store.SaveNode(ctx, domain.CollectionNode{
		ID: "r-1", CollectionID: "col-1", Name: "New request", Position: 0, Method: "GET",
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	hits, err := store.FindRequests(ctx, ws)
	if err != nil {
		t.Fatalf("FindRequests: %v", err)
	}

	row := findHit(t, hits, "r-1")
	if row.Title != "New request" {
		t.Errorf("title = %q, want the name it was given", row.Title)
	}
	// The name is the title now, so it must not also be the trail's last step: the trail is where the
	// request sits, and it sits in one place.
	if len(row.Path) != 1 || row.Path[0] != "234" {
		t.Errorf("trail = %v, want the collection alone", row.Path)
	}
}

// The other half of the same rule, on a tree that reaches deeper: with an address to draw, the name
// becomes the trail's last step instead — so a request is still findable by the name it was given.
func TestFindRequestsKeepsTheNameReachableUnderAnAddress(t *testing.T) {
	store := newMigratedStore(t)
	seedTree(t, store)

	hits, err := store.FindRequests(context.Background(), ws)
	if err != nil {
		t.Fatalf("FindRequests: %v", err)
	}

	deep := findHit(t, hits, "r-1")
	if deep.Path[len(deep.Path)-1] != "List" {
		t.Errorf("trail = %v, want the request's own name at the end", deep.Path)
	}
}

// A collection is named by what is above it, not by itself: the title is the name, and repeating it
// in the trail would draw it twice.
func TestFindCollectionsNamesWhatIsAbove(t *testing.T) {
	store := newMigratedStore(t)
	seedTree(t, store)

	hits, err := store.FindCollections(context.Background(), ws)
	if err != nil {
		t.Fatalf("FindCollections: %v", err)
	}

	top := findHit(t, hits, "col-1")
	if top.Title != "Users" || len(top.Path) != 0 {
		t.Errorf("top level = %q %v, want the name and nothing above it", top.Title, top.Path)
	}
	folder := findHit(t, hits, "f-1")
	if folder.Title != "Admin" {
		t.Errorf("folder title = %q, want Admin", folder.Title)
	}
	if len(folder.Path) != 1 || folder.Path[0] != "Users" {
		t.Errorf("folder trail = %v, want its parent alone", folder.Path)
	}
}

// A secret is found by its name and never by what it holds: the value is not put next to a query,
// not even to compare it, so there is nothing to leak when the row is drawn.
func TestFindEnvironmentsKeepsSecretValuesToThemselves(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveEnvironment(ctx, ws,
		domain.Environment{ID: "env-1", Name: "Local", Position: 0}); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-1"}, domain.Variable{
		ID: "v-text", Name: "role", Value: "admin", Kind: domain.VariableText, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-1"}, domain.Variable{
		ID: "v-secret", Name: "token", Value: "s3cr3t", Kind: domain.VariableSecret, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}
	if err := store.SetActiveEnvironment(ctx, ws, "env-1"); err != nil {
		t.Fatalf("SetActiveEnvironment: %v", err)
	}

	hits, err := store.FindEnvironments(ctx, ws)
	if err != nil {
		t.Fatalf("FindEnvironments: %v", err)
	}

	text := findHit(t, hits, "v-text")
	if text.MatchText != "admin" {
		t.Errorf("a text variable answers with its value, got %q", text.MatchText)
	}
	if len(text.Path) != 1 || text.Path[0] != "Local" {
		t.Errorf("variable trail = %v, want the environment holding it", text.Path)
	}
	if text.Open.Target != domain.TargetVariable || text.Open.Scope != "env-1" {
		t.Errorf("open = %+v, want the variable and the environment to open it in", text.Open)
	}

	secret := findHit(t, hits, "v-secret")
	if secret.MatchText != "" {
		t.Errorf("a secret carries its value: %q", secret.MatchText)
	}
	if secret.Title != "token" {
		t.Errorf("a secret is found by its name, got %q", secret.Title)
	}

	if env := findHit(t, hits, "env-1"); env.Note.Kind != domain.NoteActive {
		t.Errorf("note = %+v, want the environment on screen marked", env.Note)
	}
}

// The history is the one area that answers an empty field, and it answers newest first: that order
// is what "what was I just doing" means.
func TestFindHistoryIsNewestFirst(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	for _, at := range []int64{100, 300, 200} {
		rec := sampleRecord("rec-"+strconv.FormatInt(at, 10), domain.SourceManual)
		rec.StartedAt = at
		if err := store.SaveRecord(ctx, ws, rec); err != nil {
			t.Fatalf("SaveRecord: %v", err)
		}
	}

	hits, err := store.FindHistory(ctx, ws)
	if err != nil {
		t.Fatalf("FindHistory: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("history = %d rows, want 3", len(hits))
	}

	want := []int64{300, 200, 100}
	for i, at := range want {
		if hits[i].At != at {
			t.Fatalf("row %d happened at %d, want %d", i, hits[i].At, at)
		}
	}
	if hits[0].Badge != "GET" || hits[0].Open.Target != domain.TargetHistory {
		t.Errorf("row = %+v, want a call that can be opened", hits[0])
	}
}
