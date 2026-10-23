package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
	"golang.org/x/sys/windows/registry"

	"schemo/internal/platform"
)

// Archive updates replace the executable without running NSIS. Refresh its existing uninstall
// entry only after the new executable starts, so failed updates never advertise a new version.
func syncInstalledVersion(version string) error {
	version = strings.TrimPrefix(version, "v")
	if !semver.IsValid("v" + version) {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	path := `Software\Microsoft\Windows\CurrentVersion\Uninstall`
	var failure error
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		failure = errors.Join(failure, syncUninstallVersions(root, path, executable, version))
	}
	return failure
}

func syncUninstallVersions(root registry.Key, path, executable, version string) error {
	key, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return err
	}
	var failure error
	for _, name := range names {
		// NSIS includes the publisher in its key name; it can change between releases.
		if strings.HasSuffix(name, platform.Name) {
			failure = errors.Join(failure, syncUninstallVersion(root, path+`\`+name, executable, version))
		}
	}
	return failure
}

func syncUninstallVersion(root registry.Key, path, executable, version string) error {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()

	icon, _, err := key.GetStringValue("DisplayIcon")
	if err != nil {
		return err
	}
	// A portable copy must not change the version of a separate installed copy.
	if !strings.EqualFold(filepath.Clean(strings.Trim(icon, `"`)), filepath.Clean(executable)) {
		return nil
	}
	current, _, err := key.GetStringValue("DisplayVersion")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	if current == version {
		return nil
	}
	writable, err := registry.OpenKey(root, path, registry.SET_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	defer writable.Close()
	return writable.SetStringValue("DisplayVersion", version)
}
