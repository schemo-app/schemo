//go:build darwin || linux

package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"schemo/internal/platform"
)

func TestRegisterWritesManifestsForChromiumAndFirefoxWithoutAProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	installed := chromiumProfiles(home)[0]
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := Register("/opt/schemo/schemo", platform.DataDir(filepath.Join(home, ".schemo"))); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ids, err := loadBrowserIDs()
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{chromiumHosts(installed), firefoxHosts(home)} {
		written, err := os.ReadFile(filepath.Join(dir, HostName+".json"))
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", dir, err)
		}
		var m manifest
		if err := json.Unmarshal(written, &m); err != nil {
			t.Fatalf("Unmarshal(%q): %v", dir, err)
		}
		if m.Path != "/opt/schemo/schemo" || len(m.AllowedOrigins)+len(m.AllowedExtensions) == 0 {
			t.Errorf("Register manifest in %q = %+v", dir, m)
		}
		if dir == firefoxHosts(home) && (len(m.AllowedExtensions) != 1 || m.AllowedExtensions[0] != ids.Firefox) {
			t.Errorf("Firefox allowed extensions = %v, want [%s]", m.AllowedExtensions, ids.Firefox)
		}
	}
	if _, err := os.Stat(chromiumProfiles(home)[1]); err == nil {
		t.Errorf("a folder was made for a browser that is not installed")
	}
}
