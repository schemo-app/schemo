// Package desktop is the OS shell: windows, menus, tray, dialogs, deep links and app updates.
// It knows nothing about the services; internal/app assembles them.
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

// FileDrop identifies the files and the designated element they were dropped onto.
type FileDrop struct {
	Files  []string `json:"files"`
	Target string   `json:"target"`
}

// NewApp is the application itself: its name, its assets, how a rejected call crosses to the
// window, and what a second launch does instead of starting another copy.
//
// It takes the storage's InitFunc for the order it brings: by the time the app is made the data
// folder has been made or has failed, and the web view's profile is placed knowing which.
func NewApp(
	host *Host,
	info platform.BuildInfo,
	assets Assets,
	dir platform.DataDir,
	status *Status,
	_ InitFunc,
) *application.App {
	app := application.New(application.Options{
		Name:         info.Name,
		PostShutdown: host.relaunch,
		Description:  info.Description,
		Icon:         assets.Icon,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets.FS),
		},
		MarshalError: marshalFailure,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewDataPath(status, dir),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               info.Identifier,
			EncryptionKey:          singleInstanceKey,
			OnSecondInstanceLaunch: host.OnSecondInstance,
		},
	})
	if err := syncInstalledVersion(info.Version); err != nil {
		app.Logger.Warn("Could not update the installed application version", "err", err)
	}
	host.SetAppName(info.Name)
	host.Attach(app)
	return app
}

// webviewDataPath is where the web view keeps its profile on Windows, which otherwise files it in
// the roaming profile under the executable's name: in the app's own folder, so everything the app
// keeps is in one place. A folder that could not be made leaves the system's default: a web view
// whose profile cannot be created ends the app with a message box, and the failure screen that
// explains the folder would never be drawn.
func webviewDataPath(status *Status, dir platform.DataDir) string {
	if failure := status.Failure(); failure != nil && failure.Kind == FailureDataDir {
		return ""
	}
	return dir.WebviewPath()
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
		EnableFileDrop:   true,
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
	mainWin.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		details := event.Context().DropTargetDetails()
		if details == nil {
			return
		}
		mainWin.EmitEvent(EventFilesDropped, FileDrop{
			Files:  event.Context().DroppedFiles(),
			Target: details.ElementID,
		})
	})
	hideToolbarInFullscreen(mainWin)
	if !UseCustomTitlebar() {
		// WKWebView consumes these accelerators before JavaScript receives keydown.
		mainWin.RegisterKeyBinding("Ctrl+Tab", func(window application.Window) {
			window.EmitEvent(EventMenuCommand, "tab-switch-next")
		})
		mainWin.RegisterKeyBinding("Ctrl+Shift+Tab", func(window application.Window) {
			window.EmitEvent(EventMenuCommand, "tab-switch-previous")
		})
	}

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
