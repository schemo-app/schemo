package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"

	"schemo/internal/db"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

type fakeReleases struct {
	release    domain.Release
	err        error
	asked      []domain.UpdateChannel
	installed  []string
	installErr error
	staged     []string
	stageErr   error
	stagingDir string
}

func (f *fakeReleases) Latest(
	_ context.Context,
	channel domain.UpdateChannel,
) (domain.Release, error) {
	f.asked = append(f.asked, channel)
	if f.err != nil {
		return domain.Release{}, f.err
	}
	return f.release, nil
}

func (f *fakeReleases) Install(_ context.Context, staged stagedRelease) error {
	defer staged.discard()
	f.installed = append(f.installed, staged.release.Version)
	return f.installErr
}

func (f *fakeReleases) Stage(_ context.Context, _ domain.UpdateChannel, version string) (stagedRelease, error) {
	f.staged = append(f.staged, version)
	if f.stageErr != nil {
		return stagedRelease{}, f.stageErr
	}
	dir, err := os.MkdirTemp(f.stagingDir, "stage-")
	if err != nil {
		return stagedRelease{}, err
	}
	return stagedRelease{release: &wailsupdater.Release{Version: version}, path: filepath.Join(dir, "release")}, nil
}

type harness struct {
	t        *testing.T
	updates  *Updates
	releases *fakeReleases
	store    *db.Store
}

func newHarness(t *testing.T, version string) *harness {
	t.Helper()
	releases := &fakeReleases{stagingDir: t.TempDir()}
	store := dbtest.New(t)
	return &harness{t: t, updates: NewUpdates(releases, store, nil, platform.BuildInfo{Version: version}),
		releases: releases, store: store}
}

// state is what the store remembers of the last check.
func (h *harness) state() domain.UpdateState {
	h.t.Helper()
	state, err := h.store.LoadUpdateState(context.Background())
	if err != nil {
		h.t.Fatalf("LoadUpdateState: %v", err)
	}
	return state
}

func (h *harness) remember(state domain.UpdateState) {
	h.t.Helper()
	if state.Channel == "" {
		state.Channel = domain.ChannelStable
	}
	if err := h.store.SaveUpdateState(context.Background(), state); err != nil {
		h.t.Fatalf("SaveUpdateState: %v", err)
	}
}

// prefer changes the saved preferences.
func (h *harness) prefer(change func(*domain.Settings)) {
	h.t.Helper()
	ctx := context.Background()
	prefs, err := h.store.Preferences(ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	change(&prefs)
	if err := h.store.SavePreferences(ctx, prefs); err != nil {
		h.t.Fatal(err)
	}
}

func TestCheckOffersANewerRelease(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.releases.release = domain.Release{Version: "1.2.0", Notes: []string{"Fixed a thing"}}

	got, err := h.updates.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !got.Available {
		t.Error("Available = false, want true for 1.2.0 over 1.0.0")
	}
	if got.Latest != "1.2.0" || got.Current != "1.0.0" {
		t.Errorf("Latest/Current = %q/%q, want 1.2.0/1.0.0", got.Latest, got.Current)
	}
	if len(got.Notes) != 1 {
		t.Errorf("Notes = %v, want the release's own line", got.Notes)
	}
	if h.state().LastCheckAt == 0 {
		t.Error("the check was not remembered: the next launch would ask again immediately")
	}
}

func TestCheckDoesNotOfferTheSameOrAnOlderRelease(t *testing.T) {
	for _, version := range []string{"1.0.0", "0.9.0"} {
		h := newHarness(t, "1.0.0")
		h.releases.release = domain.Release{Version: version}

		got, err := h.updates.Check(context.Background())
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if got.Available {
			t.Errorf("Available = true for %s over 1.0.0", version)
		}
		if len(got.Notes) != 0 {
			t.Error("notes were shown for a release that is not being offered")
		}
	}
}

func TestAFailedCheckIsNotRemembered(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.releases.err = errors.New("no network")

	if _, err := h.updates.Check(context.Background()); err == nil {
		t.Fatal("Check returned no error")
	}
	if state := h.state(); state.LastCheckAt != 0 {
		t.Errorf("the failed check was recorded: %+v", state)
	}
}

func TestStatusReportsWithoutAsking(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.remember(domain.UpdateState{
		LastCheckAt: time.Now().UnixMilli(),
		Latest:      "1.2.0",
		Notes:       []string{"Fixed a thing"},
	})

	got, err := h.updates.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !got.Available || got.Latest != "1.2.0" {
		t.Errorf("Status = %+v, want the remembered release offered", got)
	}
	if len(h.releases.asked) != 0 {
		t.Error("Status reached the release source; it must answer from what is already known")
	}
}

func TestStatusBeforeTheFirstCheckIsEmptyAndNotAnError(t *testing.T) {
	h := newHarness(t, "1.0.0")

	got, err := h.updates.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Available || got.CheckedAt != 0 || got.Current != "1.0.0" {
		t.Errorf("Status = %+v, want only the running version", got)
	}
}

func TestNotesFollowTheDisplayedRelease(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.updates = NewUpdates(h.releases, h.store, nil, platform.BuildInfo{
		Version: "1.0.0", ReleaseNotes: "- Installed release changes",
	})
	ctx := context.Background()
	assertNotes := func(want string) {
		t.Helper()
		got, err := h.updates.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Notes) != 1 || got.Notes[0] != want {
			t.Errorf("Status.Notes = %q, want [%q]", got.Notes, want)
		}
	}
	assertNotes("Installed release changes")
	h.releases.release = domain.Release{Version: "1.0.0"}
	if _, err := h.updates.Check(ctx); err != nil {
		t.Fatal(err)
	}
	assertNotes("Installed release changes")
	h.releases.release = domain.Release{Version: "1.2.0", Notes: []string{"New release changes"}}
	if _, err := h.updates.Check(ctx); err != nil {
		t.Fatal(err)
	}
	assertNotes("New release changes")
	if _, err := h.updates.Skip(ctx); err != nil {
		t.Fatal(err)
	}
	assertNotes("Installed release changes")
}

func TestLaunchChecksAgainAfterARecentCheck(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.remember(domain.UpdateState{LastCheckAt: time.Now().Add(-time.Minute).UnixMilli(), Latest: "1.0.0"})
	h.releases.release = domain.Release{Version: "1.2.0"}

	h.updates.refresh(t.Context(), true)
	if got, err := h.updates.Status(t.Context()); err != nil || !got.Available || got.Latest != "1.2.0" {
		t.Fatalf("Status after launch = %+v, %v; want the new release", got, err)
	}
}

func TestUpdatesCheckAgainWhileRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, "1.0.0")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go h.updates.Run(ctx)
		synctest.Wait()
		time.Sleep(checkInterval)
		synctest.Wait()
		if got := len(h.releases.asked); got != 2 {
			t.Errorf("checks = %d, want one on launch and one after the interval", got)
		}
	})
}

func TestLaunchRetainsRememberedReleaseWhenOffline(t *testing.T) {
	for _, skipped := range []string{"", "1.2.0"} {
		t.Run("skipped="+skipped, func(t *testing.T) {
			h := newHarness(t, "1.0.0")
			h.remember(domain.UpdateState{
				LastCheckAt: time.Now().Add(-time.Hour).UnixMilli(),
				Latest:      "1.2.0",
				Skipped:     skipped,
				Notes:       []string{"Release notes"},
			})
			h.releases.err = errors.New("offline")
			h.updates.refresh(t.Context(), true)
			got, err := h.updates.Status(t.Context())
			if err != nil || got.Latest != "1.2.0" || got.Available != (skipped == "") {
				t.Errorf("Status after failed check = %+v, %v; want remembered release respecting skip", got, err)
			}
		})
	}
}

func TestAutoCheckOffStopsOnlyTheLaunchTimeCheck(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.prefer(func(s *domain.Settings) { s.UpdateCheckAuto = false })
	h.releases.release = domain.Release{Version: "1.2.0"}

	h.updates.refresh(t.Context(), true)
	if len(h.releases.asked) != 0 {
		t.Error("the release source was asked with the preference off")
	}

	if _, err := h.updates.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(h.releases.asked) != 1 {
		t.Errorf("asked %d times, want 1 from the manual check", len(h.releases.asked))
	}
}

func TestADevBuildNeverChecksOnItsOwn(t *testing.T) {
	h := newHarness(t, devVersion)
	h.releases.release = domain.Release{Version: "1.2.0"}

	h.updates.Run(t.Context())
	if len(h.releases.asked) != 0 {
		t.Error("a dev build asked the release source")
	}
}

func TestCheckAsksTheChosenChannel(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.prefer(func(s *domain.Settings) { s.UpdateChannel = domain.ChannelBeta })
	h.releases.release = domain.Release{Version: "2.0.0-beta.1"}

	if _, err := h.updates.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(h.releases.asked) != 1 || h.releases.asked[0] != domain.ChannelBeta {
		t.Errorf("asked %v, want the beta channel", h.releases.asked)
	}
}

func TestChangingChannelChecksEvenWithARecentResult(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.remember(domain.UpdateState{Latest: "1.2.0", LastCheckAt: time.Now().UnixMilli()})
	h.prefer(func(s *domain.Settings) { s.UpdateChannel = domain.ChannelBeta })
	got, err := h.updates.Status(t.Context())
	if err != nil || got.Available {
		t.Fatalf("Status before checking beta = %+v, %v; want no offer from stable", got, err)
	}
	h.releases.release = domain.Release{Version: "2.0.0-beta.1"}
	h.updates.refresh(t.Context(), false)
	got, err = h.updates.Status(t.Context())
	if err != nil || !got.Available || got.Latest != "2.0.0-beta.1" {
		t.Fatalf("Status after checking beta = %+v, %v; want the beta release", got, err)
	}
}

func TestBackgroundDownloadStagesAnOfferedReleaseOnce(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.releases.release = domain.Release{Version: "1.2.0"}
	h.updates.refresh(t.Context(), true)
	if len(h.releases.staged) != 0 {
		t.Fatal("update was staged before automatic downloads were enabled")
	}

	h.prefer(func(s *domain.Settings) { s.UpdateDownloadAuto = true })
	h.updates.refresh(t.Context(), false)
	h.updates.refresh(t.Context(), true)
	got, err := h.updates.Status(t.Context())
	if err != nil || got.Phase != phaseDownloaded {
		t.Fatalf("Status after download = %+v, %v; want the staged release", got, err)
	}
	if len(h.releases.staged) != 1 || len(h.releases.installed) != 0 {
		t.Fatalf("staged %v and installed %v; want one download awaiting installation", h.releases.staged, h.releases.installed)
	}
	h.prefer(func(s *domain.Settings) { s.UpdateDownloadAuto = false })
	h.updates.refresh(t.Context(), false)
	got, err = h.updates.Status(t.Context())
	if err != nil || !got.Available || got.Phase != "" {
		t.Fatalf("Status after disabling background downloads = %+v, %v; want the release without a staged download", got, err)
	}
	if entries, err := os.ReadDir(h.releases.stagingDir); err != nil || len(entries) != 0 {
		t.Fatalf("staging directory = %v, %v; want empty after disabling background downloads", entries, err)
	}
	h.prefer(func(s *domain.Settings) { s.UpdateDownloadAuto = true })
	h.updates.refresh(t.Context(), false)
	if _, err := h.updates.Skip(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.updates.refresh(t.Context(), true)
	if entries, err := os.ReadDir(h.releases.stagingDir); err != nil || len(entries) != 0 {
		t.Fatalf("staging directory = %v, %v; want empty after skipping", entries, err)
	}
	if len(h.releases.staged) != 2 {
		t.Fatalf("staged %v; skipping must prevent a new download", h.releases.staged)
	}
}

func TestFailedBackgroundDownloadCanBeRetried(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.prefer(func(s *domain.Settings) { s.UpdateDownloadAuto = true })
	h.releases.release = domain.Release{Version: "1.2.0"}
	h.releases.stageErr = errors.New("download failed")
	h.updates.refresh(t.Context(), true)
	got, err := h.updates.Status(t.Context())
	if err != nil || !got.Available || got.Phase != "" {
		t.Fatalf("Status after failed download = %+v, %v; want the release available", got, err)
	}
	h.releases.stageErr = nil
	h.updates.refresh(t.Context(), true)
	got, err = h.updates.Status(t.Context())
	if err != nil || got.Phase != phaseDownloaded || len(h.releases.staged) != 2 {
		t.Fatalf("Status after retry = %+v, %v; want the staged release on the second attempt", got, err)
	}
}

func TestSkippingHidesThatVersionAndNoOther(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.releases.release = domain.Release{Version: "1.2.0", Notes: []string{"Fixed a thing"}}
	if _, err := h.updates.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}

	got, err := h.updates.Skip(context.Background())
	if err != nil {
		t.Fatalf("Skip: %v", err)
	}
	if got.Available {
		t.Error("the skipped version is still offered")
	}
	if h.state().Skipped != "1.2.0" {
		t.Errorf("Skipped = %q, want 1.2.0", h.state().Skipped)
	}

	h.releases.release = domain.Release{Version: "1.3.0", Notes: []string{"Added a thing"}}
	next, err := h.updates.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !next.Available || next.Latest != "1.3.0" {
		t.Errorf("Check after skipping = %+v, want 1.3.0 offered", next)
	}
}

func TestInstallHandsTheVersionToTheMechanism(t *testing.T) {
	h := newHarness(t, "1.0.0")

	if err := h.updates.Install(context.Background(), "1.2.0"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(h.releases.installed) != 1 || h.releases.installed[0] != "1.2.0" {
		t.Errorf("installed %v, want [1.2.0]", h.releases.installed)
	}
}

func TestInstallReusesTheBackgroundDownload(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.prefer(func(s *domain.Settings) { s.UpdateDownloadAuto = true })
	h.releases.release = domain.Release{Version: "1.2.0"}
	h.updates.refresh(t.Context(), true)

	if err := h.updates.Install(t.Context(), "1.2.0"); err != nil {
		t.Fatal(err)
	}
	if len(h.releases.staged) != 1 || len(h.releases.installed) != 1 {
		t.Fatalf("staged %v, installed %v; want one download and one installation", h.releases.staged, h.releases.installed)
	}
}

// A build from the sources says so in its status and never installs a release over itself.
func TestABuildFromTheSourcesDoesNotInstall(t *testing.T) {
	h := newHarness(t, "dev")

	err := h.updates.Install(context.Background(), "1.2.0")
	if code := domain.CodeOf(err); code != domain.CodeDevBuild {
		t.Fatalf("Install = %v, want %q", err, domain.CodeDevBuild)
	}
	if len(h.releases.installed) != 0 {
		t.Error("a build from the sources reached the installer")
	}
	if status, err := h.updates.Status(context.Background()); err != nil || !status.Development {
		t.Errorf("Status = %+v, %v; want Development", status, err)
	}
	if status, _ := newHarness(t, "1.0.0").updates.Status(context.Background()); status.Development {
		t.Error("a released version reports Development")
	}
}

func TestInstallRefusesAnEmptyVersion(t *testing.T) {
	h := newHarness(t, "1.0.0")

	if err := h.updates.Install(context.Background(), ""); err == nil {
		t.Fatal("Install with no version returned no error")
	}
	if len(h.releases.installed) != 0 {
		t.Error("an empty version reached the mechanism")
	}
}

func TestAStoreFailureIsReported(t *testing.T) {
	h := newHarness(t, "1.0.0")
	_ = h.store.Close()

	if _, err := h.updates.Check(context.Background()); err == nil {
		t.Error("Check returned no error with an unreadable store")
	}
	if _, err := h.updates.Status(context.Background()); err == nil {
		t.Error("Status returned no error with an unreadable store")
	}
}
