package cloud

import (
	"context"
	"errors"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"testing"

	planv1 "github.com/schemo-app/proto/go/plan/v1"
)

// fakePlanServer is the organisation, as the subscription call answers it.
type fakePlanServer struct {
	held domain.Plan
	err  error
}

func (s fakePlanServer) Plan(context.Context, domain.Caller) (domain.Plan, error) {
	return s.held, s.err
}

// planCaller is the account: a caller, or the refusal of one.
type planCaller struct{ err error }

func (s planCaller) Caller(context.Context) (domain.Caller, error) {
	if s.err != nil {
		return domain.Caller{}, s.err
	}
	return domain.Caller{Address: "https://api.example.com"}, nil
}

// newTest is a plan service whose store already keeps a team plan.
func newPlan(t *testing.T, server fakePlanServer, session planCaller) *Plan {
	t.Helper()
	store := dbtest.New(t)
	if err := store.SavePlan(context.Background(), domain.Plan{Tier: "team", Seats: 4}); err != nil {
		t.Fatal(err)
	}
	return NewPlan(store, server, session)
}

func current(t *testing.T, s *Plan) domain.Plan {
	t.Helper()
	held, err := s.Current(context.Background())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	return held
}

// A device that is not signed in has no organisation to ask about: the plan is forgotten.
func TestASignedOutDeviceForgetsThePlan(t *testing.T) {
	s := newPlan(t, fakePlanServer{}, planCaller{err: domain.Refuse(domain.CodeNotSignedIn, nil, nil)})

	held, err := s.Refresh(context.Background())
	if err != nil || held.Tier != "" {
		t.Fatalf("Refresh = %+v, %v; want nothing known", held, err)
	}
	if kept := current(t, s); kept.Tier != "" {
		t.Errorf("kept = %+v, want the plan forgotten", kept)
	}
}

// An account that cannot be reached, or a server that does not answer, is not an answer: the kept
// plan stands and is what the window draws.
func TestAServerThatIsAwayKeepsThePlan(t *testing.T) {
	for name, s := range map[string]*Plan{
		"account": newPlan(t, fakePlanServer{},
			planCaller{err: domain.Refuse(domain.CodeServerUnreachable, nil, nil)}),
		"server": newPlan(t, fakePlanServer{err: errors.New("unreachable")}, planCaller{}),
	} {
		held, err := s.Refresh(context.Background())
		if err != nil || held.Tier != "team" {
			t.Errorf("%s: Refresh = %+v, %v; want the kept plan", name, held, err)
		}
		if kept := current(t, s); kept.Tier != "team" || kept.Seats != 4 {
			t.Errorf("%s: kept = %+v, want it untouched", name, kept)
		}
	}
}

func TestTheServersAnswerIsKept(t *testing.T) {
	s := newPlan(t, fakePlanServer{held: domain.Plan{Tier: "enterprise", Seats: 20}}, planCaller{})

	held, err := s.Refresh(context.Background())
	if err != nil || held.Tier != "enterprise" || held.Seats != 20 {
		t.Fatalf("Refresh = %+v, %v; want the server's", held, err)
	}
	if kept := current(t, s); kept.Tier != "enterprise" {
		t.Errorf("kept = %+v, want the server's answer", kept)
	}
}

// The caps the server sends and what it counts against them become the bars a window draws, in the
// kinds this app knows — and only where something is capped: a quota the server leaves unlimited
// is a number without a meaning, and one nothing is counted against has no bar to stand on.
func TestTheCapsAndWhatStandsAgainstThemBecomeMeters(t *testing.T) {
	quotas := map[string]int{
		"workspace.collections":  20,
		"org.members":            1,
		"org.storage_mb":         100,
		"history.retention_days": 7,
		"org.workspaces":         -1,
	}
	used := planv1.Usage_builder{
		Collections:  18,
		Members:      1,
		Workspaces:   3,
		StorageBytes: 12 * megabyte,
	}.Build()

	meters := metersOf(quotas, used)

	want := []domain.Meter{
		{Kind: "collections", Used: 18, Limit: 20},
		{Kind: "members", Used: 1, Limit: 1},
		{Kind: "storage", Used: 12, Limit: 100},
	}
	if len(meters) != len(want) {
		t.Fatalf("meters = %+v, want %+v", meters, want)
	}
	for at, one := range meters {
		if one != want[at] {
			t.Errorf("meter %d = %+v, want %+v", at, one, want[at])
		}
	}
}

// A server that says nothing about what stands against the caps has no meters to draw, and neither
// has a plan with no caps in it at all: a bar needs both halves.
func TestMetersNeedBothTheCapAndTheCount(t *testing.T) {
	usage := planv1.Usage_builder{Collections: 18}.Build()

	if meters := metersOf(nil, usage); len(meters) != 0 {
		t.Errorf("meters = %+v, want nothing drawn from caps nobody sent", meters)
	}
	if meters := metersOf(map[string]int{"workspace.collections": 20}, nil); len(meters) != 0 {
		t.Errorf("meters = %+v, want nothing drawn from a count nobody sent", meters)
	}
}
