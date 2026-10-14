package desktop

// The windows that are not the main one, and the geometry the design gives them.

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/domain"
)

// The pages the app opens as windows of their own. Each is a Vite entry as well — see
// `frontend/vite.config.ts` — and a test holds the two lists together: a page in one and not the
// other is a window that opens blank.
const (
	pageAbout    = "/about.html"
	pageUpdate   = "/update.html"
	pageSettings = "/settings.html"
)

func (h *Host) ShowAbout() {
	app := h.App()
	if app == nil {
		return
	}

	if w, ok := h.windowByName(windowAbout); ok {
		bringToFront(w)
		return
	}

	theme := h.heldTheme()
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      windowAbout,
		Title:     h.windowTitle(),
		Width:     aboutWidth,
		Height:    aboutHeight(),
		MinWidth:  aboutWidth,
		MinHeight: aboutHeight(),
		// This flag disables resizing; v3 inverted it from v2's Resizable.
		DisableResize: true,
		Frameless:     UseCustomTitlebar(),
		// The page's own ground — `--bg-panel` — so the window does not sit in another colour while it
		// arrives: it is drawn before the page is, and nothing in it can be reached by a stylesheet.
		BackgroundColour: h.groundColour(38, 38, 42),
		URL:              pageAbout + h.windowQuery(false),
		Mac: application.MacWindow{
			TitleBar:   application.MacTitleBarHiddenInset,
			Appearance: macWindowAppearance(theme),
		},
		Windows: application.WindowsWindow{Theme: windowTheme(theme)},
	})
}

func (h *Host) ShowUpdate() {
	app := h.App()
	if app == nil {
		return
	}
	if w, ok := h.windowByName(windowUpdate); ok {
		bringToFront(w)
		return
	}

	theme := h.heldTheme()
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             windowUpdate,
		Title:            h.windowTitle(),
		Width:            updateWidth,
		Height:           updateHeight,
		MinWidth:         updateWidth,
		MinHeight:        updateHeight,
		DisableResize:    true,
		Frameless:        UseCustomTitlebar(),
		BackgroundColour: h.groundColour(38, 38, 42),
		URL:              pageUpdate + h.windowQuery(false),
		Mac: application.MacWindow{
			TitleBar:   application.MacTitleBarHiddenInset,
			Appearance: macWindowAppearance(theme),
		},
		Windows: application.WindowsWindow{Theme: windowTheme(theme)},
	})
}

// CloseMainWindow closes the main window; on macOS the app goes with it, which is what the last
// window closing has always meant here.
func (h *Host) CloseMainWindow() {
	if win := h.MainWindow(); win != nil {
		win.Close()
	}
}

func (h *Host) windowByName(name string) (application.Window, bool) {
	app := h.App()
	if app == nil {
		return nil, false
	}
	return app.Window.GetByName(name)
}

func (h *Host) ShowSettings(category string) {
	app := h.App()
	if app == nil {
		return
	}
	if w, ok := h.windowByName(windowSettings); ok {
		bringToFront(w)
		if category != "" {
			w.EmitEvent(EventSettingsTab, category)
		}
		return
	}

	params := h.windowParams(false)
	if category != "" {
		params.Set("tab", category)
	}

	theme := h.heldTheme()
	settingsWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             windowSettings,
		Title:            h.windowTitle(),
		Width:            settingsWidth,
		Height:           settingsHeight,
		MinWidth:         settingsMinWidth,
		MinHeight:        settingsMinHeight,
		Frameless:        UseCustomTitlebar(),
		BackgroundColour: h.groundColour(28, 28, 30),
		URL:              pageSettings + encodeQuery(params),
		Mac: application.MacWindow{
			TitleBar:   application.MacTitleBarHiddenInset,
			Appearance: macWindowAppearance(theme),
		},
		Windows: application.WindowsWindow{Theme: windowTheme(theme)},
	})
	hideToolbarInFullscreen(settingsWin)
}

// heldTheme is the theme the app is drawing itself in: the stored choice, or the system's when the
// choice follows it — the same answer windowTheme gives to a window about to be made.
func (h *Host) heldTheme() domain.Theme {
	theme, _ := h.theme.Load().(string)
	if theme == "" {
		return domain.ThemeSystem
	}
	return domain.Theme(theme)
}

// groundColour is what a window shows before its page has painted: the theme's own ground for the
// dark case it is given and the light one otherwise. The palette's value is spelled here because
// the frame is the platform's — drawn before any page, out of every stylesheet's reach.
func (h *Host) groundColour(darkR, darkG, darkB uint8) application.RGBA {
	if windowTheme(h.heldTheme()) == application.Dark {
		return application.NewRGB(darkR, darkG, darkB)
	}
	return application.NewRGB(255, 255, 255)
}

func UseCustomTitlebar() bool {
	return application.System.IsPlatform(application.PlatformWindows) ||
		application.System.IsPlatform(application.PlatformLinux)
}

func aboutHeight() int {
	if UseCustomTitlebar() {
		return aboutHeightBarred
	}
	return aboutHeightInset
}

func bringToFront(w application.Window) {
	w.Show()
	w.UnMinimise()
	w.Focus()
}
