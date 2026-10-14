package server

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/schemo-app/proto/go/history/v1"

	"schemo/internal/domain"
)

// PullRecords fetches a page of history metadata changes for a workspace after a sequence number.
func (c *Client) PullRecords(
	ctx context.Context,
	caller domain.Caller,
	spaceID string,
	afterSeq int64,
	limit int,
) (domain.RecordPage, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*history.PullRecordsResponse, error) {
			return history.NewHistoryServiceClient(conn).PullRecords(ctx,
				history.PullRecordsRequest_builder{
					WorkspaceId: spaceID,
					AfterSeq:    afterSeq,
					Limit:       int32(limit),
				}.Build())
		})
	if err != nil {
		return domain.RecordPage{}, err
	}

	page := domain.RecordPage{
		Cursor:  answer.GetNextSeq(),
		HasMore: answer.GetHasMore(),
		Floor:   answer.GetFloorSeq(),
		Head:    answer.GetHeadSeq(),
	}
	for _, change := range answer.GetChanges() {
		one := domain.RecordChange{
			Seq: change.GetSeq(), RecordID: change.GetRecordId(), Deleted: change.GetDeleted(),
		}
		if !change.GetDeleted() {
			one.Record = recordIn(spaceID, change.GetRecord())
		}
		if run := change.GetRun(); run != nil {
			// A run is an entry of the log with nothing else in it: a record that names it travels
			// under its own number, and the run is written once by its id.
			held := runIn(run)
			one.Run = &held
		}
		page.Changes = append(page.Changes, one)
	}
	return page, nil
}

// ListRecords retrieves a paginated list of history records within a timestamp range.
func (c *Client) ListRecords(
	ctx context.Context,
	caller domain.Caller,
	spaceID string,
	from, to int64,
	limit int,
	token string,
) (domain.RecordList, error) {
	request := history.ListRequest_builder{
		WorkspaceId: spaceID, PageToken: token, PageSize: int32(limit),
	}
	if from > 0 {
		request.From = timestamppb.New(time.UnixMilli(from))
	}
	if to > 0 {
		request.To = timestamppb.New(time.UnixMilli(to))
	}
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*history.ListResponse, error) {
			return history.NewHistoryServiceClient(conn).List(ctx, request.Build())
		})
	if err != nil {
		return domain.RecordList{}, err
	}

	list := domain.RecordList{Head: answer.GetHeadSeq(), Next: answer.GetNextPageToken()}
	for _, one := range answer.GetRecords() {
		list.Records = append(list.Records, recordIn(spaceID, one))
	}
	return list, nil
}

// Sent appends history to the remote workspace and returns a verdict for each record and each run.
// The two travel in one call because they are one document set: a record names the run it was sent
// by, and a run names back the records it produced.
//
// A payload this build cannot read is refused rather than skipped: the caller sent it and is owed
// an answer about it, and a body that quietly disappeared between two devices is the one failure
// nobody would ever notice.
func (c *Client) Sent(
	ctx context.Context,
	caller domain.Caller,
	spaceID string,
	records, runs []domain.Pushed,
) ([]domain.RecordVerdict, []domain.RecordVerdict, error) {
	entries := make([]*history.RecordEntry, 0, len(records))
	for _, record := range records {
		entry := &history.RecordEntry{}
		if err := proto.Unmarshal(record.Entity.Payload, entry); err != nil {
			return nil, nil, fmt.Errorf("reading record %s: %w", record.Entity.ID, err)
		}
		entries = append(entries, entry)
	}

	held := make([]*history.Run, 0, len(runs))
	for _, run := range runs {
		one := &history.Run{}
		if err := proto.Unmarshal(run.Entity.Payload, one); err != nil {
			return nil, nil, fmt.Errorf("reading run %s: %w", run.Entity.ID, err)
		}
		held = append(held, one)
	}

	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*history.AppendResponse, error) {
			return history.NewHistoryServiceClient(conn).Append(ctx,
				history.AppendRequest_builder{
					WorkspaceId: spaceID,
					Records:     entries,
					Runs:        held,
				}.Build())
		})
	if err != nil {
		return nil, nil, err
	}

	return verdictsOf(answer.GetResults()), verdictsOf(answer.GetRunResults()), nil
}

// verdictsOf reads one list of the answer. The id is where the record or the run stood, and a
// reason is what was wrong with it — nothing else is said about either.
func verdictsOf(results []*history.AppendResult) []domain.RecordVerdict {
	out := make([]domain.RecordVerdict, 0, len(results))
	for _, result := range results {
		out = append(out, domain.RecordVerdict{
			RecordID: result.GetId(),
			Reason:   recordReasonOf(result.GetReason()),
		})
	}
	return out
}

// Retract requests deletion of specific history records from the server by their IDs.
func (c *Client) Retract(
	ctx context.Context,
	caller domain.Caller,
	spaceID string,
	ids []string,
) (int, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*history.DeleteResponse, error) {
			return history.NewHistoryServiceClient(conn).Delete(ctx,
				history.DeleteRequest_builder{
					WorkspaceId: spaceID,
					RecordIds:   ids,
				}.Build())
		})
	if err != nil {
		return 0, err
	}
	return int(answer.GetDeleted()), nil
}

// recordReasonOf maps protobuf push refusal reason strings to domain PushReason types.
func recordReasonOf(reason string) domain.PushReason {
	switch reason {
	case "tooLarge":
		return domain.ReasonTooLarge
	case "forbidden":
		return domain.ReasonForbidden
	case "quotaExceeded":
		return domain.ReasonQuotaExceeded
	case "parentNotFound":
		return domain.ReasonParentMissing
	case "empty":
		return domain.ReasonEmpty
	case "":
		return ""
	}
	return domain.ReasonUnspecified
}

// recordIn converts a protobuf history Record object into a domain Record model.
func recordIn(spaceID string, one *history.Record) domain.Record {
	started := time.Time{}
	if at := one.GetStartedAt(); at != nil {
		started = at.AsTime()
	}

	return domain.Record{
		RecordSummary: domain.RecordSummary{
			ID:          one.GetId(),
			WorkspaceID: spaceID,
			Source:      domain.RecordSource(one.GetSource()),
			Method:      one.GetMethod(),
			URL:         one.GetUrl(),
			Status:      int(one.GetStatus()),
			StatusText:  one.GetStatusText(),
			ContentType: one.GetContentType(),
			Error:       one.GetError(),
			DurationUs:  one.GetDurationUs(),
			StartedAt:   started.UnixMilli(),
			NodeID:      one.GetNodeId(),
			RunID:       one.GetRunId(),
			Agent:       one.GetAgent(),
			TabID:       int(one.GetTabId()),
			TabTitle:    one.GetTabTitle(),
			TabURL:      one.GetTabUrl(),
			FavIconURL:  one.GetFaviconUrl(),
			IsRemote:    true,
		},
		Cancelled:       one.GetCancelled(),
		DNSUs:           phaseIn(one.HasDnsUs(), one.GetDnsUs()),
		ConnectUs:       phaseIn(one.HasConnectUs(), one.GetConnectUs()),
		TLSUs:           phaseIn(one.HasTlsUs(), one.GetTlsUs()),
		WaitUs:          phaseIn(one.HasWaitUs(), one.GetWaitUs()),
		DownloadUs:      phaseIn(one.HasDownloadUs(), one.GetDownloadUs()),
		RequestBytes:    one.GetRequestBytes(),
		ResponseBytes:   one.GetResponseBytes(),
		RequestHeaders:  headersIn(one.GetRequestHeaders()),
		ResponseHeaders: headersIn(one.GetResponseHeaders()),
		CookieWrites:    writesIn(one.GetCookieWrites()),
	}
}

// scriptIn converts one script report with everything it printed and asserted.
func scriptIn(report *history.ScriptRun) domain.ScriptRun {
	out := domain.ScriptRun{
		ID:         report.GetId(),
		NodeID:     report.GetNodeId(),
		Scope:      domain.ScriptScope(report.GetScope()),
		OK:         report.GetOk(),
		Error:      report.GetError(),
		DurationUs: report.GetDurationUs(),
	}
	if at := millisIn(report.GetCreatedAt()); at != nil {
		out.CreatedAt = *at
	}
	for _, line := range report.GetLogs() {
		out.Logs = append(out.Logs, domain.ScriptLog{Level: line.GetLevel(), Message: line.GetMessage()})
	}
	for _, test := range report.GetTests() {
		out.Tests = append(out.Tests, domain.TestResult{
			Name: test.GetName(), Passed: test.GetPassed(), Error: test.GetError(),
			DurationUs: test.GetDurationUs(),
		})
	}
	return out
}

// runIn converts a collection run with the rows it produced.
func runIn(run *history.Run) domain.CollectionRun {
	out := domain.CollectionRun{
		ID:           run.GetId(),
		CollectionID: run.GetCollectionId(),
		NodeID:       run.GetNodeId(),
		Environment:  run.GetEnvironment(),
		Passed:       int(run.GetPassed()),
		Failed:       int(run.GetFailed()),
		DurationUs:   run.GetDurationUs(),
		Results:      []domain.CollectionRunResult{},
	}
	if at := millisIn(run.GetStartedAt()); at != nil {
		out.StartedAt = *at
	}
	if at := millisIn(run.GetFinishedAt()); at != nil {
		out.FinishedAt = *at
	}
	for _, result := range run.GetResults() {
		row := domain.CollectionRunResult{
			NodeID:           result.GetNodeId(),
			Position:         int64(result.GetPosition()),
			OK:               result.GetOk(),
			DurationUs:       result.GetDurationUs(),
			Error:            result.GetError(),
			RecordID:         result.GetRecordId(),
			AssertionsPassed: int(result.GetAssertionsPassed()),
			AssertionsTotal:  int(result.GetAssertionsTotal()),
			Skipped:          result.GetSkipped(),
		}
		if result.HasStatus() {
			// A row whose status was never read is not a row that came back with a zero in it, and the
			// two draw differently: the first never reached a server, the second reached one.
			code := int(result.GetStatus())
			row.Status = &code
		}
		if failure := result.GetFailure(); failure != nil {
			row.Failure = &domain.Failure{
				Code: domain.Code(failure.GetCode()), Args: domain.Args(failure.GetArgs()),
			}
		}
		out.Results = append(out.Results, row)
	}
	return out
}

// phaseIn wraps a microsecond duration pointer if present in the protobuf payload.
func phaseIn(present bool, micros int64) *int64 {
	if !present {
		return nil
	}
	return &micros
}

// headersIn converts protobuf header pairs into a slice of domain HeaderPair models.
func headersIn(headers []*history.Header) []domain.HeaderPair {
	out := make([]domain.HeaderPair, 0, len(headers))
	for _, header := range headers {
		out = append(out, domain.HeaderPair{Name: header.GetName(), Value: header.GetValue()})
	}
	return out
}

// writesIn converts protobuf CookieWrite structures into domain CookieWrite models. A value the
// server withheld arrives flagged: what this device makes of it — a value of its own — is decided
// where the record is written.
func writesIn(writes []*history.CookieWrite) []domain.CookieWrite {
	out := make([]domain.CookieWrite, 0, len(writes))
	for _, write := range writes {
		out = append(out, domain.CookieWrite{
			Name:      write.GetName(),
			Value:     write.GetValue(),
			Domain:    write.GetDomain(),
			Path:      write.GetPath(),
			ExpiresAt: millisIn(write.GetExpiresAt()),
			Secure:    write.GetSecure(),
			HTTPOnly:  write.GetHttpOnly(),
			SameSite:  write.GetSameSite(),
			HostOnly:  write.GetHostOnly(),
			State:     domain.CookieState(write.GetState()),
			Previous:  jarCookieIn(write.GetPrevious()),

			Withheld:         write.GetValueWithheld(),
			PreviousWithheld: write.GetPrevious().GetValueWithheld(),
		})
	}
	return out
}

// jarCookieIn converts a protobuf JarCookie structure into a domain JarCookie model.
func jarCookieIn(cookie *history.JarCookie) *domain.JarCookie {
	if cookie == nil {
		return nil
	}
	return &domain.JarCookie{
		Name:      cookie.GetName(),
		Value:     cookie.GetValue(),
		Domain:    cookie.GetDomain(),
		Path:      cookie.GetPath(),
		ExpiresAt: millisIn(cookie.GetExpiresAt()),
		Secure:    cookie.GetSecure(),
		HTTPOnly:  cookie.GetHttpOnly(),
		SameSite:  cookie.GetSameSite(),
		HostOnly:  cookie.GetHostOnly(),
	}
}

// millisIn extracts unix millisecond timestamp pointer from protobuf Timestamp.
func millisIn(at *timestamppb.Timestamp) *int64 {
	if at == nil {
		return nil
	}
	millis := at.AsTime().UnixMilli()
	if millis == 0 {
		return nil
	}
	return &millis
}

// Fetched retrieves inline HTTP request and response payload bodies for a specific record.
func (c *Client) Fetched(
	ctx context.Context,
	caller domain.Caller,
	spaceID, recordID string,
) (domain.RecordFetched, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*history.GetResponse, error) {
			return history.NewHistoryServiceClient(conn).Get(ctx,
				history.GetRequest_builder{
					WorkspaceId:   spaceID,
					RecordId:      recordID,
					IncludeBodies: true,
				}.Build())
		})
	if err != nil {
		return domain.RecordFetched{}, err
	}

	fetched := domain.RecordFetched{Bodies: map[domain.BodySide]domain.BodyRef{}}
	for _, body := range answer.GetBodies() {
		fetched.Bodies[domain.BodySide(body.GetSide())] = domain.BodyRef{
			Inline:    string(body.GetContent()),
			Size:      body.GetSize(),
			Truncated: body.GetTruncated(),
		}
	}
	for _, report := range answer.GetScripts() {
		// The report arrives inside the record's answer and names no record of its own: it is this
		// one, and without saying so the lines would land here attached to nothing.
		one := scriptIn(report)
		one.RecordID = recordID
		fetched.Scripts = append(fetched.Scripts, one)
	}

	// The run comes with the record rather than in it: the record names the run it was sent by, and
	// the run is one document however many records name it. A run the server no longer holds is not a
	// reason to refuse the record — the two sides disagree about a run, and the record is still
	// readable.
	if runID := answer.GetRecord().GetRunId(); runID != "" {
		run, err := c.run(ctx, caller, spaceID, runID)
		switch {
		case err == nil:
			fetched.Run = &run
		case domain.CodeOf(err) != domain.CodeNotFound:
			return domain.RecordFetched{}, err
		}
	}
	return fetched, nil
}

// run reads one collection run and the rows it produced.
func (c *Client) run(
	ctx context.Context,
	caller domain.Caller,
	spaceID, runID string,
) (domain.CollectionRun, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*history.GetRunResponse, error) {
			return history.NewHistoryServiceClient(conn).GetRun(ctx,
				history.GetRunRequest_builder{WorkspaceId: spaceID, RunId: runID}.Build())
		})
	if err != nil {
		return domain.CollectionRun{}, err
	}
	return runIn(answer.GetRun()), nil
}
