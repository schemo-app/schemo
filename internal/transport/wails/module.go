package wails

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"go.uber.org/fx"

	"schemo/internal/domain"
	"schemo/internal/infra/authflow"
	"schemo/internal/infra/channel"
	"schemo/internal/infra/credentials"
	"schemo/internal/infra/fetch"
	"schemo/internal/infra/files"
	"schemo/internal/infra/httpx"
	"schemo/internal/infra/oidc"
	"schemo/internal/infra/scriptengine"
	"schemo/internal/infra/server"
	"schemo/internal/infra/sqlite"
	"schemo/internal/platform"
	"schemo/internal/transport/desktop"
	"schemo/internal/transport/extension"
	"schemo/internal/transport/mcp"
	"schemo/internal/transport/updater"
	"schemo/internal/usecase/account"
	"schemo/internal/usecase/collection"
	"schemo/internal/usecase/cookiejar"
	"schemo/internal/usecase/draft"
	"schemo/internal/usecase/environment"
	"schemo/internal/usecase/inspect"
	"schemo/internal/usecase/members"
	"schemo/internal/usecase/plan"
	"schemo/internal/usecase/publication"
	"schemo/internal/usecase/record"
	"schemo/internal/usecase/scripting"
	"schemo/internal/usecase/search"
	"schemo/internal/usecase/settings"
	"schemo/internal/usecase/sync"
	"schemo/internal/usecase/tabstrip"
	"schemo/internal/usecase/transfer"
	"schemo/internal/usecase/update"
	"schemo/internal/usecase/workspace"
)

type ServicesIn struct {
	fx.In
	System       *SystemService
	Account      *AccountService
	Settings     *SettingsService
	Update       *UpdateService
	Records      *RecordsService
	Drafts       *DraftService
	Environments *EnvironmentsService
	Collections  *CollectionsService
	Scripting    *ScriptingService
	Bridge       *BridgeService
	Workspaces   *WorkspaceService
	Search       *SearchService
	Inspect      *InspectService
	Commands     *CommandService
	Cookies      *CookiesService
	Sync         *SyncService
	Members      *MembersService
	Plan         *PlanService
	Mcp          *McpService
	Tabs         *TabsService
	Transfer     *TransferService
	Publications *PublicationsService
}

var Module = fx.Module("wails",
	fx.Provide(
		newStartup,
		extension.NewServer,
		mcp.NewServer,
		platform.NewDataDir, platform.NewIDGen, platform.NewBuildInfo,
		func(info platform.BuildInfo) httpx.Config { return httpx.Config{UserAgent: info.UserAgent()} },
		fx.Annotate(sqlite.NewStore,
			fx.As(fx.Self()),
			fx.As(new(environment.Store)),
			fx.As(new(settings.Store)),
			fx.As(new(account.Store)),
			fx.As(new(sync.Store)),
			fx.As(new(members.Store)),
			fx.As(new(plan.Store)),
			fx.As(new(record.Store)),
			fx.As(new(draft.Store)),
			fx.As(new(collection.Store)),
			fx.As(new(scripting.Tree)),
			fx.As(new(scripting.Store)),
			fx.As(new(search.Index)),
			fx.As(new(domain.Scope)),
			fx.As(new(update.State)),
			fx.As(new(workspace.Store)),
			fx.As(new(inspect.Tree)),
			fx.As(new(cookiejar.Store)),
			fx.As(new(tabstrip.Store)),
			fx.As(new(publication.Store)),
			fx.As(new(collection.Assertions)),
			fx.As(new(account.Data)),
		),
		fx.Annotate(httpx.NewEngine,
			fx.As(fx.Self()),
			fx.As(new(draft.NetDefaults)),
		),
		fx.Annotate(files.NewReader,
			fx.As(fx.Self()),
			fx.As(new(transfer.Files)),
			fx.As(new(draft.FileSource)),
		),
		fx.Annotate(scriptengine.NewEngine,
			fx.As(fx.Self()),
			fx.As(new(scripting.Engine)),
		),
		fx.Annotate(account.NewUseCase,
			fx.As(fx.Self()),
			fx.As(new(server.Session)),
			fx.As(new(domain.Principal)),
			fx.As(new(plan.Session)),
			fx.As(new(publication.Session)),
		),
		fx.Annotate(collection.NewUseCase,
			fx.As(fx.Self()),
			fx.As(new(transfer.Collections)),
			fx.As(new(publication.Collections)),
			fx.As(new(treeSource)),
			fx.As(new(nodeSource)),
		),
		fx.Annotate(cookiejar.NewUseCase,
			fx.As(fx.Self()),
			fx.As(new(record.Jar)),
		),
		fx.Annotate(draft.NewUseCase,
			fx.As(fx.Self()),
			fx.As(new(draftSource)),
		),
		fx.Annotate(environment.NewUseCase,
			fx.As(fx.Self()),
			fx.As(new(draft.VariableSource)),
			fx.As(new(publication.Environments)),
			fx.As(new(scripting.Variables)),
		),
		inspect.NewUseCase,
		members.NewUseCase,
		plan.NewUseCase,
		publication.NewUseCase,
		record.NewUseCase,
		fx.Annotate(scripting.NewUseCase,
			fx.As(fx.Self()),
			fx.As(new(record.Screener)),
		),
		search.NewUseCase,
		fx.Annotate(settings.NewUseCase,
			fx.As(fx.Self()),
			fx.As(new(record.RetentionSource)),
			fx.As(new(sync.Retention)),
			fx.As(new(tabstrip.Policy)),
			fx.As(new(update.Settings)),
		),
		fx.Annotate(sync.NewUseCase,
			fx.As(fx.Self()),
			fx.As(new(workspace.Linker)),
			fx.As(new(record.Source)),
		),
		tabstrip.NewUseCase,
		transfer.NewUseCase,
		update.NewUseCase,
		fx.Annotate(workspace.NewUseCase,
			fx.As(fx.Self()),
			fx.As(new(transfer.Workspaces)),
		),

		// Network & Clients
		channel.New,
		fx.Annotate(credentials.New, fx.As(new(account.Credentials))),
		func(dialer *channel.Dialer) account.Server {
			return oidc.New(oidc.HTTPClient(), dialer)
		},
		fx.Annotate(server.New,
			fx.As(fx.Self()),
			fx.As(new(sync.Server)),
			fx.As(new(members.Server)),
			fx.As(new(plan.Server)),
			fx.As(new(publication.Server)),
		),

		// Domain Adapters & Cross-Feature Bindings
		func(client *server.Client, uc *account.UseCase) workspace.Remote {
			return spaceRemote{client: client, account: uc}
		},
		func(uc *members.UseCase) workspace.Inviter { return inviteSender{members: uc} },
		func(host *desktop.Host) domain.Notifier { return newBus(host) },
		func(
			drafts *draft.UseCase, records *record.UseCase, scripts *scripting.UseCase,
		) collection.Sender {
			return collectionSender{drafts: drafts, records: records, scripts: scripts}
		},
		func(engine *httpx.Engine, jar *cookiejar.UseCase, policy *cookiePolicy) record.Executor {
			return engineExecutor{engine: engine, jar: jar, cookies: policy}
		},
		newCookiePolicy,
		func(engine *httpx.Engine, policy *cookiePolicy) settings.Outbound {
			return outbound{engine: engine, cookies: policy}
		},
		func(uc *environment.UseCase) collection.Environment { return activeEnvironment{uc} },
		func(uc *environment.UseCase) transfer.Environments { return environmentImport{uc} },
		func(engine *httpx.Engine) transfer.Remote { return fetch.New(engine.Client()) },
		func(uc *draft.UseCase) transfer.Commands { return commandCopies{uc} },
		func(engine *httpx.Engine, host *desktop.Host) *authflow.Materializer {
			auth := authflow.New(engine.Client(), host)
			auth.SetPages(signInPagesFallback)
			return auth
		},
		func(auth *authflow.Materializer) draft.AuthMaterializer { return auth },

		// Updates & Settings
		fx.Annotate(updater.New,
			fx.As(fx.Self()),
			fx.As(new(update.Releases)),
		),

		// Workspaces & Cookies Bindings
		func(jar *cookiejar.UseCase, policy *cookiePolicy) draft.Jar {
			return cookieSwitch{jar: jar, policy: policy}
		},
		func(drafts *draft.UseCase, values *environment.UseCase) record.Masker {
			return requestMask{drafts: drafts, values: values}
		},

		// Wails Services & Constructors
		fx.Annotate(desktop.NewHost,
			fx.As(fx.Self()),
			fx.As(new(account.Browser)),
			fx.As(new(rootPicker)),
		),
		desktop.NewStatus,
		desktop.OpenStorage,
		NewSystemService,
		NewAccountService,
		NewSettingsService,
		NewUpdateService,
		NewRecordsService,
		NewDraftService,
		NewEnvironmentsService,
		NewCollectionsService,
		NewScriptingService,
		NewBridgeService,
		NewWorkspaceService,
		NewSearchService,
		NewInspectService,
		NewCommandService,
		NewCookiesService,
		NewSyncService,
		NewMembersService,
		NewPlanService,
		NewPublicationsService,
		NewMcpService,
		NewTabsService,
		NewTransferService,
		newCaptureIngest,
		newMCPTools,
		desktop.NewApp,
	),
	fx.Invoke(closeStorageOnStop, closeAccountOnStop, setup, startWaking, followWake, configureLifecycle),
)

// startWaking connects storage write events directly to the sync engine wake signal.
func startWaking(store *sqlite.Store, uc *sync.UseCase) { store.SetWake(uc.WakeSpace) }

// followWake triggers a sync cycle when the system resumes from sleep.
func followWake(app *application.App, uc *sync.UseCase) {
	app.Event.OnApplicationEvent(events.Common.SystemDidWake, func(*application.ApplicationEvent) {
		uc.Wake()
	})
}
