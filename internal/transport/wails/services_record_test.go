package wails

import (
	"context"
	"testing"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/internal/transport/desktop"
	"schemo/internal/usecase/record"
)

// heard is the Notifier this file's test watches: what the app would have broadcast, kept in order.
type heard struct {
	topics []string
}

func (h *heard) Publish(topic string, _ any) {
	h.topics = append(h.topics, topic)
}

// The settings window's "clear history now", as the app really runs it: the records of the space on
// screen go, the list the other windows read comes back empty, and every window is told.
//
// The telling is the half a sidebar depends on — nothing else reloads it — so it is checked here
// beside the deletion rather than trusted to the wiring.
func TestClearingHistoryEmptiesTheListAndTellsTheWindows(t *testing.T) {
	dir := platform.DataDir(t.TempDir())
	store := storeOn(t, dir)
	ctx := context.Background()
	// The database as the app opens it: the folder made and the schema written, which is the one
	// thing a store on its own does not do.
	if err := desktop.OpenStorage(store, desktop.NewStatus(), dir)(ctx); err != nil {
		t.Fatalf("desktop.OpenStorage: %v", err)
	}

	notifier := &heard{}
	uc := record.NewUseCase(store, store, nil, nil, notifier,
		record.RetentionSourceFunc(func(context.Context) (domain.Retention, error) {
			return domain.RetainForever, nil
		}), nil, nil, nil, platform.NewIDGen(), platform.BuildInfo{Name: "Schemo"})
	service := NewRecordsService(uc, nil, nil, nil)

	for _, id := range []string{"rec-1", "rec-2"} {
		if err := store.SaveRecord(ctx, domain.WorkspacePersonalID, domain.Record{
			RecordSummary: domain.RecordSummary{
				ID: id, Source: domain.SourceManual, Method: "GET",
				URL: "https://api.example.com/articles", Status: 200,
				StartedAt: time.Now().UnixMilli(),
			},
		}); err != nil {
			t.Fatalf("SaveRecord(%s): %v", id, err)
		}
	}

	before, err := service.List(ctx, domain.RecordSource(""), 0)
	if err != nil || len(before) != 2 {
		t.Fatalf("List = %d row(s), %v; want the two that were saved", len(before), err)
	}

	removed, err := service.ClearAll(ctx)
	if err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	if removed != 2 {
		t.Errorf("cleared %d records, want both", removed)
	}

	after, err := service.List(ctx, domain.RecordSource(""), 0)
	if err != nil {
		t.Fatalf("List after the clear: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("the list still holds %d row(s), want none — it is what a sidebar draws", len(after))
	}

	found := false
	for _, topic := range notifier.topics {
		if topic == record.TopicHistoryCleared {
			found = true
		}
	}
	if !found {
		t.Errorf("broadcast %v, want %q among them", notifier.topics, record.TopicHistoryCleared)
	}
}
