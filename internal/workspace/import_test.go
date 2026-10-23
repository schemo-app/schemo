package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"schemo/internal/domain"
)

// A file carries what each level runs around its requests, and an import writes it where the level
// does: SaveCollection cannot, because the code is written against a row that has to exist first.
func TestImportWritesTheCodeOfEveryLevel(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	tree, err := collections.Import(ctx, []domain.Collection{{
		Name:    "From a file",
		Scripts: &domain.Scripts{Pre: "vars.set('top', '1')"},
		Items: []domain.CollectionNode{{
			Name: "Inside", Method: "GET", URL: "https://api.example.com",
			Scripts: &domain.Scripts{Post: "pm.test('ok', () => {});"},
		}},
		Children: []domain.Collection{{
			Name:    "Nested",
			Scripts: &domain.Scripts{Pre: "vars.set('nested', '1')"},
		}},
	}}, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	collection := only(t, tree)
	for _, level := range []struct {
		what string
		id   string
		want string
	}{
		{"collection", collection.ID, "vars.set('top', '1')"},
		{"nested", collection.Children[0].ID, "vars.set('nested', '1')"},
		{"request", collection.Items[0].ID, "pm.test('ok', () => {});"},
	} {
		scripts, err := store.Scripts(ctx, ws, level.id)
		if err != nil {
			t.Fatalf("%s: Scripts: %v", level.what, err)
		}
		if scripts == nil || (scripts.Pre != level.want && scripts.Post != level.want) {
			t.Errorf("%s scripts = %+v, want %q", level.what, scripts, level.want)
		}
	}
}

// A name longer than the ceiling is clipped rather than refused: refusing a whole document over one
// line of it is not what the person asked for.
func TestImportClipsALongName(t *testing.T) {
	collections, _ := newCollections(t)

	long := strings.Repeat("n", maxCollectionName+30)
	tree, err := collections.Import(context.Background(), []domain.Collection{{Name: long}}, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if name := only(t, tree).Name; name != long[:maxCollectionName] {
		t.Errorf("name = %q, want it clipped to the ceiling", name)
	}
}

// An empty name is refused before anything is written: an import that failed halfway would leave
// half a document in the tree.
func TestImportRefusesAnEmptyNameAndWritesNothing(t *testing.T) {
	collections, store := newCollections(t)

	_, err := collections.Import(context.Background(), []domain.Collection{
		{Name: "Fine", Items: []domain.CollectionNode{{Name: "One", Method: "GET"}}},
		{Name: "   "},
	}, "")
	if !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("Import = %v, want ErrNotAllowed", err)
	}
	if tree, err := store.Collections(context.Background(), ws); err != nil || len(tree) != 0 {
		t.Errorf("the store holds %+v (%v), want nothing written", tree, err)
	}
}

// Full is what an export writes down: the requests whole — the tree carries the method and nothing
// else — with the variables of every level and the code beside them.
func TestFullReadsWholeLevelsAndTheirCode(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	tree, err := collections.Import(ctx, []domain.Collection{{
		Name:      "Exported",
		Variables: []domain.Variable{{Name: "version", Value: "v2"}},
		Items: []domain.CollectionNode{{
			Name: "One", Method: "GET", URL: "https://api.example.com/users?page=2",
			Params: []domain.Row{{Name: "page", Value: "2", Enabled: true}},
		}},
	}}, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	id := only(t, tree).ID
	if err := store.SaveScripts(ctx, ws, id, &domain.Scripts{Pre: "vars.set('a', '1')"}); err != nil {
		t.Fatalf("SaveScripts: %v", err)
	}

	full, err := collections.Full(ctx, id)
	if err != nil {
		t.Fatalf("Full: %v", err)
	}
	if len(full.Items) != 1 || full.Items[0].URL != "https://api.example.com/users?page=2" ||
		len(full.Items[0].Params) != 1 {
		t.Errorf("items = %+v, want the request whole", full.Items)
	}
	if full.Scripts == nil || full.Scripts.Pre != "vars.set('a', '1')" {
		t.Errorf("scripts = %+v, want the level's own code", full.Scripts)
	}
	if len(full.Variables) != 1 || full.Variables[0].Name != "version" {
		t.Errorf("variables = %+v, want the level's own answers", full.Variables)
	}
}

// FullOf reads a level of a named workspace rather than of whichever one is active: a copy out of
// another workspace reads the collection there and writes it here.
func TestFullOfReadsANamedWorkspace(t *testing.T) {
	collections, store := newCollections(t)
	ctx := context.Background()

	tree, err := collections.InWorkspace(addWorkspace(t, store, "another-space")).Import(ctx, []domain.Collection{{
		Name:  "Elsewhere",
		Items: []domain.CollectionNode{{Name: "One", Method: "GET", URL: "https://api.example.com"}},
	}}, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	full, err := collections.FullOf(ctx, "another-space", only(t, tree).ID)
	if err != nil {
		t.Fatalf("FullOf: %v", err)
	}
	if full.Name != "Elsewhere" || len(full.Items) != 1 || full.Items[0].URL == "" {
		t.Errorf("FullOf = %+v, want the level read whole from the named workspace", full)
	}
}

// Requests is the walk a run makes, kept for what copies a collection out: every request whole,
// with the authorization and the variables of the levels above it.
func TestRequestsCarriesTheAnswerOfEveryLevelAbove(t *testing.T) {
	collections, _ := newCollections(t)
	ctx := context.Background()

	tree, err := collections.Import(ctx, []domain.Collection{{
		Name: "Copyable",
		Auth: &domain.Auth{Type: domain.AuthBearer, Fields: map[string]string{"token": "{{token}}"}},
		Variables: []domain.Variable{
			{Name: "version", Value: "v2", Kind: domain.VariableText, Enabled: true},
		},
		Items: []domain.CollectionNode{
			{Name: "Plain", Method: "GET", URL: "https://api.example.com/b"},
		},
		Children: []domain.Collection{{
			Name: "Inner",
			Auth: &domain.Auth{Type: domain.AuthBasic, Fields: map[string]string{
				"username": "u", "password": "p",
			}},
			Items: []domain.CollectionNode{
				{Name: "Nested", Method: "POST", URL: "https://api.example.com/c"},
			},
		}},
	}}, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	requests, err := collections.Requests(ctx, only(t, tree).ID, "")
	if err != nil {
		t.Fatalf("Requests: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want both", len(requests))
	}
	if requests[0].Node.Name != "Plain" || requests[0].Node.URL != "https://api.example.com/b" {
		t.Errorf("first = %+v, want the request whole", requests[0])
	}
	if requests[0].Auth == nil || requests[0].Auth.Type != domain.AuthBearer {
		t.Errorf("first auth = %+v, want the collection's", requests[0].Auth)
	}
	if len(requests[0].Variables) != 1 || requests[0].Variables[0].Name != "version" {
		t.Errorf("first variables = %+v, want the level's own", requests[0].Variables)
	}
	if requests[1].Auth == nil || requests[1].Auth.Type != domain.AuthBasic {
		t.Errorf("second auth = %+v, want the nearest level's", requests[1].Auth)
	}
}
