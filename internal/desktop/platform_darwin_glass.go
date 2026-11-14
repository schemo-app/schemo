//go:build darwin && private_mac_apis

package desktop

// glassShows is true with the `private_mac_apis` tag, which makes the webview transparent through a
// private WebKit property; acceptable outside the App Store.
func glassShows() bool {
	return true
}
