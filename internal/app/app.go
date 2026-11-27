// Package app wires every feature's service, binds them to the window and registers their events.
// The desktop application itself is internal/desktop.
package app

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/db"
	"schemo/internal/desktop"
	"schemo/internal/platform"
)

// setup builds the services and registers them on the application.
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
		application.NewService(in.Specs),
		application.NewService(in.Publications),
	)

	desktop.NewMainWindow(app, host, info, current.Theme)
	desktop.WatchApplication(app, host)

	if desktop.UseCustomTitlebar() {
		app.Menu.Set(app.NewMenu())
	} else {
		// English labels until the window sends its catalogue; the window's rows follow once it is up.
		app.Menu.Set(desktop.BuildMenu(host, info.Name, desktop.EnglishMenuLabels(), nil))
	}
}

func register(app *application.App, services ...application.Service) {
	for _, service := range services {
		app.RegisterService(service)
	}
}
