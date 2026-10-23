package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"schemo/internal/agent"
	"schemo/internal/cloud"
	"schemo/internal/convert"
	"schemo/internal/db"
	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/internal/request"
	"schemo/internal/script"
	"schemo/internal/workspace"
)

// App is the assembled application: the Wails app plus the hooks run around app.Run.
type App struct {
	*application.App
	Start func(context.Context) error
	Stop  func(context.Context) error
}

// Keyring is the OS credential store: account refresh tokens and saved publication passwords.
type Keyring interface {
	cloud.Credentials
	cloud.Passwords
}

// Config is what the services are assembled from. Tests fill it with a temporary folder, an
// in-memory keyring and a fake browser; the desktop app with the real ones.
type Config struct {
	Dir      platform.DataDir
	Info     platform.BuildInfo
	Host     *desktop.Host
	Keyring  Keyring         // nil: the OS credential store
	Browser  cloud.Browser   // nil: the host's
	Notifier domain.Notifier // nil: broadcast to the windows
}

// Services is every part of the app that does not need a window.
type Services struct {
	ServicesIn
	Store    *db.Store
	Status   *desktop.Status
	Notifier domain.Notifier
	Network  outbound
	Auth     *request.Authorizer
}

// Assemble builds every service by hand. Order matters: a part is built after everything it
// uses. The store is created but not opened; desktop.OpenStorage (or a test) opens it.
func Assemble(cfg Config) (*Services, error) {
	ids := platform.NewIDGen()
	info, host, dir := cfg.Info, cfg.Host, cfg.Dir

	store, err := db.NewStore(dir)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	var notifier domain.Notifier = newBus(host)
	if cfg.Notifier != nil {
		notifier = cfg.Notifier
	}
	var keyring Keyring = cloud.NewKeyring(dir)
	if cfg.Keyring != nil {
		keyring = cfg.Keyring
	}
	var browser cloud.Browser = host
	if cfg.Browser != nil {
		browser = cfg.Browser
	}

	engine := request.NewEngine(request.EngineConfig{UserAgent: info.UserAgent()}, ids)
	reader := platform.NewFileReader()
	sandbox := script.NewEngine()
	policy := newCookiePolicy()
	auth := request.NewAuthorizer(engine.Client(), host)
	auth.SetPages(signInPagesFallback)

	dialer := cloud.NewDialer()
	account := cloud.NewAccount(store, keyring, cloud.NewOIDC(cloud.HTTPClient(), dialer), browser, notifier)
	client := cloud.NewClient(dialer, account)

	network := outbound{engine: engine, cookies: policy}
	settings := workspace.NewSettings(store, notifier, engine, reader, host.PickRootCertificate)
	environments := workspace.NewEnvironments(store, store, ids)
	cookies := request.NewCookies(store, store, ids)
	scripts := script.NewScripts(sandbox, store, environments, store, ids)
	sync := cloud.NewSync(store, client, account, store, notifier, ids)
	// The mask needs the draft, and the draft sends through history: the mask gets it once it exists.
	mask := &requestMask{environments: environments}
	history := request.NewHistory(request.HistoryDeps{
		Store: store, Scope: store, Jar: cookies, Executor: engineExecutor{engine: engine, jar: cookies, cookies: policy},
		Notifier: notifier, Retention: store, Source: sync, Screen: scripts, Mask: mask, IDs: ids,
		Build: info, Host: host,
	})
	collections := workspace.NewCollections(store, store, ids)
	drafts := request.NewDrafts(request.DraftDeps{
		Store: store, Scope: store, Vars: environments, Jar: cookieSwitch{cookies: cookies, policy: policy},
		Files: reader, Auth: auth, Net: engine, IDs: ids, Collections: collections, History: history,
		Host: host,
	})
	mask.drafts = drafts
	runs := request.NewRuns(store, collections, environments, drafts, history, scripts, notifier, ids)
	members := cloud.NewMembers(store, client, account)
	plan := cloud.NewPlan(store, client, account)
	publications := cloud.NewPublications(store, collections, environments, client, account, keyring)
	workspaces := workspace.NewWorkspaces(store, ids, notifier, workspaceRemote{client: client, account: account},
		sync, inviteSender{members: members})
	transfer := convert.NewTransfer(collections, environments, workspaces, reader,
		convert.NewFetcher(engine.Client()), commandCopies{drafts}, ids, host)

	extension := agent.NewExtension(agent.NewCapture(host, history, settings), dir, info)
	mcp := agent.NewMCP(agent.NewTools(workspaces, collections, runs, history, environments), notifier, info)
	watchSettings(settings, network, host, mcp, extension, history)

	store.SetWake(sync.WakeWorkspace)
	store.SetNotify(func(changed domain.Changed) { notifier.Publish(domain.TopicChanged, changed) })

	return &Services{
		ServicesIn: ServicesIn{
			Account:      account,
			Settings:     settings,
			History:      history,
			Drafts:       drafts,
			Environments: environments,
			Collections:  collections,
			Runs:         runs,
			Scripts:      scripts,
			Extension:    extension,
			Workspaces:   workspaces,
			Search:       workspace.NewSearch(store, store),
			Inspector:    request.NewInspector(store, store),
			Cookies:      cookies,
			Sync:         sync,
			Members:      members,
			Plan:         plan,
			MCP:          mcp,
			Transfer:     transfer,
			Publications: publications,
		},
		Store:    store,
		Status:   desktop.NewStatus(),
		Notifier: notifier,
		Network:  network,
		Auth:     auth,
	}, nil
}

// Build assembles the services and the window around them.
func Build(assets desktop.Assets) (*App, error) {
	dir, err := platform.NewDataDir()
	if err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	info := platform.NewBuildInfo()
	host := desktop.NewHost()
	s, err := Assemble(Config{Dir: dir, Info: info, Host: host})
	if err != nil {
		return nil, err
	}
	initStorage := desktop.OpenStorage(s.Store, s.Status, dir)
	app := desktop.NewApp(host, info, assets, dir, s.Status, initStorage)
	s.Updates = desktop.NewUpdates(desktop.NewUpdater(app, info, host), s.Store, host, info)

	start, stop := newStartup(startupDeps{
		Status:     s.Status,
		Workspaces: s.Workspaces,
		Records:    s.History,
		Sync:       s.Sync,
		Pages:      s.Publications,
		Bridge:     s.Extension,
		MCP:        s.MCP,
		Updates:    s.Updates,
		Store:      s.Store,
		Network:    s.Network,
		Host:       host,
	})
	s.System = NewSystem(host, info, s.Status, initStorage, start, dir, s.Auth)
	setup(app, host, s.Status, s.ServicesIn, info, s.Store)

	app.Event.OnApplicationEvent(events.Common.SystemDidWake, func(*application.ApplicationEvent) {
		s.Sync.Wake()
	})

	return &App{
		App:   app,
		Start: start,
		Stop: func(ctx context.Context) error {
			err := stop(ctx)
			s.Account.Close()
			return errors.Join(err, s.Store.Close())
		},
	}, nil
}

const lifecycleTimeout = 5 * time.Second

// Run builds the application, runs it and shuts it down.
func Run(assets desktop.Assets) error {
	app, err := Build(assets)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), lifecycleTimeout)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("startup: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), lifecycleTimeout)
		defer cancel()
		_ = app.Stop(ctx)
	}()
	return app.Run()
}
