package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"schemo/internal/domain"
)

// LoadUpdateState reads what the last check saw. It is kept as one JSON value under one key rather
// than a key per field: the store's part is to hand it back unchanged, and splitting it would make
// a half-written state — a version without its notes — expressible.
//
// A value this build cannot read answers as no state at all rather than as an error. The state is a
// cache of somebody else's answer, so the cost of dropping it is one extra check; refusing to open
// the app over a corrupt one would be out of proportion.
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
