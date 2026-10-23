package cloud

import (
	"context"
	"log"
	"maps"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"slices"
	"strings"
	"sync"
	"time"
)

// TopicSyncChanged is published when sync status or data changes.
const TopicSyncChanged = "sync:changed"

const (
	defaultInterval = 10 * time.Minute
	settle          = 300 * time.Millisecond
	firstRetry      = 2 * time.Second
	maxBackoff      = 5 * time.Minute
	pullLimit       = 200
)

// Sync is the sync engine: the rounds that push what this device owes and pull what the
// account holds, the live stream, and the status the window draws.
type Sync struct {
	store    SyncStore
	server   SyncServer
	session  domain.Principal
	notifier domain.Notifier
	now      func() time.Time
	ids      platform.IDGen
	interval time.Duration
	settle   time.Duration
	till     time.Duration

	// slot is held by whatever is changing the queue or the cursors of the workspaces: a round, a
	// resync, a link. Two of them at once would claim the same entries, release each other's, and
	// make two workspaces in the account for one.
	slot chan struct{}

	mu         sync.Mutex
	cancel     context.CancelFunc
	done       chan struct{}
	wake       chan struct{}
	everything bool
	dirty      map[string]struct{}

	running  bool
	signedIn bool
	failure  *domain.Failure
	conflict *Conflict
	roundAt  int64
	// inRound is a round in flight, and pushed/pulled are what it has moved so far: the row that
	// says "sending 2, receiving 5" is read while the round is still going.
	inRound bool
	pushed  int
	pulled  int
	// retryAt is when a failed round is tried again, and nothing while the rounds are going through.
	retryAt int64
	// contentAt and historyAt are the moments content and history last arrived; noticed… remember
	// which of those the windows were already told about.
	contentAt      int64
	historyAt      int64
	noticedAt      int64
	noticedHistory int64

	seen      map[string]bool
	known     bool
	mine      string
	taken     domain.Limits
	retention Retention
	stream    *stream
}

func NewSync(
	store SyncStore,
	server SyncServer,
	session domain.Principal,
	retention Retention,
	notifier domain.Notifier,
	ids platform.IDGen,
) *Sync {
	return &Sync{
		store: store, server: server, session: session, retention: retention,
		notifier: notifier, ids: ids,
		now:      time.Now,
		interval: defaultInterval,
		settle:   settle,
		till:     reconnect,
		slot:     make(chan struct{}, 1),
		wake:     make(chan struct{}, 1),
		dirty:    map[string]struct{}{},
	}
}

// exclusive runs work with the slot, waiting for whatever holds it. Waiting rather than giving up
// is what a "sync now" button needs: the edits made before the click must be covered by the round
// it starts, and a round already going may have read the queue before them.
func (s *Sync) exclusive(ctx context.Context, work func() error) error {
	select {
	case s.slot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.slot }()
	return work()
}

// Recover starts the loop and, more importantly, puts back what a crash left in flight: an entry
// that was being sent when the process died is an entry nobody is sending.
//
//wails:ignore
func (s *Sync) Recover(ctx context.Context) error {
	if err := s.store.ReleaseInFlight(ctx); err != nil {
		// Not fatal: the queue is read again on the next round, and a database that cannot be
		// written to is reported by everything else as well.
		log.Printf("[sync] releasing what was in flight: %v", err)
	}
	return s.Start(ctx)
}

// Start launches the background sync loop asynchronously.
//
//wails:ignore
func (s *Sync) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return nil
	}
	running, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	s.cancel, s.done, s.running = cancel, done, true
	s.mu.Unlock()

	go s.loop(running, done)
	return nil
}

// Stop terminates the background sync loop and waits for completion.
//
//wails:ignore
func (s *Sync) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done, s.running = nil, nil, false
	s.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()

	s.mu.Lock()
	s.stream = nil
	s.mu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Wake triggers an immediate sync round for all workspaces.
func (s *Sync) Wake() {
	s.mu.Lock()
	s.everything = true
	s.mu.Unlock()
	s.ring()
}

// WakeWorkspace triggers an immediate sync round for a specific workspace.
//
//wails:ignore
func (s *Sync) WakeWorkspace(workspaceID string) {
	s.mu.Lock()
	s.dirty[workspaceID] = struct{}{}
	s.mu.Unlock()
	s.ring()
}

func (s *Sync) ring() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// takeRequestedWorkspaces returns pending workspaces to sync since the last round and resets the queue.
func (s *Sync) takeRequestedWorkspaces() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	everything, dirty := s.everything, s.dirty
	s.everything, s.dirty = false, map[string]struct{}{}
	if everything || len(dirty) == 0 {
		return nil
	}
	return slices.Collect(maps.Keys(dirty))
}

func (s *Sync) isFollowing(workspaceID string) bool {
	if s.stream == nil {
		return false
	}
	_, ok := s.stream.workspaces[workspaceID]
	return ok
}

// Status returns current sync engine state and workspace statistics.
func (s *Sync) Status(ctx context.Context) (SyncStatus, error) {
	workspaces, err := s.visibleWorkspaces(ctx)
	if err != nil {
		return SyncStatus{}, err
	}
	counts, err := s.store.QueueCounts(ctx)
	if err != nil {
		return SyncStatus{}, err
	}

	waiting := map[string][]domain.QueuedChange{}
	for _, workspace := range workspaces {
		if counts[workspace.ID].Pending+counts[workspace.ID].Failed+counts[workspace.ID].Parked == 0 {
			continue
		}
		entries, err := s.store.Waiting(ctx, workspace.ID)
		if err != nil {
			return SyncStatus{}, err
		}
		waiting[workspace.ID] = entries
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for at := range workspaces {
		workspaces[at].Live = s.isFollowing(workspaces[at].ID)
		workspaces[at].Gone = isMissingFromAccount(s.known, s.seen, s.mine, workspaces[at])
	}
	return SyncStatus{
		Running:    s.running,
		Round:      s.inRound,
		Pushed:     s.pushed,
		Pulled:     s.pulled,
		SignedIn:   s.signedIn,
		Workspaces: workspaces,
		Counts:     counts,
		Waiting:    waiting,
		Failure:    s.failure,
		Conflict:   s.conflict,
		RoundAt:    s.roundAt,
		RetryAt:    s.retryAt,
		ContentAt:  s.contentAt,
		HistoryAt:  s.historyAt,
	}, nil
}

// visibleWorkspaces filters workspaces to include only unlinked workspaces or those owned by active account.
func (s *Sync) visibleWorkspaces(ctx context.Context) ([]domain.SyncWorkspace, error) {
	workspaces, err := s.store.SyncWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	key, err := s.store.Context(ctx)
	if err != nil || key == "" {
		return workspaces, err
	}
	out := workspaces[:0]
	for _, workspace := range workspaces {
		if workspace.RemoteAccount == "" || workspace.RemoteAccount == key {
			out = append(out, workspace)
		}
	}
	return out, nil
}

// Fetched retrieves external record payload for a workspace.
//
//wails:ignore
func (s *Sync) Fetched(
	ctx context.Context,
	workspaceID, id string,
) (domain.RecordFetched, error) {
	workspaces, err := s.store.SyncWorkspaces(ctx)
	if err != nil {
		return domain.RecordFetched{}, err
	}
	var found domain.SyncWorkspace
	for _, workspace := range workspaces {
		if workspace.ID == workspaceID {
			found = workspace
		}
	}
	if !found.Exchangeable() {
		return domain.RecordFetched{},
			domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}

	caller, err := s.session.Caller(ctx)
	if err != nil {
		return domain.RecordFetched{}, err
	}
	return s.server.Fetched(ctx, caller, found.RemoteID, id)
}

//wails:ignore
func (s *Sync) Workspaces(ctx context.Context) ([]domain.SyncWorkspace, error) {
	return s.store.SyncWorkspaces(ctx)
}

// RunRound executes a single sync round synchronously and returns updated Status.
func (s *Sync) RunRound(ctx context.Context) (SyncStatus, error) {
	if err := s.round(ctx, nil); err != nil && !isRefusal(err) {
		return SyncStatus{}, err
	}
	return s.Status(ctx)
}

// Resync resets local workspace state and queues a fresh sync round.
func (s *Sync) Resync(ctx context.Context, workspaceID string) (SyncStatus, error) {
	err := s.exclusive(ctx, func() error { return s.store.Reset(ctx, workspaceID) })
	if err != nil {
		return SyncStatus{}, err
	}
	s.WakeWorkspace(workspaceID)
	return s.Status(ctx)
}

func (s *Sync) loop(ctx context.Context, done chan struct{}) {
	defer close(done)

	var (
		backoff time.Duration
		only    []string
	)
	for {
		wait := s.interval
		failed := false
		if err := s.round(ctx, only); err != nil {
			if ctx.Err() != nil {
				return
			}
			failed = true
			backoff = retryAfter(backoff)
			wait = backoff
		} else {
			backoff = 0
		}
		s.recordRetry(wait, failed)

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
			backoff = 0
			select {
			case <-time.After(s.settle):
			case <-ctx.Done():
				return
			}
			only = s.takeRequestedWorkspaces()
		case <-timer.C:
			only = nil
		}
	}
}

func retryAfter(previous time.Duration) time.Duration {
	return min(max(2*previous, firstRetry), maxBackoff)
}

// Pause pauses syncing for all linked workspaces.
func (s *Sync) Pause(ctx context.Context) (SyncStatus, error) {
	if err := s.eachWorkspace(ctx, func(workspace domain.SyncWorkspace) error {
		return s.store.SetState(ctx, workspace.ID, domain.SyncPaused)
	}); err != nil {
		return SyncStatus{}, err
	}

	s.mu.Lock()
	if s.stream != nil {
		s.stream.cancel()
		s.stream = nil
	}
	s.mu.Unlock()

	return s.Status(ctx)
}

// Resume resumes syncing for all linked workspaces.
func (s *Sync) Resume(ctx context.Context) (SyncStatus, error) {
	if err := s.eachWorkspace(ctx, func(workspace domain.SyncWorkspace) error {
		state := domain.SyncLocal
		if workspace.SyncedAt > 0 {
			state = domain.SyncSynced
		}
		return s.store.SetState(ctx, workspace.ID, state)
	}); err != nil {
		return SyncStatus{}, err
	}

	s.Wake()
	return s.Status(ctx)
}

func (s *Sync) eachWorkspace(ctx context.Context, do func(workspace domain.SyncWorkspace) error) error {
	workspaces, err := s.store.SyncWorkspaces(ctx)
	if err != nil {
		return err
	}
	for _, workspace := range workspaces {
		if workspace.RemoteID == "" {
			continue
		}
		if err := do(workspace); err != nil {
			return err
		}
	}
	return nil
}

// SyncStatus represents the current sync engine snapshot for UI.
type SyncStatus struct {
	Running    bool                             `json:"running"`
	SignedIn   bool                             `json:"signedIn"`
	Round      bool                             `json:"round"`
	Pushed     int                              `json:"pushed"`
	Pulled     int                              `json:"pulled"`
	Workspaces []domain.SyncWorkspace           `json:"workspaces"`
	Counts     map[string]domain.SyncCounts     `json:"counts"`
	Waiting    map[string][]domain.QueuedChange `json:"waiting,omitempty"`
	Failure    *domain.Failure                  `json:"failure,omitempty"`
	Conflict   *Conflict                        `json:"conflict,omitempty"`
	RoundAt    int64                            `json:"roundAt,omitempty"`
	ContentAt  int64                            `json:"contentAt,omitempty"`
	HistoryAt  int64                            `json:"historyAt,omitempty"`
	// RetryAt is when the engine will try the round that failed again, zero when the last round was
	// fine: the row counts the seconds down from it.
	RetryAt int64 `json:"retryAt,omitempty"`
}

// Conflict represents a local change overwritten by the server.
type Conflict struct {
	WorkspaceID string            `json:"workspaceId"`
	Name        string            `json:"name,omitempty"`
	EntityID    string            `json:"entityId"`
	Kind        domain.EntityType `json:"kind"`
	At          int64             `json:"at"`
}

// SyncStore provides local sync persistence and handles applying server changes.
type SyncStore interface {
	SyncWorkspaces(ctx context.Context) ([]domain.SyncWorkspace, error)

	// Link links a local workspace with a remote workspace and queues local contents for push.
	Link(ctx context.Context, workspaceID string, remote domain.RemoteWorkspace, account string) error

	// Mirror adds a server-owned workspace locally without queuing existing contents.
	Mirror(ctx context.Context, remote domain.RemoteWorkspace, account string) error

	// Seen returns the account state as last recorded on this device.
	Seen(ctx context.Context, key string) (userID string, known bool, err error)
	Remember(ctx context.Context, key, userID string) error
	// Rebind detaches workspaces from a re-created server account and updates the known user ID.
	Rebind(ctx context.Context, key, userID string) error

	// Context returns the currently active account key on this device.
	Context(ctx context.Context) (string, error)
	SetContext(ctx context.Context, key string) error
	// Release drops local workspace copies for inactive accounts and returns released count.
	Release(ctx context.Context, key string) (int, error)

	Discard(ctx context.Context, workspaceID string) error
	EnsureOne(ctx context.Context) error
	// Adopt takes what the account says of a paired workspace, and answers whether the row moved.
	Adopt(ctx context.Context, workspaceID string, remote domain.RemoteWorkspace) (bool, error)
	MarkLinkPending(ctx context.Context, workspaceID, remoteID, account string) error
	Holds(ctx context.Context, workspaceID string) (bool, error)

	// Owed fetches pending local changes and marks them in-flight, bounded by what the server takes.
	Owed(
		ctx context.Context,
		workspace domain.SyncWorkspace,
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
		workspaceID string,
		changes []domain.SyncChange,
		cursor int64,
	) (int, error)

	// ApplyHistoryPage applies history changes and atomically advances the history cursor.
	ApplyHistoryPage(
		ctx context.Context,
		workspaceID string,
		changes []domain.RecordChange,
		cursor int64,
	) (int, error)

	ApplyHistorySnapshot(
		ctx context.Context,
		workspaceID string,
		records []domain.Record,
		from, head int64,
	) error

	Reset(ctx context.Context, workspaceID string) error
	SetState(ctx context.Context, workspaceID string, state domain.SyncState) error
	RecordRound(
		ctx context.Context,
		workspaceID string,
		state domain.SyncState,
		at int64,
		failed domain.Code,
		attempts int,
	) error

	QueueCounts(ctx context.Context) (map[string]domain.SyncCounts, error)
	Waiting(ctx context.Context, workspaceID string) ([]domain.QueuedChange, error)
}

// SyncServer defines remote sync API operations.
type SyncServer interface {
	Workspaces(ctx context.Context, caller domain.Caller) (domain.AccountWorkspaces, error)
	Create(
		ctx context.Context,
		caller domain.Caller,
		orgID, id, name, colour string,
		kind domain.WorkspaceKind,
	) (domain.RemoteWorkspace, error)

	Limits(ctx context.Context, caller domain.Caller) (domain.Limits, error)
	Push(
		ctx context.Context,
		caller domain.Caller,
		workspaceID string,
		entities []domain.Pushed,
	) ([]domain.Verdict, error)
	Pull(
		ctx context.Context,
		caller domain.Caller,
		workspaceID string,
		cursor int64,
		limit int,
	) (domain.Page, error)

	// ListRecords fetches history metadata sorted by time descending, omitting bodies.
	ListRecords(
		ctx context.Context,
		caller domain.Caller,
		workspaceID string,
		from, to int64,
		limit int,
		token string,
	) (domain.RecordList, error)

	PullRecords(
		ctx context.Context,
		caller domain.Caller,
		workspaceID string,
		afterSeq int64,
		limit int,
	) (domain.RecordPage, error)

	// Connect opens a live stream for remote workspace updates and invokes on for each signal.
	Connect(
		ctx context.Context,
		caller domain.Caller,
		cursors []domain.WorkspaceCursor,
		on func(domain.Signal) error,
	) error

	// Sent files records and runs in one `history/v1.Append` and answers about each list in turn.
	Sent(
		ctx context.Context,
		caller domain.Caller,
		workspaceID string,
		records, runs []domain.Pushed,
	) ([]domain.RecordVerdict, []domain.RecordVerdict, error)
	Retract(
		ctx context.Context,
		caller domain.Caller,
		workspaceID string,
		ids []string,
	) (int, error)
	Fetched(
		ctx context.Context,
		caller domain.Caller,
		workspaceID, recordID string,
	) (domain.RecordFetched, error)
}

// Retention says how long this device keeps history.
type Retention interface {
	Retention(ctx context.Context) (domain.Retention, error)
}

func (s *Sync) recordFailure(workspace domain.SyncWorkspace, err error) {
	state := workspace.State
	if state == domain.SyncSynced && workspace.SyncedAt == 0 {
		state = domain.SyncLocal
	}
	if back := s.store.RecordRound(context.Background(), workspace.ID, state, 0, domain.CodeOf(err),
		workspace.Attempts+1); back != nil {
		// Losing this row costs the state its memory across a restart, and the window its reason: the
		// round's own failure still goes out, so it is the log that has to say this one went missing.
		log.Printf("[sync] recording the failed round of %s: %v", workspace.ID, back)
	}
}

// recordRetry writes down that the next attempt is `wait` away — or that nothing is being waited
// on, when the round went through — and says so, because the row counts the seconds down from it.
func (s *Sync) recordRetry(wait time.Duration, failed bool) {
	s.mu.Lock()
	before := s.retryAt
	s.retryAt = 0
	if failed {
		s.retryAt = s.now().Add(wait).UnixMilli()
	}
	changed := s.retryAt != before
	s.mu.Unlock()

	if changed {
		s.publishStatus()
	}
}

// beginRound opens a round: the counters start at nothing, and the windows are told a round is on —
// the row that reads them draws "syncing" from that alone.
func (s *Sync) beginRound() {
	s.mu.Lock()
	s.inRound, s.pushed, s.pulled, s.retryAt = true, 0, 0, 0
	s.mu.Unlock()
	s.publishStatus()
}

// addProgress counts what the round has moved so far and says so: the numbers are for the row that
// reads them while the round is still going, so they are published as they grow.
func (s *Sync) addProgress(pushed, pulled int) {
	if pushed == 0 && pulled == 0 {
		return
	}
	s.mu.Lock()
	s.pushed += pushed
	s.pulled += pulled
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Sync) finishRound(err error) {
	s.mu.Lock()
	before := s.failure
	wasRound := s.inRound
	s.inRound = false
	s.roundAt = s.now().UnixMilli()
	s.failure = nil
	if err != nil {
		s.failure = domain.FailureOf(err, domain.CodeServerRefused)
	}
	failed := (before == nil) != (s.failure == nil) ||
		(before != nil && s.failure != nil && before.Code != s.failure.Code)
	arrived := s.contentAt != s.noticedAt || s.historyAt != s.noticedHistory
	s.mu.Unlock()

	// The round closing is itself news: a window drawing "syncing" has to be told it is over even
	// when nothing else about the engine moved.
	if failed || arrived || wasRound {
		s.publishStatus()
	}
}

func (s *Sync) noteContentArrived() {
	s.mu.Lock()
	s.contentAt = s.now().UnixMilli()
	s.mu.Unlock()
}

func (s *Sync) noteHistoryArrived() {
	s.mu.Lock()
	s.historyAt = s.now().UnixMilli()
	s.mu.Unlock()
}

func (s *Sync) setSignedIn(in bool) {
	s.mu.Lock()
	s.signedIn = in
	s.mu.Unlock()
}

func (s *Sync) recordConflict(workspaceID string, change domain.PendingChange) {
	s.mu.Lock()
	s.conflict = &Conflict{
		WorkspaceID: workspaceID,
		Name:        change.Entity.Name,
		EntityID:    change.Entity.ID,
		Kind:        change.Entity.Kind,
		At:          s.now().UnixMilli(),
	}
	s.mu.Unlock()
	s.publishStatus()
}

func (s *Sync) publishStatus() {
	if s.notifier == nil {
		return
	}

	s.mu.Lock()
	s.noticedAt, s.noticedHistory = s.contentAt, s.historyAt
	s.mu.Unlock()
	status, err := s.Status(context.Background())
	if err != nil {
		return
	}
	s.notifier.Publish(TopicSyncChanged, status)
}

func (s *Sync) activeWorkspaces(workspaces []domain.SyncWorkspace) []domain.SyncWorkspace {
	s.mu.Lock()
	known, seen, mine := s.known, s.seen, s.mine
	s.mu.Unlock()

	if !known {
		return workspaces
	}
	out := make([]domain.SyncWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.LinkPending {
			continue
		}
		if isMissingFromAccount(known, seen, mine, workspace) || isForeign(workspace, mine) {
			continue
		}
		out = append(out, workspace)
	}
	return out
}

func allLinkedWorkspacesPaused(workspaces []domain.SyncWorkspace) bool {
	linked := 0
	for _, workspace := range workspaces {
		if workspace.RemoteID == "" {
			continue
		}
		linked++
		if workspace.State != domain.SyncPaused {
			return false
		}
	}
	return linked > 0
}

func isMissingFromAccount(known bool, seen map[string]bool, mine string, workspace domain.SyncWorkspace) bool {
	return known && belongsToAccount(workspace, mine) && workspace.RemoteID != "" && !seen[workspace.RemoteID]
}

func isForeign(workspace domain.SyncWorkspace, mine string) bool {
	return workspace.RemoteID != "" && workspace.RemoteAccount != mine
}

func belongsToAccount(workspace domain.SyncWorkspace, mine string) bool {
	return workspace.RemoteID != "" && workspace.RemoteAccount == mine
}

func isWaitingToLink(workspace domain.SyncWorkspace) bool {
	return workspace.RemoteID == "" || workspace.LinkPending
}

func findWorkspace(workspaces []domain.SyncWorkspace, id string) *domain.SyncWorkspace {
	for at := range workspaces {
		if workspaces[at].ID == id {
			return &workspaces[at]
		}
	}
	return nil
}

func personalWorkspaceOfAccount(there []domain.RemoteWorkspace) (domain.RemoteWorkspace, bool) {
	for _, one := range there {
		if one.OwnOrg && one.Kind == domain.WorkspacePersonal && strings.TrimSpace(one.Name) == "" {
			return one, true
		}
	}
	return domain.RemoteWorkspace{}, false
}
