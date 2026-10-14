//go:build darwin || linux

package extension

import (
	"errors"
	"os"
)

// Register points every installed browser at the host binary.
func Register(host string) error {
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
	if base, hosts := firefoxProfile(home); exists(base) {
		failures = append(failures, writeManifest(hosts, firefoxManifest(host, ids)))
	}
	return errors.Join(failures...)
}

// exists is used to skip browsers not installed for this user.
func exists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}
