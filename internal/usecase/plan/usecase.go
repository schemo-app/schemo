// Package plan is what the organisation this device works in is entitled to. One question — "does
// the plan carry this feature" — asked by the windows that draw a control as one that is not
// offered. The answer never decides anything: it is the server that refuses, from the price list
// and the subscription row, and a client anybody may rebuild is refused there exactly as this one
// would be. What this package is for is not offering somebody a door that is shut. The answer is
// kept, because the window draws it before it asks anybody: a device that is offline still draws
// the same control the same way. And it is only ever a copy — see the note on `domain.Plan`.
package plan

import (
	"context"

	"schemo/internal/domain"
)

// UseCase answers what the plan carries, over one store and one server.
type UseCase struct {
	store  Store
	server Server
	caller Session
}

func NewUseCase(store Store, server Server, caller Session) *UseCase {
	return &UseCase{store: store, server: server, caller: caller}
}

// Current is what the server last said, without asking it.
func (u *UseCase) Current(ctx context.Context) (domain.Plan, error) {
	return u.store.Plan(ctx)
}

// Refresh asks the server and keeps the answer. It is asked where the answer may have moved: when
// the app starts, when the connection is checked, when the window that draws a control opens, and
// after the server refuses something for want of a feature. There is no timer — a plan changes when
// somebody buys one, which is a thing that happens once in a while, and a device polling the
// question all day is one burning its battery to learn nothing. A server that does not answer is
// not a failure of this call: the plan that was kept stands, and the window draws from it. What a
// person must never get is a window that offers less than they paid for because the question could
// not be asked — so the stored answer is what survives.
func (u *UseCase) Refresh(ctx context.Context) (domain.Plan, error) {
	caller, err := u.caller.Caller(ctx)
	if err != nil {
		// Not signed in: there is no organisation to ask about, and what the last signed-in account
		// was entitled to is not this one's answer — the plan is forgotten rather than kept, which is
		// what a window draws nothing for. A server that is merely away is not that: a token that
		// could not be refreshed is weather, and the kept plan stands.
		if domain.CodeOf(err) != domain.CodeNotSignedIn {
			return domain.Plan{}, err
		}
		if err := u.store.SavePlan(ctx, domain.Plan{}); err != nil {
			return domain.Plan{}, err
		}
		return domain.Plan{}, nil
	}

	held, err := u.server.Plan(ctx, caller)
	if err != nil {
		return domain.Plan{}, err
	}
	if err := u.store.SavePlan(ctx, held); err != nil {
		return domain.Plan{}, err
	}
	return held, nil
}
