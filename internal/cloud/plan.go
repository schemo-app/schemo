package cloud

import (
	"context"
	"schemo/internal/db"
	"schemo/internal/domain"

	planv1 "github.com/schemo-app/proto/go/plan/v1"
	"google.golang.org/grpc"
)

// PlanServer is the account's side of the question: what the organisation the caller works in is
// entitled to (`plan/v1.GetPlan`). The server resolves the organisation from the caller.
type PlanServer interface {
	Plan(ctx context.Context, caller domain.Caller) (domain.Plan, error)
}

// planSession is who this device is signed in as.
type planSession interface {
	Caller(ctx context.Context) (domain.Caller, error)
}

// Plan is the plan as the window reads it.
type Plan struct {
	store  *db.Store
	server PlanServer
	caller planSession
}

func NewPlan(store *db.Store, server PlanServer, caller planSession) *Plan {
	return &Plan{store: store, server: server, caller: caller}
}

// Current is what the server last said, without asking it. A device that has never asked answers
// with a plan nobody described (no tier), which the window reads as not known.
func (s *Plan) Current(ctx context.Context) (domain.Plan, error) {
	return s.store.Plan(ctx)
}

// Refresh asks the server and keeps the answer. It is asked where the answer may have moved: at
// startup, when the connection is checked, when a window that draws a control opens, and after the
// server refuses something for want of a feature. There is no timer: a plan changes when somebody
// buys one.
//
// A server that does not answer is not a failure: the kept plan stands and is the answer, so
// nobody gets a window offering less than they paid for because the question could not be asked.
// A device that is signed out forgets the plan, since the last account's entitlement is not this
// one's answer.
func (s *Plan) Refresh(ctx context.Context) (domain.Plan, error) {
	caller, err := s.caller.Caller(ctx)
	if domain.CodeOf(err) == domain.CodeNotSignedIn {
		if err := s.store.SavePlan(ctx, domain.Plan{}); err != nil {
			return domain.Plan{}, err
		}
		return domain.Plan{}, nil
	}
	if err != nil {
		return s.store.Plan(ctx)
	}
	held, err := s.server.Plan(ctx, caller)
	if err != nil {
		return s.store.Plan(ctx)
	}
	if err := s.store.SavePlan(ctx, held); err != nil {
		return domain.Plan{}, err
	}
	return held, nil
}

// Plan asks the account what the organisation is entitled to. The organisation is not named: a
// device works in one organisation and holds no id for it, so the request leaves it empty and the
// server resolves the one its caller works in, after checking the caller's right to read it. A
// named one would be a stranger's organisation and refused.
func (c *Client) Plan(ctx context.Context, caller domain.Caller) (domain.Plan, error) {
	answer, err := callServer(c, ctx, caller,
		func(ctx context.Context, conn *grpc.ClientConn) (*planv1.GetPlanResponse, error) {
			return planv1.NewPlanServiceClient(conn).GetPlan(ctx,
				planv1.GetPlanRequest_builder{}.Build())
		})
	if err != nil {
		return domain.Plan{}, err
	}
	return planOf(answer.GetPlan(), answer.GetUsage()), nil
}

func planOf(held *planv1.Plan, used *planv1.Usage) domain.Plan {
	if held == nil {
		return domain.Plan{}
	}
	out := domain.Plan{
		Tier: held.GetTier(),

		Features: db.Features(held.GetFeatures()),
		Quotas:   map[string]int{},
	}
	if at := held.GetExpiresAt(); at != nil {
		out.PlanUntil = at.AsTime().UnixMilli()
	}
	for _, quota := range held.GetQuotas() {
		if !quota.HasLimit() {
			continue
		}
		var name string
		switch quota.GetKind() {
		case planv1.QuotaKind_QUOTA_KIND_ORG_MEMBERS:
			name = "org.members"
		case planv1.QuotaKind_QUOTA_KIND_WORKSPACE_COLLECTIONS:
			name = "workspace.collections"
		case planv1.QuotaKind_QUOTA_KIND_ORG_STORAGE_MB:
			name = "org.storage_mb"
		case planv1.QuotaKind_QUOTA_KIND_HISTORY_RETENTION_DAYS:
			name = "history.retention_days"
		default:
			continue
		}
		out.Quotas[name] = int(quota.GetLimit())
	}
	out.Seats = out.Quotas["org.members"]
	out.Meters = metersOf(out.Quotas, used)
	return out
}

// megabyte is what the storage quota is written in, and what the bytes the server counts are read
// as: a bar labelled in one unit and measured in another is a bar nobody can check.
const megabyte = 1 << 20

// metersOf pairs the quotas the server caps with what it says stands against them, in kinds this
// window knows: "collections" and "members" count things, "storage" is in megabytes. A quota with
// no cap (or not sent) has no bar, and one capped at zero is a full bar.
func metersOf(quotas map[string]int, used *planv1.Usage) []domain.Meter {
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
