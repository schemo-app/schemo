package sync

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"schemo/internal/domain"
)

type streams struct {
	*fakeServer

	opened   int
	cursors  [][]domain.SpaceCursor
	give     []domain.Signal
	refuse   error
	endFirst int
	called   chan struct{}
}

func (s *streams) Connect(
	ctx context.Context,
	_ domain.Caller,
	cursors []domain.SpaceCursor,
	on func(domain.Signal) error,
) error {
	for _, one := range cursors {
		s.namedStream = append(s.namedStream, one.SpaceID)
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

func newLive() (*UseCase, *fakeStore, *streams, *fakeNotifier) {
	store := &fakeStore{spaces: []domain.SyncSpace{
		{
			ID:            spaceA,
			Name:          "Team",
			RemoteID:      remote,
			State:         domain.SyncSynced,
			Cursor:        40,
			RemoteAccount: accountOf(),
		},
	}}
	server := &streams{
		fakeServer: &fakeServer{},
		called:     make(chan struct{}, 8),
	}
	notifier := &fakeNotifier{}
	uc := NewUseCase(store, server, fakeSession{}, nil, notifier, fakeIDs())
	uc.till = time.Millisecond
	uc.settle = 0
	// A stream follows only the spaces of the signed-in account.
	uc.mine = accountOf()
	return uc, store, server, notifier
}

// TestAPokeAsksForARoundAndAppliesNothing verifies that a poke only wakes the sync round.
func TestAPokeAsksForARoundAndAppliesNothing(t *testing.T) {
	uc, store, server, _ := newLive()
	for at := 0; at < 50; at++ {
		server.give = append(server.give,
			domain.Signal{Poke: domain.Poke{SpaceID: remote, Content: int64(41 + at)}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote}})

	select {
	case <-uc.wake:
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
		case <-uc.wake:
			if asked++; asked > 2 {
				t.Fatalf("a burst of %d pokes asked for %d rounds, want one", len(server.give), asked)
			}
		default:
			break drain
		}
	}
}

func TestAPokeBelowTheCursorsAsksForNothing(t *testing.T) {
	uc, _, server, _ := newLive()
	server.give = []domain.Signal{
		{Poke: domain.Poke{SpaceID: remote, Content: 40}},
		{Poke: domain.Poke{SpaceID: remote, Content: 12}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote}})

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("no stream was opened")
	}

	time.Sleep(50 * time.Millisecond)
	if len(uc.wake) != 0 {
		t.Error("a poke that named nothing new asked for a round")
	}
}

func TestAPokeOfTheHistoryAsksForARound(t *testing.T) {
	uc, _, server, _ := newLive()
	server.give = []domain.Signal{{Poke: domain.Poke{SpaceID: remote, Content: 40, History: 3}}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote}})

	select {
	case <-uc.wake:
	case <-ctx.Done():
		t.Fatal("a poke of the history did not ask for a round")
	}
}

func TestAStreamIsOpenedWithTheCursorsTheDeviceHolds(t *testing.T) {
	uc, store, server, _ := newLive()
	store.spaces[0].HistoryCursor = 7

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote}})

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("no stream was opened")
	}

	want := []domain.SpaceCursor{{SpaceID: remote, Content: 40, History: 7}}
	if len(server.cursors) != 1 || !slices.Equal(server.cursors[0], want) {
		t.Errorf("stream opened with %v, want the cursors the device holds: %v", server.cursors, want)
	}
}

func TestAStreamFollowsTheSpaceTheServerKnowsItBy(t *testing.T) {
	uc, _, server, _ := newLive()
	server.give = []domain.Signal{{Poke: domain.Poke{SpaceID: remote, Content: 41}}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote}})

	select {
	case <-uc.wake:
	case <-ctx.Done():
		t.Fatal("a poke did not ask for a round")
	}
	namedOnce(t, "the stream", server.namedStream, remote)
}

func TestNoStreamIsOpenedForASpaceTheAccountDoesNotHave(t *testing.T) {
	uc, _, server, _ := newLive()
	uc.remember([]domain.RemoteSpace{{ID: "theirs", Name: "Payments"}}, accountOf())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote}})

	time.Sleep(50 * time.Millisecond)
	if server.opened != 0 {
		t.Errorf("streams opened = %d, want none for a space the account does not have", server.opened)
	}
}

func TestAStreamThatEndedIsOpenedAgain(t *testing.T) {
	uc, _, server, _ := newLive()
	server.refuse = domain.Refuse(domain.CodeServerUnreachable, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote}})

	for at := 0; at < 2; at++ {
		select {
		case <-server.called:
		case <-ctx.Done():
			t.Fatalf("the stream was opened %d times, want it reopened after it ended", server.opened)
		}
	}
}

func TestAStreamOfASessionThatEndedIsNotOpenedAgain(t *testing.T) {
	uc, _, server, _ := newLive()
	server.refuse = domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote}})

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
	uc, _, server, _ := newLive()
	server.endFirst = 1

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote}})

	select {
	case <-uc.wake:
	case <-ctx.Done():
		t.Fatal("a stream the server ended of its own accord did not ask for a round")
	}
}

func TestAnOrdinaryRefusalWaitsForTheNextRound(t *testing.T) {
	uc, _, server, _ := newLive()
	server.refuse = domain.Refuse(domain.CodeServerUnreachable, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote}})

	for at := 0; at < 2; at++ {
		select {
		case <-server.called:
		case <-ctx.Done():
			t.Fatal("the stream was not reopened")
		}
	}

	if len(uc.wake) != 0 {
		t.Error("a stream that ended in an error asked for a round")
	}
}

func TestASpaceNobodyLinkedIsNotWatched(t *testing.T) {
	uc, _, server, _ := newLive()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{
		{ID: spaceA, Name: "Team"},
		{ID: "paused", RemoteID: remote, State: domain.SyncPaused},
	})

	time.Sleep(50 * time.Millisecond)
	if server.opened != 0 {
		t.Errorf("streams opened = %d, want none for a space with nowhere to go", server.opened)
	}
}

func TestAStreamIsOnePerSpace(t *testing.T) {
	uc, _, server, _ := newLive()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	spaces := []domain.SyncSpace{{ID: spaceA, RemoteID: remote}}
	uc.watch(ctx, spaces)

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("no stream was opened")
	}

	uc.watch(ctx, spaces)
	time.Sleep(50 * time.Millisecond)
	if server.opened != 1 {
		t.Errorf("streams opened = %d, want one per space", server.opened)
	}
}

func TestAStreamIsGivenUpWhenTheEngineStops(t *testing.T) {
	uc, _, server, _ := newLive()
	server.refuse = domain.Refuse(domain.CodeServerUnreachable, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote}})

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("no stream was opened")
	}

	cancel()
	uc.Stop(context.Background())

	time.Sleep(50 * time.Millisecond)
	opened := server.opened
	time.Sleep(50 * time.Millisecond)
	if server.opened != opened {
		t.Errorf("streams opened = %d after the engine stopped, want no more", server.opened-opened)
	}
}

func twoFollowed() (*UseCase, *streams) {
	uc, store, server, _ := newLive()
	store.spaces = append(store.spaces, domain.SyncSpace{
		ID:            "space-b",
		Name:          "Second",
		RemoteID:      "remote-b",
		State:         domain.SyncSynced,
		Cursor:        5,
		RemoteAccount: accountOf(),
	})
	return uc, server
}

func TestOneStreamFollowsEverySpace(t *testing.T) {
	uc, server := twoFollowed()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{
		{ID: spaceA, RemoteID: remote, State: domain.SyncSynced},
		{ID: "space-b", RemoteID: "remote-b", State: domain.SyncSynced},
	})

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("no stream was opened")
	}

	if server.opened != 1 || len(server.cursors[0]) != 2 {
		t.Fatalf("opened %d streams with %v, want one stream following both spaces",
			server.opened, server.cursors)
	}
}

func TestAPokeWakesOnlyTheSpaceItIsAbout(t *testing.T) {
	uc, server := twoFollowed()
	server.give = []domain.Signal{{Poke: domain.Poke{SpaceID: "remote-b", Content: 9}}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{
		{ID: spaceA, RemoteID: remote, State: domain.SyncSynced},
		{ID: "space-b", RemoteID: "remote-b", State: domain.SyncSynced},
	})

	select {
	case <-uc.wake:
	case <-ctx.Done():
		t.Fatal("a poke did not ask for a round")
	}
	if got := uc.asked(); len(got) != 1 || got[0] != "space-b" {
		t.Errorf("asked = %v, want only the space the poke was about", got)
	}
}

func TestAChangeOfTheListAsksForAFullRound(t *testing.T) {
	uc, server := twoFollowed()
	server.give = []domain.Signal{{SpacesChanged: true}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	uc.watch(ctx, []domain.SyncSpace{{ID: spaceA, RemoteID: remote, State: domain.SyncSynced}})

	select {
	case <-uc.wake:
	case <-ctx.Done():
		t.Fatal("a change of the list did not ask for a round")
	}
	if got := uc.asked(); got != nil {
		t.Errorf("asked = %v, want every space (nil)", got)
	}
}

func TestTheStreamIsOpenedAgainWhenTheSpacesChange(t *testing.T) {
	uc, server := twoFollowed()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	a := domain.SyncSpace{ID: spaceA, RemoteID: remote, State: domain.SyncSynced}
	b := domain.SyncSpace{ID: "space-b", RemoteID: "remote-b", State: domain.SyncSynced}
	uc.watch(ctx, []domain.SyncSpace{a})
	<-server.called
	uc.watch(ctx, []domain.SyncSpace{a})
	uc.watch(ctx, []domain.SyncSpace{a, b})

	select {
	case <-server.called:
	case <-ctx.Done():
		t.Fatal("the stream was not opened again for the new space")
	}
	if server.opened != 2 {
		t.Errorf("streams opened = %d, want two: the first, and one for the new set", server.opened)
	}
}

// A stream that held for a while and then dropped is reopened after the base wait.
func TestAStreamThatHeldBeforeItFailedIsNotWaitedForLonger(t *testing.T) {
	base := 5 * time.Second
	failed := errors.New("unavailable")

	if got := reopenWait(2*time.Minute, base, time.Hour, failed); got != base {
		t.Errorf("wait after a stream that held for an hour = %v, want the base %v", got, base)
	}
	if got := reopenWait(base, base, time.Second, failed); got != 2*base {
		t.Errorf("wait after a stream that failed at once = %v, want it doubled", got)
	}
	if got := reopenWait(maxBackoff, base, time.Second, failed); got != maxBackoff {
		t.Errorf("wait at the ceiling = %v, want it kept", got)
	}
	if got := reopenWait(2*time.Minute, base, time.Second, nil); got != base {
		t.Errorf("wait after a clean end = %v, want the base %v", got, base)
	}
}
