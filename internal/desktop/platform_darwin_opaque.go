//go:build darwin && !private_mac_apis

package desktop

// glassShows is false without the `private_mac_apis` tag (see platform_darwin_glass.go): a
// translucent window under an opaque page is just opaque, so the window stays opaque and the glass
// is the CSS the chrome paints.
func glassShows() bool {
	return false
}
