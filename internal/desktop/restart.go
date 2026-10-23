package desktop

import (
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// Restart launches the installed application after the current instance has shut down.
func (h *Host) Restart() error {
	path, err := os.Executable()
	if err != nil {
		return err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	h.mu.Lock()
	app := h.app
	if app == nil {
		h.mu.Unlock()
		return errors.New("application is not running")
	}
	h.restartPath = path
	h.mu.Unlock()
	app.Quit()
	return nil
}

func (h *Host) relaunch() {
	h.mu.Lock()
	path := h.restartPath
	h.restartPath = ""
	h.mu.Unlock()
	if path == "" {
		return
	}
	command := exec.Command(path)
	command.Dir = filepath.Dir(path)
	if err := command.Start(); err != nil {
		log.Printf("[updater] restart: %v", err)
		return
	}
	_ = command.Process.Release()
}
