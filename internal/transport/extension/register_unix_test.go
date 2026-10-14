//go:build darwin || linux

package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterWritesAManifestOnlyForInstalledBrowsers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	base, hosts := firefoxProfile(home)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	installed := chromiumProfiles(home)[0]
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := Register("/opt/schemo/schemo"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ids, err := loadBrowserIDs()
	if err != nil {
		t.Fatal(err)
	}
	manifestDir := chromiumHosts(installed)
	if len(ids.origins()) == 0 {
		manifestDir = hosts
	}
	written, err := os.ReadFile(filepath.Join(manifestDir, HostName+".json"))
	if err != nil {
		t.Fatalf("the installed browser got no manifest: %v", err)
	}
	var m manifest
	_ = json.Unmarshal(written, &m)
	if m.Path != "/opt/schemo/schemo" || len(m.AllowedOrigins)+len(m.AllowedExtensions) == 0 {
		t.Errorf("manifest %+v", m)
	}
	if _, err := os.Stat(chromiumProfiles(home)[1]); err == nil {
		t.Errorf("a folder was made for a browser that is not installed")
	}
}
