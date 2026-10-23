package app

import (
	"context"
	"errors"

	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/internal/request"
)

// System is the app talking about itself: what it is, what its windows do and whether it
// managed to open its own data. Updating has a service of its own; everything that belongs to no
// feature in particular stays here.
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

// ApplyLanguage is the window handing over the words of the two surfaces Go draws but does not own:
// the native menu and the pages a sign-in puts in the browser. The catalogue is the window's and Go
// cannot resolve "system", so this is the one piece of the interface that is sent instead of read.
// They travel together because they are needed at the same two moments: as the window mounts and
// whenever the language moves.
func (s *System) ApplyLanguage(labels desktop.MenuLabels, pages domain.SignInPages) {
	s.host.ApplyMenu(labels)
	s.auth.SetPages(pages)
}

// ApplyWindowMenu is the main window handing over the rows of the menu it draws itself, so
// platforms where the system draws one can carry the same rows. It is called whenever what the menu
// shows moves (a tab opens, the theme changes, a shortcut is rebound); other windows do not call
// it, since the rows name the main window's own tabs and stores.
func (s *System) ApplyWindowMenu(groups []desktop.MenuGroup) {
	s.host.ApplyWindowMenu(groups)
}

// SetHeaderHeight keeps native controls aligned with the rendered main window header.
func (s *System) SetHeaderHeight(height float64) error {
	return s.host.SetHeaderHeight(height)
}

// ShowSettings opens the preferences window. The category is the one to show ("account" for the
// account menu); an empty one leaves the window where it was, which is what the rail's gear asks
// for.
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
	app := s.host.App()
	if app == nil {
		return errors.New("the application is not ready yet")
	}
	return app.Env.OpenFileManager(string(s.dataDir), false)
}
