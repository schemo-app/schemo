package draft

import (
	"context"
	"slices"
	"testing"

	"schemo/internal/domain"
)

func pathRowNames(rows []domain.Row) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}

func typeURL(t *testing.T, uc *UseCase, text string) State {
	t.Helper()
	result, err := uc.SetText(context.Background(), domain.DraftCommandLine,
		TextInput{Field: FieldURL, Text: text})
	if err != nil {
		t.Fatalf("SetText: %v", err)
	}
	return result.State
}

func fillPathRow(t *testing.T, uc *UseCase, state State, name, value string) State {
	t.Helper()
	for _, row := range state.Draft.PathParams {
		if row.Name != name {
			continue
		}
		patched, err := uc.PatchRow(context.Background(), domain.DraftCommandLine,
			domain.RowPathParams, row.ID, RowPatch{Value: &value})
		if err != nil {
			t.Fatalf("PatchRow: %v", err)
		}
		return patched
	}
	t.Fatalf("no path row %q in %v", name, state.Draft.PathParams)
	return State{}
}

// The rows follow the address: a `:name` typed into it is a row to fill, and one deleted from it
// takes its row away.
func TestAPathParameterTypedIntoTheAddressIsARow(t *testing.T) {
	uc, _ := loaded(t)

	state := typeURL(t, uc, "https://{{host}}/products/:product/prices/:price")
	if got := pathRowNames(state.Draft.PathParams); !slices.Equal(got, []string{"product", "price"}) {
		t.Fatalf("path rows = %v, want one for each name in the address", got)
	}

	state = typeURL(t, uc, "https://{{host}}/products/:product")
	if got := pathRowNames(state.Draft.PathParams); !slices.Equal(got, []string{"product"}) {
		t.Errorf("path rows = %v, want the name the address lost gone with its row", got)
	}
}

// The value is the row's, so typing further into the address leaves it where it was.
func TestAPathRowKeepsItsValueAndIdWhileTheAddressIsEdited(t *testing.T) {
	uc, _ := loaded(t)

	state := typeURL(t, uc, "https://{{host}}/products/:product")
	state = fillPathRow(t, uc, state, "product", "42")
	before := state.Draft.PathParams[0]

	state = typeURL(t, uc, "https://{{host}}/products/:product/prices")
	after := state.Draft.PathParams[0]
	if after.ID != before.ID || after.Value != "42" {
		t.Errorf("path row = %+v, want %+v kept through the edit", after, before)
	}
}

// A path row can be given a value and a description. Its name is the address's, and a row is
// neither added nor removed nor moved by hand.
func TestAPathRowCanOnlyBeFilled(t *testing.T) {
	ctx := context.Background()
	uc, _ := loaded(t)
	state := typeURL(t, uc, "/products/:product")
	row := state.Draft.PathParams[0]

	renamed, description := "other", "which product"
	patched, err := uc.PatchRow(ctx, domain.DraftCommandLine, domain.RowPathParams, row.ID,
		RowPatch{Name: &renamed, Description: &description})
	if err != nil {
		t.Fatalf("PatchRow: %v", err)
	}
	if got := patched.Draft.PathParams[0]; got.Name != "product" || got.Description != description {
		t.Errorf("path row = %+v, want its description written and its name left", got)
	}

	if _, err := uc.AddRow(ctx, domain.DraftCommandLine, domain.RowPathParams); err == nil {
		t.Error("AddRow on the path rows = nil, want a refusal")
	}
	if _, err := uc.RemoveRow(ctx, domain.DraftCommandLine, domain.RowPathParams, row.ID); err == nil {
		t.Error("RemoveRow on the path rows = nil, want a refusal")
	}
	_, err = uc.MoveRow(ctx, domain.DraftCommandLine, domain.RowPathParams, row.ID, 0)
	if err == nil {
		t.Error("MoveRow on the path rows = nil, want a refusal")
	}
}

// What goes out is the address with the values written in, and the variables inside a value are
// resolved like the ones in the rest of the address.
func TestThePathValuesGoOutInTheAddress(t *testing.T) {
	ctx := context.Background()
	uc, _ := loaded(t)
	state := typeURL(t, uc, "https://{{host}}/products/:product/prices/:price")
	state = fillPathRow(t, uc, state, "product", "a b")
	fillPathRow(t, uc, state, "price", "{{token}}")

	prepared, err := uc.Prepared(ctx, domain.DraftCommandLine, nil, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	if want := "https://api.example.com/products/a%20b/prices/abc123"; prepared.URL != want {
		t.Errorf("url = %q, want %q", prepared.URL, want)
	}
	masked := "https://api.example.com/products/a%20b/prices/" + fakeMask
	if prepared.MaskedURL != masked {
		t.Errorf("masked url = %q, want %q", prepared.MaskedURL, masked)
	}
}

// A name with no value stays in the address as written: an empty slot is a person's to fill.
func TestAnEmptyPathValueLeavesTheNameInTheAddress(t *testing.T) {
	ctx := context.Background()
	uc, _ := loaded(t)
	typeURL(t, uc, "https://{{host}}/products/:product")

	prepared, err := uc.Prepared(ctx, domain.DraftCommandLine, nil, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	if want := "https://api.example.com/products/:product"; prepared.URL != want {
		t.Errorf("url = %q, want %q", prepared.URL, want)
	}
}

// The jar is asked about the address the request goes to, which has its values written in.
func TestTheJarIsAskedAboutTheFilledInAddress(t *testing.T) {
	ctx := context.Background()
	uc, _, _, _, jar := newWithJar()
	if err := uc.Load(ctx); err != nil {
		t.Fatalf("Load: %v", err)
	}
	state := typeURL(t, uc, "https://{{host}}/shops/:shop/cart")
	fillPathRow(t, uc, state, "shop", "eu")

	if _, err := uc.Prepared(ctx, domain.DraftCommandLine, nil, nil); err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	if want := "https://api.example.com/shops/eu/cart"; jar.got.url != want {
		t.Errorf("jar asked about %q, want %q", jar.got.url, want)
	}
}

// A saved request opens with the values it was saved with, and the rows still follow its address.
func TestASavedRequestOpensWithItsPathValues(t *testing.T) {
	ctx := context.Background()
	uc, _ := newUseCase()

	state, err := uc.Open(ctx, FromNode(domain.CollectionNode{
		ID: "node-1", Method: "GET", URL: "/products/:product",
		PathParams: []domain.Row{{Name: "product", Value: "7", Enabled: true}},
	}))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if rows := state.Draft.PathParams; len(rows) != 1 || rows[0].Value != "7" || rows[0].ID == "" {
		t.Errorf("path rows = %+v, want the saved value under an id of its own", rows)
	}
}

// A request that came from outside with no rows of its own — an older save, a synced node — is
// laid out the same way: the rows are derived from the address.
func TestAnAddressWithNoSavedRowsGetsItsRowsWhenOpened(t *testing.T) {
	ctx := context.Background()
	uc, _ := newUseCase()

	state, err := uc.Open(ctx, FromNode(domain.CollectionNode{
		ID: "node-2", Method: "GET", URL: "/groups/:group/items/:item",
	}))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := pathRowNames(state.Draft.PathParams); !slices.Equal(got, []string{"group", "item"}) {
		t.Errorf("path rows = %v, want a row for each name", got)
	}
}

// A draft stored before the rows existed has an address and none of them: it is laid out when it is
// read back, and not only when somebody edits the address.
func TestAStoredDraftWithNoPathRowsGetsThemWhenItIsRead(t *testing.T) {
	uc, store := newUseCase()
	store.saved, store.has = domain.Draft{
		ID: domain.DraftCommandLine, Method: "GET", URL: "/groups/:group",
	}, true
	if err := uc.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	state, err := uc.Snapshot(context.Background(), domain.DraftCommandLine)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got := pathRowNames(state.Draft.PathParams); !slices.Equal(got, []string{"group"}) {
		t.Errorf("path rows = %v, want a row for the name in the address", got)
	}
}
