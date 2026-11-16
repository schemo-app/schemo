package app

import (
	"context"

	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/internal/request"
)

// System is the app's own service: info, windows and storage status.
type System struct {
	host    *desktop.Host
	info    platform.BuildInfo
	status  *desktop.Status
	init    desktop.InitFunc
	start   startApplication
	dataDir platform.DataDir
	// auth is here for the words it cannot look up itself: see ApplyLanguage.
	auth *request.Authorizer
}

func NewSystem(
	host *desktop.Host,
	info platform.BuildInfo,
	status *desktop.Status,
	init desktop.InitFunc,
	start startApplication,
	dataDir platform.DataDir,
	auth *request.Authorizer,
) *System {
	return &System{host: host, info: info, status: status, init: init, start: start, dataDir: dataDir,
		auth: auth}
}

// Info is who the app is. UserAgent is what requests carry when the settings hold no other.
type Info struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Build     string `json:"build"` // CI run number, empty for local builds
	UserAgent string `json:"userAgent"`
}

func (s *System) Info() Info {
	return Info{Name: s.info.Name, Version: s.info.Version, Build: s.info.Build, UserAgent: s.info.UserAgent()}
}

func (s *System) ShowAbout() {
	s.host.ShowAbout()
}

// signInPagesFallback is what a sign-in puts in the browser before the window has said anything. It
// is English, the language of the build's own words; the window's choice replaces it as it mounts.
var signInPagesFallback = domain.SignInPages{
	Waiting: domain.SignInPage{
		Title: "Done",
		Text:  "Handing the token to the application…",
	},
	Done: domain.SignInPage{
		Title: "Done",
		Text:  "You can close this tab and go back to the application.",
	},
	Refused: domain.SignInPage{
		Title: "Refused",
		Text:  "The provider did not grant access. Go back to the application.",
	},
	Failed: "Could not hand the token to the application.",
}

// ApplyLanguage takes the window's words for the native menu and the sign-in pages: the catalogue is
// the window's, and Go cannot resolve "system".
func (s *System) ApplyLanguage(labels desktop.MenuLabels, pages domain.SignInPages) {
	s.host.ApplyMenu(labels)
	s.auth.SetPages(pages)
}

// ApplyWindowMenu takes the rows of the main window's drawn menu so the system bar can carry them.
// Only the main window calls it.
func (s *System) ApplyWindowMenu(groups []desktop.MenuGroup) {
	s.host.ApplyWindowMenu(groups)
}

// SetHeaderHeight keeps native controls aligned with the rendered main window header.
func (s *System) SetHeaderHeight(height float64) error {
	return s.host.SetHeaderHeight(height)
}

// ShowSettings opens the settings window on a category; an empty one keeps the current page.
func (s *System) ShowSettings(category string) {
	s.host.ShowSettings(category)
}

// StartupStatus is what the frontend reads first: without a database there is nothing else to
// show, and this is where it learns why.
func (s *System) StartupStatus() desktop.StartupStatus {
	return desktop.StartupStatus{
		Ready:   s.status.Ready(),
		Failure: s.status.Failure(),
		DataDir: string(s.dataDir),
		DBPath:  s.dataDir.DatabasePath(),
	}
}

// RetryInit re-runs the startup step behind the failure screen's "Retry" — the database may
// have been locked, the disk full, the directory permissions wrong.
func (s *System) RetryInit() (desktop.StartupStatus, error) {
	if s.status.Ready() {
		return s.StartupStatus(), nil
	}
	ctx := context.Background()
	err := s.init(ctx)
	if err == nil {
		err = s.start(ctx)
		if err != nil {
			s.status.Fail(desktop.FailureDatabase, err.Error())
		}
	}
	return s.StartupStatus(), err
}

// OpenDataFolder shows the user where the database lives, which is the one useful action left
// when it cannot be opened.
func (s *System) OpenDataFolder() error {
	return s.host.RevealFolder(string(s.dataDir))
}
