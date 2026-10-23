//go:build darwin

package desktop

/*
#cgo CFLAGS: -mmacosx-version-min=12.0 -x objective-c -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// The material is an NSVisualEffectView, which macOS tints from the appearance of its window, so
// moving the material means moving that appearance. No appearance at all follows the system.
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

// An opaque window flattens the material into its panel, so Wails' translucent backdrop alone shows
// a solid ground instead of the desktop. Clearing the flag lets the window composite with what is
// behind it.
void setWindowOpaque(void* nsWindow, bool opaque) {
	NSWindow* window = (NSWindow*)nsWindow;
	if (window == nil) {
		return;
	}

	window.opaque = opaque;
}

// The material decides how much of the desktop reaches the chrome. The default and every semantic
// material pass almost nothing, so the glass reads as a plain colour; only .light (light chrome)
// and .hudWindow (dark chrome) keep the hues behind them, as acrylic does on Windows.
//
// .light is deprecated in favour of the appearance property, but a semantic material under a forced
// light appearance measured flatter still, so the deprecated constant is used and its warning is
// silenced in the CFLAGS above.
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

// The palettes the C side reads, in its order: zero is no appearance, which follows the system.
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

// setWindowMaterial swaps the default material (see the C side above). The theme it takes is already
// resolved, so "follow the system" was answered before this call.
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
