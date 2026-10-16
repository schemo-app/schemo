package publication

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"schemo/internal/domain"
)

// tiers is a collection of one request with a folder inside it, and a folder inside that: the least
// a page can hold beyond the collection's own requests, and so the least a folder of a page can be
// read against.
func tiers() domain.Collection {
	return domain.Collection{
		ID:   "collection-1",
		Name: "Example API",
		Items: []domain.CollectionNode{
			{ID: "node-1", Name: "List products", Method: "GET", URL: "{{baseUrl}}/v1/products"},
		},
		Children: []domain.Collection{{
			ID:   "folder-1",
			Name: "Orders",
			Items: []domain.CollectionNode{
				{ID: "node-2", Name: "List orders", Method: "GET", URL: "{{baseUrl}}/v1/orders"},
			},
			Children: []domain.Collection{{
				ID:   "folder-2",
				Name: "Archive",
				Items: []domain.CollectionNode{
					{ID: "node-3", Name: "List archived", Method: "GET",
						URL: "{{baseUrl}}/v1/orders/archive"},
				},
			}},
		}},
	}
}

// put sends the collection out, which is the start of every scenario here: a folder is only a
// question at all once something above it is up.
func put(t *testing.T, uc *UseCase, collectionID string) {
	t.Helper()
	if _, err := uc.Publish(context.Background(), "space-1", collectionID, "env-1",
		domain.VisibilityUnlisted, "en", "", true, nil); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

// A page is a whole tree, so a folder inside a published one is published by it: what the window
// asks about the folder is the page above it, said to be above it, and the folder has nothing of
// its own to publish into.
func TestAFolderInsideAPublishedOneAnswersAtThePageThatHoldsIt(t *testing.T) {
	store := &fakeStore{}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tiers()}
	put(t, uc, "collection-1")

	change, err := uc.Changes(context.Background(), "space-1", "folder-2")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}

	if change.Page == nil || change.Page.CollectionID != "collection-1" {
		t.Fatalf("change = %+v, want the row of the page that holds the folder", change)
	}
	if change.InheritedFrom != "Example API" {
		t.Errorf("inheritedFrom = %q, want the folder whose page this is", change.InheritedFrom)
	}
	if change.Behind || len(change.Changes) != 0 {
		t.Errorf("change = %+v, want a page that just went up and nothing moved at it", change)
	}
}

// A folder's list is the requests a reader of that folder sees and not the ones beside it: a page
// holds the whole tree, and a folder that showed the page's whole history would be claiming its
// siblings' work as its own.
func TestAFolderIsToldOfItsOwnRequestsAlone(t *testing.T) {
	store := &fakeStore{}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tiers()}
	ctx := context.Background()
	put(t, uc, "collection-1")

	moved := tiers()
	moved.Items[0].Name = "List every product"
	moved.Children[0].Items[0].Name = "List every order"
	uc.collections = fakeCollections{tree: moved}

	change, err := uc.Changes(ctx, "space-1", "folder-1")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}

	want := domain.PageItem{Sign: signChanged, Method: "GET", Name: "List every order", Fields: []string{"name"}}
	if len(change.Changes) != 1 || !reflect.DeepEqual(change.Changes[0], want) {
		t.Errorf("changes = %+v, want the folder's own request and not the page's", change.Changes)
	}
}

// Whether a page is behind is the page's question, not the folder's: the revision is the page's,
// so a folder above a request that moved is behind, while the list under it — the folder's own —
// stays empty rather than naming a change that is no business of this one.
func TestAFolderIsBehindForWhatMovedBesideItAndNamesNothing(t *testing.T) {
	store := &fakeStore{}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tiers()}
	ctx := context.Background()
	put(t, uc, "collection-1")

	moved := tiers()
	moved.Items[0].Name = "List every product"
	uc.collections = fakeCollections{tree: moved}

	change, err := uc.Changes(ctx, "space-1", "folder-2")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}

	if !change.Behind {
		t.Error("behind = false, want the page said to be behind: the page is what the revision is")
	}
	if len(change.Changes) != 0 {
		t.Errorf("changes = %+v, want nothing of the folder's own to have moved", change.Changes)
	}
}

// A folder does not publish the levels above it: a page is a whole tree read downwards, so a folder
// nobody put up is its own collection still, and the folder that holds it is not one it can be said
// to be inside of.
func TestAParentIsNotPublishedByTheFolderUnderIt(t *testing.T) {
	store := &fakeStore{}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tiers()}
	put(t, uc, "folder-2")

	change, err := uc.Changes(context.Background(), "space-1", "folder-1")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}

	if change.Page != nil || change.InheritedFrom != "" {
		t.Errorf("change = %+v, want a collection past a page rather than inside one", change)
	}
}

// A publish from a folder carries the page that holds it, and carries the whole page: the names a
// reader has are the page's own, so a document built from the folder alone would be a page whose
// every request went missing, and a second page claiming the same collection.
func TestAPublishFromAFolderCarriesThePageThatHoldsIt(t *testing.T) {
	store := &fakeStore{}
	server := &fakeServer{}
	uc := newTestUseCase(store, server, fakeSession{})
	uc.collections = fakeCollections{tree: tiers()}
	ctx := context.Background()
	put(t, uc, "collection-1")
	server.sent = nil

	held, err := uc.Publish(ctx, "space-1", "folder-2", "env-1", domain.VisibilityUnlisted,
		"en", "", true, nil)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(server.sent) != 1 || server.sent[0].Collection != "collection-1" {
		t.Fatalf("sent %+v, want the page that holds the folder", server.sent)
	}
	if held.CollectionID != "collection-1" {
		t.Errorf("published %q, want the page the folder sits in", held.CollectionID)
	}
	if len(store.held) != 1 || store.held[0].CollectionID != "collection-1" {
		t.Errorf("kept %+v, want one row, of the page", store.held)
	}
	carried := expanded(t, server.sent[0].Document)
	for _, node := range []string{"node-1", "node-2", "node-3"} {
		if !strings.Contains(carried, PageID("collection-1", node)) {
			t.Errorf("the document leaves out %q, want the whole tree carried", node)
		}
	}
}

// The popover a folder opens is the page's own with the folder's changes under it: what an author
// reads there is the page they would be updating, and the folder it is of is named rather than left
// to be mistaken for a page of the folder. The collection the page is of says no such thing.
func TestAFoldersPopoverIsThePagesWithTheFoldersChanges(t *testing.T) {
	store := &fakeStore{}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tiers()}
	ctx := context.Background()
	put(t, uc, "collection-1")

	moved := tiers()
	moved.Children[0].Items[0].Name = "List every order"
	uc.collections = fakeCollections{tree: moved}

	folder, err := uc.Preview(ctx, "space-1", "folder-1", "env-1", nil)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if folder.Collection != "collection-1" || folder.InheritedFrom != "Example API" {
		t.Errorf("preview = %+v, want the page that holds the folder, named", folder)
	}
	if folder.Publication == nil || !folder.Behind {
		t.Errorf("preview = %+v, want the page that went up and is behind", folder)
	}
	want := domain.PageItem{Sign: signChanged, Method: "GET", Name: "List every order", Fields: []string{"name"}}
	if len(folder.Changes) != 1 || !reflect.DeepEqual(folder.Changes[0], want) {
		t.Errorf("changes = %+v, want the folder's own request", folder.Changes)
	}

	page, err := uc.Preview(ctx, "space-1", "collection-1", "env-1", nil)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if page.InheritedFrom != "" {
		t.Errorf("inheritedFrom = %q, want a page that is of no folder above it", page.InheritedFrom)
	}
}
