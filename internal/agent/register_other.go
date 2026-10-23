//go:build !darwin && !linux && !windows

package agent

import "schemo/internal/platform"

// Register is a no-op on platforms the app does not ship for.
func Register(string, platform.DataDir) error { return nil }
