package desktop

import "testing"

func TestIsNewerFollowsSemverPrecedence(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"1.2.0", "1.1.9", true},
		{"v1.10.0", "1.9.0", true},
		{"1.2.0", "1.2.0", false},
		{"1.3.0", "1.3.0-beta.1", true},
		{"1.3.0-beta.2", "1.3.0-beta.1", true},
		{"1.3.0-beta.1", "1.2.0", true},
		{"1.2.0", "dev", false},
		{"", "1.0.0", false},
	}
	for _, c := range cases {
		if got := isNewer(c.candidate, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", c.candidate, c.current, got, c.want)
		}
	}
}
