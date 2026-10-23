package domain

import (
	"context"
	"strings"
	"time"
)

// Principal is who this device is signed in as: the token every call to the cloud carries, and who
// the person is. An account that is gone is the answer, not a question to keep asking.
type Principal interface {
	Caller(ctx context.Context) (Caller, error)
	Me(ctx context.Context) (Account, error)
}

type Account struct {
	Server     string    `json:"server"`
	UserID     string    `json:"userId"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	SessionID  string    `json:"sessionId"`
	SignedInAt time.Time `json:"signedInAt"`
}

func AccountKey(address, email string) string {
	server := strings.ToLower(strings.TrimRight(strings.TrimSpace(address), "/"))
	return server + "|" + strings.ToLower(strings.TrimSpace(email))
}

type Identity struct {
	Key    string
	UserID string
}

func (a Account) SignedIn() bool { return a.UserID != "" }

type Device struct {
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	App        string `json:"app"`
	AppVersion string `json:"appVersion"`
}

type Session struct {
	ID         string    `json:"id"`
	Device     Device    `json:"device"`
	IP         string    `json:"ip,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	IsCurrent  bool      `json:"isCurrent"`
}

type Tokens struct {
	Access    string
	Refresh   string
	ExpiresAt time.Time
}

type Challenge struct {
	UserCode  string    `json:"userCode"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Plan is what an organisation is entitled to, and how full those entitlements are.
type Plan struct {
	Tier      string         `json:"tier"`
	Seats     int            `json:"seats"`
	PlanUntil int64          `json:"planUntil,omitempty"`
	Features  []string       `json:"features,omitempty"`
	Quotas    map[string]int `json:"quotas,omitempty"`
	// Meters are the limits with something standing against them — what the window draws as bars.
	// A quota with nothing counted against it is a number without a meaning for a person to read.
	Meters []Meter `json:"meters,omitempty"`
}

// Meter is one resource against its cap. Kind is the app's own name for the resource; mapping the
// server's quota vocabulary onto it happens where the contract is read.
type Meter struct {
	Kind  string `json:"kind"`
	Used  int    `json:"used"`
	Limit int    `json:"limit"`
}

// Publication is a collection published at a public address, as this device remembers it. It is a
// copy: the server issues the address and keeps the page, and this is its last answer, so a window
// can draw a published collection before asking anything.
type Publication struct {
	Options      *PublicationOptions `json:"options,omitempty"`
	CollectionID string              `json:"collectionId"`

	// WorkspaceID is the workspace the collection lives in, which is where the window's list of pages
	// asks for it.
	WorkspaceID string `json:"workspaceId,omitempty"`

	// ID is the server's row. Empty until a publish has been answered.
	ID string `json:"id,omitempty"`

	// Slug is the last segment of the address, and URL the whole of it to hand out.
	Slug string `json:"slug,omitempty"`
	URL  string `json:"url,omitempty"`

	Visibility string `json:"visibility,omitempty"`
	Status     string `json:"status,omitempty"`
	Revision   int    `json:"revision,omitempty"`

	// ContentHash is the digest of what was published, so that a device can say whether the page is
	// behind what it holds without publishing to find out.
	ContentHash     string `json:"contentHash,omitempty"`
	EnvironmentName string `json:"environmentName,omitempty"`
	UpdatedAt       int64  `json:"updatedAt,omitempty"`

	// CanManage is the server's answer about the caller who asked, and never about the row.
	CanManage bool `json:"canManage"`

	// PendingUnpublish marks a takedown that has not reached the server yet — the one thing here that
	// is this device's own state rather than the server's.
	PendingUnpublish bool `json:"pendingUnpublish,omitempty"`

	// Outline and Changes are this device's own state: the shape of the last publish and what has
	// changed since. No server answer carries them, so a dedicated save writes them and every other
	// write of the row leaves them alone.
	Outline []OutlineItem `json:"outline,omitempty"`
	Changes []PageRelease `json:"changes,omitempty"`
}

type PublicationOptions struct {
	Docs     bool `json:"docs"`
	Examples bool `json:"examples"`
}

func (p Publication) PageOptions() PublicationOptions {
	if p.Options != nil {
		return *p.Options
	}
	return PublicationOptions{Docs: true, Examples: true}
}

// OutlineItem is one request as the last publish put it up: enough to recognise it and to tell a
// changed request from a new one. The fingerprint is of the request as the page drew it.
type OutlineItem struct {
	ID          string            `json:"id"`
	Method      string            `json:"method"`
	Name        string            `json:"name"`
	Fingerprint string            `json:"fingerprint"`
	Parts       map[string]string `json:"parts"`
}

// PageRelease is one entry of a page's changelog and PageItem a request it touched. Sign is "+" for
// added, "~" for changed and "-" for removed; the page draws the colour without translating it.
type PageRelease struct {
	Revision int        `json:"revision"`
	At       time.Time  `json:"at"`
	Items    []PageItem `json:"items"`
}

// PageItem is one request a release touched: what happened to it, and which request it was.
type PageItem struct {
	Fields []string `json:"fields"`
	Sign   string   `json:"sign"`
	Method string   `json:"method"`
	Name   string   `json:"name"`
}

type PublicationVersion struct {
	Revision        int    `json:"revision"`
	PublishedAt     int64  `json:"publishedAt"`
	EnvironmentName string `json:"environmentName"`
	Requests        int    `json:"requests"`
	RestoredFrom    int    `json:"restoredFrom"`
}

type PublicationDetails struct {
	Views    int64                `json:"views"`
	Imports  int64                `json:"imports"`
	Versions []PublicationVersion `json:"versions"`
}

// PublicationDraft carries the public document, its digest and access settings to the server.
type PublicationDraft struct {
	Collection  string
	Workspace   string
	Visibility  string
	Document    []byte
	ContentHash string
	// Confirmed is the author saying they have been told the page answers to whoever holds the link.
	Confirmed bool
	// Password is sent separately from the public document; local copies belong in the OS keyring.
	Password string
}

// Who a page is for. The vocabulary is the server's, because it is the server that refuses one.
const (
	// VisibilityUnlisted answers at the address and nowhere else, and is what a publish that says
	// nothing gets.
	VisibilityUnlisted = "unlisted"

	// VisibilityPublic names the collection to anyone who finds the page.
	VisibilityPublic = "public"

	// VisibilityPassword answers only to a reader who sends the password. The page keeps a hash and
	// never the text, and this device hands the text over once rather than writing it down.
	VisibilityPassword = "password"
)

// Page password limits in bytes, as the server holds them: a shorter one is guessable and a longer
// one is silently truncated by the server's hash. The window draws the same two from `layout.ts`.
const (
	PasswordLeast = 8
	PasswordMost  = 72
)

// Whether a page answers at all.
const (
	PublicationActive  = "active"
	PublicationRevoked = "revoked"
)

// Runnable reports whether a page answers at the address. A revoked page is not gone: its row keeps
// both the bytes and the address, so putting the collection back up answers with the page it had.
func (p Publication) Runnable() bool {
	return p.Status == PublicationActive && p.ID != "" && !p.PendingUnpublish
}

// HiddenReason says why a variable of a collection is not on the page. A page carries no reason of
// its own for having left something out, and this is only ever said in the window that publishes.
type HiddenReason string

const (
	// HiddenSecret is a variable this app already marks as a secret.
	HiddenSecret HiddenReason = "secret"

	// HiddenLinked is a variable an environment reaches only through one that is hidden, and so is
	// one whose value is on the page by another name.
	HiddenLinked HiddenReason = "linked"

	// HiddenLooksSecret is a variable whose name looks like a credential. Nothing else is known; it
	// stays behind so a secret nobody marked is not published by accident.
	HiddenLooksSecret HiddenReason = "looksSecret"
)

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
	// EntityRun is a collection run pushed through the same endpoint, once by id however many
	// records name it. It is not in EntityTypes: that order is the sync endpoint's, and a run goes
	// through history beside its records.
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

// SyncState defines current synchronization state of a workspace.
type SyncState string

const (
	SyncLocal  SyncState = "local"
	SyncSynced SyncState = "synced"
	SyncPaused SyncState = "paused"
)

// SyncWorkspace contains synchronization metadata for a local workspace.
type SyncWorkspace struct {
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
	// LinkPending indicates workspace creation confirmation is in flight.
	LinkPending bool `json:"linkPending,omitempty"`
	// Gone indicates remote workspace was deleted or unassigned.
	Gone bool `json:"gone,omitempty"`
	// Live indicates active SSE/WebSocket stream status.
	Live bool `json:"live"`
	// ReadOnly is whether the account last said this person may not write in the workspace; a round
	// does not push into one that would only refuse it. The zero value is a writer's.
	ReadOnly bool `json:"-"`
}

// Exchangeable reports whether the workspace is linked and active for sync operations.
func (s SyncWorkspace) Exchangeable() bool {
	return s.RemoteID != "" && s.State != SyncPaused
}

// SyncEntity is one row as it travels: an envelope and the row's data. For content kinds Data is a
// JSON object whose secret slots are named by Secrets, and Withheld names the slots left empty; for
// a record or a run it is the history message.
type SyncEntity struct {
	Kind      EntityType
	ID        string
	Parent    string
	Name      string
	Position  int64
	Data      []byte
	Secrets   []string
	Withheld  []string
	CreatedAt int64
	UpdatedAt int64
	Version   int32
	Deleted   bool
}

// WorkspaceCursor tracks local content and history sequence numbers for a workspace.
type WorkspaceCursor struct {
	WorkspaceID string
	Content     int64
	History     int64
}

// Poke represents lightweight notification of remote workspace sequence progress.
type Poke struct {
	WorkspaceID string
	Content     int64
	History     int64
}

// Signal wraps stream events regarding sequence progress or workspace list changes.
type Signal struct {
	Poke
	WorkspacesChanged bool
}

// SyncChange represents an incoming content log entry from the server.
type SyncChange struct {
	WorkspaceID string
	Seq         int64
	Entity      SyncEntity
}

// RecordChange represents an incoming history log entry.
type RecordChange struct {
	Seq      int64
	RecordID string
	Deleted  bool
	Record   Record
	// Run is the collection run the entry carries. It is an entry of its own: a run is written once
	// by id, so the log brings it alone, with no record beside it.
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
	WorkspaceID string
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
	ReasonInvalidData   PushReason = "invalidData"
	ReasonForbidden     PushReason = "forbidden"
	ReasonTooLarge      PushReason = "tooLarge"
	ReasonEmpty         PushReason = "empty"
	ReasonUnspecified   PushReason = "unspecified"
)

// ArgTooLarge is the arg of the server's size refusal. The call was refused, not a row, so there is
// nothing else to name; a caller can act on it only when the batch is a single document.
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

// RemoteWorkspace represents workspace metadata provided by the server contract.
type RemoteWorkspace struct {
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

// AccountWorkspaces holds accessible remote workspaces and user's primary organization ID.
type AccountWorkspaces struct {
	Workspaces []RemoteWorkspace `json:"workspaces"`
	OwnOrgID   string            `json:"ownOrgId,omitempty"`
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
