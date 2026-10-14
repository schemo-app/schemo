package desktop

import (
	"net/url"
	"strconv"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/domain"
)

// The material behind a window, and the palette it is drawn in. Neither is something the page
// can reach — the platform owns the frame — so the choice is said twice: once on the URL, for
// the frame before the first paint, and once here, for the frame that is already on screen.

// SetTheme is the one writer of the theme: the window on screen is re-tinted here, one not yet
// created reads the choice from its URL. The tint is the platform's own: Windows repaints the
// frame and shadow a little after the call, macOS on its main thread — hence window first.
func (h *Host) SetTheme(theme domain.Theme) {
	h.theme.Store(string(theme))
	h.tint(theme)
}

// SystemThemeChanged follows the system's own switch, and only while the app follows it: the
// material is the platform's, so no page can move it — the frontend wipes its palette on its own.
func (h *Host) SystemThemeChanged() {
	if theme, _ := h.theme.Load().(string); theme != string(domain.ThemeSystem) {
		return
	}
	// The choice stays "system": a material that can follow it does so, one that cannot moves here.
	h.tint(domain.ThemeSystem)
}

func (h *Host) tint(theme domain.Theme) {
	appearance := appearanceFor(theme)
	if h.tinted.Load() {
		if applied, _ := h.appliedAppearance.Load().(string); applied == string(appearance) {
			// A different choice that resolves to the palette already in force — «System» under a dark
			// system while the app is dark — must not repaint the window: there is nothing to re-tint.
			return
		}
	}
	h.tinted.Store(true)
	h.appliedAppearance.Store(string(appearance))
	// Only the window that shows the material: the About window is opaque.
	if main := h.MainWindow(); main != nil {
		retintWindow(main, appearance)
	}
}

// windowQuery is what goes on a window's URL: the pre-paint script reads it before the first frame,
// which no IPC call can do. Both windows share one stylesheet, so the flag is what makes the same
// page the ground of one window and the glass of another.
func (h *Host) windowQuery(translucent bool) string {
	return encodeQuery(h.windowParams(translucent))
}

// windowParams is the frame's own choices, kept apart from the string they are written as: a window
// that needs one more thing on its address — the preferences window's category — adds it here.
func (h *Host) windowParams(translucent bool) url.Values {
	query := url.Values{}
	if theme, _ := h.theme.Load().(string); theme != "" {
		query.Set("theme", theme)
	}
	if language, _ := h.language.Load().(string); language != "" {
		query.Set("lang", language)
	}
	if translucent {
		query.Set("translucent", "1")
	}
	if appearance, ok := h.appearance.Load().(domain.Appearance); ok {
		query.Set("accent", string(appearance.Accent))
		query.Set("size", string(appearance.Size))
		query.Set("mono", string(appearance.MonoFont))
		query.Set("monoSize", strconv.Itoa(appearance.MonoSize))
	}
	return query
}

// encodeQuery writes parameters as an address suffix, or as nothing at all: an address ending in a
// bare "?" is one a page has to be careful with, and most windows ask for nothing.
func encodeQuery(query url.Values) string {
	if len(query) == 0 {
		return ""
	}
	return "?" + query.Encode()
}

// glassBackgroundType is translucent only where the machine can draw a material: a window that lets
// the desktop through with nothing to blur reads as a bug, not as glass.
func glassBackgroundType() application.BackgroundType {
	if glassShows() {
		return application.BackgroundTypeTranslucent
	}
	return application.BackgroundTypeSolid
}

// Acrylic rather than Mica: Mica tints the desktop wallpaper, and this chrome is glass over what
// lies behind the window. Read only when the window is translucent.
func glassWindowsBackdrop() application.BackdropType {
	return application.Acrylic
}

// glassMacBackdrop is the system's own glass, not the legacy vibrancy material Wails falls back to:
// measured on macOS 26 the legacy one frosts nothing, passing the desktop through so sharply the
// chrome reads as plain translucency. It needs a build carrying `private_mac_apis`.
func glassMacBackdrop() application.MacBackdrop {
	return application.MacBackdropLiquidGlass
}

// windowTheme is set once, when the window is created: the stored theme is read before the window
// exists, so the two agree at startup — a theme switched while the app runs is moved by tint.
func windowTheme(theme domain.Theme) application.Theme {
	switch theme {
	case domain.ThemeDark:
		return application.Dark
	case domain.ThemeLight:
		return application.Light
	}
	// Not SystemDefault: it leaves a window's attributes alone, and a window that never says which way
	// it is dark gets the light ones, and the material would be tinted against the app. Set once,
	// when the window is created.
	if systemIsDark() {
		return application.Dark
	}
	return application.Light
}

func macWindowAppearance(theme domain.Theme) application.MacAppearanceType {
	switch theme {
	case domain.ThemeDark:
		return application.NSAppearanceNameDarkAqua
	case domain.ThemeLight:
		return application.NSAppearanceNameAqua
	default:
		return application.DefaultAppearance
	}
}
