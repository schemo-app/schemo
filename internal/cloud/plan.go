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

// Refresh asks the server and keeps the answer. There is no timer: a plan changes when somebody buys
// one. An unreachable server keeps the stored plan; signing out forgets it.
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

// Plan asks for the caller's organisation's entitlement; the server resolves the organisation.
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

// metersOf pairs capped quotas with usage ("storage" is in megabytes). No cap means no meter; a zero
// cap is a full one.
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
