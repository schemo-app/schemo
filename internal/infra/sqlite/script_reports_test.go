package sqlite

import (
	"reflect"
	"testing"

	"schemo/internal/domain"
)

func TestScriptReportArraysPreserveOrderAndReplaceWhole(t *testing.T) {
	store := newMigratedStore(t)
	ctx := t.Context()
	for _, id := range []string{"record", "other-record"} {
		if err := store.SaveRecord(ctx, ws, domain.Record{RecordSummary: domain.RecordSummary{
			ID: id, Source: domain.SourceManual, Method: "GET", URL: "https://example.com",
		}}); err != nil {
			t.Fatal(err)
		}
	}
	run := domain.ScriptRun{
		ID: "report", RecordID: "record", Scope: domain.ScriptPost, OK: false, CreatedAt: 100,
		Logs:  []domain.ScriptLog{{Level: "log", Message: "first\n\"quoted\""}, {Level: "warn", Message: "Second"}},
		Tests: []domain.TestResult{{Name: "first", Passed: true, DurationUs: 3}, {Name: "second", Passed: false, Error: "failure"}},
	}
	if err := store.SaveScriptRun(ctx, ws, run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveScriptRun(ctx, ws, domain.ScriptRun{ID: "other", RecordID: "other-record", Scope: domain.ScriptPre,
		Tests: []domain.TestResult{{Name: "unrelated", Passed: true}},
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := store.ScriptRuns(ctx, "record")
	if err != nil || len(stored) != 1 || !reflect.DeepEqual(stored[0], run) {
		t.Fatalf("report did not round trip: %+v, error=%v", stored, err)
	}
	passed, total, err := store.AssertionCounts(ctx, "record")
	if err != nil || passed != 1 || total != 2 {
		t.Fatalf("assertions=%d/%d, error=%v", passed, total, err)
	}
	wire, err := reportsOf(ctx, store.db, "record")
	if err != nil || len(wire) != 1 || len(wire[0].GetLogs()) != 2 || len(wire[0].GetTests()) != 2 {
		t.Fatalf("wire reports=%+v, error=%v", wire, err)
	}
	for position, line := range wire[0].GetLogs() {
		if line.GetPosition() != int32(position) || line.GetMessage() != run.Logs[position].Message {
			t.Fatalf("wire log position or content changed: %v", line)
		}
	}
	if wire[0].GetTests()[1].GetPosition() != 1 || wire[0].GetTests()[1].GetPassed() {
		t.Fatalf("wire assertions changed: %v", wire[0])
	}
	run.Logs, run.Tests = nil, nil
	if err := store.SaveScriptRun(ctx, ws, run); err != nil {
		t.Fatal(err)
	}
	stored, err = store.ScriptRuns(ctx, "record")
	if err != nil || len(stored) != 1 || stored[0].Logs == nil || stored[0].Tests == nil || len(stored[0].Logs)+len(stored[0].Tests) != 0 {
		t.Fatalf("empty replacement left old entries or null arrays: %+v, error=%v", stored, err)
	}
	passed, total, err = store.AssertionCounts(ctx, "record")
	if err != nil || passed != 0 || total != 0 {
		t.Fatalf("empty assertions=%d/%d, error=%v", passed, total, err)
	}
}

func TestScriptReportsRejectNonArrayJSON(t *testing.T) {
	store := newMigratedStore(t)
	ctx := t.Context()
	if err := store.SaveScriptRun(ctx, ws, domain.ScriptRun{ID: "report", Scope: domain.ScriptPre}); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"logs_json", "tests_json"} {
		for _, value := range []string{"{", "{}", "null", `"text"`} {
			if _, err := store.DB().ExecContext(ctx, `UPDATE script_runs SET `+column+` = ? WHERE id = 'report'`, value); err == nil {
				t.Fatalf("%s accepted %q", column, value)
			}
		}
	}
}
