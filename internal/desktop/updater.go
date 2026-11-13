package desktop

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Updater checks and applies releases. Updates serializes its operations because Wails
// downloads the release selected by its most recent successful check.
type Updater struct {
	updater *wailsupdater.Updater
	source  *source

	// Setup errors disable updates without preventing application startup.
	initErr         error
	installPackage  func(context.Context, string) error
	packageDownload *packageDownload
	restart         func() error
}

type stagedRelease struct {
	release *wailsupdater.Release
	path    string
}

func (r stagedRelease) discard() {
	if r.path == "" {
		return
	}
	// Both download paths put the payload inside a private staging directory.
	if err := os.RemoveAll(filepath.Dir(r.path)); err != nil {
		slog.Warn("Could not remove staged update", "err", err)
	}
}

// NewUpdater configures and initializes the Wails application updater.
// Setup errors are returned by subsequent checks.
func NewUpdater(app *application.App, info platform.BuildInfo, host *Host) *Updater {
	engine := &Updater{updater: app.Updater}

	if block, _ := pem.Decode(publicKey); block == nil {
		engine.initErr = errors.New("updater: this build carries no release signing key")
		return engine
	}

	suffix := ""
	if configurePackage(engine, host) {
		suffix = "-deb"
	}
	source, err := newSource(platform.Homepage() + "/releases/download/updates/{{channel}}" + suffix + ".json")
	if err != nil {
		engine.initErr = fmt.Errorf("updater: %w", err)
		return engine
	}

	err = app.Updater.Init(wailsupdater.Config{
		CurrentVersion: strings.TrimPrefix(info.Version, "v"),
		Providers:      []wailsupdater.Provider{source},
		PublicKey:      publicKey,
		Window:         wailsupdater.WindowNone,
	})
	if err != nil {
		engine.initErr = fmt.Errorf("updater: %w", err)
		return engine
	}

	engine.source = source
	if engine.installPackage != nil {
		engine.packageDownload = &packageDownload{source: source, key: publicKey, emit: host.Broadcast}
	}
	return engine
}

// Latest checks the specified channel manifest for the newest available release.
// If no newer version is available, it returns the current running version.
func (e *Updater) Latest(ctx context.Context, channel domain.UpdateChannel) (domain.Release, error) {
	rel, err := e.check(ctx, channel)
	if err != nil {
		return domain.Release{}, err
	}
	if rel == nil {
		return domain.Release{Version: e.updater.CurrentVersion()}, nil
	}
	return domain.Release{Version: rel.Version, Notes: ParseNotes(rel.Notes)}, nil
}

// Stage downloads and verifies a release for a later installation.
func (e *Updater) Stage(ctx context.Context, channel domain.UpdateChannel, version string) (stagedRelease, error) {
	if e.installPackage == nil {
		if err := updatable(); err != nil {
			return stagedRelease{}, err
		}
	}

	// Check again when staging from a remembered offer, which may have been superseded.
	rel, err := e.check(ctx, channel)
	if err != nil {
		return stagedRelease{}, err
	}
	if rel == nil || rel.Version != version {
		return stagedRelease{}, fmt.Errorf("updater: release %s is no longer offered", version)
	}
	staged := stagedRelease{release: rel}
	if e.installPackage != nil {
		staged.path, err = e.packageDownload.download(ctx, rel)
		return staged, err
	}
	if err := e.updater.DownloadAndInstall(ctx); err != nil {
		return stagedRelease{}, err
	}
	staged.path = e.updater.DownloadedPath()
	return staged, nil
}

// Install consumes the staged release. On a portable install, the Wails helper
// owns the staging directory once it accepts the restart.
func (e *Updater) Install(ctx context.Context, staged stagedRelease) error {
	if e.installPackage == nil {
		err := e.updater.Restart(ctx)
		if err != nil {
			staged.discard()
		}
		return err
	}
	defer staged.discard()
	e.packageDownload.publish(wailsupdater.EventInstalling, staged.release)
	if err := e.installPackage(ctx, staged.path); err != nil {
		return err
	}
	e.packageDownload.publish(wailsupdater.EventUpdateReady, staged.release)
	return e.restart()
}

func (e *Updater) check(
	ctx context.Context,
	channel domain.UpdateChannel,
) (*wailsupdater.Release, error) {
	if e.initErr != nil {
		return nil, e.initErr
	}
	if err := e.source.use(channel); err != nil {
		return nil, err
	}
	return e.updater.Check(ctx)
}

// updatable checks if the current binary location is writable and allowed to be updated. Wails
// stages the download and swaps the binary in the folder the executable lives in, so that folder
// has to take a new file. It refuses updates running from mounted volumes (e.g. DMG) or read-only
// locations.
func updatable() error {
	target, err := installTarget()
	if err != nil {
		return err
	}

	path := filepath.ToSlash(target)
	if strings.HasPrefix(path, "/Volumes/") || strings.Contains(path, "/AppTranslocation/") {
		return domain.Refuse(domain.CodeUpdateFromImage, domain.ErrNotAllowed, nil)
	}

	probe, err := os.CreateTemp(filepath.Dir(target), ".write-probe-*")
	if err != nil {
		return domain.Refuse(domain.CodeUpdateReadOnly, domain.ErrNotAllowed, nil)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return nil
}

// installTarget resolves the executable path to be replaced.
func installTarget() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	if i := strings.Index(self, ".app/Contents/MacOS/"); i >= 0 {
		return self[:i+len(".app")], nil
	}
	return self, nil
}
