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

// glassShows is the one question the build tag answers for this platform: it lives in
// window_darwin_glass.go under `private_mac_apis` and in window_darwin_opaque.go without it.

// systemIsDark says which way the system's own theme points: macOS reads this from its preferences
// rather than from a window, and the app's own appearance would be the wrong answer — the app pins
// that one to its theme. appearanceFor is what asks, for the window that follows the system.
func systemIsDark() bool {
	// The app is reached through the global because this function has no window to hang off, and it
	// can be asked before the app exists — the theme is read before the window that will show it.
	app := application.Get()
	if app == nil || app.Env == nil {
		return false
	}
	return app.Env.IsDarkMode()
}

// appearanceFor is the palette the material behind the window is chosen by. "Follow the system" is
// resolved here rather than left to AppKit, as macOS could do: the material is one of two, so the
// system's own answer has to be known when it is picked, and the window's appearance is what
// carries that answer to the re-tint when the system changes under a running app.
func appearanceFor(theme domain.Theme) domain.Theme {
	if theme != domain.ThemeSystem {
		return theme
	}
	if systemIsDark() {
		return domain.ThemeDark
	}
	return domain.ThemeLight
}

// prepareGlassWindow lets the material through: Wails gives a translucent window its vibrancy view
// but never clears the window's own opaque flag — only its "transparent" backdrop does, which this
// project does not use — and an opaque window flattens the material into a panel. A gap in the
// window options, so the flag is cleared here, on the main thread for the same reason retint is.
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

// retintWindow moves the material behind an existing window to another palette: macOS takes a
// window's appearance once, at creation, and this Wails has no setter — without this a theme
// switched at runtime would leave the vibrancy tinted the other way until the next launch. The
// native handle is asked for on the main thread, so a window closed in between cannot dangle.
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
