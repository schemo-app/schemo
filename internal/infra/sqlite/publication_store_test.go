package sqlite

import (
	"context"
	"testing"

	"schemo/internal/domain"
)

// page is a row the way a publish leaves one: what the server answered, kept under the space this
// device holds it in.
func page(id, workspace string) domain.Publication {
	return domain.Publication{
		CollectionID: id, WorkspaceID: workspace, ID: "publication-" + id,
		Slug: "example-api-3f9a2c10bd", URL: "https://share.example.com/example-api-3f9a2c10bd",
		Visibility: domain.VisibilityUnlisted, Status: domain.PublicationActive, Revision: 3,
		ContentHash: "a-digest-of-what-was-published", EnvironmentName: "Production",
		CanManage: true, UpdatedAt: 1_700_000_000_000,
	}
}

// A page goes down and comes back whole, the space it is kept under included: a row read without
// one is a row dropped by the next write of it, which is how a takedown loses a page it meant to
// keep.
func TestAPublicationRoundTrip(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SavePublication(ctx, page("col-1", ws)); err != nil {
		t.Fatalf("SavePublication: %v", err)
	}

	held, known, err := store.Publication(ctx, "col-1")
	if err != nil {
		t.Fatalf("Publication: %v", err)
	}
	if !known {
		t.Fatal("a collection that was published is answered as one nobody published")
	}
	if held.CollectionID != "col-1" || held.WorkspaceID != ws {
		t.Errorf("held = %+v, want the collection in the space it was kept under", held)
	}
	if held.ID != "publication-col-1" || held.Slug != "example-api-3f9a2c10bd" {
		t.Errorf("held = %+v, want the server's row and the address it issued", held)
	}
	if held.Visibility != domain.VisibilityUnlisted || held.Status != domain.PublicationActive {
		t.Errorf("held = %+v, want the access and the state it was published with", held)
	}
	if held.Revision != 3 || held.ContentHash != "a-digest-of-what-was-published" {
		t.Errorf("held = %+v, want the revision and the digest the page answers to", held)
	}
	if held.EnvironmentName != "Production" || !held.CanManage || held.UpdatedAt != 1_700_000_000_000 {
		t.Errorf("held = %+v, want the rest of the row", held)
	}
}

// A collection nobody published answers nothing rather than an error: the popover asks about it the
// moment it opens, and a question nobody has answered is not a failure.
func TestACollectionNobodyPublishedIsAnsweredAsNothing(t *testing.T) {
	store := newMigratedStore(t)

	_, known, err := store.Publication(context.Background(), "col-1")
	if err != nil {
		t.Fatalf("Publication: %v", err)
	}
	if known {
		t.Error("a collection nobody published is answered as one that was")
	}
}

// A takedown is the same row with two fields changed, and it stays in the space it belongs to: a
// row that left the space would be a page a window stopped drawing rather than one it drew as gone.
func TestATakedownKeepsTheRowInItsSpace(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	if err := store.SavePublication(ctx, page("col-1", ws)); err != nil {
		t.Fatalf("SavePublication: %v", err)
	}

	held, _, err := store.Publication(ctx, "col-1")
	if err != nil {
		t.Fatalf("Publication: %v", err)
	}
	held.Status = domain.PublicationRevoked
	held.PendingUnpublish = true
	if err := store.SavePublication(ctx, held); err != nil {
		t.Fatalf("SavePublication again: %v", err)
	}

	listed, err := store.OfWorkspace(ctx, ws)
	if err != nil {
		t.Fatalf("OfWorkspace: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed %d pages, want the one that was taken down", len(listed))
	}
	if listed[0].Status != domain.PublicationRevoked || !listed[0].PendingUnpublish {
		t.Errorf("listed = %+v, want a takedown waiting for the server", listed[0])
	}
}

// The list of a space is that space's, newest first: a page of another space is not this one's, and
// a window asks by the id it holds rather than by the one the account knows.
func TestTheListOfASpaceHoldsOnlyItsPagesNewestFirst(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	older := page("col-1", ws)
	older.UpdatedAt = 1_600_000_000_000
	newer := page("col-2", ws)
	elsewhere := page("col-3", "another-space")
	for _, held := range []domain.Publication{older, newer, elsewhere} {
		if err := store.SavePublication(ctx, held); err != nil {
			t.Fatalf("SavePublication: %v", err)
		}
	}

	listed, err := store.OfWorkspace(ctx, ws)
	if err != nil {
		t.Fatalf("OfWorkspace: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed %d pages, want this space's two", len(listed))
	}
	if listed[0].CollectionID != "col-2" || listed[1].CollectionID != "col-1" {
		t.Errorf("listed = %+v, want the newest first", listed)
	}
}

// What a takedown owes the server is the takedowns and nothing else: a page that answers is not one
// this device is waiting to say something about, and a row with no server row has nothing to take.
func TestAwaitingUnpublishHoldsOnlyWhatIsOwed(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	owed := page("col-1", ws)
	owed.Status = domain.PublicationRevoked
	owed.PendingUnpublish = true
	answered := page("col-2", ws)
	unpublished := page("col-3", ws)
	unpublished.ID = ""
	unpublished.Status = domain.PublicationRevoked
	unpublished.PendingUnpublish = true
	for _, held := range []domain.Publication{owed, answered, unpublished} {
		if err := store.SavePublication(ctx, held); err != nil {
			t.Fatalf("SavePublication: %v", err)
		}
	}

	waiting, err := store.AwaitingUnpublish(ctx)
	if err != nil {
		t.Fatalf("AwaitingUnpublish: %v", err)
	}
	if len(waiting) != 1 || waiting[0].CollectionID != "col-1" {
		t.Errorf("waiting = %+v, want the one takedown the server never heard", waiting)
	}
}

func TestPendingUnpublishSurvivesWorkspaceDeletion(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	owed := page("removed-collection", ws)
	owed.Status = domain.PublicationRevoked
	owed.PendingUnpublish = true
	if err := store.SavePublication(ctx, owed); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	waiting, err := store.AwaitingUnpublish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 1 || waiting[0].ID != owed.ID || !waiting[0].PendingUnpublish {
		t.Fatalf("AwaitingUnpublish after deletion = %+v; want pending publication %q", waiting, owed.ID)
	}
}

// A republish replaces what was known rather than adding a second row: a collection has one page,
// and the address it no longer answers at is not one this window can speak of.
func TestARepublishReplacesTheRow(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	if err := store.SavePublication(ctx, page("col-1", ws)); err != nil {
		t.Fatalf("SavePublication: %v", err)
	}

	again := page("col-1", ws)
	again.Slug = "example-api-9d2e5b71ca"
	again.URL = "https://share.example.com/" + again.Slug
	again.Revision = 4
	if err := store.SavePublication(ctx, again); err != nil {
		t.Fatalf("SavePublication again: %v", err)
	}

	listed, err := store.OfWorkspace(ctx, ws)
	if err != nil {
		t.Fatalf("OfWorkspace: %v", err)
	}
	if len(listed) != 1 || listed[0].Revision != 4 {
		t.Fatalf("listed = %+v, want the one row at the new revision", listed)
	}
	if listed[0].URL != again.URL {
		t.Errorf("url = %q, want the address it was republished at", listed[0].URL)
	}
}

// Forgetting a page is not taking it down — the server is — so the row goes and nothing else does.
func TestForgettingAPageDropsTheRow(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	if err := store.SavePublication(ctx, page("col-1", ws)); err != nil {
		t.Fatalf("SavePublication: %v", err)
	}

	if err := store.ForgetPublication(ctx, "col-1"); err != nil {
		t.Fatalf("ForgetPublication: %v", err)
	}

	if _, known, err := store.Publication(ctx, "col-1"); err != nil {
		t.Fatalf("Publication: %v", err)
	} else if known {
		t.Error("the page is still known after being forgotten")
	}
}
