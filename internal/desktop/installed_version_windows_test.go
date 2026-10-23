package desktop

import (
	"crypto/rand"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"

	"schemo/internal/platform"
)

func TestSyncUninstallVersionAfterArchiveUpdate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		other   bool
		want    string
	}{
		{name: "updated executable", version: "0.1.0", want: "0.1.0"},
		{name: "prerelease", version: "0.1.1-beta.1", want: "0.1.1-beta.1"},
		{name: "rollback", version: "0.0.1", want: "0.0.1"},
		{name: "portable copy", version: "0.1.0", other: true, want: "0.0.2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parent := `Software\Schemo-Version-Test-` + rand.Text()
			path := parent + `\Previous Publisher` + platform.Name
			key, _, err := registry.CreateKey(registry.CURRENT_USER, path,
				registry.QUERY_VALUE|registry.SET_VALUE|registry.WOW64_64KEY)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				key.Close()
				if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil {
					t.Errorf("delete temporary uninstall entry: %v", err)
				}
				if err := registry.DeleteKey(registry.CURRENT_USER, parent); err != nil {
					t.Errorf("delete temporary uninstall parent: %v", err)
				}
			})
			executable := filepath.Join(t.TempDir(), "Schemo", "schemo.exe")
			if err := key.SetStringValue("DisplayIcon", `"`+strings.ToUpper(executable)+`"`); err != nil {
				t.Fatal(err)
			}
			if err := key.SetStringValue("DisplayVersion", "0.0.2"); err != nil {
				t.Fatal(err)
			}
			if tc.other {
				executable = filepath.Join(t.TempDir(), "portable", "schemo.exe")
			}
			if err := syncUninstallVersions(registry.CURRENT_USER, parent, executable, tc.version); err != nil {
				t.Fatalf("syncUninstallVersions(%q): %v", tc.version, err)
			}
			got, _, err := key.GetStringValue("DisplayVersion")
			if err != nil || got != tc.want {
				t.Errorf("DisplayVersion after launch of %q = %q, %v; want %q", tc.version, got, err, tc.want)
			}
		})
	}
}

func TestSyncUninstallVersionDoesNotRegisterPortableCopy(t *testing.T) {
	t.Parallel()
	path := `Software\Schemo-Version-Test-` + rand.Text()
	if err := syncUninstallVersions(registry.CURRENT_USER, path, `C:\portable\schemo.exe`, "0.1.0"); err != nil {
		t.Fatal(err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err == nil {
		key.Close()
		registry.DeleteKey(registry.CURRENT_USER, path)
		t.Fatal("syncUninstallVersion created an uninstall entry for a portable copy")
	}
	if !errors.Is(err, registry.ErrNotExist) {
		t.Fatalf("open missing uninstall entry: %v", err)
	}
}
