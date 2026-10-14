//go:build !windows

package updater

import (
	"os"
	"path/filepath"
	"syscall"

	"schemo/internal/platform"
)

// stageBesideTarget configures TMPDIR to sit on the same filesystem as the application target.
// This prevents cross-device rename failures on Unix platforms (e.g. when /tmp is on tmpfs).
func stageBesideTarget() {
	target, err := installTarget()
	if err != nil {
		return
	}
	parent := filepath.Dir(target)
	if sameDevice(parent, os.TempDir()) {
		return
	}

	dir := filepath.Join(parent, "."+platform.Slug+"-update")
	if os.MkdirAll(dir, 0o700) == nil {
		_ = os.Setenv("TMPDIR", dir)
	}
}

// sameDevice reports whether two filesystem paths reside on the same device.
func sameDevice(one, other string) bool {
	var left, right syscall.Stat_t
	if syscall.Stat(one, &left) != nil || syscall.Stat(other, &right) != nil {
		return true
	}
	return left.Dev == right.Dev
}
