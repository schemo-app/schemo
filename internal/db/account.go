package db

import (
	"context"
	"encoding/json"
	"fmt"

	"schemo/internal/domain"
)

// The account and the plan are small documents kept in the settings table. Credentials are not:
// they belong to the OS credential store.
const (
	keyAccount = "account"
	keyPlan    = "plan"
)

func (s *Store) readDoc(ctx context.Context, key string, into any) (bool, error) {
	raw, ok, err := s.Setting(ctx, key)
	if err != nil || !ok {
		return false, err
	}
	if err := json.Unmarshal([]byte(raw), into); err != nil {
		return false, fmt.Errorf("reading %s: %w", key, err)
	}
	return true, nil
}

func (s *Store) saveDoc(ctx context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("saving %s: %w", key, err)
	}
	return s.SaveSetting(ctx, key, string(raw))
}

// Account reads the account. Nothing signed in answers ErrNotFound.
func (s *Store) Account(ctx context.Context) (domain.Account, error) {
	var account domain.Account
	ok, err := s.readDoc(ctx, keyAccount, &account)
	if err == nil && !ok {
		err = domain.ErrNotFound
	}
	return account, err
}

func (s *Store) Save(ctx context.Context, account domain.Account) error {
	return s.saveDoc(ctx, keyAccount, account)
}

func (s *Store) Forget(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, keyAccount); err != nil {
		return fmt.Errorf("forgetting the account: %w", err)
	}
	return nil
}

// Plan is what the server last said about the plan, so a window draws a disabled control disabled
// before anybody is asked. A device that never asked gets an empty tier.
func (s *Store) Plan(ctx context.Context) (domain.Plan, error) {
	var plan domain.Plan
	_, err := s.readDoc(ctx, keyPlan, &plan)
	return plan, err
}

func (s *Store) SavePlan(ctx context.Context, plan domain.Plan) error {
	return s.saveDoc(ctx, keyPlan, plan)
}
