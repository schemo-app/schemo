package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// bridgeShutdownTimeout limits how long the bridge socket waits to close during app exit.
const bridgeShutdownTimeout = 2 * time.Second

// PauseCapture broadcasts a pause command to all connected extensions.
func (s *Extension) PauseCapture() {
	s.Broadcast([]byte(`{"type":"pause"}`))
}

// ResumeCapture broadcasts a resume command to all connected extensions.
func (s *Extension) ResumeCapture() {
	s.Broadcast([]byte(`{"type":"resume"}`))
}

// SetTabCapture pauses or resumes one tab in the specified browser.
func (s *Extension) SetTabCapture(tabID int, browser string, paused bool) error {
	if tabID <= 0 || browser == "" {
		return errors.New("invalid capture tab")
	}
	frame := struct {
		Type    string `json:"type"`
		TabID   int    `json:"tabId"`
		Browser string `json:"browser"`
	}{Type: "resume", TabID: tabID, Browser: browser}
	if paused {
		frame.Type = "pause"
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	s.Broadcast(payload)
	return nil
}

// ApplyCaptureFilters broadcasts updated capture rules to all connected extension clients.
func (s *Extension) ApplyCaptureFilters(filters domain.CaptureFilters) error {
	frame, err := json.Marshal(FiltersFrame{Type: "filters", Filters: filters})
	if err != nil {
		return err
	}
	s.Broadcast(frame)
	return nil
}

// ApplyExtensionSettings hands the two switches of the extension section to every connected
// extension. The extension carries them out (which tabs to arm, whether to come up on its own); the
// app only says what was chosen.
func (s *Extension) ApplyExtensionSettings(settings domain.ExtensionSettings) error {
	frame, err := json.Marshal(SettingsFrame{
		Type:         "settings",
		AutoConnect:  settings.AutoConnect,
		RememberTabs: settings.RememberTabs,
	})
	if err != nil {
		return err
	}
	s.Broadcast(frame)
	return nil
}

// Open registers the native messaging host and starts listening. Neither failure stops the app:
// the window stays usable, capture is what is lost.
//
//wails:ignore
func (s *Extension) Open() {
	// Re-register on every startup in case the binary location changed.
	if err := registerHost(s.dir); err != nil {
		log.Printf("[extension] native messaging host registration failed: %v", err)
	}

	if err := s.Start(); err != nil {
		// Log the error but do not fail startup so the app UI remains accessible.
		log.Printf("[extension] %v — capture disabled", err)
	}
}

// ServiceShutdown gracefully stops the extension bridge server within the timeout limit.
func (s *Extension) ServiceShutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), bridgeShutdownTimeout)
	defer cancel()
	return s.Shutdown(ctx)
}

// registerHost registers the current executable as a browser native messaging host.
func registerHost(dir platform.DataDir) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return Register(exe, dir)
}
