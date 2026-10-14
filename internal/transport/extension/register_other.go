//go:build !darwin && !linux && !windows

package extension

// Register is a no-op on platforms the app does not ship for.
func Register(string) error { return nil }
