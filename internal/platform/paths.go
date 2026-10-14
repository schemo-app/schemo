// Package platform provides shared application infrastructure.
package platform

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

type DataDir string

func NewDataDir() (DataDir, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving the config directory: %w", err)
	}
	return DataDir(filepath.Join(base, DataDirName)), nil
}

func (d DataDir) Create() error {
	if err := os.MkdirAll(string(d), 0o700); err != nil {
		var failed *fs.PathError
		if errors.As(err, &failed) {
			err = failed.Err
		}
		return fmt.Errorf("creating %s: %w", d, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(string(d), 0o700); err != nil {
			return fmt.Errorf("securing %s: %w", d, err)
		}
	}
	return nil
}

func (d DataDir) DatabasePath() string {
	return filepath.Join(string(d), "app.db")
}

func (d DataDir) BridgeSocketPath() string {
	return filepath.Join(string(d), "bridge.sock")
}
