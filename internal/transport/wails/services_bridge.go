package wails

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"schemo/internal/domain"
	"schemo/internal/transport/extension"
)

// bridgeShutdownTimeout limits how long the bridge socket waits to close during app exit.
const bridgeShutdownTimeout = 2 * time.Second

// BridgeService exposes the browser extension bridge to the Wails window.
type BridgeService struct {
	server *extension.Server
}

// NewBridgeService creates a new BridgeService instance.
func NewBridgeService(server *extension.Server) *BridgeService {
	return &BridgeService{server: server}
}

// Listening reports whether the extension bridge server is running and accepting connections.
func (s *BridgeService) Listening() bool {
	return s.server.Listening()
}

// PauseCapture broadcasts a pause command to all connected extensions.
func (s *BridgeService) PauseCapture() {
	s.server.Broadcast([]byte(`{"type":"pause"}`))
}

// ResumeCapture broadcasts a resume command to all connected extensions.
func (s *BridgeService) ResumeCapture() {
	s.server.Broadcast([]byte(`{"type":"resume"}`))
}

// ApplyCaptureFilters broadcasts updated capture rules to all connected extension clients.
func (s *BridgeService) ApplyCaptureFilters(filters domain.CaptureFilters) error {
	frame, err := json.Marshal(extension.FiltersFrame{Type: "filters", Filters: filters})
	if err != nil {
		return err
	}
	s.server.Broadcast(frame)
	return nil
}

// ApplyExtensionSettings hands the two switches of the extension section to every connected
// extension. It is the extension that carries them out — which tabs to arm, and whether to come up
// on its own — so the app only says what was chosen.
func (s *BridgeService) ApplyExtensionSettings(settings domain.ExtensionSettings) error {
	frame, err := json.Marshal(extension.SettingsFrame{
		Type:         "settings",
		AutoConnect:  settings.AutoConnect,
		RememberTabs: settings.RememberTabs,
	})
	if err != nil {
		return err
	}
	s.server.Broadcast(frame)
	return nil
}

// Status is what the extension section draws at the top: whether a browser is connected, and what
// it last said about itself. It is a reading rather than an answer — a window that opens after the
// extension has already spoken has nothing coming to it.
func (s *BridgeService) Status() extension.Status {
	return s.server.Status()
}

func (s *BridgeService) start() error {
	// Re-register on every startup in case the binary location changed.
	if err := registerHost(); err != nil {
		log.Printf("[extension] native messaging host registration failed: %v", err)
	}

	if err := s.server.Start(); err != nil {
		// Log the error but do not fail startup so the app UI remains accessible.
		log.Printf("[extension] %v — capture disabled", err)
	}
	return nil
}

// ServiceShutdown gracefully stops the extension bridge server within the timeout limit.
func (s *BridgeService) ServiceShutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), bridgeShutdownTimeout)
	defer cancel()
	return s.server.Shutdown(ctx)
}

// registerHost registers the current executable as a browser native messaging host.
func registerHost() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return extension.Register(exe)
}
