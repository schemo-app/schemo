// Package nativehost registers the app as a native messaging host for browser extensions.
package extension

import (
	"slices"
	"strings"
)

// HostName is the native messaging host identifier used by browser extensions.
const HostName = "app.schemo.bridge"

// FirefoxExtensionID is the extension Gecko ID specified in browser_specific_settings.
const FirefoxExtensionID = "bridge@schemo.app"

// ChromiumExtensionIDs lists allowed Chromium extension IDs authorized to launch the host.
var ChromiumExtensionIDs = []string{
	"klimdljbmlanhmdjfgbilkkhpahgjmhd",
}

// Launched reports whether the current process was started by a browser as a native messaging host.
func Launched(args []string) bool {
	return slices.ContainsFunc(args, func(arg string) bool {
		return strings.HasPrefix(arg, "chrome-extension://") || arg == FirefoxExtensionID
	})
}
