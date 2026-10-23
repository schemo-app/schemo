package platform

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// DataDir is the folder the app keeps its files in: the database, the socket the browser extension
// talks to, the native messaging manifests and the web view's profile.
type DataDir string

// NewDataDir is where those files live: the folder $SCHEMO_DATA_DIR names, or `.schemo` in the
// user's home directory. It touches nothing: the folder is made by Create, which is what a retry runs.
func NewDataDir() (DataDir, error) {
	if custom := os.Getenv(DataDirEnv); custom != "" {
		absolute, err := filepath.Abs(custom)
		if err != nil {
			return "", fmt.Errorf("resolving %s: %w", DataDirEnv, err)
		}
		return DataDir(absolute), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving the home directory: %w", err)
	}
	return DataDir(filepath.Join(home, DataFolder)), nil
}

// Create makes the folder, private to the user.
func (d DataDir) Create() error {
	if err := os.MkdirAll(string(d), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", d, reasonOf(err))
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

// NativeMessagingPath holds the manifests that tell the browsers where the extension's host is.
func (d DataDir) NativeMessagingPath() string {
	return filepath.Join(string(d), "native-messaging")
}

// WebviewPath is where the web view keeps its profile: its storage and caches.
func (d DataDir) WebviewPath() string {
	return filepath.Join(string(d), "webview")
}

// reasonOf is the system's own words for what went wrong, without the operation and path an
// os.PathError repeats: the callers name those once, in front.
func reasonOf(err error) error {
	var failed *fs.PathError
	if errors.As(err, &failed) {
		return failed.Err
	}
	var linked *os.LinkError
	if errors.As(err, &linked) {
		return linked.Err
	}
	return err
}
