package extension

import "path/filepath"

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

func firefoxProfile(home string) (base, hosts string) {
	base = filepath.Join(home, "Library", "Application Support", "Mozilla")
	return base, filepath.Join(base, "NativeMessagingHosts")
}
