package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"schemo/internal/domain"
)

// Plan is what the server last said about the plan: one row, kept so that a window draws a
// disabled control disabled before anybody is asked anything.
//
// It is a copy in the plainest sense. Nothing here is checked against the server and nothing here
// decides anything: the lists are stored whole, including names this build has never heard of,
// because the price list is the server's and a feature added there is one this window draws nothing
// for rather than one it must be rebuilt to understand.
func (s *Store) Plan(ctx context.Context) (domain.Plan, error) {
	const query = `SELECT tier, seats, plan_until, features, quotas, meters FROM plan WHERE id = 1`

	var (
		held                           domain.Plan
		features, quotasRaw, metersRaw string
	)
	err := s.db.QueryRowContext(ctx, query).Scan(&held.Tier, &held.Seats, &held.PlanUntil,
		&features, &quotasRaw, &metersRaw)
	if err != nil {
		// No row is a device that has never asked, and that is not a failure: `Tier` comes back empty,
		// which is what "nobody told us" looks like to everything that reads this.
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Plan{}, nil
		}
		return domain.Plan{}, fmt.Errorf("reading the plan: %w", err)
	}

	if err := json.Unmarshal([]byte(features), &held.Features); err != nil {
		return domain.Plan{}, fmt.Errorf("reading the plan's features: %w", err)
	}
	if err := json.Unmarshal([]byte(quotasRaw), &held.Quotas); err != nil {
		return domain.Plan{}, fmt.Errorf("reading the plan's quotas: %w", err)
	}
	if err := json.Unmarshal([]byte(metersRaw), &held.Meters); err != nil {
		return domain.Plan{}, fmt.Errorf("reading the plan's meters: %w", err)
	}
	return held, nil
}

// SavePlan replaces the cached entitlement snapshot.
func (s *Store) SavePlan(ctx context.Context, held domain.Plan) error {
	features, err := json.Marshal(domain.OrEmpty(held.Features))
	if err != nil {
		return fmt.Errorf("saving the plan's features: %w", err)
	}
	quotas, err := json.Marshal(orEmptyMap(held.Quotas))
	if err != nil {
		return fmt.Errorf("saving the plan's quotas: %w", err)
	}
	meters, err := json.Marshal(held.Meters)
	if err != nil {
		return fmt.Errorf("saving the plan's meters: %w", err)
	}

	const query = `
		INSERT INTO plan (id, tier, seats, plan_until, features, quotas, meters)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			tier = excluded.tier, seats = excluded.seats, plan_until = excluded.plan_until,
			features = excluded.features, quotas = excluded.quotas, meters = excluded.meters`

	if _, err := s.db.ExecContext(ctx, query, held.Tier, held.Seats, held.PlanUntil,
		string(features), string(quotas), string(meters)); err != nil {
		return fmt.Errorf("saving the plan: %w", err)
	}
	return nil
}

func orEmptyMap(held map[string]int) map[string]int {
	if held == nil {
		return map[string]int{}
	}
	return held
}
