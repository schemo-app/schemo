package settings

import (
	"context"
	"encoding/json"

	"schemo/internal/domain"
)

func (u *UseCase) SetAppearance(
	ctx context.Context,
	patch AppearancePatch,
) (domain.Settings, error) {
	current, err := u.appearance(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	if patch.Accent != nil {
		current.Accent = *patch.Accent
	}
	if patch.Size != nil {
		current.Size = *patch.Size
	}
	if patch.MonoFont != nil {
		current.MonoFont = *patch.MonoFont
	}
	if patch.MonoSize != nil {
		current.MonoSize = clampMonoSize(*patch.MonoSize)
	}
	if err := current.Valid(); err != nil {
		return domain.Settings{}, err
	}
	if err := u.saveBlob(ctx, domain.SettingAppearance, current); err != nil {
		return domain.Settings{}, err
	}
	return u.announced(u.Snapshot(ctx))
}

func (u *UseCase) appearance(ctx context.Context) (domain.Appearance, error) {
	stored, err := u.store.Settings(ctx)
	if err != nil {
		return domain.Appearance{}, err
	}
	return parseAppearance(stored[domain.SettingAppearance]), nil
}

func parseAppearance(raw string) domain.Appearance {
	out := domain.DefaultAppearance()
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return domain.DefaultAppearance()
	}
	if !out.Accent.Valid() {
		out.Accent = domain.AccentBlue
	}
	out.Size = domain.SizeOf(string(out.Size))
	if !out.MonoFont.Valid() {
		out.MonoFont = domain.MonoSystem
	}
	out.MonoSize = clampMonoSize(out.MonoSize)
	return out
}

func clampMonoSize(size int) int {
	return min(max(size, domain.MinMonoSize), domain.MaxMonoSize)
}
