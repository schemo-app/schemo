package server

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/schemo-app/proto/go/sync/v1"

	"schemo/internal/domain"
)

// The log: a batch of edits in, and everything this device missed out.
//
// The payload is bytes here and stays bytes: which fields a collection has is the store's business,
// and this package puts them into the oneof arm the entity's kind says and never looks inside.

// Push sends a batch of edits and answers one verdict per entity.
func (c *Client) Push(
	ctx context.Context,
	caller domain.Caller,
	spaceID string,
	entities []domain.Pushed,
) ([]domain.Verdict, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*sync.PushResponse, error) {
			return sync.NewSyncServiceClient(conn).Push(ctx, sync.PushRequest_builder{
				WorkspaceId: spaceID,
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
	spaceID string,
	cursor int64,
	limit int,
) (domain.Page, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*sync.PullResponse, error) {
			return sync.NewSyncServiceClient(conn).Pull(ctx, sync.PullRequest_builder{
				WorkspaceId: spaceID,
				LastSyncSeq: cursor,
				Limit:       int32(limit),
			}.Build())
		})
	if err != nil {
		return domain.Page{}, err
	}

	page := domain.Page{
		Cursor:  answer.GetNextSyncSeq(),
		HasMore: answer.GetHasMore(),
		Resync:  answer.GetResyncRequired(),
	}
	for _, change := range answer.GetChanges() {
		one, err := changeOf(change)
		if err != nil {
			return domain.Page{}, err
		}
		// Where the change landed is not filled in here: this side knows the space by the id the
		// server gave it, and the local space the page is applied to is the engine's to name.
		page.Changes = append(page.Changes, one)
	}
	return page, nil
}

// pushable is a batch as the contract carries it: the envelope on the outside, and the payload put
// into the arm its kind names.
func pushable(entities []domain.Pushed) []*sync.SyncEntity {
	out := make([]*sync.SyncEntity, 0, len(entities))
	for _, pushed := range entities {
		out = append(out, entityData(pushed.OperationID, pushed.Entity))
	}
	return out
}

// entityData is one row on the wire. The payload is carried in the arm its kind says, which is what
// makes the field numbers of a payload permanent: a server reads the one it was sent.
//
// A payload that cannot be read as the kind it claims is sent as the envelope alone rather than
// refused here. That is deliberate: a row is worth more arriving without its content than not
// arriving at all, and the server's own validation is the one that decides.
func entityData(operationID string, entity domain.SyncEntity) *sync.SyncEntity {
	builder := sync.SyncEntity_builder{
		EntityType:  entityTypeOut(entity.Kind),
		EntityId:    entity.ID,
		OperationId: operationID,
		Version:     int64(entity.Version),
		IsDeleted:   entity.Deleted,
		SortOrder:   int32(entity.SortOrder),
		CreatedAt:   moment(entity.CreatedAt),
		UpdatedAt:   moment(entity.UpdatedAt),
	}

	switch entity.Kind {
	case domain.EntityWorkspace:
		builder.Workspace = &sync.WorkspaceData{}
		decode(entity.Payload, builder.Workspace)
	case domain.EntityCollection:
		builder.Collection = &sync.CollectionData{}
		decode(entity.Payload, builder.Collection)
	case domain.EntityRequest:
		builder.Request = &sync.RequestData{}
		decode(entity.Payload, builder.Request)
	case domain.EntityEnvironment:
		builder.Environment = &sync.EnvironmentData{}
		decode(entity.Payload, builder.Environment)
	case domain.EntityVariable:
		builder.Variable = &sync.VariableData{}
		decode(entity.Payload, builder.Variable)
	case domain.EntityCookie:
		builder.Cookie = &sync.CookieData{}
		decode(entity.Payload, builder.Cookie)
	case domain.EntityJarDomain:
		builder.JarDomain = &sync.JarDomainData{}
		decode(entity.Payload, builder.JarDomain)
	}
	return builder.Build()
}

func decode(payload []byte, into proto.Message) {
	// Best effort on purpose: what a payload this build cannot read is, is the server's to refuse, and
	// the envelope beside it still says which row changed.
	_ = proto.Unmarshal(payload, into)
}

// verdictOf is what became of one entity, in the app's own words.
func verdictOf(result *sync.PushResult) (domain.Verdict, error) {
	verdict := domain.Verdict{
		EntityID: result.GetEntityId(),
		Version:  int32(result.GetNewVersion()),
	}

	switch result.GetStatus() {
	case sync.PushStatus_PUSH_STATUS_ACCEPTED:
		verdict.Status = domain.PushStored
	case sync.PushStatus_PUSH_STATUS_DUPLICATE:
		// The operation was applied a while ago and the answer was lost on the way back. What this
		// device has is what the server holds, and the version it answers is the one to keep.
		verdict.Status = domain.PushDuplicate
	case sync.PushStatus_PUSH_STATUS_CONFLICT_RESOLVED:
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

// entityOf is the other direction: a row as the server holds it, with its payload put back together
// from the arm it arrived in.
func entityOf(whole *sync.SyncEntity) (domain.SyncEntity, error) {
	entity := domain.SyncEntity{
		Kind:      entityTypeIn(whole.GetEntityType()),
		ID:        whole.GetEntityId(),
		Version:   int32(whole.GetVersion()),
		Deleted:   whole.GetIsDeleted(),
		SortOrder: int64(whole.GetSortOrder()),
		CreatedAt: millis(whole.GetCreatedAt()),
		UpdatedAt: millis(whole.GetUpdatedAt()),
	}

	var payload proto.Message
	switch {
	case whole.GetWorkspace() != nil:
		payload = whole.GetWorkspace()
	case whole.GetCollection() != nil:
		payload = whole.GetCollection()
	case whole.GetRequest() != nil:
		payload = whole.GetRequest()
	case whole.GetEnvironment() != nil:
		payload = whole.GetEnvironment()
	case whole.GetVariable() != nil:
		payload = whole.GetVariable()
	case whole.GetCookie() != nil:
		payload = whole.GetCookie()
	case whole.GetJarDomain() != nil:
		payload = whole.GetJarDomain()
	}
	if payload == nil {
		return entity, nil
	}

	encoded, err := proto.Marshal(payload)
	if err != nil {
		return domain.SyncEntity{}, err
	}
	entity.Payload = encoded
	return entity, nil
}

// changeOf is one row of the log: where it landed, and the row whole. What happened to it is not
// read from anywhere else — a tombstone is a row with is_deleted set — and the row carries its own
// kind and id even when it is a deletion and has no payload.
func changeOf(change *sync.SyncChange) (domain.SyncChange, error) {
	entity, err := entityOf(change.GetEntity())
	if err != nil {
		return domain.SyncChange{}, err
	}
	return domain.SyncChange{Seq: change.GetSyncSeq(), Entity: entity}, nil
}

func entityTypeOut(kind domain.EntityType) sync.EntityType {
	switch kind {
	case domain.EntityWorkspace:
		return sync.EntityType_ENTITY_TYPE_WORKSPACE
	case domain.EntityCollection:
		return sync.EntityType_ENTITY_TYPE_COLLECTION
	case domain.EntityRequest:
		return sync.EntityType_ENTITY_TYPE_REQUEST
	case domain.EntityEnvironment:
		return sync.EntityType_ENTITY_TYPE_ENVIRONMENT
	case domain.EntityVariable:
		return sync.EntityType_ENTITY_TYPE_VARIABLE
	case domain.EntityCookie:
		return sync.EntityType_ENTITY_TYPE_COOKIE
	case domain.EntityJarDomain:
		return sync.EntityType_ENTITY_TYPE_JAR_DOMAIN
	default:
		return sync.EntityType_ENTITY_TYPE_UNSPECIFIED
	}
}

func entityTypeIn(kind sync.EntityType) domain.EntityType {
	switch kind {
	case sync.EntityType_ENTITY_TYPE_WORKSPACE:
		return domain.EntityWorkspace
	case sync.EntityType_ENTITY_TYPE_COLLECTION:
		return domain.EntityCollection
	case sync.EntityType_ENTITY_TYPE_REQUEST:
		return domain.EntityRequest
	case sync.EntityType_ENTITY_TYPE_ENVIRONMENT:
		return domain.EntityEnvironment
	case sync.EntityType_ENTITY_TYPE_VARIABLE:
		return domain.EntityVariable
	case sync.EntityType_ENTITY_TYPE_COOKIE:
		return domain.EntityCookie
	case sync.EntityType_ENTITY_TYPE_JAR_DOMAIN:
		return domain.EntityJarDomain
	default:
		return ""
	}
}

// reasonOf is why a row was refused, in the contract's own words: what the window words a sentence
// from, and what the engine decides between trying again and giving up on.
func reasonOf(reason sync.PushRejectReason) domain.PushReason {
	switch reason {
	case sync.PushRejectReason_PUSH_REJECT_REASON_PARENT_NOT_FOUND:
		return domain.ReasonParentMissing
	case sync.PushRejectReason_PUSH_REJECT_REASON_QUOTA_EXCEEDED:
		return domain.ReasonQuotaExceeded
	case sync.PushRejectReason_PUSH_REJECT_REASON_ID_CONFLICT:
		return domain.ReasonIDConflict
	case sync.PushRejectReason_PUSH_REJECT_REASON_FORBIDDEN:
		return domain.ReasonForbidden
	case sync.PushRejectReason_PUSH_REJECT_REASON_TOO_LARGE:
		return domain.ReasonTooLarge
	default:
		return domain.ReasonUnspecified
	}
}

// moment is a moment this app keeps in Unix milliseconds, as the contract carries it. A zero is
// left out rather than sent as 1970: a row nobody has a moment for is not one that happened at the
// epoch.
func moment(millis int64) *timestamppb.Timestamp {
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
