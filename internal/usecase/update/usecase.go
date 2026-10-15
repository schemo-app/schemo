package update

import (
	"context"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

const checkInterval = 24 * time.Hour

const devVersion = "dev"

type UseCase struct {
	releases Releases
	state    State
	settings Settings
	version  string
	notes    []string
}

func NewUseCase(
	releases Releases,
	state State,
	settings Settings,
	info platform.BuildInfo,
) *UseCase {
	return &UseCase{releases: releases, state: state, settings: settings, version: info.Version, notes: ParseNotes(info.ReleaseNotes)}
}

func (u *UseCase) Check(ctx context.Context) (Status, error) {
	prefs, err := u.settings.Snapshot(ctx)
	if err != nil {
		return Status{}, err
	}

	release, err := u.releases.Latest(ctx, prefs.UpdateChannel)
	if err != nil {
		return Status{}, err
	}

	state, err := u.state.LoadUpdateState(ctx)
	if err != nil {
		return Status{}, err
	}
	state.LastCheckAt = time.Now().UnixMilli()
	state.Latest = release.Version
	state.Notes = release.Notes
	if err := u.state.SaveUpdateState(ctx, state); err != nil {
		return Status{}, err
	}

	return u.offer(state), nil
}

func (u *UseCase) Status(ctx context.Context) (Status, error) {
	state, err := u.state.LoadUpdateState(ctx)
	if err != nil {
		return Status{}, err
	}
	if state.LastCheckAt == 0 {
		return Status{Current: u.version, Notes: u.notes, Development: u.Development()}, nil
	}
	return u.offer(state), nil
}

func (u *UseCase) StartupCheck(ctx context.Context) *Status {
	prefs, err := u.settings.Snapshot(ctx)
	if err != nil || !prefs.UpdateCheckAuto {
		return nil
	}
	if u.version == devVersion {
		return nil
	}

	state, err := u.state.LoadUpdateState(ctx)
	if err != nil {
		return nil
	}
	if state.LastCheckAt != 0 && time.Since(time.UnixMilli(state.LastCheckAt)) < checkInterval {
		info := u.offer(state)
		if info.Available {
			return &info
		}
		return nil
	}

	info, err := u.Check(ctx)
	if err != nil || !info.Available {
		return nil
	}
	return &info
}

func (u *UseCase) Skip(ctx context.Context) (Status, error) {
	state, err := u.state.LoadUpdateState(ctx)
	if err != nil {
		return Status{}, err
	}
	state.Skipped = state.Latest
	if err := u.state.SaveUpdateState(ctx, state); err != nil {
		return Status{}, err
	}
	return u.offer(state), nil
}

func (u *UseCase) Install(ctx context.Context, version string) error {
	if version == "" {
		return domain.Refuse(domain.CodeNoVersion, domain.ErrNotAllowed, nil)
	}
	if u.Development() {
		return domain.Refuse(domain.CodeDevBuild, domain.ErrNotAllowed, nil)
	}
	prefs, err := u.settings.Snapshot(ctx)
	if err != nil {
		return err
	}
	return u.releases.Install(ctx, prefs.UpdateChannel, version)
}

func (u *UseCase) Development() bool { return u.version == devVersion }

func (u *UseCase) offer(state domain.UpdateState) Status {
	info := Status{
		Current:     u.version,
		Latest:      state.Latest,
		CheckedAt:   state.LastCheckAt,
		Development: u.Development(),
		Notes:       u.notes,
	}
	if state.Latest == state.Skipped || !isNewer(state.Latest, u.version) {
		return info
	}
	info.Available = true
	info.Notes = state.Notes
	return info
}
