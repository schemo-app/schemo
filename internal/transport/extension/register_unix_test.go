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
	installed := chromiumProfiles(home)[0]
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := Register("/opt/schemo/schemo"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(installed, "NativeMessagingHosts", HostName+".json"))
	if err != nil {
		t.Fatalf("the installed browser got no manifest: %v", err)
	}
	var m manifest
	_ = json.Unmarshal(written, &m)
	if m.Path != "/opt/schemo/schemo" || len(m.AllowedOrigins) == 0 {
		t.Errorf("manifest %+v", m)
	}
	if _, err := os.Stat(chromiumProfiles(home)[1]); err == nil {
		t.Errorf("a folder was made for a browser that is not installed")
	}
}
