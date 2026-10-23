package desktop

import (
	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"

	"context"
	"time"

	"schemo/internal/db"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

const checkInterval = 24 * time.Hour

const devVersion = "dev"

// Releases is where releases come from and how one is installed: the Wails updater.
type Releases interface {
	Latest(ctx context.Context, channel domain.UpdateChannel) (domain.Release, error)
	Install(ctx context.Context, channel domain.UpdateChannel, version string) error
}

// UpdateStatus is what the app has to say about updates: the running version, the latest one seen, and
// whether it is offered.
type UpdateStatus struct {
	Current     string   `json:"current"`
	Latest      string   `json:"latest"`
	Notes       []string `json:"notes,omitempty"`
	CheckedAt   int64    `json:"checkedAt,omitempty"`
	Available   bool     `json:"available"`
	Development bool     `json:"development,omitempty"`
}

// Updates is everything a window can ask about updates: the settings window checks, the
// main window's status bar reads what the last check found, and the update window installs it.
// What a check finds is published to every window. Host is nil in tests.
type Updates struct {
	releases Releases
	store    *db.Store
	host     *Host
	version  string
	notes    []string
}

func NewUpdates(releases Releases, store *db.Store, host *Host, info platform.BuildInfo) *Updates {
	return &Updates{releases: releases, store: store, host: host, version: info.Version,
		notes: ParseNotes(info.ReleaseNotes)}
}

// Check runs a check the user asked for. Unlike the launch-time one it is never skipped and never
// gated on the preference, and what it found is published to every window.
func (s *Updates) Check(ctx context.Context) (UpdateStatus, error) {
	info, err := s.check(ctx)
	if err == nil {
		s.publish(info)
	}
	return info, err
}

func (s *Updates) check(ctx context.Context) (UpdateStatus, error) {
	settings, err := s.store.Preferences(ctx)
	if err != nil {
		return UpdateStatus{}, err
	}

	release, err := s.releases.Latest(ctx, settings.UpdateChannel)
	if err != nil {
		return UpdateStatus{}, err
	}

	state, err := s.store.LoadUpdateState(ctx)
	if err != nil {
		return UpdateStatus{}, err
	}
	state.LastCheckAt = time.Now().UnixMilli()
	state.Latest = release.Version
	state.Notes = release.Notes
	if err := s.store.SaveUpdateState(ctx, state); err != nil {
		return UpdateStatus{}, err
	}

	return s.offer(state), nil
}

// Status is what the last check saw, without touching the network.
func (s *Updates) Status(ctx context.Context) (UpdateStatus, error) {
	state, err := s.store.LoadUpdateState(ctx)
	if err != nil {
		return UpdateStatus{}, err
	}
	if state.LastCheckAt == 0 {
		return UpdateStatus{Current: s.version, Notes: s.notes, Development: s.Development()}, nil
	}
	return s.offer(state), nil
}

// CheckOnStartup is the launch-time check: gated on the preference and on the last check's age, and
// published only when it found something to offer.
//
//wails:ignore
func (s *Updates) CheckOnStartup(ctx context.Context) {
	if info := s.startupCheck(ctx); info != nil {
		s.publish(*info)
	}
}

func (s *Updates) startupCheck(ctx context.Context) *UpdateStatus {
	settings, err := s.store.Preferences(ctx)
	if err != nil || !settings.UpdateCheckAuto {
		return nil
	}
	if s.version == devVersion {
		return nil
	}

	state, err := s.store.LoadUpdateState(ctx)
	if err != nil {
		return nil
	}
	if state.LastCheckAt != 0 && time.Since(time.UnixMilli(state.LastCheckAt)) < checkInterval {
		info := s.offer(state)
		if info.Available {
			return &info
		}
		return nil
	}

	info, err := s.check(ctx)
	if err != nil || !info.Available {
		return nil
	}
	return &info
}

// Skip records that the user does not want to hear about the version on screen again and publishes
// what the app has to say now.
func (s *Updates) Skip(ctx context.Context) (UpdateStatus, error) {
	state, err := s.store.LoadUpdateState(ctx)
	if err != nil {
		return UpdateStatus{}, err
	}
	state.Skipped = state.Latest
	if err := s.store.SaveUpdateState(ctx, state); err != nil {
		return UpdateStatus{}, err
	}
	info := s.offer(state)
	s.publish(info)
	return info, nil
}

// Install downloads the release and replaces the running binary. On success it does not return.
func (s *Updates) Install(ctx context.Context, version string) error {
	err := s.install(ctx, version)
	if err != nil && s.host != nil {
		// Wails returns a failure to start the restart helper without emitting an error event.
		s.host.Broadcast(wailsupdater.EventError, wailsupdater.ErrorInfo{
			Stage: wailsupdater.StageInstall, Message: err.Error(),
		})
	}
	return err
}

func (s *Updates) install(ctx context.Context, version string) error {
	if version == "" {
		return domain.Refuse(domain.CodeNoVersion, domain.ErrNotAllowed, nil)
	}
	if s.Development() {
		return domain.Refuse(domain.CodeDevBuild, domain.ErrNotAllowed, nil)
	}
	settings, err := s.store.Preferences(ctx)
	if err != nil {
		return err
	}
	return s.releases.Install(ctx, settings.UpdateChannel, version)
}

// Development says this is a build with no version, which never updates itself.
//
//wails:ignore
func (s *Updates) Development() bool { return s.version == devVersion }

// ShowWindow opens the window that describes a release and offers to install it.
func (s *Updates) ShowWindow() { s.host.ShowUpdate() }

// RequestCheck opens the settings' updates page and asks it to run a check. The menu and the
// window's own key both come here, so the two paths park and send the same request.
func (s *Updates) RequestCheck() { s.host.RequestUpdateCheck() }

// TakeRequest reports and clears a check parked by RequestCheck. Destructive on purpose: both the
// mount and the page's own handler call it, and only the first may act on it.
func (s *Updates) TakeRequest() bool { return s.host.TakeUpdateCheckRequest() }

func (s *Updates) publish(info UpdateStatus) {
	if s.host != nil {
		s.host.PublishUpdate(info, info.Available)
	}
}

func (s *Updates) offer(state domain.UpdateState) UpdateStatus {
	info := UpdateStatus{
		Current:     s.version,
		Latest:      state.Latest,
		CheckedAt:   state.LastCheckAt,
		Development: s.Development(),
		Notes:       s.notes,
	}
	if state.Latest == state.Skipped || !isNewer(state.Latest, s.version) {
		return info
	}
	info.Available = true
	info.Notes = state.Notes
	return info
}
