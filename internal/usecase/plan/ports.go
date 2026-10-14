package plan

import (
	"context"

	"schemo/internal/domain"
)

// Store is where the last answer the server gave about the plan is kept.
//
// One row, like the account: a device belongs to one organisation, and the plan is that
// organisation's. Nothing here decides anything — a window reads it to know which controls are not
// offered, and the server decides on its own what may be done.
type Store interface {
	// Plan is what the server last said. A device that has never asked answers with a plan nobody
	// described — no tier, which the window reads as not known — and that is not an error: a
	// question nobody asked is not a failure.
	Plan(ctx context.Context) (domain.Plan, error)
	SavePlan(ctx context.Context, held domain.Plan) error
}

// Server is the account's side of the question: what the organisation the caller works in is
// entitled to. It is `subscription/v1.GetPlan`, and the organisation is not named — the server
// resolves the one the caller works in, because a device works in one and does not hold its id.
type Server interface {
	Plan(ctx context.Context, caller domain.Caller) (domain.Plan, error)
}

// Session is who this device is signed in as. There is nothing to ask without a caller: an account
// that is gone is the answer and not a question to keep asking.
type Session interface {
	Caller(ctx context.Context) (domain.Caller, error)
}
