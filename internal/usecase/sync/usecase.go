// Package sync coordinates background push/pull sync loops and change notifications.
package sync

import (
	"cmp"
	"context"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// TopicChanged is published when sync status or data changes.
const TopicChanged = "sync:changed"

const (
	defaultInterval = 10 * time.Minute
	settle          = 300 * time.Millisecond
	firstRetry      = 2 * time.Second
	maxBackoff      = 5 * time.Minute
	pullLimit       = 200
)

type UseCase struct {
	store    Store
	server   Server
	session  domain.Principal
	notifier domain.Notifier
	now      func() time.Time
	ids      platform.IDGen
	interval time.Duration
	settle   time.Duration
	till     time.Duration

	// slot is held by whatever is changing the queue or the cursors of the spaces: a round, a resync,
	// a link. Two of them at once would claim the same entries, release each other's, and make two
	// spaces in the account for one.
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

func NewUseCase(
	store Store,
	server Server,
	session domain.Principal,
	retention Retention,
	notifier domain.Notifier,
	ids platform.IDGen,
) *UseCase {
	return &UseCase{
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
// is what a button that says "sync now" needs: the edits made before the click must be covered by
// the round it starts, and a round already going is one that may have read the queue before them.
func (u *UseCase) exclusive(ctx context.Context, work func() error) error {
	select {
	case u.slot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-u.slot }()
	return work()
}

// Recover starts the loop and, more importantly, puts back what a crash left in flight: an entry
// that was being sent when the process died is an entry nobody is sending.
func (u *UseCase) Recover(ctx context.Context) error {
	if err := u.store.ReleaseInFlight(ctx); err != nil {
		// Not fatal: the queue is read again on the next round, and a database that cannot be
		// written to is reported by everything else as well.
		log.Printf("[sync] releasing what was in flight: %v", err)
	}
	return u.Start(ctx)
}

// Start launches the background sync loop asynchronously.
func (u *UseCase) Start(ctx context.Context) error {
	u.mu.Lock()
	if u.cancel != nil {
		u.mu.Unlock()
		return nil
	}
	running, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	u.cancel, u.done, u.running = cancel, done, true
	u.mu.Unlock()

	go u.loop(running, done)
	return nil
}

// Stop terminates the background sync loop and waits for completion.
func (u *UseCase) Stop(ctx context.Context) error {
	u.mu.Lock()
	cancel, done := u.cancel, u.done
	u.cancel, u.done, u.running = nil, nil, false
	u.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()

	u.mu.Lock()
	u.stream = nil
	u.mu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Wake triggers an immediate sync round for all spaces.
func (u *UseCase) Wake() {
	u.mu.Lock()
	u.everything = true
	u.mu.Unlock()
	u.ring()
}

// WakeSpace triggers an immediate sync round for a specific space.
func (u *UseCase) WakeSpace(spaceID string) {
	u.mu.Lock()
	u.dirty[spaceID] = struct{}{}
	u.mu.Unlock()
	u.ring()
}

func (u *UseCase) ring() {
	select {
	case u.wake <- struct{}{}:
	default:
	}
}

// asked returns pending spaces to sync since the last round and resets the queue.
func (u *UseCase) asked() []string {
	u.mu.Lock()
	defer u.mu.Unlock()

	everything, dirty := u.everything, u.dirty
	u.everything, u.dirty = false, map[string]struct{}{}
	if everything || len(dirty) == 0 {
		return nil
	}
	spaces := make([]string, 0, len(dirty))
	for space := range dirty {
		spaces = append(spaces, space)
	}
	return spaces
}

func (u *UseCase) following(spaceID string) bool {
	if u.stream == nil {
		return false
	}
	_, ok := u.stream.spaces[spaceID]
	return ok
}

// Status returns current sync engine state and space statistics.
func (u *UseCase) Status(ctx context.Context) (Status, error) {
	spaces, err := u.visibleSpaces(ctx)
	if err != nil {
		return Status{}, err
	}
	counts, err := u.store.QueueCounts(ctx)
	if err != nil {
		return Status{}, err
	}

	waiting := map[string][]domain.QueuedChange{}
	for _, space := range spaces {
		if counts[space.ID].Pending+counts[space.ID].Failed+counts[space.ID].Parked == 0 {
			continue
		}
		entries, err := u.store.Waiting(ctx, space.ID)
		if err != nil {
			return Status{}, err
		}
		waiting[space.ID] = entries
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	for at := range spaces {
		spaces[at].Live = u.following(spaces[at].ID)
		spaces[at].Gone = notInAccount(u.known, u.seen, u.mine, spaces[at])
	}
	return Status{
		Running:   u.running,
		Round:     u.inRound,
		Pushed:    u.pushed,
		Pulled:    u.pulled,
		SignedIn:  u.signedIn,
		Spaces:    spaces,
		Counts:    counts,
		Waiting:   waiting,
		Failure:   u.failure,
		Conflict:  u.conflict,
		RoundAt:   u.roundAt,
		RetryAt:   u.retryAt,
		ContentAt: u.contentAt,
		HistoryAt: u.historyAt,
	}, nil
}

// visibleSpaces filters spaces to include only unlinked spaces or those owned by active account.
func (u *UseCase) visibleSpaces(ctx context.Context) ([]domain.SyncSpace, error) {
	spaces, err := u.store.Spaces(ctx)
	if err != nil {
		return nil, err
	}
	key, err := u.store.Context(ctx)
	if err != nil || key == "" {
		return spaces, err
	}
	out := spaces[:0]
	for _, space := range spaces {
		if space.RemoteAccount == "" || space.RemoteAccount == key {
			out = append(out, space)
		}
	}
	return out, nil
}

// Fetched retrieves external record payload for a workspace.
func (u *UseCase) Fetched(
	ctx context.Context,
	workspaceID, id string,
) (domain.RecordFetched, error) {
	spaces, err := u.store.Spaces(ctx)
	if err != nil {
		return domain.RecordFetched{}, err
	}
	var found domain.SyncSpace
	for _, space := range spaces {
		if space.ID == workspaceID {
			found = space
		}
	}
	if !found.Exchangeable() {
		return domain.RecordFetched{},
			domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}

	caller, err := u.session.Caller(ctx)
	if err != nil {
		return domain.RecordFetched{}, err
	}
	return u.server.Fetched(ctx, caller, found.RemoteID, id)
}

func (u *UseCase) Spaces(ctx context.Context) ([]domain.SyncSpace, error) {
	return u.store.Spaces(ctx)
}

// RunRound executes a single sync round synchronously and returns updated Status.
func (u *UseCase) RunRound(ctx context.Context) (Status, error) {
	if err := u.round(ctx, nil); err != nil && !isRefusal(err) {
		return Status{}, err
	}
	return u.Status(ctx)
}

// Resync resets local space state and queues a fresh sync round.
func (u *UseCase) Resync(ctx context.Context, spaceID string) (Status, error) {
	err := u.exclusive(ctx, func() error { return u.store.Reset(ctx, spaceID) })
	if err != nil {
		return Status{}, err
	}
	u.WakeSpace(spaceID)
	return u.Status(ctx)
}

func (u *UseCase) loop(ctx context.Context, done chan struct{}) {
	defer close(done)

	var (
		backoff time.Duration
		only    []string
	)
	for {
		wait := u.interval
		failed := false
		if err := u.round(ctx, only); err != nil {
			if ctx.Err() != nil {
				return
			}
			failed = true
			backoff = retryAfter(backoff)
			wait = backoff
		} else {
			backoff = 0
		}
		u.retrying(wait, failed)

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-u.wake:
			timer.Stop()
			backoff = 0
			select {
			case <-time.After(u.settle):
			case <-ctx.Done():
				return
			}
			only = u.asked()
		case <-timer.C:
			only = nil
		}
	}
}

func retryAfter(previous time.Duration) time.Duration {
	return min(max(2*previous, firstRetry), maxBackoff)
}

func (u *UseCase) round(ctx context.Context, only []string) error {
	return u.exclusive(ctx, func() error { return u.exchangeRound(ctx, only) })
}

func (u *UseCase) exchangeRound(ctx context.Context, only []string) error {
	u.beginRound()
	caller, err := u.session.Caller(ctx)
	if err != nil {
		u.signed(false)
		if !isRefusal(err) {
			return err
		}
		u.finish(err)
		// A refusal ends the round quietly: asking the same question on a timer would be refused
		// again. A server that did not answer is not a refusal — the account is fine and the weather
		// is not — and the loop is where backing off for it lives.
		if domain.CodeOf(err) == domain.CodeServerUnreachable {
			return err
		}
		return nil
	}
	u.signed(true)

	limits, err := u.limits(ctx, caller)
	if err != nil {
		// The round could not even learn what the server allows, so it goes to the loop as the
		// failure it is: that is what the backoff is for, and the row counts the seconds by it.
		u.finish(err)
		return err
	}

	spaces, err := u.store.Spaces(ctx)
	if err != nil {
		return err
	}

	var discoveryErr error
	if only == nil {
		var discovered bool
		discovered, discoveryErr = u.reconcile(ctx, caller, spaces)
		if discovered {
			if spaces, err = u.store.Spaces(ctx); err != nil {
				return err
			}
		}
	}

	alive := u.living(spaces)
	if only != nil {
		alive = slices.DeleteFunc(alive, func(space domain.SyncSpace) bool {
			return !slices.Contains(only, space.ID)
		})
	}

	var first error
	for _, space := range alive {
		if err := u.exchange(ctx, caller, space, limits); err != nil {
			u.failed(space, err)
			if first == nil {
				first = err
			}
		}
	}

	if only == nil {
		u.watch(ctx, alive)
	}

	if first == nil {
		first = discoveryErr
	}

	u.finish(first)
	return first
}

// exchange is one space's half of a round: what this device owes it, and what it owes this device.
// A member with a reader's role is the ordinary case of a space read and not written, and the
// account's word about it was adopted at the top of the round, so the space is pulled and not
// pushed into: work sent there would only come back refused.
func (u *UseCase) exchange(
	ctx context.Context,
	caller domain.Caller,
	space domain.SyncSpace,
	limits domain.Limits,
) error {
	if !space.Exchangeable() {
		return nil
	}

	var pushErr error
	if !space.ReadOnly {
		pushErr = u.push(ctx, caller, space, limits)
	}
	pullErr := u.pull(ctx, caller, space)

	var historyErr error
	if pullErr == nil {
		historyErr = u.pullHistory(ctx, caller, space)
	}
	if err := cmp.Or(pushErr, pullErr, historyErr); err != nil {
		return err
	}
	return u.store.RecordRound(ctx, space.ID, domain.SyncSynced, u.now().UnixMilli(), "", 0)
}

// reconcile synchronizes local spaces with remote spaces and updates context on account
// switches. It answers whether it changed anything, alongside an error too: what was done before
// the failure is still done, and the round reads the spaces again for it.
func (u *UseCase) reconcile(
	ctx context.Context,
	caller domain.Caller,
	spaces []domain.SyncSpace,
) (bool, error) {
	if everythingPaused(spaces) {
		return false, nil
	}

	account, err := u.server.Spaces(ctx, caller)
	if err != nil {
		return false, err
	}
	remote := account.Spaces

	who, err := u.identity(ctx, caller)
	if err != nil {
		return false, err
	}
	mine := who.Key
	u.remember(remote, mine)

	again, err := u.recreated(ctx, who)
	if err != nil {
		return false, err
	}
	released, err := u.releaseOthers(ctx, mine)
	if err != nil {
		return false, err
	}
	if again || released > 0 {
		if spaces, err = u.store.Spaces(ctx); err != nil {
			return false, err
		}
	}

	gone, err := u.discard(ctx, spaces, remote, mine)
	if err != nil {
		return false, err
	}
	changed := gone || again || released > 0

	pairs := newPairing(spaces, remote)
	steps := []func() (bool, error){
		func() (bool, error) { return u.adoptKinds(ctx, pairs) },
		func() (bool, error) { return u.pairPersonal(ctx, pairs, mine) },
		func() (bool, error) { return u.mirror(ctx, pairs, mine) },
		func() (bool, error) { return u.workAs(ctx, mine) },
	}
	for _, step := range steps {
		did, err := step()
		changed = changed || did
		if err != nil {
			return changed, err
		}
	}

	made, refused, err := u.createWaiting(ctx, caller, account.OwnOrgID, pairs, mine)
	changed = changed || made
	if err != nil {
		return changed, err
	}

	if changed {
		u.arrived()
	}
	return changed, refused
}

// releaseOthers lets go of what an account this device was signed in to before has left in it, and
// says how many rows that was. Nothing is let go of the account that is signed in now.
func (u *UseCase) releaseOthers(ctx context.Context, mine string) (int, error) {
	before, err := u.store.Context(ctx)
	if err != nil || before == "" || before == mine {
		return 0, err
	}
	return u.store.Release(ctx, before)
}

// pairing is what a round of reconcile knows of which local space stands for which one in the
// account, kept up to date as the round pairs them.
type pairing struct {
	spaces []domain.SyncSpace
	remote []domain.RemoteSpace
	// paired holds the account's spaces that have a local one; linked, the local ones linked now.
	paired map[string]bool
	linked map[string]bool
}

func newPairing(spaces []domain.SyncSpace, remote []domain.RemoteSpace) *pairing {
	p := &pairing{
		spaces: spaces,
		remote: remote,
		paired: make(map[string]bool, len(spaces)),
		linked: make(map[string]bool),
	}
	for _, space := range spaces {
		if space.RemoteID != "" {
			p.paired[space.RemoteID] = true
		}
	}
	return p
}

// free is the spaces with nobody to be paired with: the local ones the account does not know
// yet and the account's ones that nothing here stands for.
func (p *pairing) free() ([]domain.SyncSpace, []domain.RemoteSpace) {
	here := make([]domain.SyncSpace, 0, len(p.spaces))
	for _, space := range p.spaces {
		if space.RemoteID == "" && !p.linked[space.ID] {
			here = append(here, space)
		}
	}
	there := make([]domain.RemoteSpace, 0, len(p.remote))
	for _, one := range p.remote {
		if !p.paired[one.ID] {
			there = append(there, one)
		}
	}
	return here, there
}

// adoptKinds takes what the account says about a paired space, what it is for and what this person
// may do in it, as the server's word: a role taken away arrives as a reader's answer, and the round
// that reads it stops pushing what the space would refuse.
func (u *UseCase) adoptKinds(ctx context.Context, p *pairing) (bool, error) {
	did := false
	for _, space := range p.spaces {
		if space.RemoteID == "" {
			continue
		}
		for _, one := range p.remote {
			if one.ID != space.RemoteID {
				continue
			}
			moved, err := u.store.Adopt(ctx, space.ID, one)
			if err != nil {
				return did, err
			}
			did = did || moved
		}
	}
	return did, nil
}

// pairPersonal links the space the app is born with to the account's own space, when both are
// still waiting for it.
func (u *UseCase) pairPersonal(ctx context.Context, p *pairing, mine string) (bool, error) {
	here, there := p.free()
	personal := spaceByID(here, domain.WorkspacePersonalID)
	if personal == nil {
		return false, nil
	}
	twin, found := spaceOfOnesOwn(there)
	if !found {
		return false, nil
	}
	if err := u.store.Link(ctx, personal.ID, twin, mine); err != nil {
		return false, err
	}
	p.paired[twin.ID] = true
	p.linked[personal.ID] = true
	return true, nil
}

// mirror makes here the spaces that are only in the account.
func (u *UseCase) mirror(ctx context.Context, p *pairing, mine string) (bool, error) {
	_, there := p.free()
	did := false
	for _, one := range there {
		if err := u.store.Mirror(ctx, one, mine); err != nil {
			return did, err
		}
		p.paired[one.ID] = true
		did = true
	}
	return did, nil
}

// createWaiting makes in the account the spaces that are only here. A refusal of the account is
// told to the window and the others still go on: the first is `refused`, and `err` is what failed
// here.
func (u *UseCase) createWaiting(
	ctx context.Context,
	caller domain.Caller,
	orgID string,
	p *pairing,
	mine string,
) (made bool, refused error, err error) {
	for _, space := range p.spaces {
		if p.linked[space.ID] || !waiting(space) || foreign(space, mine) {
			continue
		}
		if space.Name == "" {
			held, err := u.store.Holds(ctx, space.ID)
			if err != nil {
				return made, refused, err
			}
			if !held {
				continue
			}
		}

		remoteID := space.RemoteID
		if remoteID == "" {
			remoteID = u.ids()
			if err := u.store.MarkLinkPending(ctx, space.ID, remoteID, mine); err != nil {
				return made, refused, err
			}
			made = true
		}

		created, err := u.server.Create(ctx, caller, orgID, remoteID,
			space.Name, space.Color, space.Kind)
		if err != nil {
			u.failed(space, err)
			if refused == nil {
				refused = err
			}
			continue
		}
		if err := u.store.Link(ctx, space.ID, created, mine); err != nil {
			return made, refused, err
		}
		made = true
	}
	return made, refused, nil
}

// LinkNow immediately creates a remote space and links it to a local space.
func (u *UseCase) LinkNow(ctx context.Context, spaceID string) error {
	return u.exclusive(ctx, func() error { return u.linkNow(ctx, spaceID) })
}

func (u *UseCase) linkNow(ctx context.Context, spaceID string) error {
	caller, err := u.session.Caller(ctx)
	if err != nil {
		return err
	}
	account, err := u.server.Spaces(ctx, caller)
	if err != nil {
		return err
	}
	spaces, err := u.store.Spaces(ctx)
	if err != nil {
		return err
	}

	var space domain.SyncSpace
	found := false
	for _, one := range spaces {
		if one.ID == spaceID {
			space, found = one, true
			break
		}
	}
	if !found {
		return domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}

	mine, err := u.account(ctx, caller)
	if err != nil {
		return err
	}
	if foreign(space, mine) {
		return domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)
	}
	remoteID := space.RemoteID
	if remoteID == "" {
		remoteID = u.ids()
		if err := u.store.MarkLinkPending(ctx, space.ID, remoteID, mine); err != nil {
			return err
		}
	}

	made, err := u.server.Create(ctx, caller, account.OwnOrgID, remoteID,
		space.Name, space.Color, space.Kind)
	if err != nil {
		return err
	}
	if err := u.store.Link(ctx, space.ID, made, mine); err != nil {
		return err
	}

	u.arrived()
	u.Wake()
	return nil
}

func (u *UseCase) discard(
	ctx context.Context,
	spaces []domain.SyncSpace,
	remote []domain.RemoteSpace,
	mine string,
) (bool, error) {
	if len(remote) == 0 {
		return false, nil
	}
	seen := make(map[string]bool, len(remote))
	for _, one := range remote {
		seen[one.ID] = true
	}

	gone := false
	for _, space := range spaces {
		if space.RemoteID == "" || space.LinkPending || seen[space.RemoteID] ||
			!belongsTo(space, mine) {
			continue
		}
		if err := u.store.Discard(ctx, space.ID); err != nil {
			return gone, err
		}
		gone = true
	}
	if !gone {
		return false, nil
	}

	return true, u.store.EnsureOne(ctx)
}

// identity resolves current caller credentials and unique account key.
func (u *UseCase) identity(ctx context.Context, caller domain.Caller) (domain.Identity, error) {
	me, err := u.session.Me(ctx)
	if err != nil {
		return domain.Identity{}, err
	}
	if strings.TrimSpace(me.Email) == "" || me.UserID == "" {
		return domain.Identity{}, domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
	}
	return domain.Identity{Key: domain.AccountKey(caller.Address, me.Email), UserID: me.UserID}, nil
}

func (u *UseCase) account(ctx context.Context, caller domain.Caller) (string, error) {
	who, err := u.identity(ctx, caller)
	return who.Key, err
}

// recreated handles account re-creation cases on the server (e.g. database resets).
func (u *UseCase) recreated(ctx context.Context, who domain.Identity) (bool, error) {
	seen, known, err := u.store.Seen(ctx, who.Key)
	if err != nil {
		return false, err
	}
	switch {
	case !known:
		return false, u.store.Remember(ctx, who.Key, who.UserID)
	case seen == who.UserID:
		return false, nil
	default:
		return true, u.store.Rebind(ctx, who.Key, who.UserID)
	}
}

// workAs sets active local account context once spaces are available.
func (u *UseCase) workAs(ctx context.Context, key string) (bool, error) {
	held, err := u.store.Context(ctx)
	if err != nil || held == key {
		return false, err
	}
	return true, u.store.SetContext(ctx, key)
}

func (u *UseCase) remember(remote []domain.RemoteSpace, mine string) {
	seen := make(map[string]bool, len(remote))
	for _, one := range remote {
		seen[one.ID] = true
	}

	u.mu.Lock()
	u.seen, u.known, u.mine = seen, true, mine
	u.mu.Unlock()
}

func (u *UseCase) living(spaces []domain.SyncSpace) []domain.SyncSpace {
	u.mu.Lock()
	known, seen, mine := u.known, u.seen, u.mine
	u.mu.Unlock()

	if !known {
		return spaces
	}
	out := make([]domain.SyncSpace, 0, len(spaces))
	for _, space := range spaces {
		if space.LinkPending {
			continue
		}
		if notInAccount(known, seen, mine, space) || foreign(space, mine) {
			continue
		}
		out = append(out, space)
	}
	return out
}

func everythingPaused(spaces []domain.SyncSpace) bool {
	linked := 0
	for _, space := range spaces {
		if space.RemoteID == "" {
			continue
		}
		linked++
		if space.State != domain.SyncPaused {
			return false
		}
	}
	return linked > 0
}

func notInAccount(known bool, seen map[string]bool, mine string, space domain.SyncSpace) bool {
	return known && belongsTo(space, mine) && space.RemoteID != "" && !seen[space.RemoteID]
}

func foreign(space domain.SyncSpace, mine string) bool {
	return space.RemoteID != "" && space.RemoteAccount != mine
}

func belongsTo(space domain.SyncSpace, mine string) bool {
	return space.RemoteID != "" && space.RemoteAccount == mine
}

func waiting(space domain.SyncSpace) bool {
	return space.RemoteID == "" || space.LinkPending
}

func spaceByID(spaces []domain.SyncSpace, id string) *domain.SyncSpace {
	for at := range spaces {
		if spaces[at].ID == id {
			return &spaces[at]
		}
	}
	return nil
}

func spaceOfOnesOwn(there []domain.RemoteSpace) (domain.RemoteSpace, bool) {
	for _, one := range there {
		if one.OwnOrg && one.Kind == domain.WorkspacePersonal && strings.TrimSpace(one.Name) == "" {
			return one, true
		}
	}
	return domain.RemoteSpace{}, false
}

func (u *UseCase) failed(space domain.SyncSpace, err error) {
	state := space.State
	if state == domain.SyncSynced && space.SyncedAt == 0 {
		state = domain.SyncLocal
	}
	if back := u.store.RecordRound(context.Background(), space.ID, state, 0, domain.CodeOf(err),
		space.Attempts+1); back != nil {
		// Losing this row costs the state its memory across a restart, and the window its reason: the
		// round's own failure still goes out, so it is the log that has to say this one went missing.
		log.Printf("[sync] recording the failed round of %s: %v", space.ID, back)
	}
}

// retrying writes down that the next attempt is `wait` away — or that nothing is being waited on,
// when the round went through — and says so, because the row counts the seconds down from it.
func (u *UseCase) retrying(wait time.Duration, failed bool) {
	u.mu.Lock()
	before := u.retryAt
	u.retryAt = 0
	if failed {
		u.retryAt = u.now().Add(wait).UnixMilli()
	}
	changed := u.retryAt != before
	u.mu.Unlock()

	if changed {
		u.publish()
	}
}

// beginRound opens a round: the counters start at nothing, and the windows are told a round is on —
// the row that reads them draws "syncing" from that alone.
func (u *UseCase) beginRound() {
	u.mu.Lock()
	u.inRound, u.pushed, u.pulled, u.retryAt = true, 0, 0, 0
	u.mu.Unlock()
	u.publish()
}

// exchanged counts what the round has moved so far and says so: the numbers are for the row that
// reads them while the round is still going, so they are published as they grow.
func (u *UseCase) exchanged(pushed, pulled int) {
	if pushed == 0 && pulled == 0 {
		return
	}
	u.mu.Lock()
	u.pushed += pushed
	u.pulled += pulled
	u.mu.Unlock()
	u.publish()
}

func (u *UseCase) finish(err error) {
	u.mu.Lock()
	before := u.failure
	wasRound := u.inRound
	u.inRound = false
	u.roundAt = u.now().UnixMilli()
	u.failure = nil
	if err != nil {
		u.failure = domain.FailureOf(err, domain.CodeServerRefused)
	}
	failed := (before == nil) != (u.failure == nil) ||
		(before != nil && u.failure != nil && before.Code != u.failure.Code)
	arrived := u.contentAt != u.noticedAt || u.historyAt != u.noticedHistory
	u.mu.Unlock()

	// The round closing is itself news: a window drawing "syncing" has to be told it is over even
	// when nothing else about the engine moved.
	if failed || arrived || wasRound {
		u.publish()
	}
}

func (u *UseCase) arrived() {
	u.mu.Lock()
	u.contentAt = u.now().UnixMilli()
	u.mu.Unlock()
}

func (u *UseCase) historyArrived() {
	u.mu.Lock()
	u.historyAt = u.now().UnixMilli()
	u.mu.Unlock()
}

func (u *UseCase) signed(in bool) {
	u.mu.Lock()
	u.signedIn = in
	u.mu.Unlock()
}

func (u *UseCase) conflicting(spaceID string, change domain.PendingChange) {
	u.mu.Lock()
	u.conflict = &Conflict{
		SpaceID:  spaceID,
		Name:     change.Entity.Name,
		EntityID: change.Entity.ID,
		Kind:     change.Entity.Kind,
		At:       u.now().UnixMilli(),
	}
	u.mu.Unlock()
	u.publish()
}

func (u *UseCase) limits(ctx context.Context, caller domain.Caller) (domain.Limits, error) {
	u.mu.Lock()
	known := u.taken
	u.mu.Unlock()
	if known.MaxPushBytes > 0 {
		return known, nil
	}

	limits, err := u.server.Limits(ctx, caller)
	if err != nil {
		return domain.Limits{}, err
	}
	u.mu.Lock()
	u.taken = limits
	u.mu.Unlock()
	return limits, nil
}

func (u *UseCase) publish() {
	if u.notifier == nil {
		return
	}

	u.mu.Lock()
	u.noticedAt, u.noticedHistory = u.contentAt, u.historyAt
	u.mu.Unlock()
	status, err := u.Status(context.Background())
	if err != nil {
		return
	}
	u.notifier.Publish(TopicChanged, status)
}

func isRefusal(err error) bool {
	switch domain.CodeOf(err) {
	case domain.CodeNotSignedIn, domain.CodeServerUnreachable, domain.CodeServerRefused,
		domain.CodeWorkspaceMissing, domain.CodeNotFound, domain.CodeNotAllowed:
		return true
	}
	return domain.AsFailure(err) != nil
}

// Pause pauses syncing for all linked spaces.
func (u *UseCase) Pause(ctx context.Context) (Status, error) {
	if err := u.eachSpace(ctx, func(space domain.SyncSpace) error {
		return u.store.SetState(ctx, space.ID, domain.SyncPaused)
	}); err != nil {
		return Status{}, err
	}

	u.mu.Lock()
	if u.stream != nil {
		u.stream.cancel()
		u.stream = nil
	}
	u.mu.Unlock()

	return u.Status(ctx)
}

// Resume resumes syncing for all linked spaces.
func (u *UseCase) Resume(ctx context.Context) (Status, error) {
	if err := u.eachSpace(ctx, func(space domain.SyncSpace) error {
		state := domain.SyncLocal
		if space.SyncedAt > 0 {
			state = domain.SyncSynced
		}
		return u.store.SetState(ctx, space.ID, state)
	}); err != nil {
		return Status{}, err
	}

	u.Wake()
	return u.Status(ctx)
}

func (u *UseCase) eachSpace(ctx context.Context, do func(space domain.SyncSpace) error) error {
	spaces, err := u.store.Spaces(ctx)
	if err != nil {
		return err
	}
	for _, space := range spaces {
		if space.RemoteID == "" {
			continue
		}
		if err := do(space); err != nil {
			return err
		}
	}
	return nil
}
