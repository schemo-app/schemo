package db

import (
	"context"
	"encoding/json"
	"fmt"

	"schemo/internal/domain"
)

// LoadUpdateState reads the last check's state, stored as one JSON value. An unreadable value is no
// state rather than an error: it is only a cache.
func (s *Store) LoadUpdateState(ctx context.Context) (domain.UpdateState, error) {
	raw, found, err := s.Setting(ctx, domain.SettingUpdateState)
	if err != nil || !found {
		return domain.UpdateState{}, err
	}

	var state domain.UpdateState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return domain.UpdateState{}, nil
	}
	return state, nil
}

func (s *Store) SaveUpdateState(ctx context.Context, state domain.UpdateState) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encoding update state: %w", err)
	}
	return s.SaveSetting(ctx, domain.SettingUpdateState, string(encoded))
}
