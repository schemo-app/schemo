package workspace

import (
	"context"
	"errors"
	"testing"

	"schemo/internal/domain"
)

// The set travels whole and comes back as it left: every field of every example, in order — the
// pane draws its list from exactly this answer.
func TestSaveExamplesRoundTripsTheSet(t *testing.T) {
	ctx := context.Background()
	collections, store := newCollections(t)

	_, tree, err := collections.collectionAndTree(collections.CreateCollection(ctx, "Catalog", "", ""))
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID
	_, tree, err = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: collectionID, Name: "List products"}))
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	nodeID := findInTree(t, tree, "List products").ID

	saved, err := collections.SaveExamples(ctx, nodeID, []domain.ResponseExample{
		{Name: "The list", Status: 200, Body: `{"data":[]}`,
			Headers: []domain.HeaderPair{{Name: "Content-Type", Value: "application/json"}}},
		{Name: "", Status: 404, Body: "not found"},
	})
	if err != nil {
		t.Fatalf("SaveExamples: %v", err)
	}
	if len(saved.Responses) != 2 {
		t.Fatalf("responses = %+v, want both examples back", saved.Responses)
	}
	if saved.Responses[0].Name != "The list" || saved.Responses[0].Status != 200 ||
		saved.Responses[0].Body != `{"data":[]}` {
		t.Errorf("first = %+v, want the example as it was saved", saved.Responses[0])
	}
	if len(saved.Responses[0].Headers) != 1 ||
		saved.Responses[0].Headers[0].Value != "application/json" {
		t.Errorf("headers = %+v, want the header kept", saved.Responses[0].Headers)
	}
	// The nameless example is one of the set: the pane's "+" makes exactly that row.
	if saved.Responses[1].Name != "" || saved.Responses[1].Status != 404 {
		t.Errorf("second = %+v, want the nameless example in its own place", saved.Responses[1])
	}

	stored, err := store.Node(ctx, nodeID)
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if len(stored.Responses) != 2 || stored.Responses[0].Body != `{"data":[]}` {
		t.Errorf("stored = %+v, want the set written down", stored.Responses)
	}
}

// An empty set is a real answer — the last example deleted — and it clears what was there. A
// collection has no examples at all, which is the same refusal any unknown id gets.
func TestSaveExamplesEmptiesAndKnowsNoCollections(t *testing.T) {
	ctx := context.Background()
	collections, _ := newCollections(t)

	_, tree, err := collections.collectionAndTree(collections.CreateCollection(ctx, "Catalog", "", ""))
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	collectionID := only(t, tree).ID

	if _, err := collections.SaveExamples(ctx, collectionID,
		[]domain.ResponseExample{{Name: "The list"}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("SaveExamples(collection) = %v, want not-found", err)
	}

	_, tree, err = collections.nodeAndTree(collections.CreateNode(ctx, RequestDraft{CollectionID: collectionID, Name: "List products"}))
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	nodeID := findInTree(t, tree, "List products").ID

	if _, err := collections.SaveExamples(ctx, nodeID,
		[]domain.ResponseExample{{Name: "The list"}}); err != nil {
		t.Fatalf("SaveExamples: %v", err)
	}
	saved, err := collections.SaveExamples(ctx, nodeID, nil)
	if err != nil {
		t.Fatalf("SaveExamples(nil): %v", err)
	}
	if len(saved.Responses) != 0 {
		t.Errorf("responses = %+v, want the set cleared", saved.Responses)
	}
}
