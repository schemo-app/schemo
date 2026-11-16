package desktop

import (
	"net/url"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/domain"
)

// Window pages; each is also a Vite entry in frontend/vite.config.ts.
const (
	pageAbout    = "/about.html"
	pageUpdate   = "/update.html"
	pageSettings = "/settings.html"
)

// page is a secondary window: opaque, one instance, framed like the main window.
type page struct {
	name                string
	path                string
	width, height       int
	minWidth, minHeight int
	fixed               bool
	// darkGround matches the page's background so the frame does not flash before the first paint.
	darkGround application.RGBA
}

// showPage focuses the named window when it is open and returns it as existing; otherwise it opens
// the window and returns it as created.
func (h *Host) showPage(spec page, params url.Values) (existing application.Window, created *application.WebviewWindow) {
	app := h.App()
	if app == nil {
		return nil, nil
	}
	if window, ok := app.Window.GetByName(spec.name); ok {
		bringToFront(window)
		return window, nil
	}
	minWidth, minHeight := spec.minWidth, spec.minHeight
	if spec.fixed {
		minWidth, minHeight = spec.width, spec.height
	}
	theme := h.heldTheme()
	ground := application.NewRGB(255, 255, 255)
	if windowTheme(theme) == application.Dark {
		ground = spec.darkGround
	}
	return nil, app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             spec.name,
		Title:            h.windowTitle(),
		Width:            spec.width,
		Height:           spec.height,
		MinWidth:         minWidth,
		MinHeight:        minHeight,
		DisableResize:    spec.fixed,
		Frameless:        UseCustomTitlebar(),
		BackgroundColour: ground,
		URL:              spec.path + encodeQuery(params),
		Mac: application.MacWindow{
			TitleBar:   application.MacTitleBarHiddenInset,
			Appearance: macWindowAppearance(theme),
		},
		Windows: application.WindowsWindow{Theme: windowTheme(theme)},
	})
}

func (h *Host) ShowAbout() {
	// The custom title bar takes room the inset macOS bar overlays.
	height := 415
	if UseCustomTitlebar() {
		height = 439
	}
	h.showPage(page{
		name: windowAbout, path: pageAbout,
		width: 336, height: height, fixed: true,
		darkGround: application.NewRGB(38, 38, 42),
	}, h.windowParams(false))
}

func (h *Host) ShowUpdate() {
	h.showPage(page{
		name: windowUpdate, path: pageUpdate,
		width: 560, height: 488, fixed: true,
		darkGround: application.NewRGB(38, 38, 42),
	}, h.windowParams(false))
}

// ShowSettings opens the settings window on a category; an empty one keeps the current page.
func (h *Host) ShowSettings(category string) {
	params := h.windowParams(false)
	if category != "" {
		params.Set("tab", category)
	}
	existing, created := h.showPage(page{
		name: windowSettings, path: pageSettings,
		width: 1160, height: 760,
		minWidth: 820, minHeight: 560,
		darkGround: application.NewRGB(28, 28, 30),
	}, params)
	if created != nil {
		hideToolbarInFullscreen(created)
	} else if existing != nil && category != "" {
		existing.EmitEvent(EventSettingsTab, category)
	}
}

// CloseMainWindow closes the main window; on macOS the app quits with it.
func (h *Host) CloseMainWindow() {
	if win := h.MainWindow(); win != nil {
		win.Close()
	}
}

// heldTheme is the stored theme choice, or "system" when none was stored yet.
func (h *Host) heldTheme() domain.Theme {
	theme, _ := h.theme.Load().(string)
	if theme == "" {
		return domain.ThemeSystem
	}
	return domain.Theme(theme)
}

func UseCustomTitlebar() bool {
	return application.System.IsPlatform(application.PlatformWindows) ||
		application.System.IsPlatform(application.PlatformLinux)
}

func bringToFront(w application.Window) {
	w.Show()
	w.UnMinimise()
	w.Focus()
}
