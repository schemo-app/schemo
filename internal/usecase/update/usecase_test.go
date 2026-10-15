package update

import (
	"context"
	"errors"
	"testing"
	"time"

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

type fakeState struct {
	state domain.UpdateState
	err   error
	saves int
}

func (f *fakeState) LoadUpdateState(context.Context) (domain.UpdateState, error) {
	if f.err != nil {
		return domain.UpdateState{}, f.err
	}
	return f.state, nil
}

func (f *fakeState) SaveUpdateState(_ context.Context, state domain.UpdateState) error {
	if f.err != nil {
		return f.err
	}
	f.state = state
	f.saves++
	return nil
}

type fakeSettings struct {
	settings domain.Settings
	err      error
}

func (f *fakeSettings) Snapshot(context.Context) (domain.Settings, error) {
	if f.err != nil {
		return domain.Settings{}, f.err
	}
	return f.settings, nil
}

type harness struct {
	uc       *UseCase
	releases *fakeReleases
	state    *fakeState
	settings *fakeSettings
}

func newHarness(version string) *harness {
	releases := &fakeReleases{}
	state := &fakeState{}
	prefs := &fakeSettings{settings: domain.DefaultSettings()}
	return &harness{
		uc:       NewUseCase(releases, state, prefs, platform.BuildInfo{Version: version}),
		releases: releases,
		state:    state,
		settings: prefs,
	}
}

func TestCheckOffersANewerRelease(t *testing.T) {
	h := newHarness("1.0.0")
	h.releases.release = domain.Release{Version: "1.2.0", Notes: []string{"Fixed a thing"}}

	got, err := h.uc.Check(context.Background())
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
	if h.state.saves != 1 || h.state.state.LastCheckAt == 0 {
		t.Error("the check was not remembered: the next launch would ask again immediately")
	}
}

func TestCheckDoesNotOfferTheSameOrAnOlderRelease(t *testing.T) {
	for _, version := range []string{"1.0.0", "0.9.0"} {
		h := newHarness("1.0.0")
		h.releases.release = domain.Release{Version: version}

		got, err := h.uc.Check(context.Background())
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
	h := newHarness("1.0.0")
	h.releases.err = errors.New("no network")

	if _, err := h.uc.Check(context.Background()); err == nil {
		t.Fatal("Check returned no error")
	}
	if h.state.saves != 0 || h.state.state.LastCheckAt != 0 {
		t.Errorf("the failed check was recorded: %+v", h.state.state)
	}
}

func TestStatusReportsWithoutAsking(t *testing.T) {
	h := newHarness("1.0.0")
	h.state.state = domain.UpdateState{
		LastCheckAt: time.Now().UnixMilli(),
		Latest:      "1.2.0",
		Notes:       []string{"Fixed a thing"},
	}

	got, err := h.uc.Status(context.Background())
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
	h := newHarness("1.0.0")

	got, err := h.uc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Available || got.CheckedAt != 0 || got.Current != "1.0.0" {
		t.Errorf("Status = %+v, want only the running version", got)
	}
}

func TestNotesFollowTheDisplayedRelease(t *testing.T) {
	h := newHarness("1.0.0")
	h.uc = NewUseCase(h.releases, h.state, h.settings, platform.BuildInfo{
		Version: "1.0.0", ReleaseNotes: "- Installed release changes",
	})
	ctx := context.Background()
	assertNotes := func(want string) {
		t.Helper()
		got, err := h.uc.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Notes) != 1 || got.Notes[0] != want {
			t.Errorf("Status.Notes = %q, want [%q]", got.Notes, want)
		}
	}
	assertNotes("Installed release changes")
	h.releases.release = domain.Release{Version: "1.0.0"}
	if _, err := h.uc.Check(ctx); err != nil {
		t.Fatal(err)
	}
	assertNotes("Installed release changes")
	h.releases.release = domain.Release{Version: "1.2.0", Notes: []string{"New release changes"}}
	if _, err := h.uc.Check(ctx); err != nil {
		t.Fatal(err)
	}
	assertNotes("New release changes")
	if _, err := h.uc.Skip(ctx); err != nil {
		t.Fatal(err)
	}
	assertNotes("Installed release changes")
}

func TestStartupCheckSkipsWhenTheLastCheckIsYoung(t *testing.T) {
	h := newHarness("1.0.0")
	h.state.state.LastCheckAt = time.Now().Add(-time.Hour).UnixMilli()
	h.releases.release = domain.Release{Version: "1.2.0"}

	if got := h.uc.StartupCheck(context.Background()); got != nil {
		t.Errorf("StartupCheck = %+v, want nil inside the interval", got)
	}
	if len(h.releases.asked) != 0 {
		t.Error("the release source was asked despite a young check")
	}
}

func TestStartupCheckAsksAfterTheInterval(t *testing.T) {
	h := newHarness("1.0.0")
	h.state.state.LastCheckAt = time.Now().Add(-checkInterval - time.Hour).UnixMilli()
	h.releases.release = domain.Release{Version: "1.2.0"}

	got := h.uc.StartupCheck(context.Background())
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
			h := newHarness("1.0.0")
			h.state.state = domain.UpdateState{
				LastCheckAt: time.Now().Add(-time.Hour).UnixMilli(),
				Latest:      "1.2.0",
				Skipped:     skipped,
				Notes:       []string{"Release notes"},
			}
			got := h.uc.StartupCheck(context.Background())
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
	h := newHarness("1.0.0")
	h.settings.settings.UpdateCheckAuto = false
	h.releases.release = domain.Release{Version: "1.2.0"}

	if got := h.uc.StartupCheck(context.Background()); got != nil {
		t.Errorf("StartupCheck = %+v, want nil with the preference off", got)
	}
	if len(h.releases.asked) != 0 {
		t.Error("the release source was asked with the preference off")
	}

	if _, err := h.uc.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(h.releases.asked) != 1 {
		t.Errorf("asked %d times, want 1 from the manual check", len(h.releases.asked))
	}
}

func TestADevBuildNeverChecksOnItsOwn(t *testing.T) {
	h := newHarness(devVersion)
	h.releases.release = domain.Release{Version: "1.2.0"}

	if got := h.uc.StartupCheck(context.Background()); got != nil {
		t.Errorf("StartupCheck = %+v for a dev build, want nil", got)
	}
	if len(h.releases.asked) != 0 {
		t.Error("a dev build asked the release source")
	}
}

func TestCheckAsksTheChosenChannel(t *testing.T) {
	h := newHarness("1.0.0")
	h.settings.settings.UpdateChannel = domain.ChannelBeta
	h.releases.release = domain.Release{Version: "2.0.0-beta.1"}

	if _, err := h.uc.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(h.releases.asked) != 1 || h.releases.asked[0] != domain.ChannelBeta {
		t.Errorf("asked %v, want the beta channel", h.releases.asked)
	}
}

func TestSkippingHidesThatVersionAndNoOther(t *testing.T) {
	h := newHarness("1.0.0")
	h.releases.release = domain.Release{Version: "1.2.0", Notes: []string{"Fixed a thing"}}
	if _, err := h.uc.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}

	got, err := h.uc.Skip(context.Background())
	if err != nil {
		t.Fatalf("Skip: %v", err)
	}
	if got.Available {
		t.Error("the skipped version is still offered")
	}
	if h.state.state.Skipped != "1.2.0" {
		t.Errorf("Skipped = %q, want 1.2.0", h.state.state.Skipped)
	}

	h.releases.release = domain.Release{Version: "1.3.0", Notes: []string{"Added a thing"}}
	next, err := h.uc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !next.Available || next.Latest != "1.3.0" {
		t.Errorf("Check after skipping = %+v, want 1.3.0 offered", next)
	}
}

func TestSkipTakesTheVersionTheLastCheckFound(t *testing.T) {
	h := newHarness("1.0.0")
	h.state.state = domain.UpdateState{LastCheckAt: 1, Latest: "1.2.0"}

	got, err := h.uc.Skip(context.Background())
	if err != nil {
		t.Fatalf("Skip: %v", err)
	}
	if got.Available {
		t.Error("Available = true right after skipping")
	}
	if got.Latest != "1.2.0" {
		t.Errorf("Latest = %q, want the version that was found", got.Latest)
	}
}

func TestInstallHandsTheVersionToTheMechanism(t *testing.T) {
	h := newHarness("1.0.0")

	if err := h.uc.Install(context.Background(), "1.2.0"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(h.releases.installed) != 1 || h.releases.installed[0] != "1.2.0" {
		t.Errorf("installed %v, want [1.2.0]", h.releases.installed)
	}
}

func TestInstallRefusesABuildFromTheSources(t *testing.T) {
	h := newHarness("dev")

	err := h.uc.Install(context.Background(), "1.2.0")
	if code := domain.CodeOf(err); code != domain.CodeDevBuild {
		t.Fatalf("Install from a build of the tree = %v, want %q", err, domain.CodeDevBuild)
	}
	if len(h.releases.installed) != 0 {
		t.Error("a build from the tree reached the mechanism")
	}
	if !h.uc.Development() {
		t.Error("the build does not say it was made from the tree")
	}
}

func TestABuildFromTheSourcesSaysSoInWhatTheWindowReads(t *testing.T) {
	h := newHarness("dev")

	got, err := h.uc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !got.Development {
		t.Error("the status does not say this build was made from the tree")
	}

	released, err := newHarness("1.0.0").uc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if released.Development {
		t.Error("a released version is reported as a build of the tree")
	}
}

func TestInstallRefusesAnEmptyVersion(t *testing.T) {
	h := newHarness("1.0.0")

	if err := h.uc.Install(context.Background(), ""); err == nil {
		t.Fatal("Install with no version returned no error")
	}
	if len(h.releases.installed) != 0 {
		t.Error("an empty version reached the mechanism")
	}
}

func TestAStoreFailureIsReported(t *testing.T) {
	h := newHarness("1.0.0")
	h.state.err = errors.New("database is gone")

	if _, err := h.uc.Check(context.Background()); err == nil {
		t.Error("Check returned no error with an unreadable store")
	}
	if _, err := h.uc.Status(context.Background()); err == nil {
		t.Error("Status returned no error with an unreadable store")
	}
	if got := h.uc.StartupCheck(context.Background()); got != nil {
		t.Error("StartupCheck reported an update it could not have read")
	}
}
