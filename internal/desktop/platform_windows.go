//go:build windows

package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"

	"schemo/internal/domain"
)

func glassShows() bool {
	return w32.SupportsBackdropTypes()
}

func systemIsDark() bool {
	return w32.IsCurrentlyDarkMode()
}

func appearanceFor(theme domain.Theme) domain.Theme {
	if theme != domain.ThemeSystem {
		return theme
	}
	if systemIsDark() {
		return domain.ThemeDark
	}
	return domain.ThemeLight
}

func prepareGlassWindow(*application.WebviewWindow, domain.Theme) {}

func hideToolbarInFullscreen(*application.WebviewWindow) {}

func retintWindow(window *application.WebviewWindow, theme domain.Theme) {
	if window == nil {
		return
	}
	if hwnd := uintptr(window.NativeWindow()); hwnd != 0 {
		w32.SetTheme(hwnd, theme == domain.ThemeDark)
	}
}

func setWindowHeaderHeight(*application.WebviewWindow, float64) {}
