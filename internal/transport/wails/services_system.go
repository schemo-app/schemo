package wails

import (
	"context"
	"errors"

	"schemo/internal/domain"
	"schemo/internal/infra/authflow"
	"schemo/internal/platform"
	"schemo/internal/transport/desktop"
)

// SystemService is the app talking about itself: what it is, what its windows do, and whether it
// managed to open its own data. Updating has a service of its own; everything that belongs to no
// feature in particular stays here.
type SystemService struct {
	host    *desktop.Host
	info    platform.BuildInfo
	status  *desktop.Status
	init    desktop.InitFunc
	start   startApplication
	dataDir platform.DataDir
	// auth is here for the words it cannot look up itself: see ApplyLanguage.
	auth *authflow.Materializer
}

func NewSystemService(
	host *desktop.Host,
	info platform.BuildInfo,
	status *desktop.Status,
	init desktop.InitFunc,
	start startApplication,
	dataDir platform.DataDir,
	auth *authflow.Materializer,
) *SystemService {
	return &SystemService{host: host, info: info, status: status, init: init, start: start, dataDir: dataDir,
		auth: auth}
}

func (s *SystemService) Version() string {
	return s.info.Version
}

// Build is the CI run number, empty for local builds.
func (s *SystemService) Build() string {
	return s.info.Build
}

func (s *SystemService) Name() string {
	return s.info.Name
}

// UserAgent is what the requests carry when the settings hold no other: the settings field draws it
// as its own value, so the window never has to compose the name and the version itself.
func (s *SystemService) UserAgent() string {
	return s.info.UserAgent()
}

func (s *SystemService) ToggleMaximize() {
	s.host.ToggleMaximize()
}

func (s *SystemService) ShowAbout() {
	s.host.ShowAbout()
}

// signInPagesFallback is what a sign-in puts in the browser before the window has said anything.
// English, because that is the language the build's own words are in; the window's choice replaces
// it as it mounts, and the person reading a sign-in page is almost always looking at one that
// arrived after that.
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
// the native menu, which the system draws, and the pages a sign-in puts in the browser. Neither can
// be worded here — the catalogue is the window's, and Go cannot resolve "system", which is a
// question only the webview can ask — so this is the one piece of the interface that is sent
// instead of read. The two travel together because they are needed at the same two moments: as the
// window mounts, and again whenever the language moves.
func (s *SystemService) ApplyLanguage(labels desktop.MenuLabels, pages domain.SignInPages) {
	s.host.ApplyMenu(labels)
	s.auth.SetPages(pages)
}

// ApplyWindowMenu is the main window handing over the rows of the menu it draws itself on the
// platforms whose titlebar it draws, so that the platforms where the system draws one can carry
// the same rows. It is called whenever what the menu shows moves — a tab opens, the theme
// changes, a shortcut is rebound — and a window that is not the main one does not call it at all:
// the rows name that window's own tabs and stores, and only it has them.
func (s *SystemService) ApplyWindowMenu(groups []desktop.MenuGroup) {
	s.host.ApplyWindowMenu(groups)
}

// ShowSettings opens the preferences window. The category is the one to show — "account" for the
// account menu — and an empty one leaves the window where it was, which is what the rail's gear
// asks for.
func (s *SystemService) ShowSettings(category string) {
	s.host.ShowSettings(category)
}

// StartupStatus is what the frontend reads first: without a database there is nothing else to
// show, and this is where it learns why.
func (s *SystemService) StartupStatus() desktop.StartupStatus {
	return desktop.StartupStatus{
		Ready:   s.status.Ready(),
		Failure: s.status.Failure(),
		DataDir: string(s.dataDir),
		DBPath:  s.dataDir.DatabasePath(),
	}
}

// RetryInit re-runs the startup step behind the failure screen's "Retry" — the database may
// have been locked, the disk full, the directory permissions wrong.
func (s *SystemService) RetryInit() (desktop.StartupStatus, error) {
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
func (s *SystemService) OpenDataFolder() error {
	app := s.host.App()
	if app == nil {
		return errors.New("the application is not ready yet")
	}
	return app.Env.OpenFileManager(string(s.dataDir), false)
}
