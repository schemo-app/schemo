package workspace

import (
	"context"
	"testing"

	"schemo/internal/domain"
)

// The shape that says whether a request finds the level that answers for it.
type authTree struct {
	collections  *Collections
	collectionID string
	nestedID     string
	insideID     string
	besideID     string
}

func setupAuthTree(t *testing.T) authTree {
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

	return authTree{collections: collections, collectionID: collectionID, nestedID: nestedID, insideID: insideID,
		besideID: besideID}
}

func bearer(token string) domain.Auth {
	return domain.NewAuth(domain.AuthBearer).With("token", token)
}

// bearerRef is the same answer where the tree holds one by pointer. Nil there means "nothing said
// here" — a level whose requests inherit past it — which is a different thing from "None".
func bearerRef(token string) *domain.Auth {
	auth := bearer(token)
	return &auth
}

// inherited is what a request inherits, read through the call the app itself makes.
func inherited(ctx context.Context, collections *Collections, id domain.DraftID) (*domain.Auth, error) {
	above, err := collections.Above(ctx, id)
	if err != nil {
		return nil, err
	}
	return above.Auth, nil
}

func TestAuthIsInheritedDownTheTree(t *testing.T) {
	ctx := context.Background()
	a := setupAuthTree(t)

	if err := a.collections.Update(ctx, a.collectionID, patchAuth(bearer("Collection"))); err != nil {
		t.Fatalf("SaveAuth collection: %v", err)
	}

	got, err := inherited(ctx, a.collections, domain.DraftID(a.insideID))
	if err != nil {
		t.Fatalf("AuthFor: %v", err)
	}
	if got == nil || got.Answer("token") != "Collection" {
		t.Errorf("inherited = %+v, want the collection's", got)
	}

	// A collection that answers for itself stops the walk for everything inside it, and only for that.
	if err := a.collections.Update(ctx, a.nestedID, patchAuth(bearer("Nested"))); err != nil {
		t.Fatalf("SaveAuth nested: %v", err)
	}
	if got, _ := inherited(ctx, a.collections,
		domain.DraftID(a.insideID)); got == nil || got.Answer("token") != "Nested" {
		t.Errorf("inside inherited = %+v, want the nested collection's", got)
	}
	if got, _ := inherited(ctx, a.collections,
		domain.DraftID(a.besideID)); got == nil || got.Answer("token") != "Collection" {
		t.Errorf("beside inherited = %+v, want the collection's", got)
	}

	// A request that answers for itself is not the nested collection's any more.
	if err := a.collections.Update(ctx, a.insideID, patchAuth(bearer("self"))); err != nil {
		t.Fatalf("SaveAuth request: %v", err)
	}
	if got, _ := inherited(ctx, a.collections,
		domain.DraftID(a.insideID)); got == nil || got.Answer("token") != "self" {
		t.Errorf("own = %+v, want the request's own", got)
	}

	// "None" is not an answer that stops the walk: a level with nothing of its own lets the one below
	// it inherit from the one above.
	if err := a.collections.Update(ctx, a.nestedID, patchAuth(domain.Auth{Type: domain.AuthNone})); err != nil {
		t.Fatalf("SaveAuth nested none: %v", err)
	}
	if got, _ := inherited(ctx, a.collections,
		domain.DraftID(a.besideID)); got == nil || got.Answer("token") != "Collection" {
		t.Errorf("after clearing the nested one = %+v, want the collection's still", got)
	}
	tree, err := a.collections.Tree(ctx)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if nested := findIn(t, tree, a.nestedID); nested.Auth != nil {
		t.Errorf("nested auth = %+v, want it stored as nothing at all", nested.Auth)
	}
}

// "None" says who authorizes the request rather than what with, so choosing it is not an erasure:
// the answers a level was given before stay with it, and the level that did answer is the one
// above.
func TestNoKeepsWhatWasAlreadyFilled(t *testing.T) {
	ctx := context.Background()
	a := setupAuthTree(t)

	if err := a.collections.Update(ctx, a.collectionID, patchAuth(bearer("Collection"))); err != nil {
		t.Fatalf("SaveAuth collection: %v", err)
	}
	if err := a.collections.Update(ctx, a.nestedID, patchAuth(bearer("Nested"))); err != nil {
		t.Fatalf("SaveAuth nested: %v", err)
	}

	// The tab sends the whole answer, so "None" arrives carrying the fields it was holding.
	if err := a.collections.Update(ctx, a.nestedID, patchAuth(domain.Auth{
		Type:   domain.AuthNone,
		Fields: map[string]string{"prefix": "Bearer", "token": "Nested"},
	})); err != nil {
		t.Fatalf("SaveAuth nested none: %v", err)
	}

	// Nothing inside authorizes itself with what the nested level held: it said "None" there, and the
	// nearest level that did answer is the collection around it.
	if got, _ := inherited(ctx, a.collections,
		domain.DraftID(a.insideID)); got == nil || got.Answer("token") != "Collection" {
		t.Errorf("inside = %+v, want the outer collection's", got)
	}

	// Changing one's mind back finds the token, because "None" did not throw it away — which is the
	// whole difference between this and a level nobody has ever touched.
	tree, err := a.collections.Tree(ctx)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	stored := findIn(t, tree, a.nestedID).Auth
	if stored == nil || stored.Answer("token") != "Nested" {
		t.Fatalf("stored = %+v, want 'no' to have kept the token", stored)
	}

	back := *stored
	back.Type = domain.AuthBearer
	if err := a.collections.Update(ctx, a.nestedID, patchAuth(back)); err != nil {
		t.Fatalf("SaveAuth nested bearer again: %v", err)
	}
	if got, _ := inherited(ctx, a.collections,
		domain.DraftID(a.insideID)); got == nil || got.Answer("token") != "Nested" {
		t.Errorf("inside again = %+v, want the nested collection's token back", got)
	}
}

func TestAuthForAnswersNothingOutsideTheTree(t *testing.T) {
	ctx := context.Background()
	a := setupAuthTree(t)

	for _, id := range []domain.DraftID{domain.DraftID("tab:line"), "not-found"} {
		got, err := inherited(ctx, a.collections, id)
		if err != nil {
			t.Errorf("inherited(%q) = %v, want no error", id, err)
		}
		if got != nil {
			t.Errorf("inherited(%q) = %+v, want nothing to inherit", id, got)
		}
	}
}

// The command line can never answer "Inherit" — nothing is above it — so "None" is its unset
// state, and stored as a value it would keep a saved request from inheriting the collection.
func TestARequestSavedFromTheCommandLineInherits(t *testing.T) {
	ctx := context.Background()
	a := setupAuthTree(t)

	if err := a.collections.Update(ctx, a.collectionID, patchAuth(bearer("Collection"))); err != nil {
		t.Fatalf("SaveAuth: %v", err)
	}

	// Exactly what the window hands SaveDraft: the draft's own auth, which a fresh one carries as
	// "None" with no answers in it.
	fresh := domain.NewDraft().Auth
	_, tree, err := a.collections.nodeAndTree(a.collections.CreateNode(ctx, RequestDraft{
		CollectionID: a.collectionID, Name: "From command line", Auth: &fresh,
	}))
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	saved := findInTree(t, tree, "From command line")
	if saved.Auth != nil {
		t.Fatalf("stored auth = %+v, want nothing written down at all", saved.Auth)
	}

	got, err := inherited(ctx, a.collections, domain.DraftID(saved.ID))
	if err != nil {
		t.Fatalf("AuthFor: %v", err)
	}
	if got == nil || got.Answer("token") != "Collection" {
		t.Errorf("the saved request inherits %+v, want the collection's token", got)
	}
}

// "Inherit" on a folder is a choice, not an absence: the level says the one above decides, and it
// keeps saying it. Stored as nothing, the sheet would open on "None" — the one answer nobody gave —
// and the folder would look like a level that had decided to send no credentials at all.
func TestInheritIsKeptOnAFolder(t *testing.T) {
	ctx := context.Background()
	a := setupAuthTree(t)

	if err := a.collections.Update(ctx, a.collectionID, patchAuth(bearer("Collection"))); err != nil {
		t.Fatalf("SaveAuth collection: %v", err)
	}
	if err := a.collections.Update(ctx, a.nestedID, patchAuth(domain.Auth{Type: domain.AuthInherit})); err != nil {
		t.Fatalf("SaveAuth nested: %v", err)
	}

	tree, err := a.collections.Tree(ctx)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	nested := findIn(t, tree, a.nestedID)
	if nested.Auth == nil || nested.Auth.Type != domain.AuthInherit {
		t.Fatalf("nested auth = %+v, want the 'Inherit' it was given", nested.Auth)
	}

	// And it still inherits: the walk goes past a level that said somebody above it decides.
	if got, _ := inherited(ctx, a.collections, domain.DraftID(a.insideID)); got == nil ||
		got.Answer("token") != "Collection" {
		t.Errorf("inside inherited %+v, want the collection's token", got)
	}
}
