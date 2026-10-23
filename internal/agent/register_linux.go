package agent

import (
	"os"
	"path/filepath"
)

func chromiumHosts(base string) string { return filepath.Join(base, "native-messaging-hosts") }

func chromiumProfiles(home string) []string {
	config := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(config) {
		config = filepath.Join(home, ".config")
	}
	return []string{
		filepath.Join(config, "google-chrome"),
		filepath.Join(config, "google-chrome-beta"),
		filepath.Join(config, "chromium"),
		filepath.Join(config, "microsoft-edge"),
		filepath.Join(config, "BraveSoftware", "Brave-Browser"),
		filepath.Join(config, "vivaldi"),
		filepath.Join(config, "yandex-browser"),
	}
}

func firefoxHosts(home string) string {
	return filepath.Join(home, ".mozilla", "native-messaging-hosts")
}
