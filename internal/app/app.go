// Package app assembles the program: it builds every feature's service by hand, binds them to the
// window, registers the events they publish, and answers the few ports one feature needs from
// another. The application those services belong to (windows, menu, tray) is `internal/desktop`.
package app

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/db"
	"schemo/internal/desktop"
	"schemo/internal/platform"
)

// setup is the composition: what the app is made of and what it registers as a service of its own.
// Everything drawn is `internal/desktop`'s; this file only says which of it exists, following the
// framework's split into one application with windows and the services a window calls.
func setup(
	app *application.App,
	host *desktop.Host,
	status *desktop.Status,
	in ServicesIn,
	info platform.BuildInfo,
	store *db.Store,
) {
	current := startupPreferences(context.Background(), status, store)
	host.SetTheme(current.Theme)
	host.SetLanguage(current.Language)
	host.SetAppearance(current.Appearance)

	register(app,
		application.NewService(in.System),
		application.NewService(in.Settings),
		application.NewService(in.History),
		application.NewService(in.Drafts),
		application.NewService(in.Updates),
		application.NewService(in.Account),
		application.NewService(in.Environments),
		application.NewService(in.Collections),
		application.NewService(in.Runs),
		application.NewService(in.Scripts),
		application.NewService(in.Extension),
		application.NewService(in.Workspaces),
		application.NewService(in.Search),
		application.NewService(in.Inspector),
		application.NewService(in.Cookies),
		application.NewService(in.Sync),
		application.NewService(in.Members),
		application.NewService(in.Plan),
		application.NewService(in.MCP),
		application.NewService(in.Transfer),
		application.NewService(in.Publications),
	)

	desktop.NewMainWindow(app, host, info, current.Theme)
	desktop.WatchApplication(app, host)

	if desktop.UseCustomTitlebar() {
		app.Menu.Set(app.NewMenu())
	} else {
		// The fallback words, until the window says which language it is in: `ApplyMenu` is called
		// again with the catalogue's own once it is settled. No rows yet either; the window sends
		// those once it is up, and until then the bar carries the menus the system owns.
		app.Menu.Set(desktop.BuildMenu(host, info.Name, desktop.EnglishMenuLabels(), nil))
	}
}

func register(app *application.App, services ...application.Service) {
	for _, service := range services {
		app.RegisterService(service)
	}
}
