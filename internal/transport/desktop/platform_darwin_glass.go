//go:build darwin && private_mac_apis

package desktop

// glassShows is true for this build: Wails makes the webview transparent through a private WebKit
// property, and without the `private_mac_apis` tag that call is a stub — an opaque page over a
// translucent window, which is the same as opaque. The property is private because there is no
// public one, a trade this project makes knowingly: it ships as a disk image and a zip, not
// through the App Store, where it would be barred.
func glassShows() bool {
	return true
}
