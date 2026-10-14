package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/schemo-app/proto/go/history/v1"

	"schemo/internal/domain"
)

// One sent request as `history/v1` carries it: the record, its two bodies and what the code around
// it reported. The run that produced it is a document of its own — one payload per record because
// it is one record, and a run is many.
//
// All of it in one payload, because it is one document. A report whose record is missing is a
// report of nothing, so nothing here is a row of its own on the wire, exactly as nothing here is
// one in the tables. A reader that cannot be read is an error and not an entry with a hole in it: a
// record that travels without its reports reads as a request nothing was asserted about.

// historyEntry reads a record and everything that hangs off it.
func historyEntry(
	ctx context.Context,
	q querier,
	record domain.Record,
	scopes domain.SyncScopes,
) (*history.RecordEntry, error) {
	bodies, err := bodiesOf(ctx, q, record.ID)
	if err != nil {
		return nil, err
	}

	entry := history.RecordEntry_builder{
		Record: recordData(record, scopes.Secrets),
		Bodies: bodies,
	}
	// What the scripts reported is what a person lets out or keeps: the request itself — what it was
	// and what came back — is the record, and it travels either way.
	if scopes.Runs {
		reports, err := reportsOf(ctx, q, record.ID)
		if err != nil {
			return nil, err
		}
		entry.Scripts = reports
	}
	return entry.Build(), nil
}

// runEntity is one collection run as `history/v1` carries it, once by its id however many records
// name it. Its rows name the records the run produced, and the records name it back.
func runEntity(
	ctx context.Context,
	q querier,
	id string,
	scopes domain.SyncScopes,
) (domain.SyncEntity, bool, error) {
	if !scopes.Runs {
		// A run is what a person lets out or keeps, like the reports beside it. A device that keeps
		// its runs at home owes the server nothing about them.
		return domain.SyncEntity{}, false, nil
	}

	var (
		collectionID, nodeID, environment string
		startedAt, finishedAt, durationUs int64
		passed, failed                    int32
	)
	err := q.QueryRowContext(ctx,
		`SELECT collection_id, ifnull(node_id, ''), environment, started_at, ifnull(finished_at, 0),
		        passed, failed, duration_us
		   FROM collection_runs WHERE id = ?`, id).
		Scan(&collectionID, &nodeID, &environment, &startedAt, &finishedAt, &passed, &failed,
			&durationUs)
	if err != nil {
		return absent(err, "run", id)
	}

	results, err := resultsOf(ctx, q, id)
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("reading the rows of run %s: %w", id, err)
	}
	payload, err := proto.Marshal(history.Run_builder{
		Id:           id,
		CollectionId: collectionID,
		NodeId:       nodeID,
		Environment:  environment,
		StartedAt:    moment(startedAt),
		FinishedAt:   moment(finishedAt),
		Passed:       passed,
		Failed:       failed,
		DurationUs:   durationUs,
		Results:      results,
	}.Build())
	if err != nil {
		return domain.SyncEntity{}, false, fmt.Errorf("encoding run %s: %w", id, err)
	}
	return envelope(domain.EntityRun, id, "run of "+collectionID, 0, 0, false,
		startedAt, startedAt, payload), true, nil
}

// recordData is one record as the wire carries it. A person lets secrets out or keeps them: with
// them kept, the values a request went out with are the device's own and stay here, and what they
// were is still said — a header keeps its name, and a cookie write its flag.
func recordData(record domain.Record, secrets bool) *history.Record {
	return history.Record_builder{
		Id:              record.ID,
		RunId:           record.RunID,
		Agent:           record.Agent,
		WorkspaceId:     record.WorkspaceID,
		NodeId:          record.NodeID,
		Source:          string(record.Source),
		Method:          record.Method,
		Url:             record.URL,
		Status:          int32(record.Status),
		StatusText:      record.StatusText,
		ContentType:     record.ContentType,
		Error:           record.Error,
		Cancelled:       record.Cancelled,
		DurationUs:      record.DurationUs,
		DnsUs:           record.DNSUs,
		ConnectUs:       record.ConnectUs,
		TlsUs:           record.TLSUs,
		WaitUs:          record.WaitUs,
		DownloadUs:      record.DownloadUs,
		RequestBytes:    record.RequestBytes,
		ResponseBytes:   record.ResponseBytes,
		RequestHeaders:  headersOf(record.RequestHeaders, secrets),
		ResponseHeaders: headersOf(record.ResponseHeaders, secrets),
		CookieWrites:    writesOf(record.CookieWrites, secrets),
		StartedAt:       moment(record.StartedAt),
		// When it finished is not a fact this app keeps: a record is drawn by when it went out and how
		// long it took, and the two moments would be the same number twice.
		TabId:      int32(record.TabID),
		TabTitle:   record.TabTitle,
		TabUrl:     record.TabURL,
		FaviconUrl: record.FavIconURL,
	}.Build()
}

// secretHeaders are the names whose value is a credential wherever a record carries it. The server
// withholds the same three from a reader who may not see secrets, and one list for both ends is how
// the two agree about what a record holds back.
var secretHeaders = []string{"Authorization", "Cookie", "Set-Cookie"}

// headersOf is one side of a record as the wire carries it. A header that is a credential keeps its
// name either way — that one went is the shape of the request — and its value only goes when the
// person has let secrets out.
func headersOf(headers []domain.HeaderPair, secrets bool) []*history.Header {
	if len(headers) == 0 {
		return nil
	}
	out := make([]*history.Header, 0, len(headers))
	for _, header := range headers {
		value, withheld := header.Value, false
		if !secrets && value != "" && secretHeader(header.Name) {
			value, withheld = "", true
		}
		out = append(out, history.Header_builder{
			Name: header.Name, Value: value, ValueWithheld: withheld,
		}.Build())
	}
	return out
}

func secretHeader(name string) bool {
	for _, secret := range secretHeaders {
		if strings.EqualFold(name, secret) {
			return true
		}
	}
	return false
}

// writesOf is what an answer did to the jar as the wire carries it. A cookie's value is the value
// whatever the cookie is called, so it goes the same way every value does when secrets are kept.
func writesOf(writes []domain.CookieWrite, secrets bool) []*history.CookieWrite {
	if len(writes) == 0 {
		return nil
	}
	out := make([]*history.CookieWrite, 0, len(writes))
	for _, write := range writes {
		value, withheld := write.Value, write.Withheld
		if !secrets && value != "" {
			value, withheld = "", true
		}
		out = append(out, history.CookieWrite_builder{
			Name:          write.Name,
			Value:         value,
			ValueWithheld: withheld,
			Domain:        write.Domain,
			Path:          write.Path,
			ExpiresAt:     momentOf(write.ExpiresAt),
			Secure:        write.Secure,
			HttpOnly:      write.HTTPOnly,
			SameSite:      write.SameSite,
			HostOnly:      write.HostOnly,
			State:         string(write.State),
			Previous:      jarCookieOf(write.Previous, !secrets || write.PreviousWithheld),
		}.Build())
	}
	return out
}

// jarCookieOf is the cookie a write replaced. `withhold` is what the write said about it, since the
// cookie it holds has nowhere to carry a flag of its own.
func jarCookieOf(cookie *domain.JarCookie, withhold bool) *history.JarCookie {
	if cookie == nil {
		return nil
	}
	value, withheld := cookie.Value, false
	if withhold && value != "" {
		value, withheld = "", true
	}
	return history.JarCookie_builder{
		Name:          cookie.Name,
		Value:         value,
		ValueWithheld: withheld,
		Domain:        cookie.Domain,
		Path:          cookie.Path,
		ExpiresAt:     momentOf(cookie.ExpiresAt),
		Secure:        cookie.Secure,
		HttpOnly:      cookie.HTTPOnly,
		SameSite:      cookie.SameSite,
		HostOnly:      cookie.HostOnly,
	}.Build()
}

// bodiesOf reads the two sides of a record. What is stored is what the body was: a body too large
// to keep whole was never kept whole, and the size beside it says what it really weighed.
func bodiesOf(ctx context.Context, q querier, recordID string) ([]*history.Body, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT side, content, size, truncated FROM record_bodies WHERE record_id = ?`, recordID)
	if err != nil {
		return nil, fmt.Errorf("reading the bodies of %s: %w", recordID, err)
	}
	defer rows.Close()

	out := []*history.Body{}
	for rows.Next() {
		var (
			side      string
			content   []byte
			size      int64
			truncated int
		)
		if err := rows.Scan(&side, &content, &size, &truncated); err != nil {
			return nil, fmt.Errorf("reading the bodies of %s: %w", recordID, err)
		}
		out = append(out, history.Body_builder{
			Side: side, Content: content, Size: size, Truncated: truncated != 0,
		}.Build())
	}
	return out, rows.Err()
}

func resultsOf(ctx context.Context, q querier, runID string) ([]*history.RunResult, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT node_id, position, status, ok, duration_us, error, ifnull(record_id, ''),
		        assertions_passed, assertions_total, failure, skipped
		   FROM collection_run_results WHERE run_id = ? ORDER BY position`, runID)
	if err != nil {
		return nil, fmt.Errorf("reading the rows of run %s: %w", runID, err)
	}
	defer rows.Close()

	out := []*history.RunResult{}
	for rows.Next() {
		var (
			nodeID, error_, recordID, failure string
			position, durationUs              int64
			status                            sql.NullInt64
			ok, skipped                       int
			asserted, total                   int32
		)
		if err := rows.Scan(&nodeID, &position, &status, &ok, &durationUs, &error_, &recordID,
			&asserted, &total, &failure, &skipped); err != nil {
			return nil, fmt.Errorf("reading the rows of run %s: %w", runID, err)
		}

		result := history.RunResult_builder{
			NodeId:           nodeID,
			Position:         int32(position),
			Ok:               ok != 0,
			DurationUs:       durationUs,
			Error:            error_,
			RecordId:         recordID,
			AssertionsPassed: asserted,
			AssertionsTotal:  total,
			Skipped:          skipped != 0,
		}
		if status.Valid {
			value := int32(status.Int64)
			result.Status = &value
		}
		if failure != "" {
			var refused domain.Failure
			if err := json.Unmarshal([]byte(failure), &refused); err != nil {
				return nil, fmt.Errorf("reading a failure of run %s: %w", runID, err)
			}
			result.Failure = history.Failure_builder{
				Code: string(refused.Code),
				Args: refused.Args,
			}.Build()
		}
		out = append(out, result.Build())
	}
	return out, rows.Err()
}

// reportsOf maps the stored report to the public contract, preserving array positions.
func reportsOf(ctx context.Context, q querier, recordID string) ([]*history.ScriptRun, error) {
	runs, err := scriptRuns(ctx, q, recordID)
	if err != nil {
		return nil, err
	}
	out := make([]*history.ScriptRun, 0, len(runs))
	for _, run := range runs {
		logs := make([]*history.ScriptLog, 0, len(run.Logs))
		for position, line := range run.Logs {
			logs = append(logs, history.ScriptLog_builder{
				Position: int32(position), Level: line.Level, Message: line.Message,
			}.Build())
		}
		tests := make([]*history.TestResult, 0, len(run.Tests))
		for position, result := range run.Tests {
			tests = append(tests, history.TestResult_builder{
				Position: int32(position), Name: result.Name, Passed: result.Passed,
				Error: result.Error, DurationUs: result.DurationUs,
			}.Build())
		}
		out = append(out, history.ScriptRun_builder{
			Id: run.ID, NodeId: run.NodeID, Scope: string(run.Scope), Ok: run.OK,
			Error: run.Error, DurationUs: run.DurationUs, CreatedAt: moment(run.CreatedAt),
			Logs: logs, Tests: tests,
		}.Build())
	}
	return out, nil
}

// moment is a moment this app keeps as Unix milliseconds, as the contract carries it. A zero is
// left out rather than sent as 1970: a record that never finished is not a record that finished at
// the epoch, and a window can draw the difference.
func moment(millis int64) *timestamppb.Timestamp {
	if millis == 0 {
		return nil
	}
	return timestamppb.New(time.UnixMilli(millis))
}

// momentOf is the same for a moment the jar keeps behind a pointer.
func momentOf(millis *int64) *timestamppb.Timestamp {
	if millis == nil {
		return nil
	}
	return moment(*millis)
}
