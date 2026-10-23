package desktop

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Updater checks for new releases and handles the update installation.
type Updater struct {
	updater *wailsupdater.Updater
	source  *source

	// Setup errors disable updates without preventing application startup.
	initErr         error
	installPackage  func(context.Context, string) error
	packageDownload *packageDownload
	restart         func() error

	// mu synchronizes release check and installation flows.
	mu sync.Mutex
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
	e.mu.Lock()
	defer e.mu.Unlock()

	rel, err := e.check(ctx, channel)
	if err != nil {
		return domain.Release{}, err
	}
	if rel == nil {
		return domain.Release{Version: e.updater.CurrentVersion()}, nil
	}
	return domain.Release{Version: rel.Version, Notes: ParseNotes(rel.Notes)}, nil
}

// Install downloads, verifies, and installs the target release, restarting the app on success.
func (e *Updater) Install(ctx context.Context, channel domain.UpdateChannel, version string) error {
	if e.installPackage == nil {
		if err := updatable(); err != nil {
			return err
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	rel, err := e.check(ctx, channel)
	if err != nil {
		return err
	}
	if rel == nil || rel.Version != strings.TrimPrefix(version, "v") {
		return fmt.Errorf("updater: release %s is no longer offered", version)
	}

	if e.installPackage != nil {
		path, err := e.packageDownload.download(ctx, rel)
		if err != nil {
			return err
		}
		defer func() {
			_ = os.Remove(path)
			_ = os.Remove(filepath.Dir(path))
		}()
		e.packageDownload.publish(wailsupdater.EventInstalling, rel)
		if err := e.installPackage(ctx, path); err != nil {
			return err
		}
		e.packageDownload.publish(wailsupdater.EventUpdateReady, rel)
		return e.restart()
	}
	if err := e.updater.DownloadAndInstall(ctx); err != nil {
		return err
	}
	return e.updater.Restart(ctx)
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
