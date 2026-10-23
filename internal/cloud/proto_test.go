package cloud

import (
	"schemo/internal/domain"
	"testing"
	"time"

	historyv1 "github.com/schemo-app/proto/go/history/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A record of another device is its envelope and its document, and a cookie value the server kept
// back arrives flagged so this device does not take the empty value for the real one.
func TestARecordIsReadFromItsDocument(t *testing.T) {
	one := historyv1.Record_builder{
		Id: "rec-1", NodeId: "req-1", RunId: "run-1",
		Source:    historyv1.RecordSource_RECORD_SOURCE_BROWSER,
		StartedAt: timestamppb.New(time.UnixMilli(1_700_000_000_000)),
		Data: []byte(`{"method":"GET","url":"https://api.test","status":200,"tabId":7,` +
			`"cookieWrites":[{"name":"session","value":"","state":"added","previous":{"name":"session","value":""}}]}`),
		Withheld: []string{"/cookieWrites/0/value", "/cookieWrites/0/previous/value"},
	}.Build()

	got := recordIn("space-1", one)
	if got.ID != "rec-1" || got.WorkspaceID != "space-1" || got.Source != domain.SourceBrowser ||
		got.NodeID != "req-1" || got.RunID != "run-1" || !got.IsRemote {
		t.Errorf("record = %+v, want the envelope the server sent", got.RecordSummary)
	}
	if got.Method != "GET" || got.Status != 200 || got.TabID != 7 ||
		got.StartedAt != 1_700_000_000_000 {
		t.Errorf("record = %+v, want the document the server sent", got.RecordSummary)
	}
	if len(got.CookieWrites) != 1 || !got.CookieWrites[0].Withheld || !got.CookieWrites[0].PreviousWithheld {
		t.Errorf("writes = %+v, want the withheld values flagged", got.CookieWrites)
	}
}

// A row of a run whose status was never read is not a row that came back with a zero in it, and the
// two draw differently: the absence travels as an absence.
func TestARowWithoutAStatusIsReadWithoutOne(t *testing.T) {
	run := historyv1.Run_builder{
		Id: "run-1", CollectionId: "col-1",
		StartedAt: timestamppb.New(time.UnixMilli(1_700_000_000_000)),
		Data: []byte(`{"passed":1,"results":[{"nodeId":"req-1","position":0,"ok":true},` +
			`{"nodeId":"req-2","position":1,"status":200,"failure":{"code":"serverRefused"}}]}`),
	}.Build()

	got := runIn(run)
	if got.ID != "run-1" || got.CollectionID != "col-1" || got.Passed != 1 {
		t.Errorf("run = %+v, want the counters the server sent", got)
	}
	if got.StartedAt != 1_700_000_000_000 {
		t.Errorf("started at = %d, want the moment it ran", got.StartedAt)
	}
	if len(got.Results) != 2 {
		t.Fatalf("results = %+v, want both rows", got.Results)
	}
	if got.Results[0].Status != nil {
		t.Errorf("status = %v, want none for a request that never reached a server",
			*got.Results[0].Status)
	}
	if got.Results[1].Status == nil || *got.Results[1].Status != 200 {
		t.Errorf("status = %v, want the code it came back with", got.Results[1].Status)
	}
	if got.Results[1].Failure == nil || got.Results[1].Failure.Code != domain.CodeServerRefused {
		t.Errorf("failure = %+v, want what the row was refused for", got.Results[1].Failure)
	}
}
