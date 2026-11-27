package db

import (
	"context"
	"encoding/json"
	"testing"

	"schemo/internal/domain"
)

// What a description said travels with its collection. Where this device reads it is not offered to
// the server, nor left behind by the collection.
func TestASpecSnapshotSyncsAndItsPathStaysOnThisDevice(t *testing.T) {
	ctx := context.Background()
	store := newMigratedStore(t)
	if err := store.SaveCollection(ctx, "personal", nested("col-1", "", 0, "Shop")); err != nil {
		t.Fatal(err)
	}
	queued := func(kind domain.EntityType) int {
		t.Helper()
		var count int
		if err := store.DB().QueryRowContext(ctx,
			`SELECT count(*) FROM sync_queue WHERE entity_type = ?`, kind).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM sync_queue`); err != nil {
		t.Fatal(err)
	}

	source := domain.SpecSource{Kind: domain.SpecFile, Location: "/tmp/a.yaml"}
	if err := store.SaveTree(ctx, "personal", domain.TreeUpdate{
		Link:     domain.SpecLink{CollectionID: "col-1", Source: source},
		Snapshot: json.RawMessage(`{"requests":[]}`),
	}); err != nil {
		t.Fatal(err)
	}
	if collections, links := queued(domain.EntityCollection), queued(kindSpec); collections != 1 || links != 0 {
		t.Errorf("queue holds %d collections and %d links, want the collection alone", collections, links)
	}
	if err := store.Reset(ctx, "personal"); err != nil {
		t.Fatal(err)
	}
	if links := queued(kindSpec); links != 0 {
		t.Errorf("a resync offers %d links, want none", links)
	}
	if held, ok, err := store.SpecLink(ctx, "personal", "col-1"); err != nil || !ok || held.Source.Location != "/tmp/a.yaml" {
		t.Fatalf("SpecLink = %+v, %v, %v", held, ok, err)
	}

	if err := store.DeleteCollection(ctx, "personal", "col-1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.SpecLink(ctx, "personal", "col-1"); err != nil || ok {
		t.Errorf("SpecLink after deleting the collection = %v, %v, want none", ok, err)
	}
}

// No other save of a collection knows of the snapshot, so each of them has to keep it.
func TestSavingACollectionKeepsItsSpecSnapshot(t *testing.T) {
	ctx := context.Background()
	store := newMigratedStore(t)
	root, folder := nested("col-1", "", 0, "Shop"), nested("col-2", "col-1", 0, "Orders")
	for _, collection := range []domain.Collection{root, folder} {
		if err := store.SaveCollection(ctx, "personal", collection); err != nil {
			t.Fatal(err)
		}
	}
	const said = `{"requests":[{"key":"GET /orders"}]}`
	keep := func(id string, update domain.TreeUpdate, snapshot string) {
		t.Helper()
		update.Link, update.Snapshot = domain.SpecLink{CollectionID: id}, json.RawMessage(snapshot)
		if err := store.SaveTree(ctx, "personal", update); err != nil {
			t.Fatal(err)
		}
	}
	kept := func(after, id string) {
		t.Helper()
		if got, err := store.SpecSnapshot(ctx, id); err != nil || string(got) != said {
			t.Errorf("after %s the snapshot of %s = %q, %v", after, id, got, err)
		}
	}
	keep("col-2", domain.TreeUpdate{}, said)
	if _, ok, err := store.SpecLink(ctx, "personal", "col-2"); err != nil || ok {
		t.Errorf("SpecLink with nowhere to read = %v, %v, want none", ok, err)
	}

	folder.Name = "Purchases"
	if err := store.SaveCollection(ctx, "personal", folder); err != nil {
		t.Fatal(err)
	}
	kept("a rename", "col-2")
	folder.Variables = []domain.Variable{{ID: "v1", Name: "region", Value: "eu"}}
	if err := store.SaveScopes(ctx, "personal", ScopeChanges{Collection: &folder}); err != nil {
		t.Fatal(err)
	}
	kept("saving its variables", "col-2")
	// Updating the collection around it patches the folder.
	keep("col-1", domain.TreeUpdate{Patches: []domain.LevelPatch{{ID: "col-2", Name: "Orders"}}}, `{"requests":[]}`)
	kept("an update of its parent", "col-2")
}
