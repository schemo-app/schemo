package wails

import (
	"context"
	"path/filepath"

	"schemo/internal/domain"
	"schemo/internal/infra/files"
	"schemo/internal/infra/httpx"
	"schemo/internal/transport/desktop"
	"schemo/internal/transport/mcp"
	"schemo/internal/usecase/settings"
)

// rootPicker is the file dialog an authority is chosen in, declared by the service that asks for it
// so that a test can answer "the dialog was closed" without a window. The cancel path is a
// behaviour — nothing is written and nothing is announced — and a behaviour wants a test.
type rootPicker interface {
	PickRootCertificate(title string) (string, error)
}

type SettingsService struct {
	settings     *settings.UseCase
	engine       *httpx.Engine
	host         *desktop.Host
	files        *files.Reader
	mcp          *mcp.Server
	certificates rootPicker
}

func NewSettingsService(
	uc *settings.UseCase,
	engine *httpx.Engine,
	host *desktop.Host,
	reader *files.Reader,
	mcpServer *mcp.Server,
	certificates rootPicker,
) *SettingsService {
	return &SettingsService{
		settings: uc, engine: engine, host: host, files: reader, mcp: mcpServer,
		certificates: certificates,
	}
}

func (s *SettingsService) Snapshot(ctx context.Context) (domain.Settings, error) {
	return s.settings.Snapshot(ctx)
}

func (s *SettingsService) SetTheme(
	ctx context.Context,
	theme domain.Theme,
) (domain.Settings, error) {
	saved, err := s.settings.SetTheme(ctx, theme)
	if err != nil {
		return saved, err
	}
	s.host.SetTheme(theme)
	return saved, nil
}

func (s *SettingsService) SetLanguage(
	ctx context.Context,
	language domain.Language,
) (domain.Settings, error) {
	saved, err := s.settings.SetLanguage(ctx, language)
	if err != nil {
		return saved, err
	}
	s.host.SetLanguage(language)
	return saved, nil
}

func (s *SettingsService) SetLayout(
	ctx context.Context,
	patch settings.LayoutPatch,
) (domain.Settings, error) {
	return s.settings.SetLayout(ctx, patch)
}

func (s *SettingsService) SetEditor(
	ctx context.Context,
	patch settings.EditorPatch,
) (domain.Settings, error) {
	return s.settings.SetEditor(ctx, patch)
}

func (s *SettingsService) SetReopenWorkspace(
	ctx context.Context,
	reopen bool,
) (domain.Settings, error) {
	return s.settings.SetReopenWorkspace(ctx, reopen)
}

func (s *SettingsService) SetRestoreTabs(
	ctx context.Context,
	restore bool,
) (domain.Settings, error) {
	return s.settings.SetRestoreTabs(ctx, restore)
}

func (s *SettingsService) SetCaptureFilters(
	ctx context.Context,
	filters domain.CaptureFilters,
) (domain.Settings, error) {
	return s.settings.SetCaptureFilters(ctx, filters)
}

// SetExtension keeps the two switches of the extension section. The window hands them to the
// extension beside this call.
func (s *SettingsService) SetExtension(
	ctx context.Context,
	patch settings.ExtensionPatch,
) (domain.Settings, error) {
	return s.settings.SetExtension(ctx, patch)
}

// SetOnboarding is what the window's welcome has been told: whether the overview has been offered,
// whether the checklist was put away, and the steps walked. The window marks a step as the person
// walks it; Go keeps the answer and announces the new settings.
func (s *SettingsService) SetOnboarding(
	ctx context.Context,
	patch settings.OnboardingPatch,
) (domain.Settings, error) {
	return s.settings.SetOnboarding(ctx, patch)
}

// SetShortcut writes down the combination one command wears; an empty one restores the combination
// the command comes with.
func (s *SettingsService) SetShortcut(
	ctx context.Context,
	id, combo string,
) (domain.Settings, error) {
	return s.settings.SetShortcut(ctx, id, combo)
}

// ResetShortcuts gives every command back the combination it comes with.
func (s *SettingsService) ResetShortcuts(ctx context.Context) (domain.Settings, error) {
	return s.settings.ResetShortcuts(ctx)
}

// SetSyncScopes is what of this device is allowed to leave it. The engine reads the answer before
// its next push, so a change lands in the round that follows rather than in one already on its way.
func (s *SettingsService) SetSyncScopes(
	ctx context.Context,
	patch settings.SyncScopesPatch,
) (domain.Settings, error) {
	return s.settings.SetSyncScopes(ctx, patch)
}

// SetMCP changes the MCP server and hands the server itself to the save: the row is written, the
// door opened or closed, and only then are the windows told. A section that showed one thing while
// the port listened for another would be the worst way to be wrong about who can call this app.
func (s *SettingsService) SetMCP(
	ctx context.Context,
	patch settings.MCPPatch,
) (domain.Settings, error) {
	return s.settings.SetMCP(ctx, patch, s.mcp.Apply)
}

func (s *SettingsService) SetRetention(
	ctx context.Context,
	retention domain.Retention,
) (domain.Settings, error) {
	return s.settings.SetRetention(ctx, retention)
}

func (s *SettingsService) SetUpdateCheck(
	ctx context.Context,
	auto bool,
) (domain.Settings, error) {
	return s.settings.SetUpdateCheck(ctx, auto)
}

func (s *SettingsService) SetUpdateChannel(
	ctx context.Context,
	channel domain.UpdateChannel,
) (domain.Settings, error) {
	return s.settings.SetUpdateChannel(ctx, channel)
}

func (s *SettingsService) SetProxy(
	ctx context.Context,
	patch settings.ProxyPatch,
) (domain.Settings, error) {
	return s.settings.SetProxy(ctx, patch)
}

func (s *SettingsService) SetRequests(
	ctx context.Context,
	patch settings.RequestsPatch,
) (domain.Settings, error) {
	return s.settings.SetRequests(ctx, patch)
}

func (s *SettingsService) SetCertificates(
	ctx context.Context,
	patch settings.CertificatesPatch,
) (domain.Settings, error) {
	return s.settings.SetCertificates(ctx, patch)
}

func (s *SettingsService) AddRootCertificate(
	ctx context.Context,
	title string,
) (domain.Settings, error) {
	path, err := s.certificates.PickRootCertificate(title)
	if err != nil {
		return domain.Settings{}, err
	}
	// A closed dialog is not a change: an empty answer would be painted over every field the pane
	// holds, so the window is given the settings it already shows.
	if path == "" {
		return s.settings.Snapshot(ctx)
	}
	contents, err := s.files.Read(path)
	if err != nil {
		return domain.Settings{}, err
	}
	// The file's own name is what the row falls back to when the certificate names no subject.
	return s.settings.AddRootCertificate(ctx, filepath.Base(path), string(contents))
}

func (s *SettingsService) RemoveRootCertificate(
	ctx context.Context,
	name string,
) (domain.Settings, error) {
	return s.settings.RemoveRootCertificate(ctx, name)
}

func (s *SettingsService) RevealProxyPassword(ctx context.Context) (string, error) {
	return s.settings.RevealProxyPassword(ctx)
}

func (s *SettingsService) SetAppearance(
	ctx context.Context,
	patch settings.AppearancePatch,
) (domain.Settings, error) {
	saved, err := s.settings.SetAppearance(ctx, patch)
	if err != nil {
		return saved, err
	}
	// The windows opened from now on are told the choice on their own address; the ones already open
	// heard it as the broadcast that answered the save.
	s.host.SetAppearance(saved.Appearance)
	return saved, nil
}

func (s *SettingsService) ParseProxyAddress(
	ctx context.Context,
	raw string,
) (settings.ProxyPatch, error) {
	return s.settings.ParseProxyAddress(raw)
}

func (s *SettingsService) ProxyEnvironment(ctx context.Context) []domain.EnvEntry {
	return s.engine.ProxyEnvironment()
}

func (s *SettingsService) TestProxy(ctx context.Context) domain.ProxyProbe {
	return s.engine.Probe(ctx)
}
