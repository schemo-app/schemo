//go:build darwin && !private_mac_apis

package desktop

// glassShows is false without the `private_mac_apis` tag — see window_darwin_glass.go. A
// translucent window under an opaque page is opaque with one surprise more, so the window stays
// opaque and the glass is the CSS the chrome paints.
func glassShows() bool {
	return false
}
