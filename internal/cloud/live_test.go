package cloud

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"schemo/internal/domain"
)

type streams struct {
	*fakeSyncServer

	opened   int
	cursors  [][]domain.WorkspaceCursor
	give     []domain.Signal
	refuse   error
	endFirst int
	called   chan struct{}
}

func (s *streams) Connect(
	ctx context.Context,
	_ domain.Caller,
	cursors []domain.WorkspaceCursor,
	on func(domain.Signal) error,
) error {
	for _, one := range cursors {
		s.namedStream = append(s.namedStream, one.WorkspaceID)
	}
	s.cursors = append(s.cursors, cursors)
	s.opened++

	select {
	case s.called <- struct{}{}:
	default:
	}

	for _, signal := range s.give {
		if err := on(signal); err != nil {
			return err
		}
	}

	if s.refuse != nil {
		return s.refuse
	}
	if s.opened <= s.endFirst {
		return nil
	}

	<-ctx.Done()
	return nil
}

func newLive() (*Sync, *fakeStore, *streams, *syncEvents) {
	store := &fakeStore{workspaces: []domain.SyncWorkspace{
		{
			ID:            workspaceA,
			Name:          "Team",
			RemoteID:      remote,
			State:         domain.SyncSynced,
			Cursor:        40,
			RemoteAccount: accountOf(),
		},
	}}
	server := &streams{
		fakeSyncServer: &fakeSyncServer{},
		called:         make(chan struct{}, 8),
	}
	notifier := &syncEvents{}
	engine := NewSync(store, server, syncSession{}, nil, notifier, fakeIDs())
	engine.till = time.Millisecond
	engine.settle = 0
	// A stream follows only the workspaces of the signed-in account.
	engine.mine = accountOf()
	return engine, store, server, notifier
}

// TestAPokeAsksForARoundAndAppliesNothing verifies that a poke only wakes the sync round.
func TestAPokeAsksForARoundAndAppliesNothing(t *testing.T) {
	engine, store, server, _ := newLive()
	for at := 0; at < 50; at++ {
		server.give = append(server.give,
			domain.Signal{Poke: domain.Poke{WorkspaceID: remote, Content: int64(41 + at)}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}})

	select {
	case <-engine.wake:
	case <-ctx.Done():
		t.Fatal("a poke did not ask for a round")
	}

	if len(store.applied) != 0 {
		t.Errorf("the stream applied %d pages, want none: a poke is a number", len(store.applied))
	}

	time.Sleep(50 * time.Millisecond)
	asked := 1
drain:
	for {
		select {
		case <-engine.wake:
			if asked++; asked > 2 {
				t.Fatalf("a burst of %d pokes asked for %d rounds, want one", len(server.give), asked)
			}
		default:
			break drain
		}
	}
}

func TestAPokeBelowTheCursorsAsksForNothing(t *testing.T) {
	engine, _, server, _ := newLive()
	server.give = []domain.Signal{
		{Poke: domain.Poke{WorkspaceID: remote, Content: 40}},
		{Poke: domain.Poke{WorkspaceID: remote, Content: 12}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}})

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("no stream was opened")
	}

	time.Sleep(50 * time.Millisecond)
	if len(engine.wake) != 0 {
		t.Error("a poke that named nothing new asked for a round")
	}
}

func TestAPokeOfTheHistoryAsksForARound(t *testing.T) {
	engine, _, server, _ := newLive()
	server.give = []domain.Signal{{Poke: domain.Poke{WorkspaceID: remote, Content: 40, History: 3}}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}})

	select {
	case <-engine.wake:
	case <-ctx.Done():
		t.Fatal("a poke of the history did not ask for a round")
	}
}

func TestAStreamIsOpenedWithTheCursorsTheDeviceHolds(t *testing.T) {
	engine, store, server, _ := newLive()
	store.workspaces[0].HistoryCursor = 7

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}})

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("no stream was opened")
	}

	want := []domain.WorkspaceCursor{{WorkspaceID: remote, Content: 40, History: 7}}
	if len(server.cursors) != 1 || !slices.Equal(server.cursors[0], want) {
		t.Errorf("stream opened with %v, want the cursors the device holds: %v", server.cursors, want)
	}
}

func TestAStreamFollowsTheWorkspaceTheServerKnowsItBy(t *testing.T) {
	engine, _, server, _ := newLive()
	server.give = []domain.Signal{{Poke: domain.Poke{WorkspaceID: remote, Content: 41}}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}})

	select {
	case <-engine.wake:
	case <-ctx.Done():
		t.Fatal("a poke did not ask for a round")
	}
	namedOnce(t, "the stream", server.namedStream, remote)
}

func TestNoStreamIsOpenedForAWorkspaceTheAccountDoesNotHave(t *testing.T) {
	engine, _, server, _ := newLive()
	engine.rememberAccountWorkspaces([]domain.RemoteWorkspace{{ID: "theirs", Name: "Payments"}}, accountOf())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}})

	time.Sleep(50 * time.Millisecond)
	if server.opened != 0 {
		t.Errorf("streams opened = %d, want none for a workspace the account does not have", server.opened)
	}
}

func TestAStreamThatEndedIsOpenedAgain(t *testing.T) {
	engine, _, server, _ := newLive()
	server.refuse = domain.Refuse(domain.CodeServerUnreachable, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}})

	for at := 0; at < 2; at++ {
		select {
		case <-server.called:
		case <-ctx.Done():
			t.Fatalf("the stream was opened %d times, want it reopened after it ended", server.opened)
		}
	}
}

func TestAStreamOfASessionThatEndedIsNotOpenedAgain(t *testing.T) {
	engine, _, server, _ := newLive()
	server.refuse = domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}})

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("the stream was never opened")
	}

	select {
	case <-server.called:
		t.Fatalf("the stream was opened %d times, want it left alone", server.opened)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAStreamTheServerEndedAsksForARoundAtOnce(t *testing.T) {
	engine, _, server, _ := newLive()
	server.endFirst = 1

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}})

	select {
	case <-engine.wake:
	case <-ctx.Done():
		t.Fatal("a stream the server ended of its own accord did not ask for a round")
	}
}

func TestAnOrdinaryRefusalWaitsForTheNextRound(t *testing.T) {
	engine, _, server, _ := newLive()
	server.refuse = domain.Refuse(domain.CodeServerUnreachable, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}})

	for at := 0; at < 2; at++ {
		select {
		case <-server.called:
		case <-ctx.Done():
			t.Fatal("the stream was not reopened")
		}
	}

	if len(engine.wake) != 0 {
		t.Error("a stream that ended in an error asked for a round")
	}
}

func TestAWorkspaceNobodyLinkedIsNotWatched(t *testing.T) {
	engine, _, server, _ := newLive()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{
		{ID: workspaceA, Name: "Team"},
		{ID: "paused", RemoteID: remote, State: domain.SyncPaused},
	})

	time.Sleep(50 * time.Millisecond)
	if server.opened != 0 {
		t.Errorf("streams opened = %d, want none for a workspace with nowhere to go", server.opened)
	}
}

func TestAStreamIsOnePerWorkspace(t *testing.T) {
	engine, _, server, _ := newLive()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	workspaces := []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}}
	engine.watch(ctx, workspaces)

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("no stream was opened")
	}

	engine.watch(ctx, workspaces)
	time.Sleep(50 * time.Millisecond)
	if server.opened != 1 {
		t.Errorf("streams opened = %d, want one per workspace", server.opened)
	}
}

func TestAStreamIsGivenUpWhenTheEngineStops(t *testing.T) {
	engine, _, server, _ := newLive()
	server.refuse = domain.Refuse(domain.CodeServerUnreachable, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote}})

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("no stream was opened")
	}

	cancel()
	engine.Stop(context.Background())

	time.Sleep(50 * time.Millisecond)
	opened := server.opened
	time.Sleep(50 * time.Millisecond)
	if server.opened != opened {
		t.Errorf("streams opened = %d after the engine stopped, want no more", server.opened-opened)
	}
}

func twoFollowed() (*Sync, *streams) {
	engine, store, server, _ := newLive()
	store.workspaces = append(store.workspaces, domain.SyncWorkspace{
		ID:            "space-b",
		Name:          "Second",
		RemoteID:      "remote-b",
		State:         domain.SyncSynced,
		Cursor:        5,
		RemoteAccount: accountOf(),
	})
	return engine, server
}

func TestOneStreamFollowsEveryWorkspace(t *testing.T) {
	engine, server := twoFollowed()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{
		{ID: workspaceA, RemoteID: remote, State: domain.SyncSynced},
		{ID: "space-b", RemoteID: "remote-b", State: domain.SyncSynced},
	})

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("no stream was opened")
	}

	if server.opened != 1 || len(server.cursors[0]) != 2 {
		t.Fatalf("opened %d streams with %v, want one stream following both workspaces",
			server.opened, server.cursors)
	}
}

func TestAPokeWakesOnlyTheWorkspaceItIsAbout(t *testing.T) {
	engine, server := twoFollowed()
	server.give = []domain.Signal{{Poke: domain.Poke{WorkspaceID: "remote-b", Content: 9}}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{
		{ID: workspaceA, RemoteID: remote, State: domain.SyncSynced},
		{ID: "space-b", RemoteID: "remote-b", State: domain.SyncSynced},
	})

	select {
	case <-engine.wake:
	case <-ctx.Done():
		t.Fatal("a poke did not ask for a round")
	}
	if got := engine.takeRequestedWorkspaces(); len(got) != 1 || got[0] != "space-b" {
		t.Errorf("asked = %v, want only the workspace the poke was about", got)
	}
}

func TestAChangeOfTheListAsksForAFullRound(t *testing.T) {
	engine, server := twoFollowed()
	server.give = []domain.Signal{{WorkspacesChanged: true}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	engine.watch(ctx, []domain.SyncWorkspace{{ID: workspaceA, RemoteID: remote, State: domain.SyncSynced}})

	select {
	case <-engine.wake:
	case <-ctx.Done():
		t.Fatal("a change of the list did not ask for a round")
	}
	if got := engine.takeRequestedWorkspaces(); got != nil {
		t.Errorf("asked = %v, want every workspace (nil)", got)
	}
}

func TestTheStreamIsOpenedAgainWhenTheWorkspacesChange(t *testing.T) {
	engine, server := twoFollowed()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a := domain.SyncWorkspace{ID: workspaceA, RemoteID: remote, State: domain.SyncSynced}
	b := domain.SyncWorkspace{ID: "space-b", RemoteID: "remote-b", State: domain.SyncSynced}
	engine.watch(ctx, []domain.SyncWorkspace{a})
	<-server.called
	engine.watch(ctx, []domain.SyncWorkspace{a})
	engine.watch(ctx, []domain.SyncWorkspace{a, b})

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("the stream was not opened again for the new workspace")
	}
	if server.opened != 2 {
		t.Errorf("streams opened = %d, want two: the first, and one for the new set", server.opened)
	}
}

// A stream that held for a while and then dropped is reopened after the base wait.
func TestAStreamThatHeldBeforeItFailedIsNotWaitedForLonger(t *testing.T) {
	base := 5 * time.Second
	failed := errors.New("unavailable")

	if got := reopenDelay(2*time.Minute, base, time.Hour, failed); got != base {
		t.Errorf("wait after a stream that held for an hour = %v, want the base %v", got, base)
	}
	if got := reopenDelay(base, base, time.Second, failed); got != 2*base {
		t.Errorf("wait after a stream that failed at once = %v, want it doubled", got)
	}
	if got := reopenDelay(maxBackoff, base, time.Second, failed); got != maxBackoff {
		t.Errorf("wait at the ceiling = %v, want it kept", got)
	}
	if got := reopenDelay(2*time.Minute, base, time.Second, nil); got != base {
		t.Errorf("wait after a clean end = %v, want the base %v", got, base)
	}
}
