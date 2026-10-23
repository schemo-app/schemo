package db

import (
	"context"
	"encoding/json"
	"fmt"

	"schemo/internal/domain"
)

// LoadUpdateState reads what the last check saw, kept as one JSON value under one key: the store
// hands it back unchanged, and splitting it would make a half-written state expressible.
//
// A value this build cannot read answers as no state at all rather than an error. The state is a
// cache of somebody else's answer, so dropping it costs one extra check.
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
