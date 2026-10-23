package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"schemo/internal/domain"
)

func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading setting %s: %w", key, err)
	}
	return value, true, nil
}

func (s *Store) SaveSetting(ctx context.Context, key, value string) error {
	return saveSettingIn(ctx, s.db, key, value)
}

// Preferences is the stored settings document, secrets included, with defaults for anything
// missing or unusable.
func (s *Store) Preferences(ctx context.Context) (domain.Settings, error) {
	raw, _, err := s.Setting(ctx, domain.SettingPreferences)
	if err != nil {
		return domain.Settings{}, err
	}
	return domain.ParseSettings(raw), nil
}

func (s *Store) SavePreferences(ctx context.Context, settings domain.Settings) error {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.SaveSetting(ctx, domain.SettingPreferences, string(encoded))
}

// SyncScopes is what the person allows to leave this device. A failed read fails the push rather
// than falling back to the permissive defaults.
func (s *Store) SyncScopes(ctx context.Context) (domain.SyncScopes, error) {
	settings, err := s.Preferences(ctx)
	if err != nil {
		return domain.SyncScopes{}, fmt.Errorf("reading the sync scopes: %w", err)
	}
	return settings.SyncScopes, nil
}

// saveSettingIn shares the upsert between standalone writes and larger transactions.
func saveSettingIn(ctx context.Context, tx execer, key, value string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("saving setting %s: %w", key, err)
	}
	return nil
}

// Which environment a request resolves against is not here: it is a column of the workspace row
// (see workspace.go), since two workspaces need two answers.

// Retention is how long this device keeps history.
func (s *Store) Retention(ctx context.Context) (domain.Retention, error) {
	settings, err := s.Preferences(ctx)
	return settings.HistoryRetention, err
}
