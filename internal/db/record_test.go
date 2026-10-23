package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"schemo/internal/domain"
)

// TestRecordRoundTrip tests saving a record with headers, cookies, and bodies, and reading it back.
func TestRecordRoundTrip(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	saved := sampleRecord("rec-1", domain.SourceManual)
	saved.ResponseBytes = 11
	if err := store.SaveRecord(ctx, ws, saved); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	list, err := store.Records(ctx, ws, domain.SourceManual, 0)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("records = %d, want one", len(list))
	}
	row := list[0]
	if row.Method != "GET" || row.Status != 200 || row.TabTitle != "Example" {
		t.Errorf("record = %+v, want the saved fields", row)
	}
	if row.WaitUs == nil || *row.WaitUs != 4000 || row.DurationUs != 42000 || row.ResponseBytes != 11 {
		t.Errorf("phases = %+v, want them back with the list", row)
	}

	var cookies []string
	for _, h := range row.ResponseHeaders {
		if h.Name == "Set-Cookie" {
			cookies = append(cookies, h.Value)
		}
	}
	if len(cookies) != 2 || cookies[0] != "a=1" || cookies[1] != "b=2" {
		t.Errorf("Set-Cookie = %v, want both in order", cookies)
	}
	if len(row.CookieWrites) != 1 || row.CookieWrites[0].Name != "session" ||
		row.CookieWrites[0].State != domain.CookieAdded {
		t.Errorf("cookie writes = %+v, want expected cookie state", row.CookieWrites)
	}

	if row.ResponseBody == nil || row.ResponseBody.Size != 11 || row.ResponseBody.Inline != "" {
		t.Errorf("responseBody = %+v, want its size and no text", row.ResponseBody)
	}
	if row.RequestBody == nil || row.RequestBody.Size != 7 {
		t.Errorf("requestBody = %+v", row.RequestBody)
	}
	for _, side := range []struct {
		kind domain.BodySide
		want string
	}{
		{domain.SideRequest, `{"a":1}`},
		{domain.SideResponse, `{"data":[]}`},
	} {
		text, err := store.ReadBody(ctx, "rec-1", side.kind)
		if err != nil || text != side.want {
			t.Errorf("%s body = %q, %v; want %q", side.kind, text, err, side.want)
		}
	}
}

// TestBodyIsReadBackWhole tests that bodies exceeding inline limits are retrieved completely.
func TestBodyIsReadBackWhole(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	big := strings.Repeat("x", domain.InlineBodyLimit+1)
	rec := sampleRecord("rec-big", domain.SourceManual)
	rec.ResponseBody = &domain.BodyRef{Inline: big, Size: int64(len(big))}
	rec.RequestBody = nil
	if err := store.SaveRecord(ctx, ws, rec); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	list, err := store.Records(ctx, ws, "", 0)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if list[0].ResponseBody == nil || list[0].ResponseBody.Size != int64(len(big)) {
		t.Errorf("responseBody = %+v, want its size", list[0].ResponseBody)
	}
	if list[0].RequestBody != nil {
		t.Errorf("requestBody = %+v, want none for a record that had no request body",
			list[0].RequestBody)
	}

	text, err := store.ReadBody(ctx, "rec-big", domain.SideResponse)
	if err != nil {
		t.Fatalf("ReadBody: %v", err)
	}
	if len(text) != len(big) {
		t.Errorf("ReadBody returned %d bytes, want %d", len(text), len(big))
	}
	if _, err := store.ReadBody(ctx, "rec-big", domain.SideRequest); !errors.Is(err,
		domain.ErrNotFound) {
		t.Errorf("a body that was never saved = %v, want ErrNotFound", err)
	}
}

// TestRecordReadsOneByID tests fetching a single record by its ID.
func TestRecordReadsOneByID(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveRecord(ctx, ws, domain.Record{
		RecordSummary: domain.RecordSummary{
			ID: "rec-1", Source: domain.SourceManual, Method: "GET",
			URL: "https://api.example.com/users", Status: 200, StatusText: "200 OK",
		},
		RequestHeaders:  []domain.HeaderPair{{Name: "Accept", Value: "application/vnd.api+json"}},
		ResponseHeaders: []domain.HeaderPair{{Name: "Content-Type", Value: "application/json"}},
		RequestBody:     &domain.BodyRef{Inline: `{"a": 1}`, Size: 8},
		ResponseBody:    &domain.BodyRef{Inline: `{"data": []}`, Size: 12},
	}); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	rec, err := store.Record(ctx, "rec-1")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if rec.URL != "https://api.example.com/users" || rec.Status != 200 || rec.Method != "GET" {
		t.Errorf("record = %+v, want the saved one", rec.RecordSummary)
	}
	if len(rec.RequestHeaders) != 1 || len(rec.ResponseHeaders) != 1 {
		t.Errorf("record = %+v, want its headers", rec)
	}
	if rec.RequestBody == nil || rec.RequestBody.Size != 8 || rec.RequestBody.Inline != "" {
		t.Errorf("request body = %+v, want a reference to it", rec.RequestBody)
	}
	if rec.ResponseBody == nil || rec.ResponseBody.Size != 12 {
		t.Errorf("response body = %+v, want a reference to it", rec.ResponseBody)
	}

	if _, err := store.Record(ctx, "not-found"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a record that does not exist = %v, want ErrNotFound", err)
	}
}

// TestRecordsFilterAndOrder tests record filtering by source and ordering by timestamp.
func TestRecordsFilterAndOrder(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	for i, spec := range []struct {
		id     string
		source domain.RecordSource
	}{{"m-1", domain.SourceManual}, {"b-1", domain.SourceBrowser}, {"m-2", domain.SourceManual}} {
		rec := sampleRecord(spec.id, spec.source)
		rec.StartedAt = int64(1000 + i)
		if err := store.SaveRecord(ctx, ws, rec); err != nil {
			t.Fatalf("SaveRecord: %v", err)
		}
	}

	manual, err := store.Records(ctx, ws, domain.SourceManual, 0)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(manual) != 2 || manual[0].ID != "m-2" || manual[1].ID != "m-1" {
		t.Errorf("manual records = %v, want newest first", ids(manual))
	}

	browser, err := store.Records(ctx, ws, domain.SourceBrowser, 0)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(browser) != 1 || browser[0].ID != "b-1" {
		t.Errorf("browser records = %v", ids(browser))
	}

	limited, err := store.Records(ctx, ws, "", 2)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(limited) != 2 || limited[0].ID != "m-2" {
		t.Errorf("limited = %v, want the two newest", ids(limited))
	}
}

// TestAbsentPhasesStayAbsent verifies that missing execution timing metrics remain nil.
func TestAbsentPhasesStayAbsent(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	rec := sampleRecord("rec-nodial", domain.SourceManual)
	rec.DNSUs, rec.ConnectUs, rec.TLSUs = nil, nil, nil
	if err := store.SaveRecord(ctx, ws, rec); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	list, err := store.Records(ctx, ws, "", 0)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	got := list[0]
	if got.DNSUs != nil || got.ConnectUs != nil || got.TLSUs != nil {
		t.Errorf("dial phases = %v/%v/%v, want them absent",
			got.DNSUs, got.ConnectUs, got.TLSUs)
	}
	if got.WaitUs == nil || *got.WaitUs != 4000 {
		t.Errorf("waitUs = %v, want the phase that did happen", got.WaitUs)
	}
}

// TestDuplicateRecordIDIsAConflict tests that saving a record with an existing ID returns
// ErrConflict.
func TestDuplicateRecordIDIsAConflict(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-1", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	err := store.SaveRecord(ctx, ws, sampleRecord("rec-1", domain.SourceManual))
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("second save = %v, want domain.ErrConflict", err)
	}
}

// TestCookieWritesAreRewrittenInPlace verifies updating cookie writes for an existing record.
func TestCookieWritesAreRewrittenInPlace(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	saved := sampleRecord("rec-1", domain.SourceManual)
	if err := store.SaveRecord(ctx, ws, saved); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.SaveCookieWrites(ctx, ws, "rec-1", nil); err != nil {
		t.Fatalf("SaveCookieWrites: %v", err)
	}

	row, err := store.Record(ctx, "rec-1")
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(row.CookieWrites) != 0 {
		t.Errorf("cookie writes = %+v, want none", row.CookieWrites)
	}
	if row.URL != saved.URL || row.Method != saved.Method {
		t.Errorf("record = %+v, want everything but the writes untouched", row)
	}
}

// TestDeleteRecordsTakesTheirBodies verifies cascading deletion of associated bodies.
func TestDeleteRecordsTakesTheirBodies(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-1", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-2", domain.SourceBrowser)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	if err := store.DeleteRecords(ctx, ws, []string{"rec-1"}); err != nil {
		t.Fatalf("DeleteRecords: %v", err)
	}

	if _, err := store.ReadBody(ctx, "rec-1",
		domain.SideResponse); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the body outlived its record: %v", err)
	}
	list, _ := store.Records(ctx, ws, "", 0)
	if len(list) != 1 || list[0].ID != "rec-2" {
		t.Errorf("records = %v, want only the survivor", ids(list))
	}
}

// TestPruneByCountAndAge tests pruning records by age and count options.
func TestPruneByCountAndAge(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	now := time.Now()

	for i := 0; i < 5; i++ {
		rec := sampleRecord("rec-"+string(rune('a'+i)), domain.SourceManual)
		rec.StartedAt = now.Add(-time.Duration(i)*24*time.Hour - 90*time.Minute).UnixMilli()
		if err := store.SaveRecord(ctx, ws, rec); err != nil {
			t.Fatalf("SaveRecord: %v", err)
		}
	}

	removed, err := store.Prune(ctx, ws, domain.PruneOptions{MaxCount: 3})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 2 {
		t.Errorf("pruned %d by count, want 2", removed)
	}
	list, _ := store.Records(ctx, ws, "", 0)
	if len(list) != 3 {
		t.Fatalf("records = %d, want the newest three", len(list))
	}

	removed, err = store.Prune(ctx, ws, domain.PruneOptions{MaxAge: 48 * time.Hour})
	if err != nil {
		t.Fatalf("Prune by age: %v", err)
	}
	if removed != 1 {
		t.Errorf("pruned %d by age, want the one older than two days", removed)
	}
	list, _ = store.Records(ctx, ws, "", 0)
	if len(list) != 2 {
		t.Errorf("records = %d, want two left", len(list))
	}
}

// TestPruneTakesOnlyItsOwnWorkspace verifies that pruning is scoped to the target workspace.
func TestPruneTakesOnlyItsOwnWorkspace(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTeam(t, store)

	for _, id := range []string{"a", "b", "c"} {
		mine := sampleRecord("rec-mine-"+id, domain.SourceManual)
		mine.StartedAt = time.Now().Add(-time.Hour).UnixMilli()
		if err := store.SaveRecord(ctx, ws, mine); err != nil {
			t.Fatalf("SaveRecord: %v", err)
		}
		theirs := sampleRecord("rec-team-"+id, domain.SourceManual)
		theirs.StartedAt = time.Now().Add(-time.Hour).UnixMilli()
		if err := store.SaveRecord(ctx, team, theirs); err != nil {
			t.Fatalf("SaveRecord(%s): %v", team, err)
		}
	}

	removed, err := store.Prune(ctx, ws, domain.PruneOptions{MaxCount: 1})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 2 {
		t.Errorf("pruned %d, want 2", removed)
	}

	list, err := store.Records(ctx, ws, "", 10)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("the personal workspace holds %d record(s), want 1", len(list))
	}

	theirs, err := store.Records(ctx, team, "", 10)
	if err != nil {
		t.Fatalf("Records(%s): %v", team, err)
	}
	if len(theirs) != 3 {
		t.Errorf("the workspace beside it holds %d record(s), want 3", len(theirs))
	}
}

// TestHistoryStatsWeighOneWorkspace tests calculating total record counts and body bytes per
// workspace.
func TestHistoryStatsWeighOneWorkspace(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTeam(t, store)

	for _, id := range []string{"rec-1", "rec-2"} {
		if err := store.SaveRecord(ctx, ws, sampleRecord(id, domain.SourceManual)); err != nil {
			t.Fatalf("SaveRecord: %v", err)
		}
	}
	if err := store.SaveRecord(ctx, team, sampleRecord("rec-team", domain.SourceBrowser)); err != nil {
		t.Fatalf("SaveRecord(%s): %v", team, err)
	}

	stats, err := store.HistoryStats(ctx, ws)
	if err != nil {
		t.Fatalf("HistoryStats: %v", err)
	}
	if stats.Count != 2 || stats.Bytes != 36 {
		t.Errorf("stats = %+v, want two records and 36 bytes", stats)
	}

	theirs, err := store.HistoryStats(ctx, team)
	if err != nil {
		t.Fatalf("HistoryStats(%s): %v", team, err)
	}
	if theirs.Count != 1 || theirs.Bytes != 18 {
		t.Errorf("the workspace beside it = %+v, want one record and 18 bytes", theirs)
	}
}

// TestDeleteAllRecordsTakesOnlyItsOwnWorkspace tests hard deletion scoped to a single workspace.
func TestDeleteAllRecordsTakesOnlyItsOwnWorkspace(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTeam(t, store)

	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-mine", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	other := sampleRecord("rec-theirs", domain.SourceBrowser)
	if err := store.SaveRecord(ctx, team, other); err != nil {
		t.Fatalf("SaveRecord(%s): %v", team, err)
	}

	gone, err := store.DeleteAllRecords(ctx, ws)
	if err != nil {
		t.Fatalf("DeleteAllRecords: %v", err)
	}
	if gone != 1 {
		t.Errorf("cleared %d records, want 1", gone)
	}

	mine, _ := store.Records(ctx, ws, "", 10)
	if len(mine) != 0 {
		t.Errorf("the cleared workspace holds %d record(s), want none", len(mine))
	}
	theirs, _ := store.Records(ctx, team, "", 10)
	if len(theirs) != 1 || theirs[0].ID != "rec-theirs" {
		t.Errorf("the workspace beside it = %v, want its own record untouched", ids(theirs))
	}
	if _, err := store.ReadBody(ctx, "rec-mine",
		domain.SideResponse); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the body outlived its record: %v", err)
	}
}

// TestADeletedRecordLeavesTheHistory tests soft-delete behavior for single records versus full
// clear.
func TestADeletedRecordLeavesTheHistory(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	rec := sampleRecord("rec-1", domain.SourceManual)
	rec.RequestBody = &domain.BodyRef{Inline: "{}", Size: 2}
	rec.ResponseBody = &domain.BodyRef{Inline: `{"data":[]}`, Size: 11}
	if err := store.SaveRecord(ctx, ws, rec); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.DeleteRecords(ctx, ws, []string{"rec-1"}); err != nil {
		t.Fatalf("DeleteRecords: %v", err)
	}

	list, err := store.Records(ctx, ws, "", 0)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("the history still holds %+v", list)
	}
	if _, err := store.Record(ctx, "rec-1"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a deleted record = %v, want not found", err)
	}
	_, err = store.ReadBody(ctx, "rec-1", domain.SideResponse)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the body of a deleted record = %v, want not found", err)
	}

	hits, err := store.FindHistory(ctx, ws)
	if err != nil {
		t.Fatalf("FindHistory: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("the search still finds %+v", hits)
	}

	var marked, living int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM records WHERE deleted_at IS NOT NULL`,
	).Scan(&marked); err != nil {
		t.Fatalf("counting the marked records: %v", err)
	}
	if err := store.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM records WHERE deleted_at IS NULL`,
	).Scan(&living); err != nil {
		t.Fatalf("counting the living records: %v", err)
	}
	if marked != 1 || living != 0 {
		t.Errorf("marked = %d, living = %d, want the row kept and marked", marked, living)
	}

	if _, err := store.DeleteAllRecords(ctx, ws); err != nil {
		t.Fatalf("DeleteAllRecords: %v", err)
	}
	var left int
	if err := store.DB().QueryRowContext(ctx, `SELECT count(*) FROM records`).Scan(&left); err != nil {
		t.Fatalf("counting the records: %v", err)
	}
	if left != 0 {
		t.Errorf("%d records survived clearing the history", left)
	}
}

// TestABodyThatIsNotHereIsDeclared tests metadata handling for remote records without inline body
// data.
func TestABodyThatIsNotHereIsDeclared(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	arrived := sampleRecord("rec-1", domain.SourceManual)
	arrived.ResponseBody = nil
	arrived.RequestBody = nil
	arrived.ResponseBytes = 4096
	if err := store.SaveRecord(ctx, ws, arrived); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		`UPDATE records SET is_remote = 1 WHERE id = 'rec-1'`); err != nil {
		t.Fatalf("marking the record as another device's: %v", err)
	}

	empty := sampleRecord("rec-2", domain.SourceManual)
	empty.ResponseBody = nil
	empty.RequestBody = nil
	empty.ResponseBytes = 0
	if err := store.SaveRecord(ctx, ws, empty); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	list, err := store.Records(ctx, ws, domain.SourceManual, 0)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	for _, row := range list {
		switch row.ID {
		case "rec-1":
			if row.ResponseBody == nil || row.ResponseBody.Size != 4096 ||
				row.ResponseBody.Inline != "" {
				t.Errorf("body of %s = %+v, want declared size and no bytes",
					row.ID, row.ResponseBody)
			}
		case "rec-2":
			if row.ResponseBody != nil {
				t.Errorf("body of %s = %+v, want none: it never had one", row.ID, row.ResponseBody)
			}
		}
	}
}

// TestCachedRecordsAreForScrollingBackNotForTheList tests reading cached remote records via
// pagination.
func TestCachedRecordsAreForScrollingBackNotForTheList(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	now := time.Now()

	own := sampleRecord("rec-own", domain.SourceManual)
	own.StartedAt = now.Add(-time.Hour).UnixMilli()
	if err := store.SaveRecord(ctx, ws, own); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	old := sampleRecord("rec-old", domain.SourceManual)
	old.StartedAt = now.Add(-40 * 24 * time.Hour).UnixMilli()
	old.IsRemote = true
	if err := store.SaveCached(ctx, ws, []domain.Record{old}); err != nil {
		t.Fatalf("SaveCached: %v", err)
	}

	list, _ := store.Records(ctx, ws, "", 0)
	if len(list) != 1 || list[0].ID != "rec-own" {
		t.Errorf("list = %v, want local record only", list)
	}
	stats, err := store.HistoryStats(ctx, ws)
	if err != nil {
		t.Fatalf("HistoryStats: %v", err)
	}
	if stats.Count != 1 {
		t.Errorf("history counts %d, want 1", stats.Count)
	}

	page, err := store.RecordsBefore(ctx, ws, now.UnixMilli(), 10)
	if err != nil {
		t.Fatalf("RecordsBefore: %v", err)
	}
	if len(page) != 2 || page[0].ID != "rec-own" || page[1].ID != "rec-old" {
		t.Errorf("page = %v, want both, newest first", page)
	}

	removed, err := store.Prune(ctx, ws, domain.PruneOptions{MaxAge: 24 * time.Hour, MaxCount: 1})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 0 {
		t.Errorf("pruned %d, want cached records untouched", removed)
	}
}

// TestTheCachePrunesByWhenItWasRead tests cache pruning policies using last-read timestamps.
func TestTheCachePrunesByWhenItWasRead(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	now := time.Now()

	for i, id := range []string{"rec-a", "rec-b", "rec-c"} {
		rec := sampleRecord(id, domain.SourceManual)
		rec.StartedAt = now.Add(-time.Duration(40+i) * 24 * time.Hour).UnixMilli()
		rec.IsRemote = true
		if err := store.SaveCached(ctx, ws, []domain.Record{rec}); err != nil {
			t.Fatalf("SaveCached: %v", err)
		}
		read := now.Add(-time.Duration(3-i) * time.Minute).UnixMilli()
		if _, err := store.db.ExecContext(ctx,
			`UPDATE records SET cached_at = ? WHERE id = ?`, read, id); err != nil {
			t.Fatalf("setting cached_at: %v", err)
		}
	}

	removed, err := store.Prune(ctx, ws, domain.PruneOptions{CacheCount: 2})
	if err != nil {
		t.Fatalf("Prune by count: %v", err)
	}
	if removed != 1 {
		t.Fatalf("pruned %d, want 1", removed)
	}
	if _, err := store.Record(ctx, "rec-a"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("rec-a = %v, want ErrNotFound", err)
	}

	removed, err = store.Prune(ctx, ws, domain.PruneOptions{CacheAge: 90 * time.Second})
	if err != nil {
		t.Fatalf("Prune by age: %v", err)
	}
	if removed != 1 {
		t.Errorf("pruned %d by age, want rec-b to be removed", removed)
	}
}
