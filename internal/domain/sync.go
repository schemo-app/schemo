package domain

// EntityType defines sync entity categories in contract notation (sync/v1.EntityType).
type EntityType string

const (
	EntityWorkspace   EntityType = "workspace"
	EntityCollection  EntityType = "collection"
	EntityRequest     EntityType = "request"
	EntityEnvironment EntityType = "environment"
	EntityVariable    EntityType = "variable"
	EntityCookie      EntityType = "cookie"
	EntityJarDomain   EntityType = "jar_domain"
	// EntityRecord represents history items pushed via history/v1 endpoint.
	EntityRecord EntityType = "record"
	// EntityRun represents a collection run pushed via the same endpoint, once by its id however many
	// records name it. It is kept out of EntityTypes: that order is the sync endpoint's, and a run
	// goes through history beside the records it produced.
	EntityRun EntityType = "run"
)

// ThroughHistory reports whether an entity travels through history/v1 rather than the sync push.
func ThroughHistory(kind EntityType) bool {
	return kind == EntityRecord || kind == EntityRun
}

// EntityTypes defines topological order required for sync push operations.
var EntityTypes = []EntityType{
	EntityWorkspace, EntityCollection, EntityEnvironment, EntityRequest, EntityVariable,
	EntityCookie, EntityJarDomain,
}

// Depth returns dependency hierarchy depth for the given EntityType.
func Depth(kind EntityType) int {
	for depth, one := range EntityTypes {
		if one == kind {
			return depth
		}
	}
	return len(EntityTypes)
}

// Action represents local mutation state before sync.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// SyncState defines current synchronization state of a space.
type SyncState string

const (
	SyncLocal  SyncState = "local"
	SyncSynced SyncState = "synced"
	SyncPaused SyncState = "paused"
)

// SyncSpace contains synchronization metadata for a local workspace.
type SyncSpace struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Color         string        `json:"color,omitempty"`
	Kind          WorkspaceKind `json:"kind,omitempty"`
	RemoteID      string        `json:"remoteId"`
	RemoteAccount string        `json:"remoteAccount,omitempty"`
	State         SyncState     `json:"state"`
	Cursor        int64         `json:"cursor"`
	HistoryCursor int64         `json:"historyCursor"`
	SyncedAt      int64         `json:"syncedAt"`
	Failed        Code          `json:"failed,omitempty"`
	Attempts      int           `json:"attempts"`
	// LinkPending indicates space creation confirmation is in flight.
	LinkPending bool `json:"linkPending,omitempty"`
	// Gone indicates remote space was deleted or unassigned.
	Gone bool `json:"gone,omitempty"`
	// Live indicates active SSE/WebSocket stream status.
	Live bool `json:"live"`
	// ReadOnly is whether the account last said this person may not write in the space: a round does
	// not push into one that would only refuse it. The window reads the whole answer from the
	// workspace, and the zero value here is the careful direction for a sync, a writer's.
	ReadOnly bool `json:"-"`
}

// Exchangeable reports whether the space is linked and active for sync operations.
func (s SyncSpace) Exchangeable() bool {
	return s.RemoteID != "" && s.State != SyncPaused
}

// SyncEntity represents wire transport unit carrying metadata and opaque payload.
type SyncEntity struct {
	Kind      EntityType
	ID        string
	Name      string
	Payload   []byte
	SortOrder int64
	CreatedAt int64
	UpdatedAt int64
	Version   int32
	Deleted   bool
}

// SpaceCursor tracks local content and history sequence numbers for a space.
type SpaceCursor struct {
	SpaceID string
	Content int64
	History int64
}

// Poke represents lightweight notification of remote space sequence progress.
type Poke struct {
	SpaceID string
	Content int64
	History int64
}

// Signal wraps stream events regarding sequence progress or workspace list changes.
type Signal struct {
	Poke
	SpacesChanged bool
}

// SyncChange represents an incoming content log entry from the server.
type SyncChange struct {
	SpaceID string
	Seq     int64
	Entity  SyncEntity
}

// RecordChange represents an incoming history log entry.
type RecordChange struct {
	Seq      int64
	RecordID string
	Deleted  bool
	Record   Record
	// Run is the collection run the entry carries, and it is an entry of its own: a run is written
	// once by its id however many records name it, so the log brings it alone, with no record beside
	// it and nothing taken back.
	Run *CollectionRun
}

// RecordPage represents a paginated history log result.
type RecordPage struct {
	Changes []RecordChange
	Cursor  int64
	HasMore bool
	Floor   int64
	Head    int64
}

// RecordList represents a chronological snapshot page of history records.
type RecordList struct {
	Records []Record
	Head    int64
	Next    string
}

// PendingChange wraps an outbound queue item with current entity payload.
type PendingChange struct {
	QueueID     string
	SpaceID     string
	Action      Action
	OperationID string
	Attempts    int
	Entity      SyncEntity
}

// PushStatus represents the processing verdict of a single pushed entity.
type PushStatus string

const (
	PushStored     PushStatus = "stored"
	PushDuplicate  PushStatus = "duplicate"
	PushSuperseded PushStatus = "superseded"
	PushRefused    PushStatus = "refused"
)

// PushReason identifies server-side push rejection causes.
type PushReason string

const (
	ReasonParentMissing PushReason = "parentNotFound"
	ReasonQuotaExceeded PushReason = "quotaExceeded"
	ReasonIDConflict    PushReason = "idConflict"
	ReasonForbidden     PushReason = "forbidden"
	ReasonTooLarge      PushReason = "tooLarge"
	ReasonEmpty         PushReason = "empty"
	ReasonUnspecified   PushReason = "unspecified"
)

// ArgTooLarge is what the server's refusal for size says in the args of the refusal. The call
// was bigger than the server takes, and the call is what was refused rather than a row, so there is
// nothing else to name — a caller can only act on it when the batch is a single document.
const ArgTooLarge = "pushTooLarge"

// RefusedForSize reports whether a refusal is the server's answer to a call bigger than it takes.
func RefusedForSize(err error) bool {
	failure := AsFailure(err)
	return failure != nil && failure.Code == CodeServerRefused &&
		failure.Args["reason"] == ArgTooLarge
}

// Verdict contains processing status and optional winner state for a pushed entity.
type Verdict struct {
	EntityID string
	Status   PushStatus
	Version  int32
	Winner   *SyncEntity
	Reason   PushReason
}

// Pushed maps an outbound SyncEntity to its idempotent operation ID.
type Pushed struct {
	OperationID string
	Entity      SyncEntity
}

// Page represents a page of pull changes from the server.
type Page struct {
	Changes []SyncChange
	Cursor  int64
	HasMore bool
	Resync  bool
}

// RecordVerdict represents server acceptance status for a history record.
type RecordVerdict struct {
	RecordID string
	Reason   PushReason
}

// Limits defines server-enforced request sizes and retention configuration.
type Limits struct {
	MaxBodyBytes  int64
	MaxPushBytes  int64
	RetentionDays int32
}

// RemoteSpace represents workspace metadata provided by the server contract.
type RemoteSpace struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Colour  string        `json:"colour,omitempty"`
	Version int32         `json:"version"`
	OwnOrg  bool          `json:"ownOrg,omitempty"`
	OrgID   string        `json:"orgId,omitempty"`
	Kind    WorkspaceKind `json:"kind,omitempty"`
	// Access is what the account says this person may do here, on every row it lists.
	Access WorkspaceAccess `json:"access"`
}

// AccountSpaces holds accessible remote workspaces and user's primary organization ID.
type AccountSpaces struct {
	Spaces   []RemoteSpace `json:"spaces"`
	OwnOrgID string        `json:"ownOrgId,omitempty"`
}

// Caller carries API endpoint configuration and client credentials for sync requests.
type Caller struct {
	Address   string
	Access    string
	SessionID string
}

// SyncCounts aggregates sync queue state counts for UI display.
type SyncCounts struct {
	Pending int `json:"pending"`
	Sending int `json:"sending"`
	Failed  int `json:"failed"`
	Parked  int `json:"parked"`
}

// QueueStatus defines outbox queue item processing state.
type QueueStatus string

const (
	QueuePending QueueStatus = "pending"
	QueueSending QueueStatus = "sending"
	QueueFailed  QueueStatus = "failed"
	QueueParked  QueueStatus = "parked"
)

// QueuedChange represents UI-friendly outbox entry description.
type QueuedChange struct {
	Kind     EntityType  `json:"kind"`
	EntityID string      `json:"entityId"`
	Action   Action      `json:"action"`
	Status   QueueStatus `json:"status"`
	Attempts int         `json:"attempts"`
	Name     string      `json:"name,omitempty"`
	Reason   string      `json:"reason,omitempty"`
	RetryAt  int64       `json:"retryAt,omitempty"`
}
