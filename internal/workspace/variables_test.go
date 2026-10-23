package workspace

import (
	"context"
	"errors"
	"testing"

	"schemo/internal/domain"
)

// The shape that says which levels answer for a request's `{{tokens}}`: a collection, a collection
// inside it, and a request in each of the two.
type varsTree struct {
	collections  *Collections
	collectionID string
	nestedID     string
	insideID     string
	besideID     string
}

func setupVarsTree(t *testing.T) varsTree {
	t.Helper()
	collections, _ := newCollections(t)
	ctx := context.Background()

	_, tree, err := collections.collectionAndTree(collections.CreateCollection(ctx, "Collection", "", ""))
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID
	nestedID := nestCollection(t, collections, "Nested", collectionID)

	_, tree, err = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: nestedID, Name: "Inside"}))
	if err != nil {
		t.Fatalf("CreateNode inside: %v", err)
	}
	insideID := findInTree(t, tree, "Inside").ID

	_, tree, err = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: collectionID, Name: "Sibling"}))
	if err != nil {
		t.Fatalf("CreateNode beside: %v", err)
	}
	besideID := findInTree(t, tree, "Sibling").ID

	return varsTree{collections: collections, collectionID: collectionID, nestedID: nestedID, insideID: insideID,
		besideID: besideID}
}

func variable(name string, value string) domain.Variable {
	return domain.Variable{
		ID:      "v-" + name,
		Name:    name,
		Value:   value,
		Kind:    domain.VariableText,
		Enabled: true,
	}
}

func saveVariables(t *testing.T, collections *Collections, id string, variables ...domain.Variable) {
	t.Helper()
	if err := collections.Update(context.Background(), id, patchVariables(variables)); err != nil {
		t.Fatalf("SaveVariables(%s): %v", id, err)
	}
}

// A request is answered by the collection it sits in and by the collections above that one, and the
// list comes outermost first: the nearest answer for a name is the last one in it, which is what
// makes the level closest to the request the one that counts.
func TestVariablesAreReadOutermostFirst(t *testing.T) {
	ctx := context.Background()
	v := setupVarsTree(t)

	saveVariables(t, v.collections, v.collectionID, variable("baseUrl", "https://outer"))
	saveVariables(t, v.collections, v.nestedID,
		variable("baseUrl", "https://inner"), variable("page", "2"))

	above, err := v.collections.Above(ctx, domain.DraftID(v.insideID))
	if err != nil {
		t.Fatalf("Above: %v", err)
	}
	if len(above.Variables) != 3 {
		t.Fatalf("above = %+v, want both levels' variables", above)
	}
	if above.Variables[0].Name != "baseUrl" || above.Variables[0].Value != "https://outer" {
		t.Errorf("first = %+v, want the outer collection's answer", above.Variables[0])
	}
	if last := above.Variables[len(above.Variables)-1]; last.Name != "page" {
		t.Errorf("last = %+v, want the nearest level's own", last)
	}
	// The same name twice is the whole point: the reader takes them in order, so the second is what
	// stands.
	if above.Variables[1].Name != "baseUrl" || above.Variables[1].Value != "https://inner" {
		t.Errorf("second = %+v, want the nested one over the outer", above.Variables[1])
	}
}

// A request beside a nested collection is not under it: the levels that answer for a request
// are the ones it is inside of, and nothing else.
func TestVariablesSkipLevelsTheRequestIsNotUnder(t *testing.T) {
	ctx := context.Background()
	v := setupVarsTree(t)

	saveVariables(t, v.collections, v.collectionID, variable("baseUrl", "https://outer"))
	saveVariables(t, v.collections, v.nestedID, variable("baseUrl", "https://inner"))

	above, err := v.collections.Above(ctx, domain.DraftID(v.besideID))
	if err != nil {
		t.Fatalf("Above: %v", err)
	}
	if len(above.Variables) != 1 || above.Variables[0].Value != "https://outer" {
		t.Fatalf("above = %+v, want the collection's own answer alone", above.Variables)
	}
}

// The command line's draft is in no tree, and neither is an id that has been deleted. Neither is a
// failure: "there is nothing above this request" is the answer.
func TestVariablesOfALevelWithNothingAboveAreNone(t *testing.T) {
	ctx := context.Background()
	v := setupVarsTree(t)
	saveVariables(t, v.collections, v.collectionID, variable("baseUrl", "https://outer"))

	for _, id := range []domain.DraftID{domain.DraftID("tab:line"), "not-found"} {
		above, err := v.collections.Above(ctx, id)
		if err != nil {
			t.Fatalf("Above(%s): %v", id, err)
		}
		if len(above.Variables) != 0 || above.Auth != nil {
			t.Errorf("above(%s) = %+v, want nothing", id, above)
		}
	}
}

// The set is stored the way the editor holds it: trimmed and numbered from one. A row with no name
// yet is one of the set and not a mistake to drop — the "+" of the editor makes exactly that row,
// and dropping it would leave the button with nothing to show for itself.
func TestSaveVariablesNumbersAndKeepsNameless(t *testing.T) {
	ctx := context.Background()
	v := setupVarsTree(t)

	tree, err := v.collections.after(v.collections.Update(ctx, v.collectionID, patchVariables([]domain.Variable{
		variable("  baseUrl  ", "https://api"),
		variable("   ", ""),
		variable("page", "2"),
	})))
	if err != nil {
		t.Fatalf("SaveVariables: %v", err)
	}

	saved := only(t, tree)
	if len(saved.Variables) != 3 {
		t.Fatalf("variables = %+v, want the row with no name among them", saved.Variables)
	}
	if saved.Variables[0].Name != "baseUrl" || saved.Variables[0].Position != 1 {
		t.Errorf("first = %+v, want a trimmed name in the first place", saved.Variables[0])
	}
	// The name is trimmed to nothing rather than to something: a row nobody has named is a row that
	// answers for no `{{token}}` at all.
	if saved.Variables[1].Name != "" || saved.Variables[1].Position != 2 {
		t.Errorf("second = %+v, want the nameless row in its own place", saved.Variables[1])
	}
	if saved.Variables[1].ID == "" {
		t.Errorf("second = %+v, want an id minted for it", saved.Variables[1])
	}
	if !saved.Variables[2].HasValue {
		t.Errorf("third = %+v, want it to say there is a value", saved.Variables[2])
	}
}

// A collection is what gets exported and handed on, so a secret has no place in one: the refusal is
// the feature, not an accident of the storage.
func TestSaveVariablesRefusesASecret(t *testing.T) {
	ctx := context.Background()
	v := setupVarsTree(t)

	secret := variable("token", "s3cret")
	secret.Kind = domain.VariableSecret
	err := v.collections.Update(ctx, v.collectionID, patchVariables([]domain.Variable{secret}))
	if !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("SaveVariables(secret) = %v, want a refusal", err)
	}
	if code := domain.CodeOf(err); code != domain.CodeVariableSecret {
		t.Errorf("code = %q, want %q", code, domain.CodeVariableSecret)
	}
}

// The page's table draws an address, which the tree deliberately does not carry — so it is a read
// of its own, and it holds everything inside the collection: the folders' requests too, because
// the table is what running the collection would send and a run reaches them.
func TestContentsCarryTheAddressAndWalkTheFolders(t *testing.T) {
	ctx := context.Background()
	v := setupVarsTree(t)

	if err := v.collections.store.SaveNode(ctx, domain.CollectionNode{
		ID: "n-url", CollectionID: v.collectionID, Name: "List", Method: "GET", Position: 5,
		URL: "{{baseUrl}}/articles",
	}); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	rows, err := v.collections.Contents(ctx, v.collectionID)
	if err != nil {
		t.Fatalf("Contents: %v", err)
	}

	byID := map[string]domain.LevelRow{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	if row, ok := byID["n-url"]; !ok {
		t.Errorf("rows = %+v, want the saved request among them", rows)
	} else if row.URL != "{{baseUrl}}/articles" || row.Method != "GET" {
		t.Errorf("row = %+v, want the address and the method", row)
	}

	// The request inside the folder is a row of the same table: a report that left it out would
	// count a run's rows in one place and draw them in another.
	if _, ok := byID[v.insideID]; !ok {
		t.Fatalf("rows = %+v, want the folder's own request among them", rows)
	}

	// A folder read as a level of its own stands alone: its rows are the level.
	nested, err := v.collections.Contents(ctx, v.nestedID)
	if err != nil {
		t.Fatalf("Contents(nested): %v", err)
	}
	if len(nested) != 1 || nested[0].ID != v.insideID {
		t.Errorf("rows = %+v, want the folder's own request and nothing else", nested)
	}
}
