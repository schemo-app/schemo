package wails

import (
	"context"

	wailsupdater "github.com/wailsapp/wails/v3/pkg/updater"

	"schemo/internal/transport/desktop"
	"schemo/internal/usecase/update"
)

// UpdateService is the update feature's whole surface towards a window: the settings window checks,
// the main window's status bar reads what the last check found, and the update window installs it.
type UpdateService struct {
	update *update.UseCase
	host   *desktop.Host
}

func NewUpdateService(uc *update.UseCase, host *desktop.Host) *UpdateService {
	return &UpdateService{update: uc, host: host}
}

// Check runs a check the user asked for. Unlike the launch-time one it is never skipped and never
// gated on the preference, and what it found is published to every window: without that the status
// bar's link would not appear until the next launch.
func (s *UpdateService) Check(ctx context.Context) (update.Status, error) {
	info, err := s.update.Check(ctx)
	if err != nil {
		return update.Status{}, err
	}
	s.host.PublishUpdate(&info)
	return info, nil
}

// Status is what the last check saw, without touching the network: what a window opens on, and what
// the status bar is rebuilt from after a restart.
func (s *UpdateService) Status(ctx context.Context) (update.Status, error) {
	return s.update.Status(ctx)
}

// Skip records that the user does not want to hear about the version on screen again, answers with
// what the app has to say now, and publishes it: the window that asked is about to close, and the
// ones that stay open are showing the release it just dropped.
func (s *UpdateService) Skip(ctx context.Context) (update.Status, error) {
	info, err := s.update.Skip(ctx)
	if err != nil {
		return update.Status{}, err
	}
	s.host.PublishUpdate(&info)
	return info, nil
}

// Install downloads the release and replaces the running binary. On success it does not return.
func (s *UpdateService) Install(ctx context.Context, version string) error {
	err := s.update.Install(ctx, version)
	if err != nil {
		// Wails beta.28 does not emit failures to start the restart helper.
		s.host.Broadcast(wailsupdater.EventError, wailsupdater.ErrorInfo{
			Stage: wailsupdater.StageInstall, Message: err.Error(),
		})
	}
	return err
}

// ShowWindow opens the window that describes a release and offers to install it.
func (s *UpdateService) ShowWindow() {
	s.host.ShowUpdate()
}

// RequestCheck opens the settings' updates page and asks it to run a check. The menu and the
// window's own key both come here, so the two paths park and send the same request.
func (s *UpdateService) RequestCheck() {
	s.host.RequestUpdateCheck()
}

// TakeRequest reports and clears a check parked by RequestCheck. Destructive on purpose: both the
// mount and the page's own handler call it, and only the first may act on it.
func (s *UpdateService) TakeRequest() bool {
	return s.host.TakeUpdateCheckRequest()
}

func (s *UpdateService) checkStartup(ctx context.Context) {
	if info := s.update.StartupCheck(ctx); info != nil {
		s.host.PublishUpdate(info)
	}
}
