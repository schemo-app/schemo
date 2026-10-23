package agent

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestChromiumProfilesUseXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(home, "custom-config")
	for _, config := range []string{"", "relative-config", custom} {
		t.Run(config, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", config)
			base := filepath.Join(home, ".config")
			if config == custom {
				base = custom
			}
			profiles := chromiumProfiles(home)
			for _, browser := range []string{"google-chrome", "chromium", "microsoft-edge"} {
				want := filepath.Join(base, browser)
				if !slices.Contains(profiles, want) {
					t.Errorf("chromiumProfiles(%q) = %v, missing %q", home, profiles, want)
				}
			}
		})
	}
}
