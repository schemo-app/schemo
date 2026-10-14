//go:build windows

package updater

// stageBesideTarget cross-volume file operations on Windows are handled automatically.
func stageBesideTarget() {}
