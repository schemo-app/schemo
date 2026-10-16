package publication

import (
	"reflect"
	"testing"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
	"schemo/internal/domain"
)

func TestChangeDetailsIdentifyDescriptionsParametersAndResponseStatuses(t *testing.T) {
	before := snapshotv1.Snapshot{Collection: snapshotv1.Collection{ID: "collection", Name: "Catalog",
		Groups: []snapshotv1.Group{{ID: "collection", Endpoints: []snapshotv1.Endpoint{{ID: "request", Method: "GET", Name: "List products",
			Parameters: []snapshotv1.Parameter{{In: "query", Name: "per_page", Example: "20"}},
			Responses:  []snapshotv1.Response{{Status: 200, Name: "OK", Body: "[]"}},
		}}}}}}
	old, err := outlineOf(before)
	if err != nil {
		t.Fatal(err)
	}
	after := before
	endpoint := &after.Collection.Groups[0].Endpoints[0]
	endpoint.Description = "Pagination details."
	endpoint.Parameters[0].Example = "100"
	endpoint.Responses = append(endpoint.Responses, snapshotv1.Response{Status: 422, Name: "Invalid input", Body: "{}"})
	next, err := outlineOf(after)
	if err != nil {
		t.Fatal(err)
	}
	changes, cut := moved(old, next)
	want := []string{"description", "parameter:query:per_page", "response:422:Invalid input"}
	if cut || len(changes) != 1 || !reflect.DeepEqual(changes[0].Fields, want) {
		t.Fatalf("changes = %+v, cut = %t", changes, cut)
	}
}

func TestRestorationKeepsTheEditorTreeAndUpdatesOnlyThePublishedBaseline(t *testing.T) {
	store, server := &fakeStore{}, &fakeServer{}
	u := newTestUseCase(store, server, fakeSession{})
	u.collections = fakeCollections{tree: catalogue()}
	put(t, u, "collection-1")
	snapshot, _, err := u.Build(t.Context(), "space-1", "collection-1", "env-1", "en")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Collection.Groups[0].Endpoints[0].Description = "Earlier published description."
	packed, hash, err := Encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	server.answered = domain.Publication{ID: "publication-1", CollectionID: "collection-1", ContentHash: hash, Revision: 3, Status: domain.PublicationActive}
	server.restoredDocument = packed
	if _, err := u.Restore(t.Context(), "space-1", "collection-1", 1); err != nil {
		t.Fatal(err)
	}
	change, err := u.Changes(t.Context(), "space-1", "collection-1")
	if err != nil || !change.Behind || change.Page.Revision != 3 {
		t.Fatalf("restored baseline = %+v, %v", change, err)
	}
	server.answered.ContentHash = "wrong-hash"
	if _, err := u.Restore(t.Context(), "space-1", "collection-1", 1); err == nil {
		t.Fatal("accepted a mismatched restored document")
	}
}
