package wails

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/platform"
	"schemo/internal/transport/desktop"
	"schemo/internal/usecase/settings"
)

// setup is the composition: what the app is made of, and what it registers as a service of its own.
// Everything drawn is `transport/desktop`'s — this file only says which of it exists, and the split
// it follows is the framework's own: one application with windows, and the services a window calls.
func setup(
	app *application.App,
	host *desktop.Host,
	status *desktop.Status,
	in ServicesIn,
	info platform.BuildInfo,
	settingsUC *settings.UseCase,
) {
	current := startupPreferences(context.Background(), status, settingsUC)
	host.SetTheme(current.Theme)
	host.SetLanguage(current.Language)
	host.SetAppearance(current.Appearance)

	register(app,
		application.NewService(in.System),
		application.NewService(in.Settings),
		application.NewService(in.Records),
		application.NewService(in.Drafts),
		application.NewService(in.Update),
		application.NewService(in.Account),
		application.NewService(in.Environments),
		application.NewService(in.Collections),
		application.NewService(in.Scripting),
		application.NewService(in.Bridge),
		application.NewService(in.Workspaces),
		application.NewService(in.Search),
		application.NewService(in.Inspect),
		application.NewService(in.Commands),
		application.NewService(in.Cookies),
		application.NewService(in.Sync),
		application.NewService(in.Members),
		application.NewService(in.Plan),
		application.NewService(in.Mcp),
		application.NewService(in.Tabs),
		application.NewService(in.Transfer),
		application.NewService(in.Publications),
	)

	desktop.NewMainWindow(app, host, info, current.Theme)
	desktop.WatchApplication(app, host)

	if desktop.UseCustomTitlebar() {
		app.Menu.Set(app.NewMenu())
	} else {
		// The fallback words, until the window says which language it is in — `ApplyMenu` is called
		// again with the catalogue's own the moment it is settled. No rows yet either: the window
		// sends those once it is up, and until then the bar carries the menus the system owns.
		app.Menu.Set(desktop.BuildMenu(host, info.Name, desktop.EnglishMenuLabels(), nil))
	}
}

func register(app *application.App, services ...application.Service) {
	for _, service := range services {
		app.RegisterService(service)
	}
}
