package extension

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"

	"schemo/internal/platform"
)

var (
	chromiumRegistry = []string{
		`Software\Google\Chrome\NativeMessagingHosts\`,
		`Software\Chromium\NativeMessagingHosts\`,
		`Software\Microsoft\Edge\NativeMessagingHosts\`,
		`Software\BraveSoftware\Brave-Browser\NativeMessagingHosts\`,
		`Software\Vivaldi\NativeMessagingHosts\`,
		`Software\Yandex\YandexBrowser\NativeMessagingHosts\`,
	}
	firefoxRegistry = `Software\Mozilla\NativeMessagingHosts\`
)

// Register writes the manifests to the app folder and points each browser's HKCU key at them.
func Register(host string) error {
	config, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(config, platform.DataDirName, "native-messaging")

	chromium := filepath.Join(dir, "chromium")
	firefox := filepath.Join(dir, "firefox")
	var failures []error
	failures = append(failures,
		writeManifest(chromium, chromiumManifest(host)),
		writeManifest(firefox, firefoxManifest(host)),
	)
	for _, key := range chromiumRegistry {
		manifest := filepath.Join(chromium, HostName+".json")
		failures = append(failures, point(key+HostName, manifest))
	}
	firefoxManifestPath := filepath.Join(firefox, HostName+".json")
	failures = append(failures, point(firefoxRegistry+HostName, firefoxManifestPath))
	return errors.Join(failures...)
}

func point(path, manifest string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue("", manifest)
}
