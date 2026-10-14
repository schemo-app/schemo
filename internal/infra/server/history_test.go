package server

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/schemo-app/proto/go/history/v1"

	"schemo/internal/domain"
)

// A report of another device's scripts maps whole: the lines it printed, the assertions it made and
// the moment it ran are what the panel draws, and a field left behind is a hole in it.
func TestAReportIsReadWithItsLinesAndItsAssertions(t *testing.T) {
	report := history.ScriptRun_builder{
		Id: "sr-1", NodeId: "req-1", Scope: "post", Ok: true, DurationUs: 1200,
		CreatedAt: timestamppb.New(time.UnixMilli(1_700_000_000_000)),
		Logs: []*history.ScriptLog{
			history.ScriptLog_builder{Level: "warn", Message: "slow"}.Build(),
		},
		Tests: []*history.TestResult{
			history.TestResult_builder{Name: "status is 200", Passed: true, DurationUs: 40}.Build(),
		},
	}.Build()

	got := scriptIn(report)
	if got.ID != "sr-1" || got.NodeID != "req-1" || got.Scope != domain.ScriptPost || !got.OK {
		t.Errorf("report = %+v, want the words the server sent", got)
	}
	if got.CreatedAt != 1_700_000_000_000 {
		t.Errorf("created at = %d, want the moment it ran", got.CreatedAt)
	}
	if len(got.Logs) != 1 || got.Logs[0].Message != "slow" {
		t.Errorf("logs = %+v, want the line it printed", got.Logs)
	}
	if len(got.Tests) != 1 || got.Tests[0].Name != "status is 200" || !got.Tests[0].Passed {
		t.Errorf("tests = %+v, want the assertion it made", got.Tests)
	}
}

// A row of a run whose status was never read is not a row that came back with a zero in it, and the
// two draw differently: the absence travels as an absence.
func TestARowWithoutAStatusIsReadWithoutOne(t *testing.T) {
	absent := int32(200)
	run := history.Run_builder{
		Id: "run-1", CollectionId: "col-1", Passed: 1, StartedAt: timestamppb.New(
			time.UnixMilli(1_700_000_000_000)),
		Results: []*history.RunResult{
			history.RunResult_builder{NodeId: "req-1", Position: 0, Ok: true}.Build(),
			history.RunResult_builder{NodeId: "req-2", Position: 1, Status: &absent,
				Failure: history.Failure_builder{Code: "serverRefused"}.Build()}.Build(),
		},
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
