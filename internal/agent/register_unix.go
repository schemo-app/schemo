//go:build darwin || linux

package agent

import (
	"errors"
	"os"

	"schemo/internal/platform"
)

// Register points every installed browser at the host binary. The manifests live in the browsers'
// own folders here, so the app's folder is not needed.
func Register(host string, _ platform.DataDir) error {
	ids, err := loadBrowserIDs()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	var failures []error
	chromium := chromiumManifest(host, ids)
	for _, base := range chromiumProfiles(home) {
		if exists(base) {
			failures = append(failures, writeManifest(chromiumHosts(base), chromium))
		}
	}
	failures = append(failures, writeManifest(firefoxHosts(home), firefoxManifest(host, ids)))
	return errors.Join(failures...)
}

// exists is used to skip browsers not installed for this user.
func exists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}
