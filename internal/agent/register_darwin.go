package agent

import "path/filepath"

func chromiumHosts(base string) string { return filepath.Join(base, "NativeMessagingHosts") }

func chromiumProfiles(home string) []string {
	support := filepath.Join(home, "Library", "Application Support")
	return []string{
		filepath.Join(support, "Google", "Chrome"),
		filepath.Join(support, "Google", "Chrome Beta"),
		filepath.Join(support, "Google", "Chrome Canary"),
		filepath.Join(support, "Chromium"),
		filepath.Join(support, "Microsoft Edge"),
		filepath.Join(support, "BraveSoftware", "Brave-Browser"),
		filepath.Join(support, "Vivaldi"),
		filepath.Join(support, "Yandex", "YandexBrowser"),
	}
}

func firefoxHosts(home string) string {
	return filepath.Join(home, "Library", "Application Support", "Mozilla", "NativeMessagingHosts")
}
