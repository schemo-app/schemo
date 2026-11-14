//go:build darwin

package desktop

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"schemo/internal/domain"
)

/*
#cgo CFLAGS: -mmacosx-version-min=12.0 -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>

#include "window_controls_darwin.h"

void setWindowToolbar(void* nsWindow, bool use) {
	NSWindow* window = (NSWindow*)nsWindow;
	if (window == nil) {
		return;
	}

	static const void* toolbarKey = &toolbarKey;
	NSToolbar* toolbar = [window toolbar];

	if (use) {
		toolbar = objc_getAssociatedObject(window, toolbarKey);
		if (toolbar != nil) {
			[window setToolbar:toolbar];
			objc_setAssociatedObject(window, toolbarKey, nil, OBJC_ASSOCIATION_RETAIN);
		}
		return;
	}

	if (toolbar != nil) {
		objc_setAssociatedObject(window, toolbarKey, toolbar, OBJC_ASSOCIATION_RETAIN);
		[window setToolbar:nil];
	}
}
*/
import "C"

// glassShows is the one question the build tag answers: platform_darwin_glass.go under
// `private_mac_apis`, platform_darwin_opaque.go without it.

// systemIsDark reads the system preference, since the app's appearance is pinned to its theme.
func systemIsDark() bool {
	// The app is reached through the global because this function has no window and can be asked
	// before the app exists.
	app := application.Get()
	if app == nil || app.Env == nil {
		return false
	}
	return app.Env.IsDarkMode()
}

// appearanceFor resolves "system" here, since the material must be picked as light or dark.
func appearanceFor(theme domain.Theme) domain.Theme {
	if theme != domain.ThemeSystem {
		return theme
	}
	if systemIsDark() {
		return domain.ThemeDark
	}
	return domain.ThemeLight
}

// prepareGlassWindow clears the window's opaque flag, which Wails leaves set and which flattens the
// material. Runs on the main thread.
func prepareGlassWindow(window *application.WebviewWindow, theme domain.Theme) {
	if window == nil || !glassShows() {
		return
	}
	// The stored choice is resolved here as the re-tint resolves it, so a window and a switch agree.
	appearance := appearanceFor(theme)
	application.InvokeAsync(func() {
		if nsWindow := window.NativeWindow(); nsWindow != nil {
			clearWindowOpaque(nsWindow)
			setWindowAppearance(nsWindow, appearance)
			setWindowMaterial(nsWindow, appearance)
		}
	})
}

// retintWindow re-tints the material on a theme switch: macOS reads the appearance once and Wails
// has no setter. The handle is taken on the main thread so a closed window cannot dangle.
func retintWindow(window *application.WebviewWindow, theme domain.Theme) {
	if window == nil {
		return
	}
	application.InvokeAsync(func() {
		if nsWindow := window.NativeWindow(); nsWindow != nil {
			setWindowAppearance(nsWindow, theme)
			// The material is chosen by the theme too, so a switch moves both halves at once.
			setWindowMaterial(nsWindow, theme)
		}
	})
}

func hideToolbarInFullscreen(window *application.WebviewWindow) {
	if window == nil {
		return
	}
	window.OnWindowEvent(events.Mac.WindowWillEnterFullScreen,
		func(*application.WindowEvent) { applyToolbar(window, false) })
	window.OnWindowEvent(events.Mac.WindowDidExitFullScreen,
		func(*application.WindowEvent) { applyToolbar(window, true) })
}

func applyToolbar(window *application.WebviewWindow, use bool) {
	application.InvokeAsync(func() {
		if nsWindow := window.NativeWindow(); nsWindow != nil {
			C.setWindowToolbar(nsWindow, C.bool(use))
		}
	})
}

// setWindowHeaderHeight receives layout measured by the frontend, in window points.
func setWindowHeaderHeight(window *application.WebviewWindow, height float64) {
	application.InvokeAsync(func() {
		if nsWindow := window.NativeWindow(); nsWindow != nil {
			C.schemoSetWindowHeaderHeight(nsWindow, C.double(height))
		}
	})
}
