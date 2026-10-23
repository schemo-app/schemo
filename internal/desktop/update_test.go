package desktop

import (
	"context"
	"errors"
	"testing"
	"time"

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

func (f *fakeReleases) Install(_ context.Context, _ domain.UpdateChannel, version string) error {
	f.installed = append(f.installed, version)
	return f.installErr
}

type harness struct {
	t        *testing.T
	updates  *Updates
	releases *fakeReleases
	store    *db.Store
}

func newHarness(t *testing.T, version string) *harness {
	t.Helper()
	releases := &fakeReleases{}
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

func TestStartupCheckSkipsWhenTheLastCheckIsYoung(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.remember(domain.UpdateState{LastCheckAt: time.Now().Add(-time.Hour).UnixMilli()})
	h.releases.release = domain.Release{Version: "1.2.0"}

	if got := h.updates.startupCheck(context.Background()); got != nil {
		t.Errorf("StartupCheck = %+v, want nil inside the interval", got)
	}
	if len(h.releases.asked) != 0 {
		t.Error("the release source was asked despite a young check")
	}
}

func TestStartupCheckAsksAfterTheInterval(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.remember(domain.UpdateState{LastCheckAt: time.Now().Add(-checkInterval - time.Hour).UnixMilli()})
	h.releases.release = domain.Release{Version: "1.2.0"}

	got := h.updates.startupCheck(context.Background())
	if got == nil {
		t.Fatal("StartupCheck = nil, want the release")
	}
	if !got.Available {
		t.Error("Available = false, want true")
	}
}

func TestStartupOffersRememberedReleaseWithoutCheckingAgain(t *testing.T) {
	for _, skipped := range []string{"", "1.2.0"} {
		t.Run("skipped="+skipped, func(t *testing.T) {
			h := newHarness(t, "1.0.0")
			h.remember(domain.UpdateState{
				LastCheckAt: time.Now().Add(-time.Hour).UnixMilli(),
				Latest:      "1.2.0",
				Skipped:     skipped,
				Notes:       []string{"Release notes"},
			})
			got := h.updates.startupCheck(context.Background())
			if skipped != "" {
				if got != nil {
					t.Errorf("StartupCheck = %+v, want nil for skipped release", got)
				}
			} else if got == nil || !got.Available || got.Latest != "1.2.0" || len(got.Notes) != 1 {
				t.Errorf("StartupCheck = %+v, want remembered release with notes", got)
			}
			if len(h.releases.asked) != 0 {
				t.Error("StartupCheck reached the network inside the check interval")
			}
		})
	}
}

func TestAutoCheckOffStopsOnlyTheLaunchTimeCheck(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.prefer(func(s *domain.Settings) { s.UpdateCheckAuto = false })
	h.releases.release = domain.Release{Version: "1.2.0"}

	if got := h.updates.startupCheck(context.Background()); got != nil {
		t.Errorf("StartupCheck = %+v, want nil with the preference off", got)
	}
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

	if got := h.updates.startupCheck(context.Background()); got != nil {
		t.Errorf("StartupCheck = %+v for a dev build, want nil", got)
	}
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
	if got := h.updates.startupCheck(context.Background()); got != nil {
		t.Error("StartupCheck reported an update it could not have read")
	}
}
