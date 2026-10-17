package wails

import (
	"reflect"
	"testing"

	"schemo/internal/domain"
	"schemo/internal/infra/authflow"
	"schemo/internal/platform"
	"schemo/internal/transport/desktop"
	"schemo/internal/usecase/collection"
	"schemo/internal/usecase/cookiejar"
	"schemo/internal/usecase/draft"
	"schemo/internal/usecase/environment"
	"schemo/internal/usecase/record"
	"schemo/internal/usecase/settings"
	"schemo/internal/usecase/tabstrip"
)

func TestStripDropsDeletedCollectionTabsAndPersistsTheSurvivors(t *testing.T) {
	ctx := t.Context()
	dir := platform.DataDir(t.TempDir())
	store := storeOn(t, dir)
	if err := desktop.OpenStorage(store, desktop.NewStatus(), dir)(ctx); err != nil {
		t.Fatal(err)
	}
	ids := platform.NewIDGen()
	preferences := settings.NewUseCase(store, &heard{}, nil)
	nodes := collection.NewUseCase(store, store, nil, nil, nil, &heard{}, ids)
	for _, id := range []string{"removed", "kept"} {
		if err := store.SaveCollection(ctx, domain.WorkspacePersonalID, domain.Collection{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveNode(ctx, domain.CollectionNode{ID: "node", CollectionID: "removed", Name: "Request", Method: "GET"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTabs(ctx, domain.WorkspacePersonalID, []domain.Tab{
		{ID: "folder", Kind: domain.TabKindCollections, Ref: "removed"},
		{ID: "request", Kind: domain.TabKindRequest, Ref: "node", DraftID: "node"},
		{ID: "kept", Kind: domain.TabKindCollections, Ref: "kept"},
	}, "kept"); err != nil {
		t.Fatal(err)
	}
	service := NewTabsService(tabstrip.NewUseCase(store, preferences, store, ids), nil, nil, nodes)
	if _, err := service.Strip(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteCollection(ctx, domain.WorkspacePersonalID, "removed"); err != nil {
		t.Fatal(err)
	}
	strip, err := service.Strip(ctx)
	if err != nil || len(strip.Tabs) != 1 || strip.Active != "kept" {
		t.Fatalf("strip after deletion = %+v, %v", strip, err)
	}
	restored := NewTabsService(tabstrip.NewUseCase(store, preferences, store, ids), nil, nil, nodes)
	strip, err = restored.Strip(ctx)
	if err != nil || len(strip.Tabs) != 1 || strip.Active != "kept" {
		t.Fatalf("restored strip = %+v, %v", strip, err)
	}
}

func TestEditCapturedRecordOpensIndependentRequest(t *testing.T) {
	ctx := t.Context()
	dir := platform.DataDir(t.TempDir())
	store := storeOn(t, dir)
	if err := desktop.OpenStorage(store, desktop.NewStatus(), dir)(ctx); err != nil {
		t.Fatal(err)
	}
	ids := platform.NewIDGen()
	notifier := &heard{}
	preferences := settings.NewUseCase(store, notifier, nil)
	records := record.NewUseCase(store, store, nil, nil, notifier, preferences,
		nil, nil, nil, ids, platform.BuildInfo{})
	drafts := draft.NewUseCase(store, store, environment.NewUseCase(store, store, ids),
		cookiejar.NewUseCase(store, store, ids), nil, authflow.New(nil, nil), nil, ids)
	service := NewTabsService(tabstrip.NewUseCase(store, preferences, store, ids), drafts, records, nil)
	headers := []domain.HeaderPair{{Name: "Content-Type", Value: "application/json"},
		{Name: "Authorization", Value: "Bearer captured-token"}}
	captured, err := records.Ingest(ctx, record.IngestInput{
		Method: "POST", URL: "https://example.com/items?page=2", RequestHeaders: headers,
		RequestBody: `{"name":"example"}`, Status: 201,
	})
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := service.Open(ctx, tabstrip.OpenInput{Kind: domain.TabKindRequest, Record: captured.ID})
	if err != nil {
		t.Fatal(err)
	}
	previous := viewer.Active
	for range 2 {
		strip, err := service.EditRecord(ctx, captured.ID)
		if err != nil {
			t.Fatal(err)
		}
		active := tabOf(strip.Tabs, strip.Active)
		if active == nil || active.ID == previous || active.Record != "" || active.Ref != "" || active.Preview {
			t.Fatalf("repeat did not open an independent editor: %+v", active)
		}
		copied, err := drafts.Current(ctx, active.DraftID)
		if err != nil {
			t.Fatal(err)
		}
		if copied.Method != captured.Method || copied.URL != captured.URL || copied.Body != `{"name":"example"}` {
			t.Fatalf("copied request lost content: %+v", copied)
		}
		var copiedHeaders []domain.HeaderPair
		for _, header := range copied.Headers {
			if !header.Enabled {
				t.Fatalf("copied header is disabled: %+v", header)
			}
			copiedHeaders = append(copiedHeaders, domain.HeaderPair{Name: header.Name, Value: header.Value})
		}
		if !reflect.DeepEqual(copiedHeaders, headers) {
			t.Fatalf("copied headers = %+v, want %+v", copiedHeaders, headers)
		}
		if original := tabOf(strip.Tabs, viewer.Active); original == nil || original.Record != captured.ID || original.DraftID == active.DraftID {
			t.Fatalf("capture viewer was changed or shares its draft: %+v", original)
		}
		previous = strip.Active
	}
}
