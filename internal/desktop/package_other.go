//go:build !linux

package desktop

func configurePackage(*Updater, *Host) bool { return false }
