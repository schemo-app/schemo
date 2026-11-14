//go:build darwin && !private_mac_apis

package desktop

// glassShows is false without the `private_mac_apis` tag: the page cannot be transparent, so the
// glass is painted in CSS.
func glassShows() bool {
	return false
}
