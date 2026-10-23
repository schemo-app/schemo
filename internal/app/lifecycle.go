package app

import (
	"context"
	"errors"
	"log"
	"time"

	"golang.org/x/sync/errgroup"

	"schemo/internal/agent"
	"schemo/internal/cloud"
	"schemo/internal/db"
	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/request"
	"schemo/internal/workspace"
)

const shutdownTimeout = 3 * time.Second

type startApplication func(context.Context) error
type stopApplication func(context.Context) error

type startupDeps struct {
	Status     *desktop.Status
	Workspaces *workspace.Workspaces
	Records    *request.History
	Sync       *cloud.Sync
	Pages      *cloud.Publications
	Bridge     *agent.Extension
	MCP        *agent.MCP
	Updates    *desktop.Updates
	Store      *db.Store
	Network    outbound
	Host       *desktop.Host
}

func newStartup(in startupDeps) (startApplication, stopApplication) {
	lifetime, cancel := context.WithCancel(context.Background())
	var jobs errgroup.Group
	start := func(ctx context.Context) error {
		if !in.Status.Ready() {
			return nil
		}
		current := startupPreferences(ctx, in.Status, in.Store)
		in.Host.SetTheme(current.Theme)
		in.Host.SetLanguage(current.Language)
		in.Host.SetAppearance(current.Appearance)
		in.Network.Apply(current.Network())
		startupError("workspace", in.Workspaces.Start(ctx))
		_, err := in.Records.Prune(ctx)
		startupError("history retention", err)
		if err := in.Sync.Recover(ctx); err != nil {
			return err
		}
		in.Bridge.Open()
		if current.MCP.Enabled {
			startupError("MCP", in.MCP.Apply(current.MCP))
		}
		jobs.Go(func() error { in.Updates.CheckOnStartup(lifetime); return nil })
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

func startupPreferences(ctx context.Context, status *desktop.Status, store *db.Store) domain.Settings {
	current := domain.DefaultSettings()
	if !status.Ready() {
		return current
	}
	loaded, err := store.Preferences(ctx)
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
