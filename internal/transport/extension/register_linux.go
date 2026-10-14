package extension

import "path/filepath"

func chromiumHosts(base string) string { return filepath.Join(base, "native-messaging-hosts") }

func chromiumProfiles(home string) []string {
	config := filepath.Join(home, ".config")
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

func firefoxProfile(home string) (base, hosts string) {
	base = filepath.Join(home, ".mozilla")
	return base, filepath.Join(base, "native-messaging-hosts")
}
