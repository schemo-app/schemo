package tabstrip

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"schemo/internal/domain"
)

// fakeStore keeps the strip in memory. saves counts the writes, so a test can say that a read
// stored nothing.
type fakeStore struct {
	tabs    []domain.Tab
	active  string
	saves   int
	saveErr error
}

func (f *fakeStore) Tabs(context.Context, string) ([]domain.Tab, string, error) {
	return f.tabs, f.active, nil
}

func (f *fakeStore) SaveTabs(_ context.Context, _ string, tabs []domain.Tab, active string) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.tabs, f.active = append([]domain.Tab{}, tabs...), active
	f.saves++
	return nil
}

func TestFailedChangesLeaveStripUnchanged(t *testing.T) {
	for _, operation := range []string{"pin", "keep", "activate", "close", "new", "move", "record"} {
		t.Run(operation, func(t *testing.T) {
			uc, store := newUseCase(true)
			first, err := uc.Open(t.Context(), OpenInput{
				Kind: domain.TabKindRequest, Record: "first", NewTab: operation != "keep",
			})
			if err != nil {
				t.Fatal(err)
			}
			before := openRequest(t, uc, "second")
			id := first.Active
			store.saveErr = errors.New("database write failed")
			switch operation {
			case "pin":
				_, err = uc.Pin(t.Context(), id, true)
			case "keep":
				_, err = uc.Keep(t.Context(), id)
			case "activate":
				_, err = uc.Activate(t.Context(), id)
			case "close":
				_, err = uc.Close(t.Context(), id)
			case "new":
				_, err = uc.New(t.Context())
			case "move":
				_, err = uc.Move(t.Context(), id, before.Active)
			case "record":
				_, err = uc.SetRecord(t.Context(), id, "replacement")
			}
			if !errors.Is(err, store.saveErr) {
				t.Fatalf("error = %v, want database failure", err)
			}
			after, err := uc.Strip(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if after.Active != before.Active || !slices.Equal(after.Tabs, before.Tabs) {
				t.Fatalf("failed %s changed strip: before %+v, after %+v", operation, before, after)
			}
		})
	}
}

func TestStripSnapshotsDoNotShareMutableTabs(t *testing.T) {
	uc, _ := newUseCase(true)
	before := openRequest(t, uc, "record")
	if _, err := uc.Pin(t.Context(), before.Active, true); err != nil {
		t.Fatal(err)
	}
	if before.Tabs[len(before.Tabs)-1].Pinned {
		t.Fatal("pinning changed an earlier snapshot")
	}
	before.Tabs[0].Ref = "outside mutation"
	first, err := uc.Strip(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first.Tabs[0].Record = "outside mutation"
	second, err := uc.Strip(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tab := range second.Tabs {
		if tab.Ref == "outside mutation" || tab.Record == "outside mutation" {
			t.Fatal("caller changed the stored strip through a snapshot")
		}
	}
}

func TestOpeningAfterPinnedTabKeepsPinnedGroupTogether(t *testing.T) {
	for _, kind := range []domain.TabKind{domain.TabKindRequest, domain.TabKindCollections, domain.TabKindHistory} {
		t.Run(string(kind), func(t *testing.T) {
			uc, _ := newUseCase(true)
			first := openRequest(t, uc, "first").Active
			second := openRequest(t, uc, "second").Active
			for _, id := range []string{first, second} {
				if _, err := uc.Pin(t.Context(), id, true); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := uc.Activate(t.Context(), first); err != nil {
				t.Fatal(err)
			}
			opened, err := uc.Open(t.Context(), OpenInput{Kind: kind, Ref: "third", NewTab: true})
			if err != nil {
				t.Fatal(err)
			}
			if opened.Tabs[0].ID != first || opened.Tabs[1].ID != second {
				t.Fatalf("opening split the pinned group: %+v", opened.Tabs)
			}
		})
	}
}

func (f *fakeStore) ClearTabs(context.Context, string) error {
	f.tabs, f.active, f.saves = nil, "", f.saves+1
	return nil
}

type fakeScope struct{}

func (fakeScope) ActiveWorkspace(context.Context) (string, error) {
	return domain.WorkspacePersonalID, nil
}

type fakePolicy bool

func (p fakePolicy) RestoreTabs(context.Context) (bool, error) { return bool(p), nil }

func newUseCase(restore bool) (*UseCase, *fakeStore) {
	store := &fakeStore{}
	seq := 0
	ids := func() string {
		seq++
		return fmt.Sprintf("x%d", seq)
	}
	return NewUseCase(store, fakePolicy(restore), fakeScope{}, ids), store
}

func ids(tabs []domain.Tab) []string {
	out := make([]string, 0, len(tabs))
	for _, t := range tabs {
		out = append(out, t.ID)
	}
	return out
}

// openRequest opens a permanent request tab seeded from a record — the shape most of these tests
// need, and the one the ⌘-click produces.
func openRequest(t *testing.T, uc *UseCase, ref string) Strip {
	t.Helper()
	stripped, err := uc.Open(context.Background(),
		OpenInput{Kind: domain.TabKindRequest, Record: ref, NewTab: true})
	if err != nil {
		t.Fatalf("Open(%s): %v", ref, err)
	}
	return stripped
}

// TestNewMintsDraftsAndKeepsTheCommandLine is the blank rule: the strip's first blank tab edits the
// command line's draft — an upgrade then finds the request where it was left — and every blank
// after it mints one of its own.
func TestNewMintsDraftsAndKeepsTheCommandLine(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	fresh, err := uc.Strip(ctx)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if len(fresh.Tabs) != 1 || fresh.Tabs[0].DraftID != domain.DraftCommandLine {
		t.Fatalf("a fresh strip = %+v, want one blank tab on the command line", fresh.Tabs)
	}

	grown, err := uc.New(ctx)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(grown.Tabs) != 2 || grown.Active != grown.Tabs[1].ID {
		t.Fatalf("after New = %+v active %q, want the blank appended and shown", grown.Tabs, grown.Active)
	}
	if draft := grown.Tabs[1].DraftID; string(draft) == string(domain.DraftCommandLine) ||
		len(string(draft)) <= len(domain.TabDraftPrefix) {
		t.Errorf("the second blank's draft = %q, want a minted one", draft)
	}
}

// TestOpenPlainLandsInThePreviewSlot is the click model: a plain open takes the preview slot — the
// preview that was there steps aside with its draft — and a ⌘-open keeps a tab of its own.
func TestOpenPlainLandsInThePreviewSlot(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	first, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindRequest, Record: "rec-1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(first.Tabs) != 2 || !first.Tabs[1].Preview || first.Active != first.Tabs[1].ID {
		t.Fatalf("a plain open = %+v active %q, want a preview shown", first.Tabs, first.Active)
	}
	previewDraft := first.Tabs[1].DraftID

	second, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindRequest, Record: "rec-2"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(second.Tabs) != 2 || second.Tabs[1].Record != "rec-2" || !second.Tabs[1].Preview {
		t.Fatalf("the second plain open = %+v, want the preview slot replaced in place", second.Tabs)
	}
	if !slices.Contains(second.Discarded, previewDraft) {
		t.Errorf("Discarded = %v, want the replaced preview's draft %q", second.Discarded, previewDraft)
	}

	kept := openRequest(t, uc, "rec-3")
	if len(kept.Tabs) != 3 || kept.Tabs[2].Preview {
		t.Fatalf("a ⌘-open = %+v, want a permanent tab beside the preview", kept.Tabs)
	}
}

// TestOpenAlreadyOpenActivatesAndPromotes — the same thing never opens twice: it is shown, and a
// ⌘-open keeps a preview for good.
func TestOpenAlreadyOpenActivatesAndPromotes(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	opened, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindCollections, Ref: "col-1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	preview := opened.Tabs[1].ID
	if _, err := uc.New(ctx); err != nil {
		t.Fatalf("New: %v", err)
	}

	again, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindCollections, Ref: "col-1", NewTab: true})
	if err != nil {
		t.Fatalf("Open again: %v", err)
	}
	if len(again.Tabs) != 3 || again.Active != preview {
		t.Fatalf("opening an open thing = %+v active %q, want it shown and nothing added",
			again.Tabs, again.Active)
	}
	if again.Tabs[1].Preview {
		t.Error("a ⌘-open left the tab a preview, want it kept for good")
	}
}

// TestCloseFallsToTheNeighbour — a close shows the tab that took the dead one's place, and closing
// the last one leaves the strip empty with the dead tab's draft discarded.
func TestCloseFallsToTheNeighbour(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	openRequest(t, uc, "rec-1")
	openRequest(t, uc, "rec-2")
	all, err := uc.Strip(ctx)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	first, second, third := all.Tabs[0], all.Tabs[1], all.Tabs[2]

	closed, err := uc.Close(ctx, second.ID)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed.Active != third.ID || !slices.Equal(ids(closed.Tabs), []string{first.ID, third.ID}) {
		t.Fatalf("closing the middle = %+v active %q, want the neighbour shown",
			closed.Tabs, closed.Active)
	}
	if !slices.Contains(closed.Discarded, second.DraftID) {
		t.Errorf("Discarded = %v, want the closed tab's draft %q", closed.Discarded, second.DraftID)
	}

	for _, id := range []string{first.ID, third.ID} {
		if _, err := uc.Close(ctx, id); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	emptied, err := uc.Strip(ctx)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if len(emptied.Tabs) != 0 || emptied.Active != "" {
		t.Errorf("closing everything = %+v active %q, want the strip empty", emptied.Tabs, emptied.Active)
	}
}

// TestCloseOthersAndRightKeepPinned — the two bulk closes never take a pinned tab, and the drafts
// of what they take go with it.
func TestCloseOthersAndRightKeepPinned(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	openRequest(t, uc, "rec-1")
	openRequest(t, uc, "rec-2")
	openRequest(t, uc, "rec-3")
	all, _ := uc.Strip(ctx)
	blank, first, second, third := all.Tabs[0], all.Tabs[1], all.Tabs[2], all.Tabs[3]

	pinned, err := uc.Pin(ctx, first.ID, true)
	if err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if pinned.Tabs[0].ID != first.ID {
		t.Fatalf("pinning = %+v, want the pinned tab first", ids(pinned.Tabs))
	}

	others, err := uc.CloseOthers(ctx, third.ID)
	if err != nil {
		t.Fatalf("CloseOthers: %v", err)
	}
	if !slices.Equal(ids(others.Tabs), []string{first.ID, third.ID}) {
		t.Errorf("CloseOthers = %v, want the pinned one and the named one", ids(others.Tabs))
	}
	if !slices.Contains(others.Discarded, second.DraftID) ||
		slices.Contains(others.Discarded, blank.DraftID) == false {
		t.Errorf("Discarded = %v, want the closed tabs' drafts", others.Discarded)
	}

	if _, err := uc.New(ctx); err != nil {
		t.Fatalf("New: %v", err)
	}
	right, err := uc.CloseRight(ctx, first.ID)
	if err != nil {
		t.Fatalf("CloseRight: %v", err)
	}
	if !slices.Equal(ids(right.Tabs), []string{first.ID}) {
		t.Errorf("CloseRight = %v, want only the pinned first one", ids(right.Tabs))
	}
}

// TestMoveTakesTheTargetsSideAndPin — a drag lands after the target when it came from before it,
// takes the target's pin, and is never a preview.
func TestMoveTakesTheTargetsSideAndPin(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	openRequest(t, uc, "rec-1")
	openRequest(t, uc, "rec-2")
	all, _ := uc.Strip(ctx)
	blank, first, second := all.Tabs[0], all.Tabs[1], all.Tabs[2]

	if _, err := uc.Pin(ctx, second.ID, true); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	moved, err := uc.Move(ctx, blank.ID, first.ID)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if !slices.Equal(ids(moved.Tabs), []string{second.ID, first.ID, blank.ID}) {
		t.Fatalf("Move = %v, want the blank dragged after the first tab", ids(moved.Tabs))
	}

	dragged, err := uc.Move(ctx, blank.ID, second.ID)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if !dragged.Tabs[0].Pinned || dragged.Tabs[0].ID != blank.ID {
		t.Errorf("dragging onto the pinned = %v, want the dragged tab pinned first", ids(dragged.Tabs))
	}
}

// TestKeepAndDuplicate — keeping takes a preview out of its slot; a duplicate lands right after its
// source on the draft the caller copied.
func TestKeepAndDuplicate(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	opened, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindRequest, Ref: "rec-1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	preview := opened.Tabs[1].ID

	kept, err := uc.Keep(ctx, preview)
	if err != nil {
		t.Fatalf("Keep: %v", err)
	}
	if kept.Tabs[1].Preview {
		t.Error("Keep left the tab a preview")
	}

	copied, err := uc.Duplicate(ctx, preview, "tab:copy")
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	if len(copied.Tabs) != 3 || copied.Tabs[2].DraftID != "tab:copy" ||
		copied.Active != copied.Tabs[2].ID || copied.Tabs[2].Preview {
		t.Errorf("Duplicate = %+v active %q, want the copy after the source and shown",
			copied.Tabs, copied.Active)
	}
}

// TestRestoreReadsWhatWasStored — with the switch on, a stored strip comes back as it was and a
// read stores nothing; with it off, the rows and their drafts are cleared and one blank remains.
func TestRestoreReadsWhatWasStored(t *testing.T) {
	uc, store := newUseCase(true)
	ctx := context.Background()

	store.tabs = []domain.Tab{
		{ID: "s1", Kind: domain.TabKindRequest, DraftID: "tab:1", Pinned: true},
		{ID: "s2", Kind: domain.TabKindCollections, Ref: "col-1"},
		{ID: "s3", Kind: domain.TabKindHistory},
	}
	store.active = "s2"
	restored, err := uc.Strip(ctx)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if !slices.Equal(ids(restored.Tabs), []string{"s1", "s2", "s3"}) || restored.Active != "s2" {
		t.Errorf("restored = %+v active %q, want the stored one", restored.Tabs, restored.Active)
	}
	if store.saves != 0 {
		t.Errorf("saves = %d, want a read to store nothing", store.saves)
	}
}

func TestRestoreOffClearsAndDiscards(t *testing.T) {
	uc, store := newUseCase(false)
	ctx := context.Background()

	store.tabs = []domain.Tab{
		{ID: "s1", Kind: domain.TabKindRequest, DraftID: "tab:1"},
		{ID: "s2", Kind: domain.TabKindRequest, Ref: "node-1", DraftID: "node-1"},
		{ID: "s3", Kind: domain.TabKindRequest, DraftID: domain.DraftCommandLine},
	}
	store.active = "s2"
	fresh, err := uc.Strip(ctx)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if len(fresh.Tabs) != 1 || fresh.Tabs[0].DraftID != domain.DraftCommandLine {
		t.Errorf("with restore off = %+v, want one blank tab", fresh.Tabs)
	}
	if !slices.Contains(fresh.Discarded, domain.DraftID("tab:1")) ||
		slices.Contains(fresh.Discarded, domain.DraftID("node-1")) {
		t.Errorf("Discarded = %v, want the request tab's draft and not the node's", fresh.Discarded)
	}
	if slices.Contains(fresh.Discarded, domain.DraftCommandLine) {
		t.Errorf("Discarded = %v, want the command line to survive", fresh.Discarded)
	}
	if len(store.tabs) != 0 {
		t.Errorf("store kept %+v, want it cleared", store.tabs)
	}
}

// TestAdoptKeepsTheKindAndBindsTheNode — saving a draft points the tab at the node it was saved as:
// it is still the request it was, and the draft it was composing goes.
func TestAdoptKeepsTheKindAndBindsTheNode(t *testing.T) {
	uc, store := newUseCase(true)
	ctx := context.Background()

	opened := openRequest(t, uc, "rec-1")
	saved := opened.Tabs[1]
	stripped, err := uc.Adopt(ctx, saved.ID, "node-1")
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if !slices.Contains(stripped.Discarded, saved.DraftID) {
		t.Errorf("Discarded = %v, want the request tab's draft %q", stripped.Discarded, saved.DraftID)
	}
	bound := stripped.Tabs[1]
	if len(stripped.Tabs) != len(opened.Tabs) ||
		bound.Kind != domain.TabKindRequest ||
		bound.Ref != "node-1" || bound.DraftID != "node-1" || bound.Preview ||
		len(store.tabs) != 2 || store.tabs[1].Ref != "node-1" {
		t.Errorf("after Adopt = %+v store %+v, want the tab bound to the node", stripped.Tabs, store.tabs)
	}
}

// TestAdoptRefusesAPage — a page is not saved as a node, and the refusal leaves it as it was.
func TestAdoptRefusesAPage(t *testing.T) {
	uc, store := newUseCase(true)
	ctx := context.Background()

	opened, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindHistory})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	page := opened.Tabs[1].ID
	if _, err := uc.Adopt(ctx, page, "node-1"); err == nil {
		t.Fatal("Adopt of a page: want a refusal, got none")
	}
	after, err := uc.Strip(ctx)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if after.Tabs[1].Ref != "" || after.Tabs[1].Kind != domain.TabKindHistory {
		t.Errorf("after a refused Adopt = %+v, want the page untouched", after.Tabs)
	}
	if len(store.tabs) != 2 || store.tabs[1].Kind != domain.TabKindHistory {
		t.Errorf("store = %+v, want the page still drawn", store.tabs)
	}
}

// TestOpenHistoryIsSingleton — the history is one: opening it twice shows the tab already open,
// and a ⌘-open adds none.
func TestOpenHistoryIsSingleton(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	opened, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindHistory})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(opened.Tabs) != 2 || opened.Tabs[1].Kind != domain.TabKindHistory ||
		opened.Tabs[1].Preview {
		t.Fatalf("opening the history = %+v, want a permanent page tab", opened.Tabs)
	}
	page := opened.Tabs[1].ID
	if _, err := uc.New(ctx); err != nil {
		t.Fatalf("New: %v", err)
	}

	again, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindHistory, NewTab: true})
	if err != nil {
		t.Fatalf("Open again: %v", err)
	}
	if len(again.Tabs) != 3 || again.Active != page {
		t.Errorf("opening the history twice = %+v active %q, want it shown and nothing added",
			again.Tabs, again.Active)
	}
}

// TestOpenCaptureIsItsOwnPage — the history and the capture live side by side, one of each.
func TestOpenCaptureIsItsOwnPage(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	if _, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindCapture}); err != nil {
		t.Fatalf("Open capture: %v", err)
	}
	both, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindHistory})
	if err != nil {
		t.Fatalf("Open history: %v", err)
	}
	kinds := []domain.TabKind{both.Tabs[1].Kind, both.Tabs[2].Kind}
	if len(both.Tabs) != 3 || !slices.Contains(kinds, domain.TabKindCapture) ||
		!slices.Contains(kinds, domain.TabKindHistory) {
		t.Errorf("both pages = %+v, want the capture and the history", both.Tabs)
	}
}

// TestOpenPageNeverTakesThePreviewSlot — the preview slot is for the thing being looked at: a page
// arrives beside it and the preview stays where it was.
func TestOpenPageNeverTakesThePreviewSlot(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	previewed, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindRequest, Record: "rec-1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	preview := previewed.Tabs[1].ID

	opened, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindHistory})
	if err != nil {
		t.Fatalf("Open history: %v", err)
	}
	if len(opened.Tabs) != 3 || !opened.Tabs[1].Preview || opened.Tabs[1].ID != preview {
		t.Errorf("a page over a preview = %+v, want the preview kept in place", opened.Tabs)
	}
	if opened.Tabs[2].Kind != domain.TabKindHistory {
		t.Errorf("a page over a preview = %+v, want the page after the active tab", opened.Tabs)
	}
}

// TestOpenRecordTwiceIsOneTab — the same record twice is the same tab, and the seed prepared for
// the open that found it open is discarded rather than orphaned.
func TestOpenRecordTwiceIsOneTab(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	first := openRequest(t, uc, "rec-1")
	tab := first.Tabs[1].ID

	again, err := uc.Open(ctx, OpenInput{
		Kind: domain.TabKindRequest, Record: "rec-1", DraftID: "tab:seed",
	})
	if err != nil {
		t.Fatalf("Open again: %v", err)
	}
	if len(again.Tabs) != 2 || again.Active != tab {
		t.Fatalf("opening a record twice = %+v active %q, want one tab shown", again.Tabs, again.Active)
	}
	if !slices.Contains(again.Discarded, domain.DraftID("tab:seed")) {
		t.Errorf("Discarded = %v, want the orphaned seed", again.Discarded)
	}
}

// TestOpenNodeTwiceIsOneTab — a node is the same rule: it is looked up by what the tab edits.
func TestOpenNodeTwiceIsOneTab(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	first, err := uc.Open(ctx, OpenInput{
		Kind: domain.TabKindRequest, Ref: "node-1", DraftID: "node-1",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	tab := first.Tabs[1].ID

	again, err := uc.Open(ctx, OpenInput{
		Kind: domain.TabKindRequest, Ref: "node-1", DraftID: "node-1",
	})
	if err != nil {
		t.Fatalf("Open again: %v", err)
	}
	if len(again.Tabs) != 2 || again.Active != tab || len(again.Discarded) != 0 {
		t.Errorf("opening a node twice = %+v active %q discarded %v, want one tab and no discard",
			again.Tabs, again.Active, again.Discarded)
	}

	other, err := uc.Open(ctx, OpenInput{
		Kind: domain.TabKindRequest, Ref: "node-2", DraftID: "node-2", NewTab: true,
	})
	if err != nil {
		t.Fatalf("Open node-2: %v", err)
	}
	if len(other.Tabs) != 3 {
		t.Errorf("another node = %+v, want a tab beside the first", other.Tabs)
	}
}

// TestDuplicateDropsTheNodeAndKeepsTheRecord — a copy of a node-bound tab is a plain request: two
// tabs must not edit one saved request.
func TestDuplicateDropsTheNodeAndKeepsTheRecord(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	opened, err := uc.Open(ctx, OpenInput{
		Kind: domain.TabKindRequest, Ref: "node-1", DraftID: "node-1", Record: "rec-1",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	source := opened.Tabs[1].ID

	copied, err := uc.Duplicate(ctx, source, "tab:copy")
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	copyTab := copied.Tabs[2]
	if copyTab.Ref != "" || copyTab.Record != "rec-1" || copyTab.DraftID != "tab:copy" {
		t.Errorf("the copy = %+v, want a plain request with the record kept", copyTab)
	}
}

// TestDuplicateRefusesAPage — a page is one, so it cannot be doubled.
func TestDuplicateRefusesAPage(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	opened, err := uc.Open(ctx, OpenInput{Kind: domain.TabKindHistory})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := uc.Duplicate(ctx, opened.Tabs[1].ID, ""); err == nil {
		t.Error("Duplicate of a page: want a refusal, got none")
	}
}

// TestRestoreDropsKindsItCannotDraw — a row written under a model this build does not know leaves
// the strip with its draft, and the store is rewritten without it.
func TestRestoreDropsKindsItCannotDraw(t *testing.T) {
	uc, store := newUseCase(true)
	ctx := context.Background()

	store.tabs = []domain.Tab{
		{ID: "s1", Kind: domain.TabKindRequest, DraftID: "tab:1"},
		{ID: "s2", Kind: domain.TabKind("browser"), Ref: "42", DraftID: "tab:2"},
		{ID: "s3", Kind: domain.TabKindHistory},
	}
	store.active = "s2"
	restored, err := uc.Strip(ctx)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	if !slices.Equal(ids(restored.Tabs), []string{"s1", "s3"}) {
		t.Errorf("restored = %v, want the drawable rows in order", ids(restored.Tabs))
	}
	if restored.Active != "s1" {
		t.Errorf("active = %q, want the first row when the shown one went", restored.Active)
	}
	if !slices.Contains(restored.Discarded, domain.DraftID("tab:2")) {
		t.Errorf("Discarded = %v, want the dropped row's draft", restored.Discarded)
	}
	if len(store.tabs) != 2 || store.tabs[1].ID != "s3" {
		t.Errorf("store = %+v, want the dropped row cleared", store.tabs)
	}
}

// TestCloseOfANodeTabLeavesTheNodesDraft — a node-bound request tab closing takes nothing with it:
// the node's draft is not written down and not the strip's to delete.
func TestCloseOfANodeTabLeavesTheNodesDraft(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	opened, err := uc.Open(ctx, OpenInput{
		Kind: domain.TabKindRequest, Ref: "node-1", DraftID: "node-1", NewTab: true,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	bound := opened.Tabs[1].ID

	closed, err := uc.Close(ctx, bound)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if slices.Contains(closed.Discarded, domain.DraftID("node-1")) {
		t.Errorf("Discarded = %v, want the node's draft left alone", closed.Discarded)
	}
}

// TestOpenBackgroundStagesWithoutShowing — the palette's ⌘↵: the tab arrives, and the work area
// stays where it was — even when the thing was already open.
func TestOpenBackgroundStagesWithoutShowing(t *testing.T) {
	uc, _ := newUseCase(true)
	ctx := context.Background()

	shown, err := uc.Strip(ctx)
	if err != nil {
		t.Fatalf("Strip: %v", err)
	}
	before := shown.Active

	staged, err := uc.Open(ctx, OpenInput{
		Kind: domain.TabKindRequest, Ref: "rec-1", Record: "rec-1",
		NewTab: true, Background: true,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(staged.Tabs) != 2 || staged.Active != before {
		t.Fatalf("a background open = %+v active %q, want the tab staged and %q shown",
			staged.Tabs, staged.Active, before)
	}

	again, err := uc.Open(ctx, OpenInput{
		Kind: domain.TabKindRequest, Ref: "rec-1", Record: "rec-1", Background: true,
	})
	if err != nil {
		t.Fatalf("Open again: %v", err)
	}
	if again.Active != before || len(again.Tabs) != 2 {
		t.Errorf("re-opening in the background = %+v active %q, want nothing moved",
			again.Tabs, again.Active)
	}
}
