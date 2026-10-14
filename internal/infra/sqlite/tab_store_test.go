package sqlite

import (
	"context"
	"testing"

	"schemo/internal/domain"
)

// TestTabStripRoundTrip is the whole strip going down and coming back: the order is the rows', the
// fields are the tab's, and the active one is the flag rather than a second list.
func TestTabStripRoundTrip(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	tabs := []domain.Tab{
		{ID: "t1", Kind: domain.TabKindRequest, DraftID: domain.DraftCommandLine, Pinned: true},
		{ID: "t2", Kind: domain.TabKindCollections, Ref: "col-7"},
		{ID: "t3", Kind: domain.TabKindRequest, Ref: "node-3", DraftID: "node-3",
			Record: "rec-9", Preview: true},
	}
	if err := store.SaveTabs(ctx, ws, tabs, "t2"); err != nil {
		t.Fatalf("SaveTabs: %v", err)
	}

	got, active, err := store.Tabs(ctx, ws)
	if err != nil {
		t.Fatalf("Tabs: %v", err)
	}
	if active != "t2" {
		t.Errorf("active = %q, want t2", active)
	}
	if len(got) != 3 || got[0].ID != "t1" || got[1].ID != "t2" || got[2].ID != "t3" {
		t.Fatalf("tabs = %+v, want them back in order", got)
	}
	if !got[0].Pinned || got[0].DraftID != domain.DraftCommandLine {
		t.Errorf("the first tab = %+v, want it pinned on the command line's draft", got[0])
	}
	if !got[2].Preview || got[2].Record != "rec-9" || got[2].Ref != "node-3" {
		t.Errorf("the last tab = %+v, want a node tab previewing rec-9", got[2])
	}

	// The whole strip is one write: saving it again leaves exactly the new list.
	if err := store.SaveTabs(ctx, ws, tabs[:1], "t1"); err != nil {
		t.Fatalf("SaveTabs again: %v", err)
	}
	if got, active, err = store.Tabs(ctx, ws); err != nil {
		t.Fatalf("Tabs: %v", err)
	}
	if len(got) != 1 || active != "t1" {
		t.Errorf("after the second save tabs = %+v active = %q, want one tab", got, active)
	}

	if err := store.ClearTabs(ctx, ws); err != nil {
		t.Fatalf("ClearTabs: %v", err)
	}
	if got, active, err = store.Tabs(ctx, ws); err != nil {
		t.Fatalf("Tabs: %v", err)
	}
	if len(got) != 0 || active != "" {
		t.Errorf("tabs = %+v active = %q after ClearTabs, want nothing", got, active)
	}
}

func TestInvalidTabStripRollsBack(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	original := []domain.Tab{{ID: "original", Kind: domain.TabKindRequest}}
	if err := store.SaveTabs(ctx, ws, original, "original"); err != nil {
		t.Fatal(err)
	}
	invalid := []domain.Tab{
		{ID: "first", Kind: domain.TabKindRequest, Preview: true},
		{ID: "second", Kind: domain.TabKindRequest, Preview: true},
	}
	if err := store.SaveTabs(ctx, ws, invalid, "first"); err == nil {
		t.Fatal("SaveTabs with two previews succeeded")
	}
	tabs, active, err := store.Tabs(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 || tabs[0].ID != "original" || active != "original" {
		t.Fatalf("Tabs after rejected save = %+v, active %q; want original strip", tabs, active)
	}
	for _, column := range []string{"pinned", "preview", "active"} {
		query := `INSERT INTO tabs (workspace_id, id, kind, ` + column + `) VALUES (?, 'invalid', 'request', 2)`
		if _, err := store.DB().ExecContext(ctx, query, ws); err == nil {
			t.Errorf("invalid %s flag was accepted", column)
		}
	}
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO tabs (workspace_id, id, kind, active) VALUES (?, 'second', 'request', 1)`, ws); err == nil {
		t.Error("two active tabs in one workspace were accepted")
	}
}
