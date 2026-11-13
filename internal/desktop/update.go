package desktop

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"

	"schemo/internal/db"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

const checkInterval = time.Hour

const devVersion = "dev"

// Releases provides signed releases and applies a verified download using the platform updater.
type Releases interface {
	Latest(ctx context.Context, channel domain.UpdateChannel) (domain.Release, error)
	Stage(ctx context.Context, channel domain.UpdateChannel, version string) (stagedRelease, error)
	Install(ctx context.Context, staged stagedRelease) error
}

// UpdateStatus describes the running version, the offer, and the state of its download.
type UpdateStatus struct {
	Current     string   `json:"current"`
	Latest      string   `json:"latest"`
	Notes       []string `json:"notes,omitempty"`
	CheckedAt   int64    `json:"checkedAt,omitempty"`
	Available   bool     `json:"available"`
	Phase       string   `json:"phase,omitempty"`
	Development bool     `json:"development,omitempty"`
}

// Updates manages release checks, background downloads, and installation for the desktop windows.
type Updates struct {
	releases Releases
	store    *db.Store
	host     *Host
	version  string
	notes    []string
	wake     chan struct{}

	// Only one release operation may change Wails' pending artifact or the saved offer.
	// Status can read the transfer phase while a network operation holds mu.
	mu        sync.Mutex
	autoCheck bool
	announced string
	transfer  atomic.Pointer[updateTransfer]
}

type updateTransfer struct {
	channel domain.UpdateChannel
	version string
	phase   string
	staged  stagedRelease
}

const (
	phaseDownloading = "downloading"
	phaseDownloaded  = "downloaded"
	phaseInstalling  = "installing"
)

// NewUpdates connects the updater to the settings and desktop windows.
func NewUpdates(releases Releases, store *db.Store, host *Host, info platform.BuildInfo) *Updates {
	return &Updates{releases: releases, store: store, host: host, version: info.Version,
		notes: ParseNotes(info.ReleaseNotes), wake: make(chan struct{}, 1)}
}

// Check checks the selected channel on demand and publishes the result.
func (s *Updates) Check(ctx context.Context) (UpdateStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, state, err := s.load(ctx)
	if err != nil {
		return UpdateStatus{}, err
	}
	state, err = s.check(ctx, state, settings.UpdateChannel)
	if err != nil {
		return UpdateStatus{}, err
	}
	info := s.offer(state, settings.UpdateChannel)
	if info.Available {
		s.announced = info.Latest
	}
	s.publish(info, info.Available)
	s.Wake()
	return info, nil
}

// Status reports the last known offer and transfer phase without using the network.
func (s *Updates) Status(ctx context.Context) (UpdateStatus, error) {
	settings, state, err := s.load(ctx)
	if err != nil {
		return UpdateStatus{}, err
	}
	return s.offer(state, settings.UpdateChannel), nil
}

// Run checks at launch and once per hour, and responds to preference changes.
//
//wails:ignore
func (s *Updates) Run(ctx context.Context) {
	if s.Development() {
		return
	}
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.discard()
	}()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	s.refresh(ctx, true)
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refresh(ctx, true)
		case <-s.wake:
			s.refresh(ctx, false)
		}
	}
}

// Wake asks the worker to read the latest preferences and offer.
//
//wails:ignore
func (s *Updates) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Skip hides the current offer and releases its staged download.
func (s *Updates) Skip(ctx context.Context) (UpdateStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, state, err := s.load(ctx)
	if err != nil {
		return UpdateStatus{}, err
	}
	if state.Channel == settings.UpdateChannel {
		state.Skipped = state.Latest
		if err := s.store.SaveUpdateState(ctx, state); err != nil {
			return UpdateStatus{}, err
		}
	}
	s.discard()
	info := s.offer(state, settings.UpdateChannel)
	s.publish(info, false)
	return info, nil
}

// Install applies the release and starts the application restart.
func (s *Updates) Install(ctx context.Context, version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.install(ctx, strings.TrimPrefix(version, "v"))
	if err != nil && s.host != nil {
		// Failures before the transfer or while starting the restart helper may have no Wails event.
		s.host.Broadcast(wailsupdater.EventError, wailsupdater.ErrorInfo{
			Stage: wailsupdater.StageInstall, Message: err.Error(),
		})
	}
	return err
}

// Development reports whether this build is eligible for self-updates.
//
//wails:ignore
func (s *Updates) Development() bool { return s.version == devVersion }

// ShowWindow opens the window that describes a release and offers to install it.
func (s *Updates) ShowWindow() { s.host.ShowUpdate() }

// RequestCheck opens the updates settings and requests a check.
func (s *Updates) RequestCheck() { s.host.RequestUpdateCheck() }

// TakeRequest consumes a check requested by RequestCheck.
func (s *Updates) TakeRequest() bool { return s.host.TakeUpdateCheckRequest() }

func (s *Updates) load(ctx context.Context) (domain.Settings, domain.UpdateState, error) {
	settings, err := s.store.Preferences(ctx)
	if err != nil {
		return settings, domain.UpdateState{}, err
	}
	state, err := s.store.LoadUpdateState(ctx)
	return settings, state, err
}

func (s *Updates) check(ctx context.Context, state domain.UpdateState, channel domain.UpdateChannel) (domain.UpdateState, error) {
	release, err := s.releases.Latest(ctx, channel)
	if err != nil {
		return state, err
	}
	next := domain.UpdateState{
		Channel: channel, Latest: release.Version, Notes: release.Notes,
		LastCheckAt: time.Now().UnixMilli(), Skipped: state.Skipped,
	}
	if err := s.store.SaveUpdateState(ctx, next); err != nil {
		return state, err
	}
	return next, nil
}

func (s *Updates) refresh(ctx context.Context, scheduled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, state, err := s.load(ctx)
	if err != nil {
		return
	}
	check := settings.UpdateCheckAuto && (scheduled || !s.autoCheck || state.Channel != settings.UpdateChannel)
	s.autoCheck = settings.UpdateCheckAuto
	if check {
		state, err = s.check(ctx, state, settings.UpdateChannel)
		if err != nil && ctx.Err() == nil {
			slog.Warn("Update check failed", "channel", settings.UpdateChannel, "err", err)
		}
		settings, err = s.store.Preferences(ctx)
		if err != nil {
			return
		}
	}

	info := s.offer(state, settings.UpdateChannel)
	keepDownload := settings.UpdateDownloadAuto && info.Phase == phaseDownloaded
	if !keepDownload {
		s.discard()
		info.Phase = ""
	}
	if settings.UpdateDownloadAuto && info.Available && !keepDownload {
		s.download(ctx, state)
		return
	}
	s.announce(info, settings.UpdateCheckAuto && !settings.UpdateDownloadAuto)
}

func (s *Updates) download(ctx context.Context, state domain.UpdateState) {
	s.transfer.Store(&updateTransfer{channel: state.Channel, version: state.Latest, phase: phaseDownloading})
	s.publish(s.offer(state, state.Channel), false)

	staged, stageErr := s.releases.Stage(ctx, state.Channel, state.Latest)
	if stageErr != nil {
		s.transfer.Store(nil)
		if ctx.Err() == nil {
			slog.Warn("Background update download failed", "version", state.Latest, "err", stageErr)
		}
	} else {
		s.transfer.Store(&updateTransfer{
			channel: state.Channel, version: state.Latest, phase: phaseDownloaded, staged: staged,
		})
	}

	settings, err := s.store.Preferences(ctx)
	if err != nil || !settings.UpdateDownloadAuto || settings.UpdateChannel != state.Channel {
		s.discard()
	}
	if err == nil {
		s.announce(s.offer(state, settings.UpdateChannel), stageErr != nil && settings.UpdateCheckAuto)
	}
}

func (s *Updates) install(ctx context.Context, version string) error {
	if version == "" {
		return domain.Refuse(domain.CodeNoVersion, domain.ErrNotAllowed, nil)
	}
	if s.Development() {
		return domain.Refuse(domain.CodeDevBuild, domain.ErrNotAllowed, nil)
	}
	settings, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	if version == state.Skipped {
		return domain.Refuse(domain.CodeNoVersion, domain.ErrNotAllowed, nil)
	}

	channel := settings.UpdateChannel
	info := s.offer(state, channel)
	transfer := s.transfer.Load()
	reuseDownload := settings.UpdateDownloadAuto && info.Phase == phaseDownloaded && version == info.Latest
	s.transfer.Store(&updateTransfer{channel: channel, version: version, phase: phaseInstalling})
	defer s.transfer.Store(nil)
	info.Phase = phaseInstalling
	s.publish(info, false)

	if transfer != nil {
		if reuseDownload {
			return s.releases.Install(ctx, transfer.staged)
		}
		transfer.staged.discard()
	}
	staged, err := s.releases.Stage(ctx, channel, version)
	if err != nil {
		return err
	}
	return s.releases.Install(ctx, staged)
}

func (s *Updates) discard() {
	if transfer := s.transfer.Swap(nil); transfer != nil {
		transfer.staged.discard()
	}
}

func (s *Updates) announce(info UpdateStatus, notify bool) {
	show := notify && info.Available && s.announced != info.Latest
	if show {
		s.announced = info.Latest
	}
	s.publish(info, show)
}

func (s *Updates) publish(info UpdateStatus, show bool) {
	if s.host != nil {
		s.host.PublishUpdate(info, show)
	}
}

func (s *Updates) offer(state domain.UpdateState, channel domain.UpdateChannel) UpdateStatus {
	info := UpdateStatus{Current: s.version, Development: s.Development(), Notes: s.notes}
	transfer := s.transfer.Load()
	if transfer != nil && transfer.phase == phaseInstalling {
		info.Phase = phaseInstalling
	}
	if state.Channel != channel {
		return info
	}
	info.Latest = state.Latest
	info.CheckedAt = state.LastCheckAt
	if state.Latest == state.Skipped || !isNewer(state.Latest, s.version) {
		return info
	}
	info.Available = true
	info.Notes = state.Notes
	if transfer != nil && transfer.channel == channel && transfer.version == state.Latest {
		info.Phase = transfer.phase
	}
	return info
}
