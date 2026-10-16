package publication

import (
	"context"
	"testing"

	"schemo/internal/domain"
)

func TestPublicationOptionsFilterTheDocumentAndPersistAfterRefresh(t *testing.T) {
	store := &fakeStore{}
	server := &fakeServer{}
	uc := newTestUseCase(store, server, fakeSession{})
	tree := published()
	tree.Description = "Overview.\n\n## Errors\nInvalid request."
	tree.Items[0].Description = "List every product."
	uc.collections = fakeCollections{tree: tree}
	options := domain.PublicationOptions{Docs: false, Examples: false}
	if _, err := uc.Publish(context.Background(), "space-1", "collection-1", "env-1",
		domain.VisibilityUnlisted, "en", "", true, &options); err != nil {
		t.Fatal(err)
	}
	preview, err := uc.Preview(context.Background(), "space-1", "collection-1", "env-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Options != options {
		t.Fatalf("options = %+v, want %+v", preview.Options, options)
	}
	snapshot, _, err := uc.Build(context.Background(), "space-1", "collection-1", "env-1", "en")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Collection.Description != "" || snapshot.Prose.Lead != "" {
		t.Fatalf("hidden page content survived: %+v", snapshot)
	}
	for _, group := range snapshot.Collection.Groups {
		for _, endpoint := range group.Endpoints {
			if endpoint.Description != "" || len(endpoint.Responses) != 0 {
				t.Errorf("hidden endpoint content survived: %+v", endpoint)
			}
		}
	}
}
