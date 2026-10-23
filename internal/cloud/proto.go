package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"schemo/internal/db"
	"schemo/internal/domain"
	"time"

	historyv1 "github.com/schemo-app/proto/go/history/v1"
	syncv1 "github.com/schemo-app/proto/go/sync/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The log: a batch of edits in, and everything this device missed out.
//
// The data is bytes here and stays bytes: which fields a collection has is the store's business.

// Push sends a batch of edits and answers one verdict per entity.
func (c *Client) Push(
	ctx context.Context,
	caller domain.Caller,
	workspaceID string,
	entities []domain.Pushed,
) ([]domain.Verdict, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*syncv1.PushResponse, error) {
			return syncv1.NewSyncServiceClient(conn).Push(ctx, syncv1.PushRequest_builder{
				WorkspaceId: workspaceID,
				Entities:    pushable(entities),
			}.Build())
		})
	if err != nil {
		return nil, err
	}

	out := make([]domain.Verdict, 0, len(answer.GetResults()))
	for _, result := range answer.GetResults() {
		verdict, err := verdictOf(result)
		if err != nil {
			return nil, err
		}
		out = append(out, verdict)
	}
	return out, nil
}

// Pull reads the changes above a cursor.
func (c *Client) Pull(
	ctx context.Context,
	caller domain.Caller,
	workspaceID string,
	cursor int64,
	limit int,
) (domain.Page, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*syncv1.PullResponse, error) {
			return syncv1.NewSyncServiceClient(conn).Pull(ctx, syncv1.PullRequest_builder{
				WorkspaceId: workspaceID,
				AfterSeq:    cursor,
				Limit:       int32(limit),
			}.Build())
		})
	if err != nil {
		return domain.Page{}, err
	}

	page := domain.Page{
		Cursor:  answer.GetNextSeq(),
		HasMore: answer.GetHasMore(),
		Resync:  answer.GetResyncRequired(),
	}
	for _, change := range answer.GetChanges() {
		one, err := changeOf(change)
		if err != nil {
			return domain.Page{}, err
		}
		// Where the change landed is not filled in here: this side knows the workspace by the id the
		// server gave it, and the local workspace the page is applied to is the engine's to name.
		page.Changes = append(page.Changes, one)
	}
	return page, nil
}

// pushable is a batch as the contract carries it.
func pushable(entities []domain.Pushed) []*syncv1.SyncEntity {
	out := make([]*syncv1.SyncEntity, 0, len(entities))
	for _, pushed := range entities {
		out = append(out, entityData(pushed.OperationID, pushed.Entity))
	}
	return out
}

// entityData is one row on the wire.
func entityData(operationID string, entity domain.SyncEntity) *syncv1.SyncEntity {
	return syncv1.SyncEntity_builder{
		Kind:        entityTypeOut(entity.Kind),
		Id:          entity.ID,
		OperationId: operationID,
		Version:     int64(entity.Version),
		Deleted:     entity.Deleted,
		ParentId:    entity.Parent,
		Name:        entity.Name,
		Position:    entity.Position,
		Data:        entity.Data,
		Secrets:     entity.Secrets,
		Withheld:    entity.Withheld,
		CreatedAt:   toTimestamp(entity.CreatedAt),
		UpdatedAt:   toTimestamp(entity.UpdatedAt),
	}.Build()
}

// verdictOf is what became of one entity, in the app's own words.
func verdictOf(result *syncv1.PushResult) (domain.Verdict, error) {
	verdict := domain.Verdict{
		EntityID: result.GetEntityId(),
		Version:  int32(result.GetNewVersion()),
	}

	switch result.GetStatus() {
	case syncv1.PushStatus_PUSH_STATUS_ACCEPTED:
		verdict.Status = domain.PushStored
	case syncv1.PushStatus_PUSH_STATUS_DUPLICATE:
		// The operation was applied a while ago and the answer was lost on the way back. What this
		// device has is what the server holds, and the version it answers is the one to keep.
		verdict.Status = domain.PushDuplicate
	case syncv1.PushStatus_PUSH_STATUS_CONFLICT_RESOLVED:
		verdict.Status = domain.PushSuperseded
		verdict.Version = int32(result.GetNewVersion())
		if winner := result.GetWinner(); winner != nil {
			entity, err := entityOf(winner)
			if err != nil {
				return domain.Verdict{}, err
			}
			verdict.Winner = &entity
			verdict.Version = entity.Version
		}
	default:
		verdict.Status = domain.PushRefused
		verdict.Reason = reasonOf(result.GetRejectReason())
	}
	return verdict, nil
}

// entityOf is a row as the server holds it.
func entityOf(whole *syncv1.SyncEntity) (domain.SyncEntity, error) {
	return domain.SyncEntity{
		Kind:      entityTypeIn(whole.GetKind()),
		ID:        whole.GetId(),
		Parent:    whole.GetParentId(),
		Name:      whole.GetName(),
		Position:  whole.GetPosition(),
		Data:      whole.GetData(),
		Secrets:   whole.GetSecrets(),
		Withheld:  whole.GetWithheld(),
		Version:   int32(whole.GetVersion()),
		Deleted:   whole.GetDeleted(),
		CreatedAt: millis(whole.GetCreatedAt()),
		UpdatedAt: millis(whole.GetUpdatedAt()),
	}, nil
}

// changeOf is one row of the log: where it landed and the row whole. A tombstone is a row with
// is_deleted set, and the row carries its own kind and id even when it has no payload.
func changeOf(change *syncv1.SyncChange) (domain.SyncChange, error) {
	entity, err := entityOf(change.GetEntity())
	if err != nil {
		return domain.SyncChange{}, err
	}
	return domain.SyncChange{Seq: change.GetSeq(), Entity: entity}, nil
}

func entityTypeOut(kind domain.EntityType) syncv1.EntityType {
	switch kind {
	case domain.EntityWorkspace:
		return syncv1.EntityType_ENTITY_TYPE_WORKSPACE
	case domain.EntityCollection:
		return syncv1.EntityType_ENTITY_TYPE_COLLECTION
	case domain.EntityRequest:
		return syncv1.EntityType_ENTITY_TYPE_REQUEST
	case domain.EntityEnvironment:
		return syncv1.EntityType_ENTITY_TYPE_ENVIRONMENT
	case domain.EntityVariable:
		return syncv1.EntityType_ENTITY_TYPE_VARIABLE
	case domain.EntityCookie:
		return syncv1.EntityType_ENTITY_TYPE_COOKIE
	case domain.EntityJarDomain:
		return syncv1.EntityType_ENTITY_TYPE_JAR_DOMAIN
	default:
		return syncv1.EntityType_ENTITY_TYPE_UNSPECIFIED
	}
}

func entityTypeIn(kind syncv1.EntityType) domain.EntityType {
	switch kind {
	case syncv1.EntityType_ENTITY_TYPE_WORKSPACE:
		return domain.EntityWorkspace
	case syncv1.EntityType_ENTITY_TYPE_COLLECTION:
		return domain.EntityCollection
	case syncv1.EntityType_ENTITY_TYPE_REQUEST:
		return domain.EntityRequest
	case syncv1.EntityType_ENTITY_TYPE_ENVIRONMENT:
		return domain.EntityEnvironment
	case syncv1.EntityType_ENTITY_TYPE_VARIABLE:
		return domain.EntityVariable
	case syncv1.EntityType_ENTITY_TYPE_COOKIE:
		return domain.EntityCookie
	case syncv1.EntityType_ENTITY_TYPE_JAR_DOMAIN:
		return domain.EntityJarDomain
	default:
		return ""
	}
}

// reasonOf is why a row was refused, in the contract's own words: what the window words a sentence
// from, and what the engine decides between trying again and giving up on.
func reasonOf(reason syncv1.PushRejectReason) domain.PushReason {
	switch reason {
	case syncv1.PushRejectReason_PUSH_REJECT_REASON_PARENT_NOT_FOUND:
		return domain.ReasonParentMissing
	case syncv1.PushRejectReason_PUSH_REJECT_REASON_QUOTA_EXCEEDED:
		return domain.ReasonQuotaExceeded
	case syncv1.PushRejectReason_PUSH_REJECT_REASON_INVALID_DATA:
		return domain.ReasonInvalidData
	case syncv1.PushRejectReason_PUSH_REJECT_REASON_FORBIDDEN:
		return domain.ReasonForbidden
	case syncv1.PushRejectReason_PUSH_REJECT_REASON_TOO_LARGE:
		return domain.ReasonTooLarge
	default:
		return domain.ReasonUnspecified
	}
}

// toTimestamp is a toTimestamp this app keeps in Unix milliseconds, as the contract carries it. A zero is
// left out rather than sent as 1970.
func toTimestamp(millis int64) *timestamppb.Timestamp {
	if millis == 0 {
		return nil
	}
	return timestamppb.New(time.UnixMilli(millis))
}

func millis(at *timestamppb.Timestamp) int64 {
	if at == nil {
		return 0
	}
	return at.AsTime().UnixMilli()
}

// PullRecords fetches a page of history metadata changes for a workspace after a sequence number.
func (c *Client) PullRecords(
	ctx context.Context,
	caller domain.Caller,
	workspaceID string,
	afterSeq int64,
	limit int,
) (domain.RecordPage, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*historyv1.PullRecordsResponse, error) {
			return historyv1.NewHistoryServiceClient(conn).PullRecords(ctx,
				historyv1.PullRecordsRequest_builder{
					WorkspaceId: workspaceID,
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
			Seq: change.GetSeq(), RecordID: change.GetDeletedRecordId(), Deleted: change.HasDeletedRecordId(),
		}
		if !change.HasDeletedRecordId() {
			one.Record = recordIn(workspaceID, change.GetRecord())
			one.RecordID = change.GetRecord().GetId()
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
	workspaceID string,
	from, to int64,
	limit int,
	token string,
) (domain.RecordList, error) {
	request := historyv1.ListRequest_builder{
		WorkspaceId: workspaceID, PageToken: token, PageSize: int32(limit),
	}
	if from > 0 {
		request.From = timestamppb.New(time.UnixMilli(from))
	}
	if to > 0 {
		request.To = timestamppb.New(time.UnixMilli(to))
	}
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*historyv1.ListResponse, error) {
			return historyv1.NewHistoryServiceClient(conn).List(ctx, request.Build())
		})
	if err != nil {
		return domain.RecordList{}, err
	}

	list := domain.RecordList{Head: answer.GetHeadSeq(), Next: answer.GetNextPageToken()}
	for _, one := range answer.GetRecords() {
		list.Records = append(list.Records, recordIn(workspaceID, one))
	}
	return list, nil
}

// Sent appends history to the remote workspace and returns a verdict for each record and each run.
// They travel in one call because they are one document set: a record names the run it was sent by,
// and a run names back the records it produced.
//
// A payload this build cannot read is refused rather than skipped: the caller is owed an answer
// about it, and a body that quietly disappeared between two devices would go unnoticed.
func (c *Client) Sent(
	ctx context.Context,
	caller domain.Caller,
	workspaceID string,
	records, runs []domain.Pushed,
) ([]domain.RecordVerdict, []domain.RecordVerdict, error) {
	entries := make([]*historyv1.RecordEntry, 0, len(records))
	for _, record := range records {
		entry := &historyv1.RecordEntry{}
		if err := proto.Unmarshal(record.Entity.Data, entry); err != nil {
			return nil, nil, fmt.Errorf("reading record %s: %w", record.Entity.ID, err)
		}
		entries = append(entries, entry)
	}

	held := make([]*historyv1.Run, 0, len(runs))
	for _, run := range runs {
		one := &historyv1.Run{}
		if err := proto.Unmarshal(run.Entity.Data, one); err != nil {
			return nil, nil, fmt.Errorf("reading run %s: %w", run.Entity.ID, err)
		}
		held = append(held, one)
	}

	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*historyv1.AppendResponse, error) {
			return historyv1.NewHistoryServiceClient(conn).Append(ctx,
				historyv1.AppendRequest_builder{
					WorkspaceId: workspaceID,
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
func verdictsOf(results []*historyv1.AppendResult) []domain.RecordVerdict {
	out := make([]domain.RecordVerdict, 0, len(results))
	for _, result := range results {
		out = append(out, domain.RecordVerdict{
			RecordID: result.GetId(),
			Reason:   recordReasonOf(result.GetRefusal()),
		})
	}
	return out
}

// Retract requests deletion of specific history records from the server by their IDs.
func (c *Client) Retract(
	ctx context.Context,
	caller domain.Caller,
	workspaceID string,
	ids []string,
) (int, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*historyv1.DeleteResponse, error) {
			return historyv1.NewHistoryServiceClient(conn).Delete(ctx,
				historyv1.DeleteRequest_builder{
					WorkspaceId: workspaceID,
					RecordIds:   ids,
				}.Build())
		})
	if err != nil {
		return 0, err
	}
	return int(answer.GetDeleted()), nil
}

// recordReasonOf maps protobuf push refusal reason strings to domain PushReason types.
func recordReasonOf(reason historyv1.AppendRefusal) domain.PushReason {
	switch reason {
	case historyv1.AppendRefusal_APPEND_REFUSAL_UNSPECIFIED:
		return ""
	case historyv1.AppendRefusal_APPEND_REFUSAL_TOO_LARGE:
		return domain.ReasonTooLarge
	case historyv1.AppendRefusal_APPEND_REFUSAL_FORBIDDEN:
		return domain.ReasonForbidden
	case historyv1.AppendRefusal_APPEND_REFUSAL_EMPTY:
		return domain.ReasonEmpty
	}
	return domain.ReasonUnspecified
}

// recordIn reads a record the server sent: the envelope, and the document under it. A document this
// build cannot read leaves the envelope standing rather than losing the record from the list.
func recordIn(workspaceID string, one *historyv1.Record) domain.Record {
	record, _ := domain.RecordFromDocument(one.GetData(), one.GetWithheld())
	record.ID = one.GetId()
	record.WorkspaceID = workspaceID
	record.Source = domain.RecordSource(db.RecordSourceIn(one.GetSource()))
	record.NodeID = one.GetNodeId()
	record.RunID = one.GetRunId()
	if at := one.GetStartedAt(); at != nil {
		record.StartedAt = at.AsTime().UnixMilli()
	}
	record.IsRemote = true
	return record
}

// runIn reads a collection run with the rows it produced.
func runIn(run *historyv1.Run) domain.CollectionRun {
	var out domain.CollectionRun
	_ = json.Unmarshal(run.GetData(), &out)
	out.ID = run.GetId()
	out.CollectionID = run.GetCollectionId()
	if at := run.GetStartedAt(); at != nil {
		out.StartedAt = at.AsTime().UnixMilli()
	}
	if out.Results == nil {
		out.Results = []domain.CollectionRunResult{}
	}
	return out
}

// Fetched retrieves inline HTTP request and response payload bodies for a specific record.
func (c *Client) Fetched(
	ctx context.Context,
	caller domain.Caller,
	workspaceID, recordID string,
) (domain.RecordFetched, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*historyv1.GetResponse, error) {
			return historyv1.NewHistoryServiceClient(conn).Get(ctx,
				historyv1.GetRequest_builder{
					WorkspaceId:   workspaceID,
					RecordId:      recordID,
					IncludeBodies: true,
				}.Build())
		})
	if err != nil {
		return domain.RecordFetched{}, err
	}

	fetched := domain.RecordFetched{Bodies: map[domain.BodySide]domain.BodyRef{}}
	for _, body := range answer.GetBodies() {
		fetched.Bodies[domain.BodySide(db.BodySideIn(body.GetSide()))] = domain.BodyRef{
			Inline:    string(body.GetContent()),
			Size:      body.GetSize(),
			Truncated: body.GetTruncated(),
		}
	}
	if scripts := answer.GetScripts(); len(scripts) > 0 {
		if err := json.Unmarshal(scripts, &fetched.Scripts); err != nil {
			return domain.RecordFetched{}, fmt.Errorf("reading the reports of %s: %w", recordID, err)
		}
		// The reports arrive inside the record's answer and name no record of their own: it is this
		// one, and without saying so the lines would land here attached to nothing.
		for i := range fetched.Scripts {
			fetched.Scripts[i].RecordID = recordID
		}
	}

	// The run comes with the record rather than in it: the run is one document however many records
	// name it. A run the server no longer holds is no reason to refuse the record, which is still
	// readable.
	if runID := answer.GetRecord().GetRunId(); runID != "" {
		run, err := c.run(ctx, caller, workspaceID, runID)
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
	workspaceID, runID string,
) (domain.CollectionRun, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*historyv1.GetRunResponse, error) {
			return historyv1.NewHistoryServiceClient(conn).GetRun(ctx,
				historyv1.GetRunRequest_builder{WorkspaceId: workspaceID, RunId: runID}.Build())
		})
	if err != nil {
		return domain.CollectionRun{}, err
	}
	return runIn(answer.GetRun()), nil
}
