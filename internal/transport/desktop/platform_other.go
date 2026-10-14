//go:build !windows && !darwin

package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/domain"
)

func glassShows() bool {
	return false
}

func systemIsDark() bool {
	return false
}

func appearanceFor(theme domain.Theme) domain.Theme {
	return theme
}

func retintWindow(*application.WebviewWindow, domain.Theme) {}

func hideToolbarInFullscreen(*application.WebviewWindow) {}

func prepareGlassWindow(*application.WebviewWindow, domain.Theme) {}
