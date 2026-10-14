package sync

import (
	"context"
	"time"

	"schemo/internal/domain"
)

// Store provides local sync persistence and handles applying server changes.
type Store interface {
	Spaces(ctx context.Context) ([]domain.SyncSpace, error)

	// Link links a local space with a remote space and queues local contents for push.
	Link(ctx context.Context, spaceID string, remote domain.RemoteSpace, account string) error

	// Mirror adds a server-owned space locally without queuing existing contents.
	Mirror(ctx context.Context, remote domain.RemoteSpace, account string) error

	// Seen returns the account state as last recorded on this device.
	Seen(ctx context.Context, key string) (userID string, known bool, err error)
	Remember(ctx context.Context, key, userID string) error
	// Rebind detaches spaces from a re-created server account and updates the known user ID.
	Rebind(ctx context.Context, key, userID string) error

	// Context returns the currently active account key on this device.
	Context(ctx context.Context) (string, error)
	SetContext(ctx context.Context, key string) error
	// Release drops local space copies for inactive accounts and returns released count.
	Release(ctx context.Context, key string) (int, error)

	Discard(ctx context.Context, spaceID string) error
	EnsureOne(ctx context.Context) error
	// Adopt takes what the account says of a paired space, and answers whether the row moved.
	Adopt(ctx context.Context, spaceID string, remote domain.RemoteSpace) (bool, error)
	MarkLinkPending(ctx context.Context, spaceID, remoteID, account string) error
	Holds(ctx context.Context, spaceID string) (bool, error)

	// Owed fetches pending local changes and marks them in-flight, bounded by what the server takes.
	Owed(
		ctx context.Context,
		space domain.SyncSpace,
		maxBytes int64,
	) ([]domain.PendingChange, error)

	Accepted(ctx context.Context, change domain.PendingChange, version int32) error
	Superseded(
		ctx context.Context,
		change domain.PendingChange,
		winner domain.SyncEntity,
	) error
	Failed(
		ctx context.Context,
		change domain.PendingChange,
		reason domain.PushReason,
		retryIn time.Duration,
	) error
	Parked(ctx context.Context, change domain.PendingChange, reason domain.PushReason) error

	// ReleaseInFlight resets state for changes left in-flight by interrupted sync rounds.
	ReleaseInFlight(ctx context.Context) error

	// ApplyPage applies entity changes and atomically advances the content cursor.
	ApplyPage(
		ctx context.Context,
		spaceID string,
		changes []domain.SyncChange,
		cursor int64,
	) (int, error)

	// ApplyHistoryPage applies history changes and atomically advances the history cursor.
	ApplyHistoryPage(
		ctx context.Context,
		spaceID string,
		changes []domain.RecordChange,
		cursor int64,
	) (int, error)

	ApplyHistorySnapshot(
		ctx context.Context,
		spaceID string,
		records []domain.Record,
		from, head int64,
	) error

	Reset(ctx context.Context, spaceID string) error
	SetState(ctx context.Context, spaceID string, state domain.SyncState) error
	RecordRound(
		ctx context.Context,
		spaceID string,
		state domain.SyncState,
		at int64,
		failed domain.Code,
		attempts int,
	) error

	QueueCounts(ctx context.Context) (map[string]domain.SyncCounts, error)
	Waiting(ctx context.Context, spaceID string) ([]domain.QueuedChange, error)
}

// Server defines remote sync API operations.
type Server interface {
	Spaces(ctx context.Context, caller domain.Caller) (domain.AccountSpaces, error)
	Create(
		ctx context.Context,
		caller domain.Caller,
		orgID, id, name, colour string,
		kind domain.WorkspaceKind,
	) (domain.RemoteSpace, error)

	Limits(ctx context.Context, caller domain.Caller) (domain.Limits, error)
	Push(
		ctx context.Context,
		caller domain.Caller,
		spaceID string,
		entities []domain.Pushed,
	) ([]domain.Verdict, error)
	Pull(
		ctx context.Context,
		caller domain.Caller,
		spaceID string,
		cursor int64,
		limit int,
	) (domain.Page, error)

	// ListRecords fetches history metadata sorted by time descending, omitting bodies.
	ListRecords(
		ctx context.Context,
		caller domain.Caller,
		spaceID string,
		from, to int64,
		limit int,
		token string,
	) (domain.RecordList, error)

	PullRecords(
		ctx context.Context,
		caller domain.Caller,
		spaceID string,
		afterSeq int64,
		limit int,
	) (domain.RecordPage, error)

	// Connect opens a live stream for remote space updates and invokes on for each signal.
	Connect(
		ctx context.Context,
		caller domain.Caller,
		cursors []domain.SpaceCursor,
		on func(domain.Signal) error,
	) error

	// Sent files records and runs in one `history/v1.Append` and answers about each list in turn.
	Sent(
		ctx context.Context,
		caller domain.Caller,
		spaceID string,
		records, runs []domain.Pushed,
	) ([]domain.RecordVerdict, []domain.RecordVerdict, error)
	Retract(
		ctx context.Context,
		caller domain.Caller,
		spaceID string,
		ids []string,
	) (int, error)
	Fetched(
		ctx context.Context,
		caller domain.Caller,
		spaceID, recordID string,
	) (domain.RecordFetched, error)
}

// Retention says how long this device keeps history.
type Retention interface {
	Retention(ctx context.Context) (domain.Retention, error)
}
