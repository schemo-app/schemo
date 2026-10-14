package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/usecase/plan"
)

// PlanService is what the organisation this device works in is entitled to, as a window sees it.
//
// It is read to draw a control as one that is not offered, and for nothing else. The server is what
// refuses — from its own price list and its own subscription row, neither of which a caller can
// reach — so a window that has this wrong offers a door that answers with a refusal, and a window
// that has it missing offers less than it might. That is the whole of what this service is: the
// drawing, and never the rule.
type PlanService struct {
	plan *plan.UseCase
}

func NewPlanService(uc *plan.UseCase) *PlanService {
	return &PlanService{plan: uc}
}

// Current is what the server last said, without asking it.
func (s *PlanService) Current(ctx context.Context) (domain.Plan, error) {
	return s.plan.Current(ctx)
}

// Refresh asks the server and answers what it said. A server that does not answer is not a failure
// of this call: the plan that was kept stands, and the window draws from it. What a person must
// never get is a window offering less than they paid for because the question could not be asked —
// so a refusal here is not raised, it is stepped over.
func (s *PlanService) Refresh(ctx context.Context) (domain.Plan, error) {
	held, err := s.plan.Refresh(ctx)
	if err != nil {
		return s.plan.Current(ctx)
	}
	return held, nil
}
