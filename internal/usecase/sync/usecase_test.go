package sync

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// These tests are about the decisions, not about the database or the wire: the store is a map and
// the server is a script, and what is asserted is what the engine does with an answer. The shapes
// it hands over — a payload, a version, a cursor — are the fakes' to invent, because none of them
// is anything this package reads.

const (
	spaceA = "space-a"
	remote = "remote-space"
	// ownOrg is the organisation the fixtures below say belongs to the person: the one their own work
	// is offered into, and the one a space of somebody else's is not in.
	ownOrg = "org-of-my-own"
	// meUser is the account these tests are signed in as: what a space linked here is linked under,
	// and what a round compares the account's own answer against.
	meUser = "user-me"
)

// A store that records what it was asked to do, so that a test asserts on the decisions rather than
// on a database's state.
type fakeStore struct {
	owing        map[string]bool
	seen         map[string]string
	holds        map[string]bool
	work         string
	spaces       []domain.SyncSpace
	owed         []domain.PendingChange
	applied      []appliedPage
	accepted     []string
	superseded   []string
	failed       []string
	parked       []string
	reset        []string
	states       []string
	marked       []domain.SyncState
	mirrored     []string
	letGo        []string
	detached     []string
	adopted      []string
	rebound      []string
	discarded    []string
	pending      []string
	historyPages []appliedHistory
	snapshots    []appliedSnapshot
	retry        time.Duration
	released     int
	ensured      bool
	claimed      bool
}

type appliedPage struct {
	spaceID string
	changes []domain.SyncChange
	cursor  int64
}

// appliedHistory is one page of the other log as the store was told to write it.
type appliedHistory struct {
	spaceID string
	changes []domain.RecordChange
	cursor  int64
}

type appliedSnapshot struct {
	spaceID string
	records []domain.Record
	from    int64
	head    int64
}

// Spaces is a reading, and a copy of one. The real store reads rows, so what a round holds is from
// before the round began — a space it links is written behind its back. A fake handing over its own
// slice would show that write, which is the one thing a guard against a stale reading is for.
func (f *fakeStore) Spaces(context.Context) ([]domain.SyncSpace, error) {
	out := make([]domain.SyncSpace, len(f.spaces))
	copy(out, f.spaces)
	return out, nil
}

func (f *fakeStore) Link(
	_ context.Context,
	spaceID string,
	remote domain.RemoteSpace,
	account string,
) error {
	for i := range f.spaces {
		if f.spaces[i].ID == spaceID {
			f.spaces[i].RemoteID = remote.ID
			f.spaces[i].Name = remote.Name
			f.spaces[i].RemoteAccount = account
			f.spaces[i].LinkPending = false
		}
	}
	return nil
}

func (f *fakeStore) Mirror(_ context.Context, remote domain.RemoteSpace, account string) error {
	f.mirrored = append(f.mirrored, remote.ID)
	f.spaces = append(f.spaces, domain.SyncSpace{
		ID: "mirror-" + remote.ID, Name: remote.Name, Color: remote.Colour, RemoteID: remote.ID,
		Kind: remote.Kind, RemoteAccount: account,
	})
	return nil
}

func (f *fakeStore) Context(context.Context) (string, error) { return f.work, nil }

func (f *fakeStore) SetContext(_ context.Context, key string) error {
	f.work = key
	return nil
}

func (f *fakeStore) Seen(_ context.Context, key string) (string, bool, error) {
	row, known := f.seen[key]
	return row, known, nil
}

func (f *fakeStore) Remember(_ context.Context, key, userID string) error {
	if f.seen == nil {
		f.seen = map[string]string{}
	}
	f.seen[key] = userID
	return nil
}

// Rebind leaves every space of the account linked to nothing and remembers the row it has now.
func (f *fakeStore) Rebind(_ context.Context, key, userID string) error {
	for i := range f.spaces {
		if f.spaces[i].RemoteAccount == key && f.spaces[i].RemoteID != "" {
			f.rebound = append(f.rebound, f.spaces[i].ID)
			f.spaces[i].RemoteID, f.spaces[i].RemoteAccount = "", ""
			f.spaces[i].LinkPending = false
		}
	}
	return f.Remember(context.Background(), key, userID)
}

// Detach leaves the account's spaces linked to nothing.
func (f *fakeStore) Detach(_ context.Context, key string) error {
	for i := range f.spaces {
		if f.spaces[i].RemoteAccount == key && f.spaces[i].RemoteID != "" {
			f.detached = append(f.detached, f.spaces[i].ID)
			f.spaces[i].RemoteID, f.spaces[i].RemoteAccount = "", ""
			f.spaces[i].LinkPending = false
		}
	}
	return nil
}

// Release drops spaces that owe nothing; the ones in owing stay.
func (f *fakeStore) Release(_ context.Context, key string) (int, error) {
	kept := f.spaces[:0]
	let := 0
	for _, space := range f.spaces {
		if space.RemoteAccount == key && space.RemoteID != "" && !space.LinkPending &&
			!f.owing[space.ID] {
			f.letGo = append(f.letGo, space.ID)
			let++
			continue
		}
		kept = append(kept, space)
	}
	f.spaces = kept
	return let, nil
}

// Discard removes a space and everything in it, which is what the account's answer about a space
// longer has comes to. The spaces it removed are written down, because that is the whole of what a
// test about this is about.
func (f *fakeStore) Discard(_ context.Context, spaceID string) error {
	f.discarded = append(f.discarded, spaceID)
	held := f.spaces[:0]
	for _, space := range f.spaces {
		if space.ID != spaceID {
			held = append(held, space)
		}
	}
	f.spaces = held
	return nil
}

// EnsureOne leaves the space the app is born with, when a discard emptied the device.
func (f *fakeStore) EnsureOne(_ context.Context) error {
	if len(f.spaces) > 0 {
		return nil
	}
	f.ensured = true
	f.spaces = append(f.spaces, domain.SyncSpace{
		ID: domain.WorkspacePersonalID, Kind: domain.WorkspacePersonal,
	})
	return nil
}

// Adopt is the account's word for what a space is for arriving at a space this device already has.
// It is written down here so that a test can see it asked, and it is what says a team space of the
// account's stays a team's on this device.
func (f *fakeStore) Adopt(
	_ context.Context, spaceID string, remote domain.RemoteSpace,
) (bool, error) {
	f.adopted = append(f.adopted, spaceID+"="+string(remote.Kind))
	for at := range f.spaces {
		if f.spaces[at].ID == spaceID {
			f.spaces[at].Kind = remote.Kind
		}
	}
	return true, nil
}

// MarkLinkPending is the id written down before the call, and the mark that says the account has
// not confirmed it. Both are kept, the way the real store keeps them, because what a test about a
// lost answer reads is that the id it chose is the one it asks under a second time.
func (f *fakeStore) MarkLinkPending(_ context.Context, spaceID, remoteID, account string) error {
	f.pending = append(f.pending, spaceID+"="+remoteID)
	for at := range f.spaces {
		if f.spaces[at].ID == spaceID {
			f.spaces[at].LinkPending = true
			f.spaces[at].RemoteID = remoteID
			f.spaces[at].RemoteAccount = account
		}
	}
	return nil
}

// Holds answers what a test said the space holds, keyed by the space: a map and not a flag, because
// what a test is about is which space is bare and which is not.
func (f *fakeStore) Holds(_ context.Context, spaceID string) (bool, error) {
	return f.holds[spaceID], nil
}

// Owed answers what a test said the space owes.
func (f *fakeStore) Owed(
	_ context.Context,
	_ domain.SyncSpace,
	_ int64,
) ([]domain.PendingChange, error) {
	f.claimed = true
	return f.owed, nil
}

func (f *fakeStore) Accepted(_ context.Context, change domain.PendingChange, _ int32) error {
	f.accepted = append(f.accepted, change.Entity.ID)
	return nil
}

func (f *fakeStore) Superseded(
	_ context.Context,
	change domain.PendingChange,
	_ domain.SyncEntity,
) error {
	f.superseded = append(f.superseded, change.Entity.ID)
	return nil
}

func (f *fakeStore) Failed(
	_ context.Context,
	change domain.PendingChange,
	_ domain.PushReason,
	retryIn time.Duration,
) error {
	f.failed = append(f.failed, change.Entity.ID)
	f.retry = retryIn
	return nil
}

func (f *fakeStore) Parked(
	_ context.Context,
	change domain.PendingChange,
	_ domain.PushReason,
) error {
	f.parked = append(f.parked, change.Entity.ID)
	return nil
}

func (f *fakeStore) ReleaseInFlight(context.Context) error {
	f.released++
	return nil
}

func (f *fakeStore) ApplyPage(
	_ context.Context,
	spaceID string,
	changes []domain.SyncChange,
	cursor int64,
) (int, error) {
	f.applied = append(f.applied, appliedPage{spaceID: spaceID, changes: changes, cursor: cursor})
	return 0, nil
}

func (f *fakeStore) ApplyHistoryPage(
	_ context.Context,
	spaceID string,
	changes []domain.RecordChange,
	cursor int64,
) (int, error) {
	f.historyPages = append(f.historyPages,
		appliedHistory{spaceID: spaceID, changes: changes, cursor: cursor})
	return 0, nil
}

func (f *fakeStore) ApplyHistorySnapshot(
	_ context.Context,
	spaceID string,
	records []domain.Record,
	from, head int64,
) error {
	f.snapshots = append(f.snapshots,
		appliedSnapshot{spaceID: spaceID, records: records, from: from, head: head})
	return nil
}

func (f *fakeStore) Reset(_ context.Context, spaceID string) error {
	f.reset = append(f.reset, spaceID)
	return nil
}

func (f *fakeStore) SetState(_ context.Context, spaceID string, state domain.SyncState) error {
	f.states = append(f.states, spaceID+":"+string(state))
	for at := range f.spaces {
		if f.spaces[at].ID == spaceID {
			f.spaces[at].State = state
		}
	}
	return nil
}

func (f *fakeStore) RecordRound(
	_ context.Context,
	_ string,
	state domain.SyncState,
	_ int64,
	_ domain.Code,
	_ int,
) error {
	f.marked = append(f.marked, state)
	return nil
}

func (f *fakeStore) QueueCounts(context.Context) (map[string]domain.SyncCounts, error) {
	return map[string]domain.SyncCounts{}, nil
}

func (f *fakeStore) Waiting(context.Context, string) ([]domain.QueuedChange, error) {
	return nil, nil
}

// A server that answers what a test tells it to, and remembers what it was sent.
//
// One thing it remembers that nothing else can: which space each call named. The engine is the only
// side that chooses it, so a fake that answers whatever it is asked cannot see the choice at all —
// and a wire named with the local id went unnoticed here for exactly as long as that was true.
type fakeServer struct {
	remote []domain.RemoteSpace
	// ownOrg is the organisation the account says is the caller's own, and the spaces of `remote` that
	// stand in it carry OwnOrg. Both are what a test about somebody else's space varies.
	ownOrg string
	limits domain.Limits
	// limitsErr is the server not answering the first question of a round, which is what the loop
	// is left to deal with.
	limitsErr error

	// hold, when a test sets it, keeps every push where it is until it is closed; inPush and most are
	// how many pushes were being made at once, now and at the most.
	hold    chan struct{}
	inPush  atomic.Int32
	most    atomic.Int32
	entered atomic.Int32

	pushed  [][]domain.Pushed
	records [][]string
	runs    [][]string
	retract []string
	page    domain.Page

	namedPush   []string
	namedRecord []string
	namedTake   []string
	namedPull   []string
	namedStream []string

	// asked is what this device asked the account to make for it, one entry per call: what a local
	// space called itself is all the account is ever told about it.
	asked   []spaceAsk
	made    domain.RemoteSpace
	makeErr error

	verdicts []domain.Verdict
	outcomes []domain.RecordVerdict
	// runOutcomes is what the server says of the runs of one Append, beside the records' own.
	runOutcomes []domain.RecordVerdict

	pushErr error
	pullErr error
	// recordsErr is the history call failing whole, which is what a batch the server will not take
	// whole comes back as.
	recordsErr error
	pulls      int
	// onSpaces is told every time the account's list is asked for.
	onSpaces func()
	mu       sync.Mutex

	// recordPages is what the history pull answers, one per call; recordErr is what it fails with.
	recordPages []domain.RecordPage
	// list is what a read by time answers with, and listed is what it was asked.
	list         domain.RecordList
	listErr      error
	listed       []listAsk
	recordErr    error
	namedHistory []string
	fromHistory  []int64

	// fetch is what one record of somebody else's answers with, and namedFetched is which record was
	// asked for in which space — the two halves of the question the history feature asks the engine.
	fetch        domain.RecordFetched
	fetchErr     error
	namedFetched []string
}

// spaceAsk is one request to make a space in the account.
type spaceAsk struct {
	org string
	// id is what the device named, and the account makes the space under it: a second call naming it
	// is a second question about one space, which is the whole of what a test about a lost answer
	// reads.
	id     string
	name   string
	colour string
	// kind is what the device said the space was for, which is what the account makes it: a space a
	// person kept for themselves is not made a team's, and the other way round.
	kind domain.WorkspaceKind
}

func (f *fakeServer) Spaces(context.Context, domain.Caller) (domain.AccountSpaces, error) {
	if f.onSpaces != nil {
		f.onSpaces()
	}
	return domain.AccountSpaces{Spaces: f.remote, OwnOrgID: f.ownOrg}, nil
}

// Create answers a space of the account, and it is the one the device named: the id is the
// caller's, and the space standing under it is answered rather than a second one made. A test that
// says which space comes back gets `made` instead.
func (f *fakeServer) Create(
	_ context.Context,
	_ domain.Caller,
	orgID, id, name, colour string,
	kind domain.WorkspaceKind,
) (domain.RemoteSpace, error) {
	f.asked = append(f.asked, spaceAsk{org: orgID, id: id, name: name, colour: colour, kind: kind})
	if f.makeErr != nil {
		return domain.RemoteSpace{}, f.makeErr
	}
	if f.made.ID != "" {
		return f.made, nil
	}
	return domain.RemoteSpace{ID: id, Name: name, Colour: colour, OwnOrg: true, Kind: kind}, nil
}

// namedOnce is what a test about the wire asserts: one call, naming the id the server knows the
// space by. `spaceA` and `remote` are two different words on purpose — a test that used one word
// for both could not see the difference this file is about.
func namedOnce(t *testing.T, what string, named []string, want string) {
	t.Helper()
	if len(named) != 1 {
		t.Fatalf("%s = %v, want one call", what, named)
	}
	if named[0] != want {
		t.Errorf("%s named %q, want %q", what, named[0], want)
	}
}

func (f *fakeServer) Limits(context.Context, domain.Caller) (domain.Limits, error) {
	if f.limitsErr != nil {
		return domain.Limits{}, f.limitsErr
	}
	limits := f.limits
	if limits.MaxPushBytes == 0 {
		limits = domain.Limits{MaxPushBytes: 1 << 20, MaxBodyBytes: 1 << 20}
	}
	return limits, nil
}

func (f *fakeServer) Push(
	ctx context.Context,
	_ domain.Caller,
	spaceID string,
	entities []domain.Pushed,
) ([]domain.Verdict, error) {
	f.entered.Add(1)
	now := f.inPush.Add(1)
	defer f.inPush.Add(-1)
	for most := f.most.Load(); now > most && !f.most.CompareAndSwap(most, now); most = f.most.Load() {
	}
	if f.hold != nil {
		select {
		case <-f.hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.namedPush = append(f.namedPush, spaceID)
	f.pushed = append(f.pushed, entities)
	if f.pushErr != nil {
		return nil, f.pushErr
	}
	return f.verdicts, nil
}

// ListRecords answers the page a test put in `list`, and remembers what it was asked.
func (f *fakeServer) ListRecords(
	_ context.Context,
	_ domain.Caller,
	spaceID string,
	from, to int64,
	limit int,
	token string,
) (domain.RecordList, error) {
	f.listed = append(f.listed,
		listAsk{spaceID: spaceID, from: from, to: to, limit: limit, token: token})
	if f.listErr != nil {
		return domain.RecordList{}, f.listErr
	}
	return f.list, nil
}

func (f *fakeServer) Fetched(
	_ context.Context,
	_ domain.Caller,
	spaceID, recordID string,
) (domain.RecordFetched, error) {
	f.namedFetched = append(f.namedFetched, spaceID+"/"+recordID)
	if f.fetchErr != nil {
		return domain.RecordFetched{}, f.fetchErr
	}
	return f.fetch, nil
}

func (f *fakeServer) PullRecords(
	_ context.Context,
	_ domain.Caller,
	spaceID string,
	afterSeq int64,
	_ int,
) (domain.RecordPage, error) {
	f.namedHistory = append(f.namedHistory, spaceID)
	f.fromHistory = append(f.fromHistory, afterSeq)
	if f.recordErr != nil {
		return domain.RecordPage{}, f.recordErr
	}
	if len(f.recordPages) == 0 {
		return domain.RecordPage{Cursor: afterSeq}, nil
	}
	page := f.recordPages[0]
	f.recordPages = f.recordPages[1:]
	return page, nil
}

func (f *fakeServer) Pull(
	_ context.Context,
	_ domain.Caller,
	spaceID string,
	_ int64,
	_ int,
) (domain.Page, error) {
	f.mu.Lock()
	f.namedPull = append(f.namedPull, spaceID)
	f.pulls++
	f.mu.Unlock()
	if f.pullErr != nil {
		return domain.Page{}, f.pullErr
	}
	return f.page, nil
}

func (f *fakeServer) Sent(
	_ context.Context,
	_ domain.Caller,
	spaceID string,
	records, runs []domain.Pushed,
) ([]domain.RecordVerdict, []domain.RecordVerdict, error) {
	if f.recordsErr != nil {
		return nil, nil, f.recordsErr
	}
	f.namedRecord = append(f.namedRecord, spaceID)
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.Entity.ID)
	}
	f.records = append(f.records, ids)

	runIDs := make([]string, 0, len(runs))
	for _, run := range runs {
		runIDs = append(runIDs, run.Entity.ID)
	}
	if len(runIDs) > 0 {
		f.runs = append(f.runs, runIDs)
	}
	return f.outcomes, f.runOutcomes, nil
}

// Connect is the live half, and the fake has none to say: it holds the stream open and quiet until
// the round that opened it is over. One that ended at once would finish a round of its own beside
// the test's, and clear the failure the test is about to read.
func (f *fakeServer) Connect(
	ctx context.Context,
	_ domain.Caller,
	_ []domain.SpaceCursor,
	_ func(domain.Signal) error,
) error {
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeServer) Retract(
	_ context.Context,
	_ domain.Caller,
	spaceID string,
	ids []string,
) (int, error) {
	f.namedTake = append(f.namedTake, spaceID)
	f.retract = append(f.retract, ids...)
	return len(ids), nil
}

// A session that is signed in unless a test says otherwise.
type fakeSession struct {
	refusal error
}

func (s fakeSession) Caller(context.Context) (domain.Caller, error) {
	if s.refusal != nil {
		return domain.Caller{}, s.refusal
	}
	return domain.Caller{Address: "https://server.test", Access: "token", SessionID: "device-1"}, nil
}

// Me is who this device is signed in as, which for most tests is the one account they hold. The two
// halves are the ones `Caller` answers with, and `meUser` is a constant so that the name a space
// is linked under and the name a round compares against are one value.
func (s fakeSession) Me(context.Context) (domain.Account, error) {
	if s.refusal != nil {
		return domain.Account{}, s.refusal
	}
	return domain.Account{UserID: meUser, Email: "me@acme.com"}, nil
}

// A notifier that remembers what it was told, so that a test can assert that a window was told.
// What it is written by is a goroutine — a stream's — so the count is read behind the same lock.
type fakeNotifier struct {
	mu     sync.Mutex
	topics []string
	// statuses are the payloads, kept where they are a status: a test about the numbers a round is
	// drawn by reads them in the order the windows were told.
	statuses []Status
}

func (n *fakeNotifier) Publish(topic string, payload any) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.topics = append(n.topics, topic)
	if status, ok := payload.(Status); ok {
		n.statuses = append(n.statuses, status)
	}
}

// told is how many times a window has been told, which is what a test about a burst asserts on.
func (n *fakeNotifier) told() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.topics)
}

// published is every status a window was told, in order.
func (n *fakeNotifier) published() []Status {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]Status{}, n.statuses...)
}

func newEngine() (*UseCase, *fakeStore, *fakeServer, *fakeNotifier) {
	store := &fakeStore{spaces: []domain.SyncSpace{
		{ID: spaceA, Name: "Team", RemoteID: remote, State: domain.SyncSynced,
			RemoteAccount: accountOf()},
	}}
	// The account still has the space this device holds, and it stands in the organisation the person
	// owns: a round asks, and a server that answered with nothing would be saying the space is gone,
	// which is a test of its own.
	server := &fakeServer{
		remote: []domain.RemoteSpace{
			{ID: remote, Name: "Team", OwnOrg: true, Kind: domain.WorkspacePersonal}},
		ownOrg: ownOrg,
	}
	notifier := &fakeNotifier{}
	uc := NewUseCase(store, server, fakeSession{}, nil, notifier, fakeIDs())
	uc.settle = 0
	return uc, store, server, notifier
}

// fakeIDs names one id per call, in order. A test about a lost answer reads the id out of the call
// the engine made and asserts the round after it asked under the same one, which a generator
// answering something different every time would hide.
func fakeIDs() platform.IDGen {
	var n int
	return func() string {
		n++
		return "chosen-" + strconv.Itoa(n)
	}
}

// accountOf is the account these tests are signed into, in the form a space keeps: the same two
// halves `fakeSession` answers with. A space linked by a test is one of this account's unless the
// test says otherwise, which is the difference the account name is there for.
func accountOf() string {
	return domain.AccountKey("https://server.test", "me@acme.com")
}

// pageWith is one page of one change, which is the shape several tests need: what a pull answered,
// and the cursor it answered with.
func pageWith(cursor int64, kind domain.EntityType, id string) domain.Page {
	return domain.Page{
		Cursor:  cursor,
		Changes: []domain.SyncChange{{Entity: domain.SyncEntity{Kind: kind, ID: id}}},
	}
}

// change is one entry of the outbox, with the row a test is about.
func change(kind domain.EntityType, id string) domain.PendingChange {
	return domain.PendingChange{
		QueueID:     "queue-" + id,
		SpaceID:     spaceA,
		Action:      domain.ActionUpdate,
		OperationID: "op-" + id,
		Entity:      domain.SyncEntity{Kind: kind, ID: id, Version: 3, Payload: []byte("payload")},
	}
}

// ---- the id every call names -------------------------------------------------------------
//
// One scenario each, because each is a different call and a different way of getting it wrong. What
// they have in common is the only thing that matters here: the space is named on the wire by what
// the server knows it by, and the local id is a word the server has never heard.

func TestAPushNamesTheSpaceTheServerKnowsItBy(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}
	server.verdicts = []domain.Verdict{{EntityID: "col-1", Status: domain.PushStored, Version: 4}}

	round(t, uc)
	namedOnce(t, "the push", server.namedPush, remote)
}

func TestARecordUploadNamesTheSpaceTheServerKnowsItBy(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityRecord, "rec-1")}

	round(t, uc)
	namedOnce(t, "the upload", server.namedRecord, remote)
}

func TestARecordRetractionNamesTheSpaceTheServerKnowsItBy(t *testing.T) {
	uc, store, server, _ := newEngine()
	gone := change(domain.EntityRecord, "rec-1")
	gone.Entity.Deleted = true
	store.owed = []domain.PendingChange{gone}

	round(t, uc)
	namedOnce(t, "the retraction", server.namedTake, remote)
}

func TestAPullNamesTheSpaceTheServerKnowsItBy(t *testing.T) {
	uc, _, server, _ := newEngine()

	round(t, uc)
	namedOnce(t, "the pull", server.namedPull, remote)
}

// And what lands here is named by this device's own word for it: the page arrives addressed to the
// server's id, and the row it writes belongs to the local space.
func TestWhatAPullAppliesIsNamedByTheLocalSpace(t *testing.T) {
	uc, store, server, _ := newEngine()
	server.page = pageWith(41, domain.EntityCollection, "col-1")

	round(t, uc)

	if len(store.applied) != 1 {
		t.Fatalf("applied = %v, want the page", store.applied)
	}
	if got := store.applied[0].changes[0].SpaceID; got != spaceA {
		t.Errorf("the change landed as %q, want %q", got, spaceA)
	}
}

func TestAnEditThatLostAdoptsTheServerRowAndSaysSo(t *testing.T) {
	uc, store, server, notifier := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}

	winner := domain.SyncEntity{
		Kind: domain.EntityCollection, ID: "col-1", Version: 9, Payload: []byte("theirs"),
	}
	server.verdicts = []domain.Verdict{
		{EntityID: "col-1", Status: domain.PushSuperseded, Winner: &winner},
	}

	round(t, uc)

	// The server's row is the one that lives, so it is applied whole and the entry goes with it.
	if len(store.superseded) != 1 || store.superseded[0] != "col-1" {
		t.Errorf("superseded = %v, want the edit applied over", store.superseded)
	}
	if len(store.accepted) != 0 {
		t.Error("an edit that lost was recorded as one that landed")
	}

	// And somebody is told: a person who typed something is owed the sentence that says it was not
	// kept.
	status, err := uc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Conflict == nil || status.Conflict.EntityID != "col-1" {
		t.Errorf("conflict = %+v, want the row that lost", status.Conflict)
	}
	if len(notifier.topics) == 0 {
		t.Error("no window was told that an edit lost")
	}
}

func TestAPushSendsParentsBeforeTheirChildren(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{
		change(domain.EntityRequest, "req-1"),
		change(domain.EntityVariable, "var-1"),
		change(domain.EntityCollection, "col-1"),
		change(domain.EntityWorkspace, remote),
		change(domain.EntityEnvironment, "env-1"),
	}
	server.verdicts = []domain.Verdict{
		{EntityID: "req-1", Status: domain.PushStored, Version: 4},
		{EntityID: "var-1", Status: domain.PushStored, Version: 4},
		{EntityID: "col-1", Status: domain.PushStored, Version: 4},
		{EntityID: remote, Status: domain.PushStored, Version: 4},
		{EntityID: "env-1", Status: domain.PushStored, Version: 4},
	}

	round(t, uc)
	if len(server.pushed) != 1 {
		t.Fatalf("pushes = %d, want one batch", len(server.pushed))
	}

	var order []domain.EntityType
	for _, entity := range server.pushed[0] {
		order = append(order, entity.Entity.Kind)
	}
	want := []domain.EntityType{
		domain.EntityWorkspace,
		domain.EntityCollection,
		domain.EntityEnvironment,
		domain.EntityRequest,
		domain.EntityVariable,
	}
	for at, kind := range want {
		if order[at] != kind {
			t.Fatalf("the batch is %v, want parents before children: %v", order, want)
		}
	}
}

func TestAStoredRowKeepsTheVersionTheServerAnswered(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}
	server.verdicts = []domain.Verdict{
		{EntityID: "col-1", Status: domain.PushStored, Version: 4},
	}

	round(t, uc)
	if len(store.accepted) != 1 {
		t.Errorf("settled = %v, want the row the server took", store.accepted)
	}
}

func TestARetryThatTheServerAlreadyAppliedIsNotAFailure(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}
	server.verdicts = []domain.Verdict{
		{EntityID: "col-1", Status: domain.PushDuplicate, Version: 4},
	}

	round(t, uc)
	// A batch retried after a timeout is asking for what it already has, and the answer is the same
	// as the one it never heard.
	if len(store.accepted) != 1 || len(store.failed) != 0 {
		t.Errorf("settled = %v, failed = %v, want a duplicate treated as landed",
			store.accepted, store.failed)
	}
}

func TestARefusalThatMayPassIsKeptWithAMomentToTryAgain(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityRequest, "req-1")}
	server.verdicts = []domain.Verdict{
		{EntityID: "req-1", Status: domain.PushRefused, Reason: domain.ReasonParentMissing},
	}

	round(t, uc)
	if len(store.failed) != 1 || store.retry == 0 {
		t.Errorf("failed = %v with a retry of %v, want it kept for another try",
			store.failed, store.retry)
	}
	if len(store.parked) != 0 {
		t.Error("a row waiting for its parent was given up on")
	}
}

func TestARefusalThatWillNotPassIsParkedRatherThanRetried(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityRequest, "req-1")}
	server.verdicts = []domain.Verdict{
		{EntityID: "req-1", Status: domain.PushRefused, Reason: domain.ReasonTooLarge},
	}

	round(t, uc)
	// A row this server will not store is not going to fit next time either, and a device that asked
	// again forever would be a device that never stopped asking.
	if len(store.parked) != 1 {
		t.Errorf("parked = %v, want the row kept where it can be seen", store.parked)
	}
	if len(store.failed) != 0 {
		t.Errorf("failed = %v, want nothing left to retry", store.failed)
	}
}

func TestARecordWithNothingToFileItUnderIsParked(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityRecord, "rec-1")}
	server.outcomes = []domain.RecordVerdict{
		{RecordID: "rec-1", Reason: domain.ReasonEmpty},
	}

	// The server refuses a record with no id and no moment it happened, and both are facts about the
	// row here: it would be refused the same way every time it were sent, so asking again is asking
	// forever. It stays visible instead.
	round(t, uc)
	if len(store.parked) != 1 {
		t.Errorf("parked = %v, want the record kept where it can be seen", store.parked)
	}
	if len(store.failed) != 0 {
		t.Errorf("failed = %v, want nothing left to retry", store.failed)
	}
}

func TestARecordGoesThroughHistoryAndNotThroughAPush(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{
		change(domain.EntityRecord, "rec-1"),
		change(domain.EntityCollection, "col-1"),
	}
	server.outcomes = []domain.RecordVerdict{{RecordID: "rec-1"}}
	server.verdicts = []domain.Verdict{
		{EntityID: "col-1", Status: domain.PushStored, Version: 4},
	}

	round(t, uc)
	if len(server.pushed) != 1 || len(server.pushed[0]) != 1 {
		t.Errorf("pushes = %+v, want only the row", server.pushed)
	}
	if len(server.records) != 1 || server.records[0][0] != "rec-1" {
		t.Errorf("records = %+v, want only the record", server.records)
	}
}

// A run is a document of its own on the wire and it goes through the same door as the record that
// names it: one call, two lists, because the server files them under two kinds of id.
func TestARunGoesThroughHistoryBesideItsRecord(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{
		change(domain.EntityRecord, "rec-1"),
		change(domain.EntityRun, "run-1"),
	}
	server.outcomes = []domain.RecordVerdict{{RecordID: "rec-1"}}
	server.runOutcomes = []domain.RecordVerdict{{RecordID: "run-1"}}

	round(t, uc)
	if len(server.pushed) != 0 {
		t.Errorf("pushes = %+v, want neither of them through the push", server.pushed)
	}
	if len(server.records) != 1 || server.records[0][0] != "rec-1" {
		t.Errorf("records = %+v, want the record", server.records)
	}
	if len(server.runs) != 1 || server.runs[0][0] != "run-1" {
		t.Errorf("runs = %+v, want the run beside it", server.runs)
	}
	// Both lists are answered, so both are settled: a run left in flight because only the records'
	// verdicts were read would go out again every round.
	if len(store.accepted) != 2 {
		t.Errorf("settled = %v, want the record and the run", store.accepted)
	}
}

// A document larger than the server takes will not be smaller next time, and a round that asked
// again would ask forever — holding the rest of the space's history behind the one row every time.
func TestARecordTheServerWillNotTakeForItsSizeIsParked(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityRecord, "rec-1")}
	server.recordsErr = domain.Refuse(domain.CodeServerRefused, nil,
		domain.Args{"reason": domain.ArgTooLarge})

	round(t, uc)
	if len(store.parked) != 1 {
		t.Errorf("parked = %v, want the document kept where it can be seen", store.parked)
	}
	if len(store.failed) != 0 {
		t.Errorf("failed = %v, want nothing left to retry", store.failed)
	}
}

// The same refusal of a batch that is more than one document is not a row's fault: the server says
// it of the call, and the next round asks again with the limits it was given.
func TestABatchTooLargeForTheServerIsTriedAgain(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{
		change(domain.EntityRecord, "rec-1"),
		change(domain.EntityRecord, "rec-2"),
	}
	server.recordsErr = domain.Refuse(domain.CodeServerRefused, nil,
		domain.Args{"reason": domain.ArgTooLarge})

	round(t, uc)
	if len(store.parked) != 0 {
		t.Errorf("parked = %v, want nothing blamed on one row", store.parked)
	}
}

// A row the server never answers about is not the same as one it refused, and asking again is worth
// trying — but only so many times. Past that it is parked where a person can see it.
func TestARecordTheServerNeverAnswersAboutIsParkedInTime(t *testing.T) {
	uc, store, _, _ := newEngine()
	waited := change(domain.EntityRecord, "rec-1")
	waited.Attempts = unansweredTries
	store.owed = []domain.PendingChange{waited}

	// Nothing is said about the record: no outcome names it, so the round has an answer it cannot
	// place. It has now been out as many times as it is allowed to be.
	round(t, uc)
	if len(store.parked) != 1 {
		t.Errorf("parked = %v, want the row kept where it can be seen", store.parked)
	}
	if len(store.failed) != 0 {
		t.Errorf("failed = %v, want nothing left to retry", store.failed)
	}
}

// The same row one round earlier is still worth trying: parking it on the first unanswered round
// would give up on a record whose push merely overlapped a restart.
func TestARecordStillWaitingIsTriedAgainRatherThanParked(t *testing.T) {
	uc, store, _, _ := newEngine()
	waited := change(domain.EntityRecord, "rec-1")
	waited.Attempts = unansweredTries - 1
	store.owed = []domain.PendingChange{waited}

	round(t, uc)
	if len(store.parked) != 0 {
		t.Errorf("parked = %v, want it out again", store.parked)
	}
	if len(store.failed) != 1 {
		t.Errorf("failed = %v, want the row waiting with a moment to try again", store.failed)
	}
}

// The same holds on the door of the space's contents: a refusal this build has no word for is tried
// again, and an entry that keeps coming back so is parked and not asked about every five minutes.
func TestAnEntityTheServerKeepsRefusingForNoKnownReasonIsParkedInTime(t *testing.T) {
	uc, store, server, _ := newEngine()
	stuck := change(domain.EntityRequest, "req-1")
	stuck.Attempts = unansweredTries
	store.owed = []domain.PendingChange{stuck}
	server.verdicts = []domain.Verdict{
		{EntityID: "req-1", Status: domain.PushRefused, Reason: domain.ReasonUnspecified},
	}

	round(t, uc)
	if len(store.parked) != 1 || len(store.failed) != 0 {
		t.Errorf("parked = %v, failed = %v, want the row kept where it can be seen",
			store.parked, store.failed)
	}
}

func TestARecordSomebodyTookBackIsTakenBackOnTheServer(t *testing.T) {
	uc, store, server, _ := newEngine()
	gone := change(domain.EntityRecord, "rec-1")
	gone.Entity.Deleted = true
	store.owed = []domain.PendingChange{gone}

	round(t, uc)
	if len(server.retract) != 1 || server.retract[0] != "rec-1" {
		t.Errorf("retracted = %v, want the record somebody deleted", server.retract)
	}
	if len(server.records) != 0 {
		t.Error("a record that is gone was uploaded")
	}
	if len(store.accepted) != 1 {
		t.Errorf("settled = %v, want the entry done with", store.accepted)
	}
}

func TestABatchThatNeverArrivedIsPutBackInTheQueue(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}
	server.pushErr = domain.Refuse(domain.CodeServerUnreachable, nil, nil)

	status := round(t, uc)

	// A round that could not reach the server is not an error out of the engine: it is what the
	// window draws, because a server that is away is the ordinary case rather than a bug.
	if status.Failure == nil || status.Failure.Code != domain.CodeServerUnreachable {
		t.Errorf("failure = %+v, want the server being away", status.Failure)
	}
	// An entry claimed by a round that failed is an entry nobody is sending, and leaving it in flight
	// would leave it there until the next start.
	if store.released != 1 {
		t.Errorf("released = %d, want the batch put back", store.released)
	}
}

func TestAPageIsAppliedWithTheCursorItCameWith(t *testing.T) {
	uc, store, server, _ := newEngine()
	server.page = domain.Page{
		Cursor: 41,
		Changes: []domain.SyncChange{
			{Seq: 40, Entity: domain.SyncEntity{Kind: domain.EntityCollection, ID: "col-1"}},
		},
	}

	round(t, uc)
	if len(store.applied) != 1 {
		t.Fatalf("applied = %+v, want the page", store.applied)
	}
	if store.applied[0].cursor != 41 || len(store.applied[0].changes) != 1 {
		t.Errorf("applied = %+v, want one change and the cursor it answered",
			store.applied[0])
	}
	if len(store.marked) == 0 || store.marked[len(store.marked)-1] != domain.SyncSynced {
		t.Errorf("marked = %v, want the space left synced", store.marked)
	}
}

func TestAServerThatCannotAnswerFromTheCursorStartsTheSpaceOver(t *testing.T) {
	uc, store, server, _ := newEngine()
	server.page = domain.Page{Resync: true}

	round(t, uc)
	if len(store.reset) != 1 || store.reset[0] != spaceA {
		t.Errorf("reset = %v, want the space offered again from nothing", store.reset)
	}
}

func TestASpaceIsLeftAloneWhileNobodyIsSignedIn(t *testing.T) {
	store := &fakeStore{spaces: []domain.SyncSpace{
		{ID: spaceA, Name: "Team", RemoteID: remote},
	}}
	server := &fakeServer{}
	uc := NewUseCase(store, server, fakeSession{
		refusal: domain.Refuse(domain.CodeNotSignedIn, nil, nil),
	}, nil, &fakeNotifier{}, fakeIDs())

	round(t, uc)

	// This is a local app first, and nothing here is on the path of opening a collection: a round
	// with nobody signed in is a round with nothing to do rather than a failure.
	if len(server.pushed) != 0 || store.claimed {
		t.Error("something was sent without an account")
	}
}

// A server that did not answer is not a device that is signed out: the round comes back as the
// failure it is, so the loop backs off for it and the row counts the seconds. A round that ended
// quietly would leave both of them waiting on the ten-minute interval.
func TestARoundThatCouldNotReachTheServerIsNotAQuietOne(t *testing.T) {
	store := &fakeStore{spaces: []domain.SyncSpace{
		{ID: spaceA, Name: "Team", RemoteID: remote},
	}}
	server := &fakeServer{}
	uc := NewUseCase(store, server, fakeSession{
		refusal: domain.Refuse(domain.CodeServerUnreachable, nil, nil),
	}, nil, &fakeNotifier{}, fakeIDs())

	if err := uc.round(context.Background(), nil); err == nil {
		t.Error("the round came back as if it had gone through")
	}
	state, err := uc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state.Failure == nil || state.Failure.Code != domain.CodeServerUnreachable {
		t.Errorf("failure = %+v, want the server being away", state.Failure)
	}
}

// A round claims the queue and moves the cursors, and two at once — the button and the loop — would
// claim the same entries and release each other's. The second waits for the first.
func TestTwoRoundsDoNotRunAtOnce(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}
	server.hold = make(chan struct{})

	var done sync.WaitGroup
	for range 2 {
		done.Add(1)
		go func() {
			defer done.Done()
			_, _ = uc.RunRound(context.Background())
		}()
	}
	time.Sleep(100 * time.Millisecond)
	if got := server.inPush.Load(); got != 1 {
		t.Errorf("%d pushes are being made, want the second round to wait for the first", got)
	}
	close(server.hold)
	done.Wait()

	if most := server.most.Load(); most != 1 {
		t.Errorf("at most %d pushes were in flight at once, want 1", most)
	}
	if len(server.pushed) != 2 {
		t.Errorf("pushes = %d, want each round to go through when its turn came", len(server.pushed))
	}
}

// A caller that gives up while it waits for the round in progress is let go and does not run.
func TestAWaitingRoundThatIsCancelledDoesNotRun(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}
	server.hold = make(chan struct{})
	first := make(chan struct{})
	go func() {
		defer close(first)
		_, _ = uc.RunRound(context.Background())
	}()
	for server.inPush.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := uc.RunRound(ctx)
	if err == nil || server.entered.Load() != 1 {
		t.Errorf("a round whose caller had gone gave %v with %d pushes begun, want it to wait and leave",
			err, server.entered.Load())
	}
	close(server.hold)
	<-first
}

func TestASpaceWithNothingToPushIsStillPulled(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = nil
	server.page = domain.Page{Cursor: 7}

	round(t, uc)
	// The other devices may have done something while this one was asleep, and an engine that only
	// pulled when it had something to say would never hear about it.
	if server.pulls != 1 {
		t.Errorf("pulls = %d, want the space read anyway", server.pulls)
	}
}

// A role the account named without the word that writes is one this device holds its work back for:
// a change sent from here would be refused, and a round that sent it anyway would fill the queue
// with refusals and park the person's work where they cannot see it. The space is still read — what
// the others put in it arrives whatever this person may do with it.
func TestASpaceTheAccountSaysThisPersonMayNotWriteInIsNotPushedTo(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces[0].ReadOnly = true
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}

	round(t, uc)

	if len(server.pushed) != 0 {
		t.Errorf("pushed = %+v, want the work kept where the person can see it", server.pushed)
	}
	if server.pulls != 1 {
		t.Errorf("pulls = %d, want the space read all the same", server.pulls)
	}
}

// A reading the account never gave is not a reading that says no: a space this device holds alone,
// and one linked before the role was ever asked for, are both a writer's. A device that withheld
// its work on an answer it did not get would quietly stop syncing, and the server judges every
// change on its own either way.
func TestASpaceNobodyAnsweredAboutIsStillWrittenTo(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}

	round(t, uc)

	if len(server.pushed) != 1 {
		t.Errorf("pushed = %+v, want the work sent where nobody said otherwise", server.pushed)
	}
}

func TestASpaceWithNoPairIsMadeInTheAccountRatherThanGuessedAt(t *testing.T) {
	store := &fakeStore{spaces: []domain.SyncSpace{
		{ID: spaceA, Name: "Mine", Kind: domain.WorkspacePersonal}}}
	server := &fakeServer{remote: []domain.RemoteSpace{
		{ID: "one", Name: "Only"}, {ID: "two", Name: "Another"},
	}}
	uc := NewUseCase(store, server, fakeSession{}, nil, &fakeNotifier{}, fakeIDs())

	round(t, uc)

	// Two spaces in the account and one here: which of the two this device's space is, is a question
	// nothing can answer, so it is neither of them — it is made as one of its own, and both of the
	// account's are taken as they are. Nobody's work lands in somebody else's space by a guess.
	if len(server.asked) != 1 || server.asked[0].name != "Mine" {
		t.Errorf("asked = %+v, want this device's space made in the account", server.asked)
	}
	if got := store.spaces[0].RemoteID; got == "one" || got == "two" || got == "" {
		t.Errorf("remote = %q, want a space of its own", got)
	}
	if len(store.mirrored) != 2 {
		t.Errorf("mirrored = %v, want both spaces the account has", store.mirrored)
	}
}

// One space here and one there is not a pair. Being the only one on each side is not evidence of
// anything — the two have nothing in common but their number — so the account's space is taken as
// it is, and the one here leans beside it until somebody names it or puts something in it.
//
// The account's own space is the nameless one; a personal space it has *named* is one somebody made
// there — in a panel, or by a build from before the server stopped allowing that rename — and it is
// as much somebody else's row as any other.
func TestASpaceIsNotPairedJustForBeingTheOnlyOneThere(t *testing.T) {
	store := &fakeStore{spaces: []domain.SyncSpace{
		{ID: spaceA, Name: "", Kind: domain.WorkspacePersonal}}}
	server := &fakeServer{
		remote: []domain.RemoteSpace{
			{ID: "only", Name: "Personal", OwnOrg: true, Kind: domain.WorkspacePersonal},
		},
		ownOrg: ownOrg,
	}
	uc := NewUseCase(store, server, fakeSession{}, nil, &fakeNotifier{}, fakeIDs())

	round(t, uc)

	if got := store.spaces[0].RemoteID; got != "" {
		t.Errorf("remote = %q, want the space left where it is", got)
	}
	if len(store.mirrored) != 1 || store.mirrored[0] != "only" {
		t.Errorf("mirrored = %v, want the account's space taken as it is", store.mirrored)
	}
	if len(server.asked) != 0 {
		t.Errorf("asked = %+v, want nothing made for a nameless empty space", server.asked)
	}
}

// ---- what the account has, and what it no longer has ------------------------------------

func TestASpaceTheAccountHasAndThisDeviceDoesNotIsTakenAsItIs(t *testing.T) {
	uc, store, server, notifier := newEngine()
	server.remote = append(server.remote, domain.RemoteSpace{ID: "theirs", Name: "Payments"})

	round(t, uc)

	if len(store.mirrored) != 1 || store.mirrored[0] != "theirs" {
		t.Fatalf("mirrored = %v, want the space this device never had", store.mirrored)
	}
	// And it is exchanged in the round it appeared in, not the next one: a space that shows up late
	// is a space whose contents show up late as well.
	if server.pulls != 2 {
		t.Errorf("pulls = %d, want both spaces read", server.pulls)
	}
	if notifier.told() == 0 {
		t.Error("no window was told that a space had appeared")
	}
}

// The space the app is born with, while nobody has named it and nothing was built in it, is not one
// somebody made: the account has a personal space of its own, and a second empty one beside it is a
// space nobody asked for.
func TestAnEmptyNamelessSpaceIsNotMadeInTheAccount(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = []domain.SyncSpace{
		{ID: domain.WorkspacePersonalID, Kind: domain.WorkspacePersonal}}
	server.remote = []domain.RemoteSpace{
		{ID: "one", Name: "Payments"}, {ID: "two", Name: "Another"},
	}

	round(t, uc)

	if len(server.asked) != 0 {
		t.Errorf("asked = %+v, want nothing made for a bare nameless space", server.asked)
	}
	if got := store.spaces[0].RemoteID; got != "" {
		t.Errorf("remote = %q, want the space left here", got)
	}
	if len(store.mirrored) != 2 {
		t.Errorf("mirrored = %v, want both of the account's spaces taken", store.mirrored)
	}
}

// And the moment it is used it is somebody's space, and it goes up with everything in it — a rule
// that kept it here would be a rule that lost the work.
func TestANamelessSpaceWithSomethingInItIsMadeInTheAccount(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = []domain.SyncSpace{
		{ID: domain.WorkspacePersonalID, Kind: domain.WorkspacePersonal}}
	store.holds = map[string]bool{domain.WorkspacePersonalID: true}
	server.remote = []domain.RemoteSpace{
		{ID: "one", Name: "Payments"}, {ID: "two", Name: "Another"},
	}

	round(t, uc)

	if len(server.asked) != 1 {
		t.Fatalf("asked = %+v, want the used space made in the account", server.asked)
	}
	if got := store.spaces[0].RemoteID; got == "" {
		t.Error("the space was made in the account and left unlinked here")
	}
}

// The app's own space and the account's own are one space under one name — the empty one, which is
// what neither side has named. Finding it is what makes a device that has never been here land on
// the space that holds the work rather than beside it.
func TestTheTwoPersonalSpacesAreFoundByTheirName(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = []domain.SyncSpace{
		{ID: domain.WorkspacePersonalID, Kind: domain.WorkspacePersonal}}
	// The account's personal space is the nameless one; the other was made in the panel.
	server.remote = []domain.RemoteSpace{
		{ID: "personal-of-account", Name: "", OwnOrg: true, Kind: domain.WorkspacePersonal},
		{ID: "two", Name: "Payments"},
	}

	round(t, uc)

	if got := store.spaces[0].RemoteID; got != "personal-of-account" {
		t.Errorf("remote = %q, want the account's own nameless space", got)
	}
	if len(server.asked) != 0 || len(store.mirrored) != 1 {
		t.Errorf("asked = %+v, mirrored = %v, want one pairing and one mirror",
			server.asked, store.mirrored)
	}
}

// Two spaces of one name are two spaces, and being one's own is not being one's alone: a space
// standing in the organisation the person owns is still somebody's, and it may be a colleague's.
// A name is what a person typed, and what a space is, is not what it is called.
//
// What this replaces paired the two — the local one's work offered whole into the account's — for
// sharing a word. That is how a month of this machine's work ends up in a space its owner never
// offered, and a name is not a question a person was asked.
func TestASpaceOfOneNameInOnesOwnOrgIsStillAnotherSpace(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = []domain.SyncSpace{
		{ID: "here", Name: "Платежи", Kind: domain.WorkspacePersonal}}
	server.remote = []domain.RemoteSpace{
		{ID: "cloud-1", Name: "Платежи", OwnOrg: true, Kind: domain.WorkspacePersonal},
		{ID: "cloud-2", Name: "Другое"},
	}

	round(t, uc)

	if got := store.spaces[0].RemoteID; got == "cloud-1" || got == "" {
		t.Errorf("remote = %q, want a space of this device's own", got)
	}
	if len(server.asked) != 1 || server.asked[0].name != "Платежи" {
		t.Errorf("asked = %+v, want this device's space made in the account", server.asked)
	}
	if len(store.mirrored) != 2 {
		t.Errorf("mirrored = %v, want both of the account's spaces taken as they are", store.mirrored)
	}
}

// A device's own space is never paired with somebody else's. A space the person was invited into is
// one this device reads and works in, and the local space beside it is not one it offers into it.
//
// What this replaces paired the two whenever they were the only ones on their side: a space
// somebody had worked in for a month, and a team's space they had just been invited to, and the
// whole of the local space moved into the team's. A name makes no difference either — two spaces of
// one name are one space only when both of them are the person's.
func TestALocalSpaceIsNotPairedWithSomebodyElsesSpace(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = []domain.SyncSpace{
		{ID: "here", Name: "Платежи", Kind: domain.WorkspacePersonal}}
	server.remote = []domain.RemoteSpace{{ID: "theirs", Name: "Платежи"}}

	round(t, uc)

	if got := store.spaces[0].RemoteID; got == "theirs" {
		t.Errorf("remote = %q, want the local space kept out of somebody else's", got)
	}
	if len(store.mirrored) != 1 || store.mirrored[0] != "theirs" {
		t.Errorf("mirrored = %v, want their space taken as it is", store.mirrored)
	}
	// Its own space is made in the organisation the person owns instead, and never wherever they
	// happened to be working last: that is where this machine's work belongs.
	if len(server.asked) != 1 || server.asked[0].org != ownOrg {
		t.Errorf("asked = %+v, want a space made in the organisation the person owns", server.asked)
	}
}

// And being one's own is not being one's alone. A team space of the person's own is one other
// people are in, and a device that paired with it would hand the whole of this machine to whoever
// was invited: the same disaster as a stranger's space, one invitation away.
func TestALocalSpaceIsNotPairedWithATeamSpaceOfOnesOwn(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = []domain.SyncSpace{
		{ID: "here", Name: "Платежи", Kind: domain.WorkspacePersonal}}
	server.remote = []domain.RemoteSpace{
		{ID: "our-team", Name: "Платежи", OwnOrg: true, Kind: domain.WorkspaceTeam},
	}

	round(t, uc)

	if got := store.spaces[0].RemoteID; got == "our-team" {
		t.Errorf("remote = %q, want the local space kept out of a shared one", got)
	}
	if len(store.mirrored) != 1 || store.mirrored[0] != "our-team" {
		t.Errorf("mirrored = %v, want the team space taken as it is", store.mirrored)
	}
	if len(server.asked) != 1 || server.asked[0].kind != domain.WorkspacePersonal {
		t.Errorf("asked = %+v, want a personal space made in the account", server.asked)
	}
}

// The space the app is born with is paired with the account's own by what the two of them are, and
// not by a name: the pair is a personal space with no name standing in the organisation the caller
// owns — the shape the server protects from a rename and a deletion — and it is found whatever this
// device has called its own.
//
// A name was the old way, and it was the reason a rename broke the pair: "whoever renames it has
// named it for everybody, and is found by that name from then on". What a space is cannot be typed
// over.
func TestTheBornWithSpaceIsPairedByWhatItIs(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = []domain.SyncSpace{
		{ID: domain.WorkspacePersonalID, Kind: domain.WorkspacePersonal}}
	server.remote = []domain.RemoteSpace{
		{ID: "their-own", OwnOrg: true, Kind: domain.WorkspacePersonal},
		{ID: "team", Name: "Работа", OwnOrg: true, Kind: domain.WorkspaceTeam},
	}

	round(t, uc)

	if got := store.spaces[0].RemoteID; got != "their-own" {
		t.Errorf("remote = %q, want the account's own space paired with the born-with one", got)
	}
	if len(server.asked) != 0 {
		t.Errorf("asked = %+v, want a pairing rather than a space made", server.asked)
	}
}

// And the pair above is not a create as well. The rows a round reads are from before it began, so
// the space it has just paired still looks unlinked while the same round is making spaces — and a
// round that made this one would leave a second space beside it in the account.
//
// Naming it is what makes it worth sending: an unnamed one is held back anyway, which is why this
// is the case the guard is for rather than the nameless one beside it.
func TestAPairedSpaceIsNotAlsoMade(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = []domain.SyncSpace{
		{ID: domain.WorkspacePersonalID, Name: "Дом", Kind: domain.WorkspacePersonal}}
	server.remote = []domain.RemoteSpace{
		{ID: "their-own", OwnOrg: true, Kind: domain.WorkspacePersonal},
	}

	round(t, uc)

	if len(server.asked) != 0 {
		t.Errorf("asked = %+v, want nothing made for a space that was just paired", server.asked)
	}
	if got := store.spaces[0].RemoteID; got != "their-own" {
		t.Errorf("remote = %q, want the account's own space", got)
	}
}

func TestASpaceIsTakenOnceAndNotEveryRound(t *testing.T) {
	uc, store, server, _ := newEngine()
	server.remote = append(server.remote, domain.RemoteSpace{ID: "theirs", Name: "Payments"})

	round(t, uc)
	round(t, uc)

	if len(store.mirrored) != 1 {
		t.Errorf("mirrored = %v, want one space taken once", store.mirrored)
	}
}

// A space the account no longer has is deleted here, with everything it holds: that is what the
// person who deleted it asked for, and the devices are the only way the deletion reaches their own
// machine. The panel keeps the row for a month, which is where a mistake is undone.
func TestASpaceTheAccountNoLongerHasIsDeletedHere(t *testing.T) {
	uc, store, server, notifier := newEngine()
	// The account has other spaces and not this one: it was deleted in the panel, or somebody was
	// removed from it.
	server.remote = []domain.RemoteSpace{{ID: "theirs", Name: "Payments"}}

	round(t, uc)

	if len(store.discarded) != 1 || store.discarded[0] != spaceA {
		t.Fatalf("discarded = %v, want the space the account does not have", store.discarded)
	}
	// Nothing was addressed to it on the way out: the account would refuse every word about a space it
	// has deleted, and a device that keeps asking is one burning somebody's battery to learn nothing.
	for _, named := range server.namedPull {
		if named == remote {
			t.Errorf("the deleted space was read: %v", server.namedPull)
		}
	}
	if notifier.told() == 0 {
		t.Error("no window was told that a space had gone")
	}
}

// A space of another account is not one this device concludes anything about: another account
// into this machine answers with its own list, and a space of the first account is absent from it
// without having been deleted by anybody.
func TestASpaceOfAnotherAccountIsNotDeleted(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces[0].RemoteAccount = domain.AccountKey("https://server.test", "somebody@else.test")
	server.remote = []domain.RemoteSpace{{ID: "theirs", Name: "Payments"}}

	round(t, uc)

	if len(store.discarded) != 0 {
		t.Errorf("discarded = %v, want another account's space left alone", store.discarded)
	}
	// And it is not drawn as gone either: what the account that is signed in answers says nothing
	// about it, which is a different fact from a space having been deleted.
	status, err := uc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, space := range status.Spaces {
		if space.ID == spaceA && space.Gone {
			t.Errorf("spaces = %+v, want another account's space not marked gone", status.Spaces)
		}
	}
}

// The app keeps its things in a space, so a deletion that emptied this device leaves it the one
// born with rather than nothing at all.
func TestADiscardThatEmptiesTheDeviceLeavesOneSpace(t *testing.T) {
	uc, store, server, _ := newEngine()
	server.remote = []domain.RemoteSpace{{ID: "theirs", Name: "Payments"}}

	round(t, uc)

	if !store.ensured {
		t.Error("the last space was removed and no space was left to keep things in")
	}
}

// A space that comes back is one this device exchanges with again: what is held is the account's
// last answer, and nothing about it has been written down.
func TestASpaceThatComesBackIsExchangedWithAgain(t *testing.T) {
	uc, store, server, _ := newEngine()
	server.remote = []domain.RemoteSpace{{ID: "theirs", Name: "Payments"}}
	round(t, uc)

	server.remote = append(server.remote, domain.RemoteSpace{ID: remote, Name: "Team"})
	store.claimed = false
	round(t, uc)

	if !store.claimed {
		t.Error("a space the account has again was left alone")
	}
}

func TestNothingIsGoneBeforeTheAccountHasAnswered(t *testing.T) {
	uc, _, _, _ := newEngine()

	// Nobody has asked yet, so no space is missing from an answer that was never given.
	status, err := uc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, space := range status.Spaces {
		if space.Gone {
			t.Errorf("space %q is gone before the account has been asked", space.Name)
		}
	}
}

func TestNothingLeavesTheMachineWhileEverythingIsPaused(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces[0].State = domain.SyncPaused
	store.spaces = append(store.spaces, domain.SyncSpace{ID: "here", Name: "Mine"})

	round(t, uc)

	// The switch means nothing leaves, and making a space in the account is something leaving.
	if len(server.asked) != 0 || len(store.mirrored) != 0 {
		t.Errorf("asked = %+v, mirrored = %v, want a pause respected",
			server.asked, store.mirrored)
	}
}

func TestASpaceTheAccountWillNotMakeStaysHereAndIsSaidSo(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = append(store.spaces, domain.SyncSpace{ID: "here", Name: "Mine"})
	server.makeErr = domain.Refuse(domain.CodeNotAllowed, nil, nil)

	status := round(t, uc)

	// An organisation may withhold the right to make a space from ordinary members, and that is not a
	// reason for the spaces beside it to stop — but it is not silence either.
	//
	// What it is left with is the id it was refused under rather than nothing: a refusal is not
	// news that the call landed, and a space that named a second id next round would be a space
	// making two if the first call turns out to have arrived after all.
	if space := store.spaces[1]; !space.LinkPending || space.RemoteID != "chosen-1" {
		t.Errorf("space = %+v, want it left here under the id it was refused under", space)
	}
	if status.Failure == nil {
		t.Error("a space the account would not make was not said out loud")
	}
}

// A create whose answer never arrived: the id was written down before the call, so the round asks
// under the same id — and the account answers the space it already made rather than making a second
// one. Nothing about a name is looked for, because a name is not what a space is.
func TestASpaceWhoseAnswerWasLostIsAskedForAgainUnderItsId(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = append(store.spaces, domain.SyncSpace{
		ID: "here", Name: "Mine", RemoteID: "named-mine", LinkPending: true,
		RemoteAccount: accountOf(),
	})

	round(t, uc)

	if len(server.asked) != 1 || server.asked[0].id != "named-mine" {
		t.Fatalf("asked = %+v, want the id already named for this space", server.asked)
	}
	if len(store.pending) != 0 {
		t.Errorf("pending = %v, want no second id named for a space that has one", store.pending)
	}
	if space := store.spaces[1]; space.RemoteID != "named-mine" || space.LinkPending {
		t.Errorf("space = %+v, want it linked under the id it was made under", space)
	}
}

// And a call that never landed is made again, under the id the space already has. A round that
// named a second id for it would be a round making a second space: it cannot tell "the call did not
// land" from "the answer did not come back", and the first one is not worth a space the account
// may already hold.
func TestACallThatNeverLandedIsMadeAgainUnderTheSameId(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = append(store.spaces, domain.SyncSpace{ID: "here", Name: "Mine"})
	server.makeErr = domain.Refuse(domain.CodeServerRefused, nil, nil)

	round(t, uc)

	if len(server.asked) != 1 || server.asked[0].id == "" {
		t.Fatalf("asked = %+v, want the space made under an id this device named", server.asked)
	}
	named := server.asked[0].id
	if space := store.spaces[1]; space.RemoteID != named || !space.LinkPending {
		t.Fatalf("space = %+v, want the id named and the making unconfirmed", space)
	}

	// The round that follows asks under that id again rather than naming another. It is also the
	// round that would delete the space, if an id nobody has confirmed were read as an absence.
	server.asked = nil
	round(t, uc)

	if len(server.asked) != 1 || server.asked[0].id != named {
		t.Errorf("asked = %+v, want the same id asked under again", server.asked)
	}
	if len(store.discarded) != 0 {
		t.Errorf("discarded = %v, want a space nobody has confirmed left alone", store.discarded)
	}
}

// ---- one direction is not the other's price ----------------------------------------------

func TestAPageIsAppliedEvenWhenThePushFailed(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}
	server.pushErr = domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	server.page = pageWith(41, domain.EntityCollection, "col-1")

	status := round(t, uc)

	// What another device sent is nobody's fault but the wire's, and the person whose edits are stuck
	// is the last person who should also stop hearing about the rest.
	if len(store.applied) != 1 {
		t.Errorf("applied = %v, want the space read anyway", store.applied)
	}
	if store.released != 1 {
		t.Errorf("released = %d, want what was in flight put back", store.released)
	}
	if status.Failure == nil || status.Failure.Code != domain.CodeServerUnreachable {
		t.Errorf("failure = %+v, want the push's own reason", status.Failure)
	}
}

func TestASpaceIsNotRecordedAsSyncedWhenItsPushFailed(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}
	server.pushErr = domain.Refuse(domain.CodeServerUnreachable, nil, nil)

	round(t, uc)

	// "Synced two minutes ago" beside a list of nineteen that have not gone is exactly the sentence
	// the rest of this package is written to avoid. A half round is not a round.
	for _, state := range store.marked {
		if state == domain.SyncSynced {
			t.Fatalf("marked = %v, want no sync recorded", store.marked)
		}
	}
}

func TestTheFailureAWindowHearsIsTheOneThatHappenedFirst(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}
	server.pushErr = domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	server.pullErr = domain.Refuse(domain.CodeServerRefused, nil, nil)

	status := round(t, uc)

	if status.Failure == nil || status.Failure.Code != domain.CodeServerUnreachable {
		t.Errorf("failure = %+v, want the push's reason rather than the pull's", status.Failure)
	}
}

func TestAStoppedEngineIsNotRunningAndSaysSo(t *testing.T) {
	uc, _, _, _ := newEngine()

	if err := uc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = uc.Stop(ctx)
	}()

	status, err := uc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Running {
		t.Error("a started engine says it is not running")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := uc.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// A second stop is what a shutdown does after a restart of the graph, and it is not an error.
	if err := uc.Stop(ctx); err != nil {
		t.Fatalf("Stop twice: %v", err)
	}
}

// round runs one round and fails the test if the engine could not even try it. It is the shape
// every test above uses: the round is the unit, and what it did to the store is the assertion.
//
// A round that came to nothing is not an error here — the engine says so in Status, which is what a
// window draws — so a test about a failed round asserts on that rather than on an error.
func round(t *testing.T, uc *UseCase) Status {
	t.Helper()

	status, err := uc.RunRound(context.Background())
	if err != nil {
		t.Fatalf("a round: %v", err)
	}
	return status
}

// The numbers the sync row is drawn by are published while the round is still going: a round with
// one change in its outbox is shown as one sent before the round is over, and the last word says
// the round has ended.
func TestARoundIsCountedWhileItIsGoing(t *testing.T) {
	uc, store, _, notifier := newEngine()
	store.owed = []domain.PendingChange{change(domain.EntityCollection, "col-1")}

	round(t, uc)

	statuses := notifier.published()
	if len(statuses) == 0 {
		t.Fatal("no window was told where the round stands")
	}
	if last := statuses[len(statuses)-1]; last.Round {
		t.Errorf("the last status still says a round is on: %+v", last)
	}
	if !slices.ContainsFunc(statuses, func(status Status) bool {
		return status.Round && status.Pushed == 1
	}) {
		t.Errorf("no status counted the one change while the round was on: %+v", statuses)
	}
}

func TestPausingStopsEverySpaceThatWasSyncing(t *testing.T) {
	uc, store, server, _ := newEngine()

	if _, err := uc.Pause(context.Background()); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if len(store.states) != 1 || store.states[0] != spaceA+":"+string(domain.SyncPaused) {
		t.Errorf("states = %v, want every linked space paused", store.states)
	}

	// And a paused space is left alone by the round that follows: what a person turned off must not be
	// pushed by the timer a moment later.
	round(t, uc)
	if len(server.pushed) != 0 || len(store.applied) != 0 {
		t.Error("a paused space was exchanged with anyway")
	}
}

func TestResumingPutsBackOnlyWhatWasSyncing(t *testing.T) {
	uc, store, _, _ := newEngine()
	store.spaces = append(store.spaces, domain.SyncSpace{
		ID: "never", Name: "Fresh", RemoteID: "remote-fresh",
	})
	store.spaces[0].SyncedAt = 1_700_000_000_000

	if _, err := uc.Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// A space that had synced before goes back to being synced; one that never has goes back to being
	// local, because it is.
	want := []string{spaceA + ":" + string(domain.SyncSynced), "never:" + string(domain.SyncLocal)}
	if len(store.states) != len(want) {
		t.Fatalf("states = %v, want %v", store.states, want)
	}
	for at, state := range want {
		if store.states[at] != state {
			t.Errorf("states = %v, want %v", store.states, want)
		}
	}
}

func TestASpaceNobodyLinkedIsNotPaused(t *testing.T) {
	uc, store, _, _ := newEngine()
	store.spaces = append(store.spaces, domain.SyncSpace{ID: "unlinked", Name: "Nowhere"})

	if _, err := uc.Pause(context.Background()); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	// There is nothing to pause about a space with nowhere to go, and saying so would be a state
	// nobody could see the point of.
	for _, state := range store.states {
		if state == "unlinked:"+string(domain.SyncPaused) {
			t.Errorf("states = %v, want only the linked spaces", store.states)
		}
	}
}

// The first wait after a failure is short — what this engine mostly meets is a network that is
// still coming back — and each one after it is longer, up to the ceiling.
func TestAFailingEngineBacksOffFromAShortFirstWait(t *testing.T) {
	wait := retryAfter(0)
	if wait != firstRetry || wait >= defaultInterval {
		t.Fatalf("the wait after one failure = %v, want %v, shorter than the interval", wait, firstRetry)
	}

	for range 20 {
		next := retryAfter(wait)
		if next < wait {
			t.Fatalf("the wait went from %v down to %v", wait, next)
		}
		wait = next
	}
	if wait != maxBackoff {
		t.Errorf("the wait after twenty failures = %v, want the ceiling %v", wait, maxBackoff)
	}
}

// A round that failed says when it will be tried again, and a round that goes through takes the
// moment back: the row counts the seconds down from it, and it must stop when there is nothing to
// count down to.
func TestTheWaitBeforeARetryIsPublishedAndTakenBack(t *testing.T) {
	uc, _, _, _ := newEngine()
	ctx := context.Background()

	uc.retrying(firstRetry, true)
	state, err := uc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state.RetryAt == 0 {
		t.Error("a failed round did not say when it is tried again")
	}

	uc.beginRound()
	state, err = uc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state.RetryAt != 0 {
		t.Errorf("retryAt = %d, want nothing waited on while a round is on", state.RetryAt)
	}
}

// History half of a round: read above the device's cursor, written where the panel reads it.

// historyOf is the space the engine holds, with the history cursor it has got to.
func historySpace(cursor int64) []domain.SyncSpace {
	return []domain.SyncSpace{{
		ID: spaceA, Name: "Team", RemoteID: remote, State: domain.SyncSynced,
		RemoteAccount: accountOf(), HistoryCursor: cursor,
	}}
}

func TestTheHistoryIsReadAboveTheCursorAndWritten(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = historySpace(4)
	server.recordPages = []domain.RecordPage{{
		Changes: []domain.RecordChange{{
			Seq:    5,
			Record: domain.Record{RecordSummary: domain.RecordSummary{ID: "rec-1", Method: "GET"}},
		}},
		Cursor: 5,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := uc.round(ctx, nil); err != nil {
		t.Fatalf("round: %v", err)
	}

	if len(server.namedHistory) != 1 || server.namedHistory[0] != remote {
		t.Errorf("the history was read as %v, want the space the server knows it by",
			server.namedHistory)
	}
	if len(server.fromHistory) != 1 || server.fromHistory[0] != 4 {
		t.Errorf("asked from %v, want the cursor this device holds", server.fromHistory)
	}
	if len(store.historyPages) != 1 {
		t.Fatalf("pages written = %d, want the one that was read", len(store.historyPages))
	}
	written := store.historyPages[0]
	if written.spaceID != spaceA || written.cursor != 5 || len(written.changes) != 1 {
		t.Errorf("written = %+v, want the record and the cursor under this device's own space id",
			written)
	}
}

// A record taken back arrives as the entry alone: there is no record to write, and what the store
// is told is that the one it holds is gone.
func TestARecordTakenBackArrivesAsItsOwnEntry(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = historySpace(1)
	server.recordPages = []domain.RecordPage{{
		Changes: []domain.RecordChange{{Seq: 3, Deleted: true}},
		Cursor:  3,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := uc.round(ctx, nil); err != nil {
		t.Fatalf("round: %v", err)
	}

	written := store.historyPages[0]
	if len(written.changes) != 1 || !written.changes[0].Deleted {
		t.Errorf("written = %+v, want the deletion", written.changes)
	}
	if written.cursor != 3 {
		t.Errorf("cursor = %d, want where the page read to", written.cursor)
	}
}

// A page with nothing in it still moves the cursor, so an empty range is not asked again.
func TestAnEmptyHistoryPageMovesTheCursorPastTheHole(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = historySpace(7)
	server.recordPages = []domain.RecordPage{{Cursor: 9}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := uc.round(ctx, nil); err != nil {
		t.Fatalf("round: %v", err)
	}

	if len(store.historyPages) != 1 || store.historyPages[0].cursor != 9 {
		t.Errorf("written = %+v, want the cursor moved past the numbers no record used",
			store.historyPages)
	}
}

// The floor is where the server's log was cut. A cursor below it is one nothing will ever answer
// above, so the reader is lifted to it rather than left asking after records that are gone.
func TestACursorBelowTheFloorIsLiftedToIt(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = historySpace(2)
	server.recordPages = []domain.RecordPage{{Floor: 40, Cursor: 40, HasMore: true},
		{Cursor: 41}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := uc.round(ctx, nil); err != nil {
		t.Fatalf("round: %v", err)
	}

	if len(server.fromHistory) != 2 || server.fromHistory[1] != 40 {
		t.Errorf("asked from %v, want the second page asked from the floor", server.fromHistory)
	}
	// The first page's number is the floor rather than the cursor this device held: what lies between
	// the two is gone, and the number written down is where the log can still be read from.
	if len(store.historyPages) != 2 || store.historyPages[0].cursor != 40 {
		t.Errorf("written = %+v, want the cursor lifted to the floor", store.historyPages)
	}
}

// two is the engine over two linked spaces, the second of which the account has as well.
func twoSpaces() (*UseCase, *fakeStore, *fakeServer) {
	uc, store, server, _ := newEngine()
	store.spaces = append(store.spaces, domain.SyncSpace{
		ID: "space-b", Name: "Second", RemoteID: "remote-b", State: domain.SyncSynced,
		RemoteAccount: accountOf(),
	})
	server.remote = append(server.remote, domain.RemoteSpace{
		ID: "remote-b", Name: "Second", Kind: domain.WorkspaceTeam,
	})
	return uc, store, server
}

// A round of one space asks the server about that space and about nothing else: the account's list
// did not change because a space did, and the other space has nothing new.
func TestARoundOfOneSpaceAsksAboutThatSpaceOnly(t *testing.T) {
	uc, _, server := twoSpaces()
	listed := 0
	server.onSpaces = func() { listed++ }

	if err := uc.round(context.Background(), []string{"space-b"}); err != nil {
		t.Fatalf("round: %v", err)
	}

	if len(server.namedPull) != 1 || server.namedPull[0] != "remote-b" {
		t.Errorf("pulled %v, want only the space that was named", server.namedPull)
	}
	if listed != 0 {
		t.Errorf("the account's list was asked for %d times, want none", listed)
	}

	if err := uc.round(context.Background(), nil); err != nil {
		t.Fatalf("round: %v", err)
	}
	if listed != 1 {
		t.Errorf("a full round asked for the list %d times, want once", listed)
	}
}

// What was asked since the last round is what the next one is of: named spaces, or all of them when
// anybody asked for all.
func TestWhatWasAskedForIsWhatTheNextRoundIsOf(t *testing.T) {
	uc, _, _ := twoSpaces()

	uc.WakeSpace("space-b")
	uc.WakeSpace("space-b")
	if got := uc.asked(); len(got) != 1 || got[0] != "space-b" {
		t.Errorf("asked = %v, want the one space, once", got)
	}
	if got := uc.asked(); got != nil {
		t.Errorf("asked = %v after it was taken, want nothing to remember", got)
	}

	uc.WakeSpace("space-b")
	uc.Wake()
	if got := uc.asked(); got != nil {
		t.Errorf("asked = %v when everything was asked for, want every space (nil)", got)
	}
}

// A round that keeps failing keeps saying when the next one happens: the row counts the seconds
// down from that moment, and the loop is the only side that knows it.
func TestTheLoopTellsWhenAFailedRoundHappensAgain(t *testing.T) {
	uc, _, server := twoSpaces()
	server.limitsErr = domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	uc.interval = 10 * time.Millisecond
	notifier, ok := uc.notifier.(*fakeNotifier)
	if !ok {
		t.Fatal("the use case was built without the fake notifier")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := uc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitFor(t, func() bool {
		for _, told := range notifier.published() {
			if told.Failure != nil && told.RetryAt > 0 {
				return true
			}
		}
		return false
	}, "the loop told when the failed round is tried again")
}

// A write to one space wakes a round of that space, and the loop runs it without touching the
// other.
func TestAWakeOfOneSpaceRunsARoundOfThatSpace(t *testing.T) {
	uc, _, server := twoSpaces()
	// Streams that stay open, as real ones do: one that ended would ask for a round of its own.
	uc.server = &streams{fakeServer: server, called: make(chan struct{}, 8)}
	uc.interval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := uc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return server.pullCount() == 2 }, "the first round pulled both spaces")

	uc.WakeSpace("space-b")
	waitFor(t, func() bool { return server.pullCount() == 3 }, "the wake ran a round")

	if last := server.lastPull(); last != "remote-b" {
		t.Errorf("the round after the wake pulled %q, want the space that was woken", last)
	}
}

func (f *fakeServer) pullCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.namedPull)
}

func (f *fakeServer) lastPull() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.namedPull[len(f.namedPull)-1]
}

// waitFor polls until the condition holds, for as long as a test can be asked to wait.
func waitFor(t *testing.T, condition func() bool, what string) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", what)
}

type listAsk struct {
	spaceID  string
	from, to int64
	limit    int
	token    string
}

// A device that has never read a space's history reads a snapshot of its window and follows the log
// from where it stood: the history is not replayed from the beginning.
func TestADeviceThatNeverReadTheHistoryReadsASnapshotOfItsWindow(t *testing.T) {
	uc, store, server, _ := newEngine()
	uc.retention = fixedRetention(domain.RetainWeek)
	uc.now = func() time.Time { return time.UnixMilli(1_000_000_000_000) }
	store.spaces = historySpace(0)
	server.list = domain.RecordList{
		Records: []domain.Record{{RecordSummary: domain.RecordSummary{ID: "rec-2", StartedAt: 20}},
			{RecordSummary: domain.RecordSummary{ID: "rec-1", StartedAt: 10}}},
		Head: 812,
	}
	server.recordPages = []domain.RecordPage{{Cursor: 1, HasMore: true, Head: 812}}

	if err := uc.round(context.Background(), nil); err != nil {
		t.Fatalf("round: %v", err)
	}

	if len(server.listed) != 1 || server.listed[0].limit != domain.HistoryKeepCount ||
		server.listed[0].from != 1_000_000_000_000-7*24*3600*1000 {
		t.Errorf("read %+v, want one page of the window: as many as it keeps, as far back as it goes",
			server.listed)
	}
	// The log is asked once, for how far it stands, and not replayed.
	if len(server.namedHistory) != 1 || len(store.historyPages) != 0 {
		t.Errorf("the log was read %d times and %d pages applied, want one look at its head",
			len(server.namedHistory), len(store.historyPages))
	}
	if len(store.snapshots) != 1 || len(store.snapshots[0].records) != 2 ||
		store.snapshots[0].head != 812 {
		t.Errorf("snapshots = %+v, want the two records and the cursor at where the log stood",
			store.snapshots)
	}
}

// A device that is far behind reads a snapshot rather than the log; one that is near reads the log.
func TestADeviceFarBehindTheLogReadsASnapshotAndOneNearItReadsTheLog(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = historySpace(10)
	server.recordPages = []domain.RecordPage{{Cursor: 200, HasMore: true, Head: 10 + maxCatchUp + 1}}
	server.list = domain.RecordList{Head: 9000}

	if err := uc.round(context.Background(), nil); err != nil {
		t.Fatalf("round: %v", err)
	}
	if len(store.snapshots) != 1 || store.snapshots[0].head != 9000 || len(store.historyPages) != 0 {
		t.Errorf("snapshots %+v and pages %+v, want the snapshot and not the page",
			store.snapshots, store.historyPages)
	}

	uc, store, server, _ = newEngine()
	store.spaces = historySpace(10)
	server.recordPages = []domain.RecordPage{{Cursor: 40, Head: 40}}
	if err := uc.round(context.Background(), nil); err != nil {
		t.Fatalf("round: %v", err)
	}
	if len(store.snapshots) != 0 || len(server.listed) != 0 || len(store.historyPages) != 1 {
		t.Errorf("snapshots %d, listed %d, pages %d, want the log read and no snapshot",
			len(store.snapshots), len(server.listed), len(store.historyPages))
	}
}

// Older history is read on request, newest first from a moment, and comes back under this device's
// name for the space.
func TestOlderHistoryIsReadOnRequestUnderTheLocalSpace(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = historySpace(5)
	server.list = domain.RecordList{
		Records: []domain.Record{{RecordSummary: domain.RecordSummary{ID: "rec-1", WorkspaceID: remote}}},
	}

	records, err := uc.Older(context.Background(), spaceA, 500, 40)
	if err != nil {
		t.Fatalf("Older: %v", err)
	}

	if len(server.listed) != 1 || server.listed[0].spaceID != remote || server.listed[0].to != 500 ||
		server.listed[0].limit != 40 {
		t.Errorf("asked %+v, want the server's space, before the moment, at the limit", server.listed)
	}
	if len(records) != 1 || records[0].WorkspaceID != spaceA {
		t.Errorf("records = %+v, want them under this device's own space", records)
	}

	if none, err := uc.Older(context.Background(), "nowhere", 500, 40); err != nil || none != nil {
		t.Errorf("a space that is not here answered %v, %v, want nothing", none, err)
	}
}

// fixedRetention is a retention that says the same thing every time.
type fixedRetention domain.Retention

func (r fixedRetention) Retention(context.Context) (domain.Retention, error) {
	return domain.Retention(r), nil
}

// A space nobody has filed anything in has a history at zero and no snapshot to take: a round that
// read one every time would be a list — and a row in the server's journal — for nothing.
func TestAnEmptyHistoryIsNotReadAsASnapshotByEveryRound(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.spaces = historySpace(0)

	for range 3 {
		if err := uc.round(context.Background(), nil); err != nil {
			t.Fatalf("round: %v", err)
		}
	}
	if len(server.listed) != 0 || len(store.snapshots) != 0 {
		t.Errorf("listed %d and snapshots %d, want no snapshot of an empty history",
			len(server.listed), len(store.snapshots))
	}
}

// A different account signing in releases the previous account's copies that owe nothing.
func TestADifferentAccountSigningInLetsGoOfWhatNothingIsOwedFrom(t *testing.T) {
	uc, store, server, _ := newEngine()
	before := domain.AccountKey("https://server.test", "the-one@before.test")
	store.work = before
	store.owing = map[string]bool{"owes": true}
	store.spaces = []domain.SyncSpace{
		{ID: "synced", Name: "A", RemoteID: "shared", RemoteAccount: before, State: domain.SyncSynced},
		{ID: "owes", Name: "B", RemoteID: "theirs-2", RemoteAccount: before, State: domain.SyncSynced},
		{ID: "local", Name: "C"},
	}
	server.remote = []domain.RemoteSpace{
		{ID: "fresh", Kind: domain.WorkspacePersonal, OwnOrg: true},
		{ID: "shared", Name: "A", Kind: domain.WorkspaceTeam},
	}

	round(t, uc)

	if len(store.letGo) != 1 || store.letGo[0] != "synced" {
		t.Fatalf("let go = %v, want only the space that owed nothing", store.letGo)
	}
	if got := spaceByID(store.spaces, "owes"); got == nil || got.RemoteAccount != before {
		t.Errorf("owes = %+v, want it kept for the account it belongs to", got)
	}
	if got := spaceByID(store.spaces, "local"); got == nil {
		t.Error("a space that is only here was let go of")
	}
	mirrored := map[string]bool{}
	for _, id := range store.mirrored {
		mirrored[id] = true
	}
	if !mirrored["shared"] || !mirrored["fresh"] {
		t.Errorf("mirrored = %v, want the space the two share taken by the account signed in",
			store.mirrored)
	}
}

// Signing out and in as the same account is not a change of account: nothing is let go of.
func TestTheSameAccountSigningInAgainLetsGoOfNothing(t *testing.T) {
	uc, store, _, _ := newEngine()
	store.work = accountOf()

	round(t, uc)

	if len(store.letGo) != 0 {
		t.Errorf("let go = %v, want nothing let go of", store.letGo)
	}
}

// Another address's spaces are left alone.
func TestASpaceOfAnotherPersonIsLeftAlone(t *testing.T) {
	uc, store, server, _ := newEngine()
	theirs := domain.AccountKey("https://server.test", "somebody@acme.com")
	store.work = theirs
	store.owing = map[string]bool{domain.WorkspacePersonalID: true}
	store.spaces = []domain.SyncSpace{{
		ID: domain.WorkspacePersonalID, Kind: domain.WorkspacePersonal, RemoteID: "theirs",
		RemoteAccount: theirs, State: domain.SyncSynced,
	}}
	server.remote = []domain.RemoteSpace{
		{ID: "fresh", Kind: domain.WorkspacePersonal, OwnOrg: true}}

	round(t, uc)

	if len(store.discarded) != 0 || len(store.detached) != 0 {
		t.Errorf("discarded %v, detached %v, want another person's space untouched",
			store.discarded, store.detached)
	}
	if store.spaces[0].RemoteID != "theirs" || store.spaces[0].RemoteAccount != theirs {
		t.Errorf("space = %+v, want it still theirs", store.spaces[0])
	}
	if len(store.mirrored) != 1 || store.mirrored[0] != "fresh" {
		t.Errorf("mirrored = %v, want this account's own space taken beside it", store.mirrored)
	}
	if store.work != accountOf() {
		t.Errorf("the device works as %q, want the account that signed in", store.work)
	}
}

// A space being made for another account is not made in this one.
func TestASpaceBeingMadeForAnotherAccountIsNotMadeInThisOne(t *testing.T) {
	uc, store, server, _ := newEngine()
	theirs := domain.AccountKey("https://server.test", "somebody@acme.com")
	store.spaces = []domain.SyncSpace{{
		ID: "pending", Name: "Секреты", Kind: domain.WorkspacePersonal, RemoteID: "id-they-named",
		RemoteAccount: theirs, LinkPending: true,
	}}
	server.remote = nil

	round(t, uc)

	if len(server.asked) != 0 {
		t.Errorf("asked = %+v, want nothing made in this account", server.asked)
	}
}

// The device works as the new account only after its spaces are here.
func TestTheDeviceWorksAsTheAccountThatSignedInAndOnlyAfterItsSpacesAreHere(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.work = domain.AccountKey("https://server.test", "the-one@before.test")
	store.spaces = nil
	server.remote = []domain.RemoteSpace{
		{ID: "fresh", Kind: domain.WorkspacePersonal, OwnOrg: true}}

	round(t, uc)

	if len(store.mirrored) != 1 {
		t.Fatalf("mirrored = %v, want the account's own space taken", store.mirrored)
	}
	if store.work != accountOf() {
		t.Errorf("the device works as %q, want the account signed in now", store.work)
	}
}

// An empty list from the account deletes nothing.
func TestAnEmptyListFromTheAccountDeletesNothing(t *testing.T) {
	uc, store, server, _ := newEngine()
	server.remote = nil

	round(t, uc)

	if len(store.discarded) != 0 {
		t.Errorf("discarded = %v, want nothing deleted on an empty answer", store.discarded)
	}
}

// Status describes only the spaces in sight.
func TestTheStatusDescribesOnlyTheSpacesInSight(t *testing.T) {
	uc, store, _, _ := newEngine()
	theirs := domain.AccountKey("https://server.test", "somebody@acme.com")
	store.work = accountOf()
	store.spaces = append(store.spaces,
		domain.SyncSpace{ID: "theirs", Name: "Чужое", RemoteID: "r", RemoteAccount: theirs},
		domain.SyncSpace{ID: "local", Name: "Своё"})

	status, err := uc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, space := range status.Spaces {
		if space.ID == "theirs" {
			t.Errorf("status names %+v, want another account's space left out", space)
		}
	}
	if len(status.Spaces) != 2 {
		t.Errorf("spaces = %d, want the account's own and the one that is only here", len(status.Spaces))
	}
}

// The same server written in another case or with a trailing slash is one account.
func TestTheSameServerWrittenTwoWaysIsOneAccount(t *testing.T) {
	a := domain.AccountKey("https://App.Example.com/", " Me@Acme.com ")
	b := domain.AccountKey("https://app.example.com", "me@acme.com")
	if a != b {
		t.Errorf("keys %q and %q, want one account", a, b)
	}
}

// The same address under a new id is the same person: spaces are taken back, not deleted.
func TestTheSameAddressUnderAnotherIdIsTheSamePersonAndTheirSpacesAreTakenBack(t *testing.T) {
	uc, store, server, _ := newEngine()
	store.seen = map[string]string{accountOf(): "id-the-server-forgot"}
	store.work = accountOf()
	store.spaces = []domain.SyncSpace{{
		ID: domain.WorkspacePersonalID, Kind: domain.WorkspacePersonal, RemoteID: "lost",
		RemoteAccount: accountOf(), State: domain.SyncSynced,
	}}
	server.remote = []domain.RemoteSpace{
		{ID: "fresh", Kind: domain.WorkspacePersonal, OwnOrg: true}}

	round(t, uc)

	if len(store.rebound) != 1 || store.rebound[0] != domain.WorkspacePersonalID {
		t.Fatalf("rebound = %v, want the space taken back", store.rebound)
	}
	got := store.spaces[0]
	if got.RemoteID != "fresh" || got.RemoteAccount != accountOf() {
		t.Errorf("space = %+v, want it paired with the account's own", got)
	}
	if len(store.mirrored) != 0 || len(store.discarded) != 0 {
		t.Errorf("mirrored %v and discarded %v, want no second copy and nothing thrown away",
			store.mirrored, store.discarded)
	}
	if store.seen[accountOf()] != meUser {
		t.Errorf("seen = %q, want the account's row as it is now", store.seen[accountOf()])
	}
}

// The same id as before: nothing is taken back.
func TestAnAccountWithTheRowItHadIsNotTakenBack(t *testing.T) {
	uc, store, _, _ := newEngine()
	store.seen = map[string]string{accountOf(): meUser}

	round(t, uc)

	if len(store.rebound) != 0 {
		t.Errorf("rebound = %v, want nothing taken back", store.rebound)
	}
}

// The first sign-in of an address only records its id.
func TestTheFirstSignInOfAnAddressOnlyRemembersItsRow(t *testing.T) {
	uc, store, _, _ := newEngine()

	round(t, uc)

	if len(store.rebound) != 0 || store.seen[accountOf()] != meUser {
		t.Errorf("rebound %v, seen %v, want the row written down and nothing taken back",
			store.rebound, store.seen)
	}
}

// A hidden account's spaces are rebound when its person returns.
func TestASpaceOfAnAccountMadeAgainWhileAnotherWasHereIsTakenBackOnReturn(t *testing.T) {
	uc, store, server, _ := newEngine()
	other := domain.AccountKey("https://server.test", "the-one@before.test")
	store.work = other
	store.seen = map[string]string{accountOf(): "id-before-the-reset"}
	store.owing = map[string]bool{"pending": true}
	store.spaces = []domain.SyncSpace{{
		ID: "pending", Name: "Не отправлено", RemoteID: "lost", RemoteAccount: accountOf(),
		State: domain.SyncSynced,
	}}
	server.remote = []domain.RemoteSpace{
		{ID: "fresh", Kind: domain.WorkspacePersonal, OwnOrg: true}}

	round(t, uc)

	if len(store.rebound) != 1 || store.rebound[0] != "pending" {
		t.Errorf("rebound = %v, want the space kept for the person taken back", store.rebound)
	}
	if len(store.discarded) != 0 {
		t.Errorf("discarded = %v, want nothing thrown away", store.discarded)
	}
}
