package platform

import "testing"

// The version arrives from the build rather than from the source, so the one thing worth pinning
// here is that it always arrives: an empty version would reach the About window, the updater's
// comparison and the extension bridge's handshake as nothing at all.
//
// It is also how the injection is verified by hand, because a wrong symbol path in `-ldflags` fails
// silently — the linker does not warn, and the string still appears in the binary inside its
// recorded build settings. So the check is the value, not the build:
//
//	go test -v -ldflags "-X schemo/internal/platform.version=9.9.9" ./internal/platform/
func TestTheBuildSuppliesAVersion(t *testing.T) {
	info := NewBuildInfo()
	if info.Version == "" {
		t.Error("the version is empty: -ldflags is not reaching the variable")
	}
	t.Logf("version=%q build=%q", info.Version, info.Build)
}

// The identity is a constant and is never injected, which is the whole point of the split: a bundle
// identifier or a scheme that arrived empty would be a broken bundle, and no build may be able to
// produce one by forgetting a flag.
func TestTheIdentityIsAlwaysThere(t *testing.T) {
	info := NewBuildInfo()
	for what, value := range map[string]string{
		"name":        info.Name,
		"slug":        info.Slug,
		"identifier":  info.Identifier,
		"description": info.Description,
		"scheme":      Scheme,
		"dataDirName": DataDirName,
	} {
		if value == "" {
			t.Errorf("%s is empty", what)
		}
	}
}
