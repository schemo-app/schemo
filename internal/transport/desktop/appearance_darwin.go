//go:build darwin

package desktop

/*
#cgo CFLAGS: -mmacosx-version-min=12.0 -x objective-c -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// The material behind the window is AppKit's own — an NSVisualEffectView, which macOS tints from
// the appearance of the window it sits in — so moving the material means moving that appearance.
// No appearance at all is how AppKit is asked to follow the system.
void setAppearance(void* nsWindow, int palette) {
	NSWindow* window = (NSWindow*)nsWindow;
	if (window == nil) {
		return;
	}

	NSString* name = nil;
	if (palette == 1) {
		name = NSAppearanceNameDarkAqua;
	} else if (palette == 2) {
		name = NSAppearanceNameAqua;
	}

	[window setAppearance:(name == nil ? nil : [NSAppearance appearanceNamed:name])];
}

// An opaque window is drawn as the panel macOS uses for windows, and the material inside it is
// flattened into that panel — which is why Wails' translucent backdrop alone shows a solid light or
// dark ground instead of the desktop. Clearing the flag is what lets the window composite with what
// is behind it; its own clear background colour then stops painting over the material.
void setWindowOpaque(void* nsWindow, bool opaque) {
	NSWindow* window = (NSWindow*)nsWindow;
	if (window == nil) {
		return;
	}

	window.opaque = opaque;
}

// The material is what decides how much of the desktop reaches the chrome, and Wails leaves it at
// the default. Measured through the chrome's own fill, the default — and every semantic material
// beside it — passes almost nothing: the desktop arrives flattened into the appearance's own panel,
// so the glass reads as a plain colour. Only these two carry it: .light for a light chrome and
// .hudWindow for a dark one, both of which keep the hues behind them, which is what acrylic does on
// Windows.
//
// .light is deprecated — the SDK points at the appearance property instead — but the appearance is
// exactly the lever that does NOT work here: a semantic material under a forced light appearance
// measured flatter still (a two-level spread where .light gives ten), so the deprecated constant is
// the one that behaves, and the warning is silenced above rather than worked around.
void setWindowMaterial(void* nsWindow, bool dark) {
	NSWindow* window = (NSWindow*)nsWindow;
	if (window == nil) {
		return;
	}

	NSVisualEffectMaterial material = dark ? NSVisualEffectMaterialHUDWindow
	                                       : NSVisualEffectMaterialLight;
	for (NSView* view in [[window contentView] subviews]) {
		if ([view isKindOfClass:[NSVisualEffectView class]]) {
			((NSVisualEffectView*)view).material = material;
		}
	}
}

*/
import "C"

import (
	"unsafe"

	"schemo/internal/domain"
)

// The palettes the C side reads, in its order: zero is the absence of an appearance and so means
// "follow the system" rather than a third colour.
const (
	paletteSystem = iota
	paletteDark
	paletteLight
)

// Belongs on the main thread, which the caller's InvokeAsync provides.
func setWindowAppearance(nsWindow unsafe.Pointer, theme domain.Theme) {
	C.setAppearance(nsWindow, C.int(paletteOf(theme)))
}

// clearWindowOpaque lets the material through: the window stops being a panel of its own and starts
// compositing with the desktop behind it. Like setWindowAppearance it belongs on the main thread.
func clearWindowOpaque(nsWindow unsafe.Pointer) {
	C.setWindowOpaque(nsWindow, C.bool(false))
}

// See the C side above for why the default material will not do. The theme it takes is already
// resolved, so «follow the system» was answered before this call.
func setWindowMaterial(nsWindow unsafe.Pointer, appearance domain.Theme) {
	C.setWindowMaterial(nsWindow, C.bool(appearance == domain.ThemeDark))
}

func paletteOf(theme domain.Theme) int {
	switch theme {
	case domain.ThemeDark:
		return paletteDark
	case domain.ThemeLight:
		return paletteLight
	}
	return paletteSystem
}
