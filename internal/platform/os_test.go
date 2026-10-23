package platform

import "testing"

// A machine is called what its owner named it, not the address a network gave it.
func TestAMachineIsCalledWithoutTheDomainANetworkGaveIt(t *testing.T) {
	for host, want := range map[string]string{
		"Mac-mini-Jurager.local":  "Mac-mini-Jurager",
		"laptop.corp.example.com": "laptop",
		"  Studio  ":              "Studio",
		"":                        "macOS",
		".local":                  "macOS",
	} {
		if got := deviceNameOf(host, "macOS"); got != want {
			t.Errorf("deviceNameOf(%q) = %q, want %q", host, got, want)
		}
	}
}
