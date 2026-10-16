package publication

import (
	"context"
	"testing"

	"schemo/internal/domain"
)

func TestDescriptionsTrackCollectionsAndNestedFolders(t *testing.T) {
	uc := newTestUseCase(&fakeStore{}, &fakeServer{}, fakeSession{})
	tree := tiers()
	tree.Description = "Original overview."
	tree.Children[0].Description = "Order documentation."
	tree.Children[0].Children[0].Description = "Archive documentation."
	tree.Children[0].Children[0].Items = nil
	uc.collections = fakeCollections{tree: tree}
	put(t, uc, tree.ID)

	tree.Description = "Updated overview."
	tree.Children[0].Description = "Updated order documentation."
	tree.Children[0].Children[0].Description = "Updated archive documentation."
	uc.collections = fakeCollections{tree: tree}
	for _, test := range []struct {
		id    string
		count int
	}{{"collection-1", 3}, {"folder-1", 2}, {"folder-2", 1}} {
		t.Run(test.id, func(t *testing.T) {
			change, err := uc.Changes(context.Background(), "space-1", test.id)
			if err != nil {
				t.Fatal(err)
			}
			if !change.Behind || len(change.Changes) != test.count {
				t.Fatalf("change = %+v, want %d description changes", change, test.count)
			}
			for _, item := range change.Changes {
				if item.Method != "" || item.Sign != signChanged {
					t.Errorf("description change = %+v, want an edited folder", item)
				}
			}
		})
	}

	snapshot, _, err := uc.Build(context.Background(), "space-1", tree.ID, "env-1", "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Collection.Groups) != 3 || snapshot.Collection.Groups[0].Description != "" ||
		snapshot.Collection.Groups[1].Description != tree.Children[0].Description ||
		snapshot.Collection.Groups[2].Description != tree.Children[0].Children[0].Description {
		t.Fatalf("groups = %+v, want descriptions including the folder without requests", snapshot.Collection.Groups)
	}
	packed, _, err := Encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	document, err := AsDocument(packed)
	if err != nil {
		t.Fatal(err)
	}
	if document.Collections[0].Children[1].Description != tree.Children[0].Children[0].Description {
		t.Fatal("import discarded the description of a folder without requests")
	}
}

func TestHiddenFolderDescriptionsDoNotChangeThePublication(t *testing.T) {
	uc := newTestUseCase(&fakeStore{}, &fakeServer{}, fakeSession{})
	tree := tiers()
	tree.Children[0].Children[0].Items = nil
	uc.collections = fakeCollections{tree: tree}
	options := domain.PublicationOptions{Docs: false, Examples: true}
	if _, err := uc.Publish(context.Background(), "space-1", tree.ID, "env-1",
		domain.VisibilityUnlisted, "en", "", true, &options); err != nil {
		t.Fatal(err)
	}
	tree.Children[0].Children[0].Description = "Private draft documentation."
	uc.collections = fakeCollections{tree: tree}
	change, err := uc.Changes(context.Background(), "space-1", tree.ID)
	if err != nil {
		t.Fatal(err)
	}
	if change.Behind || len(change.Changes) != 0 {
		t.Fatalf("hidden description changed the publication: %+v", change)
	}
}

func TestImportKeepsAnEquallyNamedChildFolder(t *testing.T) {
	uc := newTestUseCase(&fakeStore{}, &fakeServer{}, fakeSession{})
	tree := tiers()
	tree.Items = nil
	tree.Children[0].Name = tree.Name
	tree.Children[0].Description = "Child folder documentation."
	uc.collections = fakeCollections{tree: tree}
	snapshot, _, err := uc.Build(context.Background(), "space-1", tree.ID, "env-1", "en")
	if err != nil {
		t.Fatal(err)
	}
	imported := readCollection(snapshot)
	if len(imported.Items) != 0 || len(imported.Children) != 2 ||
		imported.Children[0].Description != tree.Children[0].Description {
		t.Fatalf("imported = %+v, want the equally named child folder preserved", imported)
	}
}
