package wails

import (
	"context"
	"errors"
	"log"
	"time"

	"go.uber.org/fx"
	"golang.org/x/sync/errgroup"

	"schemo/internal/domain"
	"schemo/internal/infra/sqlite"
	"schemo/internal/transport/desktop"
	"schemo/internal/usecase/account"
	"schemo/internal/usecase/draft"
	"schemo/internal/usecase/publication"
	"schemo/internal/usecase/record"
	"schemo/internal/usecase/settings"
	"schemo/internal/usecase/sync"
	"schemo/internal/usecase/tabstrip"
	"schemo/internal/usecase/workspace"
)

const shutdownTimeout = 3 * time.Second

type startApplication func(context.Context) error
type stopApplication func(context.Context) error

type startupDeps struct {
	fx.In
	Status     *desktop.Status
	Workspaces *workspace.UseCase
	Drafts     *draft.UseCase
	Tabs       *tabstrip.UseCase
	Records    *record.UseCase
	Sync       *sync.UseCase
	Pages      *publication.UseCase
	Bridge     *BridgeService
	MCP        *McpService
	Updates    *UpdateService
	Settings   *settings.UseCase
	Host       *desktop.Host
}

func closeStorageOnStop(lc fx.Lifecycle, store *sqlite.Store) {
	lc.Append(fx.Hook{OnStop: func(context.Context) error { return store.Close() }})
}

func closeAccountOnStop(lc fx.Lifecycle, uc *account.UseCase) {
	lc.Append(fx.Hook{OnStop: func(context.Context) error { uc.Close(); return nil }})
}

func newStartup(in startupDeps) (startApplication, stopApplication) {
	lifetime, cancel := context.WithCancel(context.Background())
	var jobs errgroup.Group
	start := func(ctx context.Context) error {
		if !in.Status.Ready() {
			return nil
		}
		current := startupPreferences(ctx, in.Status, in.Settings)
		in.Host.SetTheme(current.Theme)
		in.Host.SetLanguage(current.Language)
		in.Host.SetAppearance(current.Appearance)
		startupError("network preferences", in.Settings.Restore(ctx))
		startupError("workspace", in.Workspaces.Start(ctx))
		startupError("draft", in.Drafts.Load(ctx))
		strip, err := in.Tabs.Strip(ctx)
		startupError("tabs", err)
		if err == nil {
			for _, id := range strip.Discarded {
				startupError("discard draft", in.Drafts.Discard(ctx, id))
			}
		}
		_, err = in.Records.Prune(ctx)
		startupError("history retention", err)
		if err := in.Sync.Recover(ctx); err != nil {
			return err
		}
		startupError("extension", in.Bridge.start())
		startupError("MCP", in.MCP.start(ctx))
		jobs.Go(func() error { in.Updates.checkStartup(lifetime); return nil })
		jobs.Go(func() error { startupError("publication takedowns", in.Pages.Replay(lifetime)); return nil })
		return nil
	}
	stop := func(ctx context.Context) error {
		cancel()
		stopping, done := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer done()
		return errors.Join(in.Sync.Stop(stopping), jobs.Wait())
	}
	return start, stop
}

func configureLifecycle(lc fx.Lifecycle, start startApplication, stop stopApplication) {
	lc.Append(fx.Hook{OnStart: start, OnStop: stop})
}

func startupPreferences(ctx context.Context, status *desktop.Status, settings *settings.UseCase) domain.Settings {
	current := domain.DefaultSettings()
	if !status.Ready() {
		return current
	}
	loaded, err := settings.Snapshot(ctx)
	if err == nil {
		current = loaded
	}
	startupError("preferences", err)
	return current
}

func startupError(operation string, err error) {
	if err != nil {
		log.Printf("[startup] %s: %v", operation, err)
	}
}
