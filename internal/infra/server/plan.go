package server

import (
	"context"

	"google.golang.org/grpc"

	"github.com/schemo-app/proto/go/subscription/v1"

	"schemo/internal/domain"
)

// Plan asks the account what the organisation is entitled to. The organisation is not named. A
// device works in one organisation and holds no id for it — the engine learns its own from the
// spaces the account lists, and that is a fact about syncing rather than about this question — so
// the request leaves it empty and the server resolves the one its caller works in, the same way
// the panel resolves the organisation it is standing on. The server checks that caller's right to
// read it, which is why leaving it empty is the safe half: a named one would be a stranger's
// organisation and refused.
func (c *Client) Plan(ctx context.Context, caller domain.Caller) (domain.Plan, error) {
	answer, err := call(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*subscription.GetPlanResponse, error) {
			return subscription.NewSubscriptionServiceClient(conn).GetPlan(ctx,
				subscription.GetPlanRequest_builder{}.Build())
		})
	if err != nil {
		return domain.Plan{}, err
	}
	return planOf(answer.GetPlan(), answer.GetUsage()), nil
}

func planOf(held *subscription.Plan, used *subscription.Usage) domain.Plan {
	if held == nil {
		return domain.Plan{}
	}
	out := domain.Plan{
		Tier:     held.GetTier(),
		Seats:    int(held.GetSeats()),
		Features: held.GetFeatures(),
		Quotas:   map[string]int{},
	}
	if at := held.GetPlanUntil(); at != nil {
		out.PlanUntil = at.AsTime().UnixMilli()
	}
	for name, held := range held.GetQuotas() {
		out.Quotas[name] = int(held)
	}
	out.Meters = metersOf(out.Quotas, used)
	return out
}

// megabyte is what the storage quota is written in, and what the bytes the server counts are read
// as: a bar labelled in one unit and measured in another is a bar nobody can check.
const megabyte = 1 << 20

// metersOf pairs the quotas the server caps with what it says stands against them, in kinds this
// app's window knows: "collections" and "members" count things, "storage" is in megabytes. A quota
// the server leaves without a cap (or does not send at all) has no bar, and one it caps at zero is
// a bar at its end.
func metersOf(quotas map[string]int, used *subscription.Usage) []domain.Meter {
	if used == nil || len(quotas) == 0 {
		return nil
	}
	var out []domain.Meter
	add := func(quota, kind string, held int) {
		if limit, ok := quotas[quota]; ok && limit >= 0 {
			out = append(out, domain.Meter{Kind: kind, Used: held, Limit: limit})
		}
	}
	add("workspace.collections", "collections", int(used.GetCollections()))
	add("org.members", "members", int(used.GetMembers()))
	add("org.storage_mb", "storage", int((used.GetStorageBytes()+megabyte-1)/megabyte))
	return out
}
