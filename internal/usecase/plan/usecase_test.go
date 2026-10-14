package plan

import (
	"context"
	"errors"
	"testing"

	"schemo/internal/domain"
)

// fakeStore is the plan as this device keeps it: one row, and what the last write put there.
type fakeStore struct {
	held domain.Plan
	// saved counts the writes, so a scenario can say that an answer nobody has replaces nothing.
	saved int
	// refuses is the database itself failing, which is not this feature's to translate.
	refuses error
}

func (s *fakeStore) Plan(context.Context) (domain.Plan, error) {
	if s.refuses != nil {
		return domain.Plan{}, s.refuses
	}
	return s.held, nil
}

func (s *fakeStore) SavePlan(_ context.Context, held domain.Plan) error {
	if s.refuses != nil {
		return s.refuses
	}
	s.held, s.saved = held, s.saved+1
	return nil
}

// fakeServer is the organisation, as the subscription call answers it.
type fakeServer struct {
	held domain.Plan
	err  error
}

func (s fakeServer) Plan(context.Context, domain.Caller) (domain.Plan, error) {
	return s.held, s.err
}

// fakeSession is the account: a caller, or the refusal of one when nobody is signed in.
type fakeSession struct{ err error }

func (s fakeSession) Caller(context.Context) (domain.Caller, error) {
	if s.err != nil {
		return domain.Caller{}, s.err
	}
	return domain.Caller{Address: "https://api.example.com"}, nil
}

func newTestUseCase(store *fakeStore, server fakeServer, session fakeSession) *UseCase {
	return NewUseCase(store, server, session)
}

// A device that is not signed in has no organisation to ask about, and what the last account was
// entitled to is not this one's answer: the plan is forgotten rather than kept.
func TestASignedOutDeviceForgetsThePlan(t *testing.T) {
	store := &fakeStore{held: domain.Plan{Tier: "team", Seats: 4}}
	refusal := domain.Refuse(domain.CodeNotSignedIn, nil, nil)
	uc := newTestUseCase(store, fakeServer{}, fakeSession{err: refusal})

	held, err := uc.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if held.Tier != "" {
		t.Errorf("plan = %+v, want nothing known", held)
	}
	if store.held.Tier != "" || store.saved != 1 {
		t.Errorf("store = %+v after %d writes, want the plan forgotten", store.held, store.saved)
	}
}

// A server that is away is not a device that is signed out: the plan that was kept stands, and the
// window goes on drawing the last answer it was given.
func TestAServerThatIsAwayKeepsThePlan(t *testing.T) {
	store := &fakeStore{held: domain.Plan{Tier: "team", Seats: 4}}
	refusal := domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	uc := newTestUseCase(store, fakeServer{}, fakeSession{err: refusal})

	if _, err := uc.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh returned no error for an account that cannot be reached")
	}
	if store.held.Tier != "team" || store.saved != 0 {
		t.Errorf("store = %+v after %d writes, want the kept plan untouched", store.held, store.saved)
	}
}

// The cached answer remains available without a server round trip.
func TestTheServersAnswerIsKept(t *testing.T) {
	store := &fakeStore{}
	server := fakeServer{held: domain.Plan{Tier: "enterprise", Seats: 20}}
	uc := newTestUseCase(store, server, fakeSession{})

	held, err := uc.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if held.Tier != "enterprise" || held.Seats != 20 {
		t.Errorf("plan = %+v, want the server's", held)
	}
	if store.held.Tier != "enterprise" || store.saved != 1 {
		t.Errorf("store = %+v, writes = %d, want the server's answer saved once", store.held, store.saved)
	}
}

// A server that does not answer is not an answer: what was kept stands, so a window never offers
// less than the person paid for because the question could not be asked.
func TestAServerThatDoesNotAnswerChangesNothing(t *testing.T) {
	store := &fakeStore{held: domain.Plan{Tier: "team", Seats: 4}}
	uc := newTestUseCase(store, fakeServer{err: errors.New("unreachable")}, fakeSession{})

	if _, err := uc.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh returned no error for a server that did not answer")
	}
	if store.saved != 0 || store.held.Tier != "team" {
		t.Errorf("store = %+v after %d writes, want the kept plan untouched", store.held, store.saved)
	}
}

// Current is what the store holds, without asking anybody.
func TestCurrentReadsWhatIsKept(t *testing.T) {
	store := &fakeStore{held: domain.Plan{Tier: "team", Seats: 4}}
	uc := newTestUseCase(store, fakeServer{}, fakeSession{})

	held, err := uc.Current(context.Background())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if held.Tier != "team" || held.Seats != 4 {
		t.Errorf("plan = %+v, want the stored one", held)
	}
}
