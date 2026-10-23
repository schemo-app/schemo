package agent

// This file registers the app as a native messaging host for browser extensions.

import (
	"slices"
	"strings"
)

// HostName is the native messaging host identifier used by browser extensions.
const HostName = "app.schemo.bridge"

// Launched reports whether the current process was started by a browser as a native messaging host.
func Launched(args []string) bool {
	if slices.ContainsFunc(args, func(arg string) bool { return strings.HasPrefix(arg, "chrome-extension://") }) {
		return true
	}
	ids, err := loadBrowserIDs()
	return err == nil && slices.Contains(args, ids.Firefox)
}
