package sqlite

import (
	"testing"

	"schemo/internal/domain"
)

func TestAccountAndPlanRoundTrip(t *testing.T) {
	store := newMigratedStore(t)
	ctx := t.Context()
	if err := store.Save(ctx, domain.Account{UserID: "user", Email: "user@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(ctx, domain.Plan{Tier: "team", Seats: 4, Features: []string{"history"}}); err != nil {
		t.Fatal(err)
	}
	account, err := store.Account(ctx)
	if err != nil || account.UserID != "user" {
		t.Fatalf("account changed: %+v, error=%v", account, err)
	}
	plan, err := store.Plan(ctx)
	if err != nil || plan.Tier != "team" || plan.Seats != 4 || len(plan.Features) != 1 || plan.Features[0] != "history" {
		t.Fatalf("plan changed: %+v, error=%v", plan, err)
	}
	var unused int
	err = store.DB().QueryRow(`SELECT
		(SELECT count(*) FROM pragma_table_info('plan') WHERE name = 'read_at') +
		(SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'account_signed_in')`).Scan(&unused)
	if err != nil || unused != 0 {
		t.Fatalf("obsolete schema remains: count=%d, error=%v", unused, err)
	}
}
