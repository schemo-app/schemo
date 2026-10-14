// Package desktop is the application itself: the window it opens in, the menu and the tray, the
// system's dialogs, and the platform halves behind them. It knows nothing about the services a
// window may call — those are `transport/wails`’s, and this package hands them their host.
package desktop

import (
	"io/fs"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Assets is what the desktop is made of: the built window and the icon the system draws for it. The
// composition root supplies it, because that is the only place that knows where the build left it.
type Assets struct {
	FS   fs.FS
	Icon []byte
}

// NewApp is the application itself: its name, its assets, how a rejected call crosses to the
// window, and what a second launch does instead of starting another copy.
func NewApp(host *Host, info platform.BuildInfo, assets Assets) *application.App {
	app := application.New(application.Options{
		Name:        info.Name,
		Description: info.Description,
		Icon:        assets.Icon,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets.FS),
		},
		MarshalError: marshalFailure,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               info.Identifier,
			EncryptionKey:          singleInstanceKey,
			OnSecondInstanceLaunch: host.OnSecondInstance,
		},
	})
	host.SetAppName(info.Name)
	host.Attach(app)
	return app
}

// NewMainWindow draws the window the app opens in. The theme decides the material behind it, and
// the halves of that decision live in the platform files beside this one — one set per system.
func NewMainWindow(
	app *application.App,
	host *Host,
	info platform.BuildInfo,
	theme domain.Theme,
) *application.WebviewWindow {
	mainWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             windowMain,
		Title:            info.Name,
		Width:            mainWidth,
		Height:           mainHeight,
		MinWidth:         mainMinWidth,
		MinHeight:        mainMinHeight,
		Frameless:        UseCustomTitlebar(),
		BackgroundType:   glassBackgroundType(),
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/" + host.windowQuery(glassShows()),
		Mac: application.MacWindow{
			Backdrop:   glassMacBackdrop(),
			Appearance: macWindowAppearance(theme),
			TitleBar:   application.MacTitleBarHiddenInset,
		},
		Windows: application.WindowsWindow{
			BackdropType: glassWindowsBackdrop(),
			Theme:        windowTheme(theme),
		},
	})
	host.SetMainWindow(mainWin)
	hideToolbarInFullscreen(mainWin)

	// The material is prepared once the runtime is up, which is when there is a native window to
	// prepare: before that the handle is not there, and the flag would be cleared on nothing.
	mainWin.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		prepareGlassWindow(mainWin, theme)
		// The webview can miss the window's first layout while the glass view goes in: asking again
		// for the size the window already has is what lays its content out over the whole window.
		w, h := mainWin.Size()
		mainWin.SetSize(w, h)
		host.MarkReady()
	})
	return mainWin
}

// WatchApplication wires the two events that arrive at the application rather than at a window: a
// link that asked this app to open, and the system's own theme moving under it.
func WatchApplication(app *application.App, host *Host) {
	app.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl,
		func(e *application.ApplicationEvent) {
			host.HandleURLOpen(e.Context().URL())
		})
	app.Event.OnApplicationEvent(events.Common.ThemeChanged, func(*application.ApplicationEvent) {
		host.SystemThemeChanged()
	})
}
