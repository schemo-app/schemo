package settings

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"schemo/internal/domain"
)

type fakeStore map[string]string

func (f fakeStore) Settings(context.Context) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range f {
		out[k] = v
	}
	return out, nil
}

func (f fakeStore) SaveSetting(_ context.Context, key, value string) error {
	f[key] = value
	return nil
}

// SyncScopes reads the row the way the real store does: what was written down, or what the scopes
// come with.
func (f fakeStore) SyncScopes(context.Context) (domain.SyncScopes, error) {
	held := domain.DefaultSyncScopes()
	raw := f[domain.SettingSyncScopes]
	if raw == "" {
		return held, nil
	}
	if err := json.Unmarshal([]byte(raw), &held); err != nil {
		return domain.DefaultSyncScopes(), nil
	}
	return held, nil
}

type published struct {
	topic   string
	payload any
}

type fakeNotifier struct {
	seen []published
}

func (f *fakeNotifier) Publish(topic string, payload any) {
	f.seen = append(f.seen, published{topic: topic, payload: payload})
}

// fakeOutbound is the engine's side of the settings: what it was last told, so a test can see that
// a change reached it without an engine, a window or a socket.
type fakeOutbound struct {
	net  domain.Network
	seen int
}

func (f *fakeOutbound) Apply(net domain.Network) {
	f.net = net
	f.seen++
}

func newUseCase() (*UseCase, fakeStore, *fakeNotifier) {
	uc, store, notifier, _ := newUseCaseWithOutbound()
	return uc, store, notifier
}

func newUseCaseWithOutbound() (*UseCase, fakeStore, *fakeNotifier, *fakeOutbound) {
	store := fakeStore{}
	notifier := &fakeNotifier{}
	outbound := &fakeOutbound{}
	return NewUseCase(store, notifier, outbound), store, notifier, outbound
}

func TestSnapshotFallsBackToDefaults(t *testing.T) {
	uc, _, _ := newUseCase()

	got, err := uc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// `DeepEqual` rather than `==`: the capture rules are a list, and a list is not comparable.
	if !reflect.DeepEqual(got, domain.DefaultSettings()) {
		t.Errorf("Snapshot = %+v, want the defaults", got)
	}
}

// A stored value from a build that no longer exists — or a hand-edited database — must not leave
// the window without a theme or a panel width.
func TestSnapshotIgnoresUnreadableValues(t *testing.T) {
	uc, store, _ := newUseCase()
	store[domain.SettingTheme] = "solarized"
	store[domain.SettingLanguage] = "../en"
	store[domain.SettingInspectorWidth] = "wide"
	store[domain.SettingSideWidth] = "-40"
	store[domain.SettingHistoryRetention] = "forever-ish"
	store[domain.SettingInspectorOpen] = "maybe"

	got, err := uc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	want := domain.DefaultSettings()
	if got.Theme != want.Theme {
		t.Errorf("theme = %q, want the default", got.Theme)
	}
	if got.Language != want.Language {
		t.Errorf("language = %q, want the default", got.Language)
	}
	if got.InspectorWidth != want.InspectorWidth {
		t.Errorf("inspectorWidth = %d, want the default", got.InspectorWidth)
	}
	if got.SideWidth != domain.MinSideWidth {
		t.Errorf("sideWidth = %d, want it clamped to the minimum", got.SideWidth)
	}
	if got.HistoryRetention != want.HistoryRetention {
		t.Errorf("retention = %q, want the default", got.HistoryRetention)
	}
	if got.InspectorOpen {
		t.Error("inspectorOpen = true, want the default (closed)")
	}
}

// The sidebar's range is its own: a width that was inside the panel range but outside the sidebar's
// — stored by an older build or a hand-edited row — reads back as a sidebar.
func TestSnapshotClampsTheSidebarToItsOwnRange(t *testing.T) {
	uc, store, _ := newUseCase()
	store[domain.SettingSideWidth] = "500"

	got, err := uc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.SideWidth != domain.MaxSideWidth {
		t.Errorf("sideWidth = %d, want the sidebar's own maximum", got.SideWidth)
	}
}

func TestSetThemeStoresAndPublishes(t *testing.T) {
	uc, store, notifier := newUseCase()

	got, err := uc.SetTheme(context.Background(), domain.ThemeDark)
	if err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if got.Theme != domain.ThemeDark {
		t.Errorf("theme = %q, want dark back", got.Theme)
	}
	if store[domain.SettingTheme] != "dark" {
		t.Errorf("stored theme = %q, want dark", store[domain.SettingTheme])
	}

	// The event carries the choice: "system" is a question only a window can answer.
	if len(notifier.seen) != 1 {
		t.Fatalf("published %d events, want one", len(notifier.seen))
	}
	event := notifier.seen[0]
	if event.topic != TopicThemeChanged {
		t.Errorf("topic = %q, want %q", event.topic, TopicThemeChanged)
	}
	changed, ok := event.payload.(ThemeChanged)
	if !ok || changed.Theme != domain.ThemeDark {
		t.Errorf("payload = %#v, want a ThemeChanged carrying dark", event.payload)
	}

	if _, err := uc.SetTheme(context.Background(),
		"solarized"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("unknown theme = %v, want ErrNotAllowed", err)
	}
	if len(notifier.seen) != 1 {
		t.Error("a refused theme was published anyway")
	}
}

func TestSetLanguageStoresAndPublishes(t *testing.T) {
	uc, store, notifier := newUseCase()

	got, err := uc.SetLanguage(context.Background(), "ru")
	if err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if got.Language != "ru" {
		t.Errorf("language = %q, want ru back", got.Language)
	}
	if store[domain.SettingLanguage] != "ru" {
		t.Errorf("stored language = %q, want ru", store[domain.SettingLanguage])
	}

	// The event carries the choice: "system" is a question only the webview can answer.
	if len(notifier.seen) != 1 {
		t.Fatalf("published %d events, want one", len(notifier.seen))
	}
	event := notifier.seen[0]
	if event.topic != TopicLanguageChanged {
		t.Errorf("topic = %q, want %q", event.topic, TopicLanguageChanged)
	}
	changed, ok := event.payload.(LanguageChanged)
	if !ok || changed.Language != "ru" {
		t.Errorf("payload = %#v, want a LanguageChanged carrying ru", event.payload)
	}

	if _, err := uc.SetLanguage(context.Background(), "../en"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("unknown language = %v, want ErrNotAllowed", err)
	}
	if len(notifier.seen) != 1 {
		t.Error("a refused language was published anyway")
	}
}

func TestLanguageCataloguesDoNotRequireBackendChanges(t *testing.T) {
	for _, choice := range []string{"de", "pt-BR", "es-419", domain.LanguageSystem} {
		t.Run(choice, func(t *testing.T) {
			uc, store, _ := newUseCase()
			saved, err := uc.SetLanguage(t.Context(), choice)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Language != choice || store[domain.SettingLanguage] != choice {
				t.Fatalf("language was not saved: %+v", saved)
			}
		})
	}
	for _, choice := range []string{"", "../en", "en_US", "und", "invalid!"} {
		uc, _, _ := newUseCase()
		if _, err := uc.SetLanguage(t.Context(), choice); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("SetLanguage(%q) error = %v", choice, err)
		}
	}
}

func TestSetLayoutWritesOnlyWhatItIsGiven(t *testing.T) {
	uc, store, _ := newUseCase()
	ctx := context.Background()

	if _, err := uc.SetLayout(ctx, LayoutPatch{SideWidth: ptr(1000)}); err != nil {
		t.Fatalf("SetLayout: %v", err)
	}
	if store[domain.SettingSideWidth] != "360" {
		t.Errorf("sideWidth = %q, want it clamped to the maximum", store[domain.SettingSideWidth])
	}
	// Untouched settings stay absent rather than being written with a default.
	if _, ok := store[domain.SettingInspectorWidth]; ok {
		t.Error("SetLayout wrote a width it was not given")
	}

	open := true
	got, err := uc.SetLayout(ctx, LayoutPatch{InspectorOpen: &open, InspectorWidth: ptr(320)})
	if err != nil {
		t.Fatalf("SetLayout: %v", err)
	}
	if !got.InspectorOpen || got.InspectorWidth != 320 {
		t.Errorf("settings = %+v, want the inspector open at 320", got)
	}
	if got.SideWidth != domain.MaxSideWidth {
		t.Errorf("sideWidth = %d, want the value saved before", got.SideWidth)
	}
}

// A dragged request pane is kept inside its limits, and zero is the one value below them that is
// kept as it is: it is the pane told to fit what it holds again.
func TestSetLayoutClampsTheRequestHeightButKeepsFit(t *testing.T) {
	uc, _, _ := newUseCase()
	ctx := context.Background()

	got, err := uc.SetLayout(ctx, LayoutPatch{RequestHeight: ptr(9000)})
	if err != nil {
		t.Fatalf("SetLayout: %v", err)
	}
	if got.RequestHeight != domain.MaxRequestHeight {
		t.Errorf("requestHeight = %d, want it clamped to the maximum", got.RequestHeight)
	}

	got, err = uc.SetLayout(ctx, LayoutPatch{RequestHeight: ptr(0)})
	if err != nil {
		t.Fatalf("SetLayout: %v", err)
	}
	if got.RequestHeight != 0 {
		t.Errorf("requestHeight = %d, want zero kept as fit", got.RequestHeight)
	}
}

// The one field of a layout patch that is a choice and not a measurement travels: the side of the
// list is clicked in the status bar or the settings window, and a drag publishes nothing.
func TestSetLayoutPublishesASideAndNotADrag(t *testing.T) {
	uc, _, notifier := newUseCase()
	ctx := context.Background()

	drag := LayoutPatch{SideWidth: ptr(320), InspectorOpen: ptr(true)}
	if _, err := uc.SetLayout(ctx, drag); err != nil {
		t.Fatalf("SetLayout: %v", err)
	}
	if len(notifier.seen) != 0 {
		t.Errorf("a drag published %+v, want silence", notifier.seen)
	}

	side := domain.ListSideRight
	if _, err := uc.SetLayout(ctx, LayoutPatch{ListSide: &side}); err != nil {
		t.Fatalf("SetLayout: %v", err)
	}
	if len(notifier.seen) != 1 || notifier.seen[0].topic != TopicChanged {
		t.Fatalf("published %+v, want the snapshot on %s", notifier.seen, TopicChanged)
	}
	if changed, ok := notifier.seen[0].payload.(domain.Settings); !ok ||
		changed.ListSide != domain.ListSideRight {
		t.Errorf("payload = %+v, want the side as it now is", notifier.seen[0].payload)
	}
}

// The side of the list is a choice, not a measurement: a value the window cannot lay out is refused
// rather than clamped, because "hidden-ish" would leave the panel somewhere nobody asked for.
func TestSetLayoutRefusesAnUnknownListSide(t *testing.T) {
	uc, store, _ := newUseCase()
	ctx := context.Background()

	side := domain.ListSideRight
	got, err := uc.SetLayout(ctx, LayoutPatch{ListSide: &side})
	if err != nil {
		t.Fatalf("SetLayout: %v", err)
	}
	if got.ListSide != domain.ListSideRight {
		t.Errorf("listSide = %q, want right back", got.ListSide)
	}
	if store[domain.SettingListSide] != "right" {
		t.Errorf("stored listSide = %q, want right", store[domain.SettingListSide])
	}

	bad := domain.ListSide("middle")
	_, err = uc.SetLayout(ctx, LayoutPatch{ListSide: &bad})
	if !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("unknown side = %v, want ErrNotAllowed", err)
	}
	if code := domain.CodeOf(err); code != domain.CodeUnknownListSide {
		t.Errorf("code = %q, want %q", code, domain.CodeUnknownListSide)
	}
	if store[domain.SettingListSide] != "right" {
		t.Errorf("stored listSide = %q, want the refusal to leave it alone",
			store[domain.SettingListSide])
	}
}

// Hiding the list is a choice of the user's, and showing it again undoes that choice rather than
// making a new one: the side it was on comes back with it.
func TestSetLayoutRemembersTheSideTheListWasPutAwayFrom(t *testing.T) {
	uc, store, _ := newUseCase()
	ctx := context.Background()

	right := domain.ListSideRight
	if _, err := uc.SetLayout(ctx, LayoutPatch{ListSide: &right}); err != nil {
		t.Fatalf("SetLayout: %v", err)
	}
	hidden := domain.ListSideHidden
	got, err := uc.SetLayout(ctx, LayoutPatch{ListSide: &hidden})
	if err != nil {
		t.Fatalf("SetLayout: %v", err)
	}
	if got.ListSide != domain.ListSideHidden {
		t.Errorf("listSide = %q, want hidden", got.ListSide)
	}
	if got.ListSideLast != domain.ListSideRight {
		t.Errorf("listSideLast = %q, want the side it was put away from", got.ListSideLast)
	}
	if store[domain.SettingListSideLast] != "right" {
		t.Errorf("stored listSideLast = %q, want right", store[domain.SettingListSideLast])
	}
}

// The side read back is the one a window was last left on, and a stored «hidden» is not a side at
// all: a list put away before the app closed still knows which edge to come back to.
func TestASnapshotReadsTheSideTheListWasLeftOn(t *testing.T) {
	uc, store, _ := newUseCase()
	ctx := context.Background()
	store[domain.SettingListSide] = "hidden"
	store[domain.SettingListSideLast] = "right"

	got, err := uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.ListSide != domain.ListSideHidden {
		t.Errorf("listSide = %q, want hidden", got.ListSide)
	}
	if got.ListSideLast != domain.ListSideRight {
		t.Errorf("listSideLast = %q, want the side it was put away from", got.ListSideLast)
	}

	// A row from a newer build, or a hand-edited database, is not a side the window can lay out.
	store[domain.SettingListSideLast] = "middle"
	got, err = uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.ListSideLast != domain.ListSideLeft {
		t.Errorf("listSideLast = %q, want the default", got.ListSideLast)
	}
}

func TestSetRetentionValidates(t *testing.T) {
	uc, store, notifier := newUseCase()
	ctx := context.Background()

	if _, err := uc.SetRetention(ctx, domain.RetainWeek); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	if store[domain.SettingHistoryRetention] != "7" {
		t.Errorf("stored retention = %q, want 7", store[domain.SettingHistoryRetention])
	}
	// The main window draws how history is kept on the browser card, and the settings window is the
	// one that changes it: the snapshot has to travel, or that card goes on saying the old window.
	if len(notifier.seen) != 1 || notifier.seen[0].topic != TopicChanged {
		t.Fatalf("published %+v, want the snapshot on %s", notifier.seen, TopicChanged)
	}
	if changed, ok := notifier.seen[0].payload.(domain.Settings); !ok ||
		changed.HistoryRetention != domain.RetainWeek {
		t.Errorf("payload = %+v, want the settings as they now are", notifier.seen[0].payload)
	}

	if _, err := uc.SetRetention(ctx, "whenever"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("unknown retention = %v, want ErrNotAllowed", err)
	}
	if len(notifier.seen) != 1 {
		t.Errorf("a refusal published %+v, want silence", notifier.seen[1:])
	}
}

// The history and the strip ask for one value each, and a row nobody wrote reads as the default.
func TestRetentionAndRestoreTabsAreTheStoredValuesOrTheDefaults(t *testing.T) {
	uc, store, _ := newUseCase()
	ctx := context.Background()

	if got, err := uc.Retention(ctx); err != nil || got != domain.DefaultSettings().HistoryRetention {
		t.Errorf("Retention = %q, %v, want the default", got, err)
	}
	if got, err := uc.RestoreTabs(ctx); err != nil || got != domain.DefaultSettings().RestoreTabs {
		t.Errorf("RestoreTabs = %v, %v, want the default", got, err)
	}

	store[domain.SettingHistoryRetention] = string(domain.RetainMonth)
	store[domain.SettingRestoreTabs] = "false"
	if got, _ := uc.Retention(ctx); got != domain.RetainMonth {
		t.Errorf("Retention = %q, want the stored month", got)
	}
	if got, _ := uc.RestoreTabs(ctx); got {
		t.Error("RestoreTabs = true, want the stored off")
	}
}

// The three whose absence has to read as "on": a database written before they existed has no row
// for them; an editor that stopped wrapping unasked is a change nobody asked for.
func TestSnapshotKeepsDefaultsForUnreadableSwitches(t *testing.T) {
	uc, store, _ := newUseCase()
	store[domain.SettingWrapLines] = "not a bool"
	store[domain.SettingLineNumbers] = ""
	store[domain.SettingReopenWorkspace] = "0"

	got, err := uc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !got.WrapLines || !got.LineNumbers {
		t.Errorf("wrap = %v, numbers = %v; want both left on", got.WrapLines, got.LineNumbers)
	}
	// `strconv.ParseBool` reads "0" as false, and false is a value somebody may have chosen: only an
	// unreadable one falls back.
	if got.ReopenWorkspace {
		t.Error("reopenWorkspace = true, want the stored false to be honoured")
	}
}

func TestSetEditorWritesOnlyWhatItIsGiven(t *testing.T) {
	uc, store, notifier := newUseCase()
	ctx := context.Background()

	if _, err := uc.SetEditor(ctx, EditorPatch{LineNumbers: ptr(false)}); err != nil {
		t.Fatalf("SetEditor: %v", err)
	}
	if store[domain.SettingLineNumbers] != "false" {
		t.Errorf("stored lineNumbers = %q, want false", store[domain.SettingLineNumbers])
	}
	if _, written := store[domain.SettingWrapLines]; written {
		t.Error("the wrap nobody mentioned was written")
	}
	if len(notifier.seen) != 1 || notifier.seen[0].topic != TopicChanged {
		t.Errorf("published %+v, want the snapshot on %s", notifier.seen, TopicChanged)
	}

	saved, err := uc.SetEditor(ctx, EditorPatch{WrapLines: ptr(false)})
	if err != nil {
		t.Fatalf("SetEditor: %v", err)
	}
	if saved.WrapLines || saved.LineNumbers {
		t.Errorf("editor = %+v, want the line numbers left off and the wrap off", saved)
	}
}

// An indent the editors cannot type is refused at the door rather than stored for the window to
// fall back from: the choice is three names, and a fourth is a caller that is wrong.
func TestSetEditorRefusesAnIndentItCannotType(t *testing.T) {
	uc, store, _ := newUseCase()
	ctx := context.Background()
	wrong := domain.Indent("eight")

	if _, err := uc.SetEditor(ctx, EditorPatch{Indent: &wrong}); err == nil {
		t.Fatal("an unknown indent was accepted")
	}
	if _, written := store[domain.SettingIndent]; written {
		t.Error("the refused indent was written")
	}

	saved, err := uc.SetEditor(ctx, EditorPatch{Indent: ptr(domain.IndentTab)})
	if err != nil {
		t.Fatalf("SetEditor: %v", err)
	}
	if saved.Indent != domain.IndentTab {
		t.Errorf("indent = %q, want the tab", saved.Indent)
	}
}

func TestSetReopenWorkspaceStoresTheChoice(t *testing.T) {
	uc, store, notifier := newUseCase()
	ctx := context.Background()

	saved, err := uc.SetReopenWorkspace(ctx, false)
	if err != nil {
		t.Fatalf("SetReopenWorkspace: %v", err)
	}
	if store[domain.SettingReopenWorkspace] != "false" || saved.ReopenWorkspace {
		t.Errorf("stored %q and answered %v, want false",
			store[domain.SettingReopenWorkspace], saved.ReopenWorkspace)
	}
	// The pointer is not touched: turning the preference back on has to return the user to the space
	// they were last in, not to the one that was on screen when they turned it off.
	if _, written := store[domain.SettingActiveWorkspace]; written {
		t.Error("the active workspace was rewritten, want the pointer left alone")
	}
	if len(notifier.seen) != 1 || notifier.seen[0].topic != TopicChanged {
		t.Errorf("published %+v, want the snapshot on %s", notifier.seen, TopicChanged)
	}
}

// Theme is what the window reads before it exists; it has to answer even on an empty database.
func TestThemeAnswersOnAFreshDatabase(t *testing.T) {
	uc, _, _ := newUseCase()

	got, err := uc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Theme: %v", err)
	}
	if got.Theme != domain.ThemeSystem {
		t.Errorf("Theme = %q, want the default", got.Theme)
	}
}

// Language answers on the same terms, and its default is the one the settings screen offers: follow
// the system, not a language picked for the user.
func TestLanguageAnswersOnAFreshDatabase(t *testing.T) {
	uc, _, _ := newUseCase()

	got, err := uc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Language: %v", err)
	}
	if got.Language != domain.LanguageSystem {
		t.Errorf("Language = %q, want the default", got.Language)
	}
}

func ptr[T any](v T) *T { return &v }

// The capture rules are stored as they are given and read back whole: the extension filters by
// them, and a rule that came back different is a rule nobody set.
func TestExtensionSettingsRoundTrip(t *testing.T) {
	uc, _, _ := newUseCase()
	ctx := context.Background()

	// A fresh installation does not come up on its own when both sides are running — that is a
	// choice a person makes — and does arm the addresses capture was armed on again.
	fresh, err := uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if fresh.Extension.AutoConnect || !fresh.Extension.RememberTabs {
		t.Errorf("fresh extension = %+v, want no auto-connect and tabs remembered",
			fresh.Extension)
	}

	auto := true
	remember := false
	saved, err := uc.SetExtension(ctx, ExtensionPatch{AutoConnect: &auto, RememberTabs: &remember})
	if err != nil {
		t.Fatalf("SetExtension: %v", err)
	}
	if !saved.Extension.AutoConnect || saved.Extension.RememberTabs {
		t.Errorf("saved extension = %+v, want what was given", saved.Extension)
	}

	// And they survive the read that the window makes on its own.
	again, err := uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if again.Extension != saved.Extension {
		t.Errorf("read back %+v, want %+v", again.Extension, saved.Extension)
	}

	// A patch that says nothing about a switch leaves it where it stands.
	only := false
	part, err := uc.SetExtension(ctx, ExtensionPatch{RememberTabs: &only})
	if err != nil {
		t.Fatalf("SetExtension: %v", err)
	}
	if !part.Extension.AutoConnect || part.Extension.RememberTabs {
		t.Errorf("extension = %+v, want the untouched switch kept", part.Extension)
	}
}

func TestShortcutsKeepOnlyWhatWasChanged(t *testing.T) {
	uc, _, _ := newUseCase()
	ctx := context.Background()

	// Nothing changed is nothing kept: every command wears the combination it comes with.
	fresh, err := uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(fresh.Shortcuts) != 0 {
		t.Errorf("fresh shortcuts = %v, want nothing changed", fresh.Shortcuts)
	}

	saved, err := uc.SetShortcut(ctx, "palette", "mod+alt+P")
	if err != nil {
		t.Fatalf("SetShortcut: %v", err)
	}
	if saved.Shortcuts["palette"] != "mod+alt+P" {
		t.Errorf("shortcuts = %v, want the one that was set", saved.Shortcuts)
	}

	// An empty combination takes the command back to the one it comes with, and a command somebody
	// unassigned wears ShortcutNone instead — the two are different answers.
	if _, err := uc.SetShortcut(ctx, "palette", ""); err != nil {
		t.Fatalf("SetShortcut: %v", err)
	}
	saved, err = uc.SetShortcut(ctx, "run", domain.ShortcutNone)
	if err != nil {
		t.Fatalf("SetShortcut: %v", err)
	}
	if _, held := saved.Shortcuts["palette"]; held {
		t.Errorf("shortcuts = %v, want the command back at its default", saved.Shortcuts)
	}
	if saved.Shortcuts["run"] != domain.ShortcutNone {
		t.Errorf("shortcuts = %v, want the unassigned one kept", saved.Shortcuts)
	}

	// And a combination nothing can carry out is refused rather than kept.
	_, err = uc.SetShortcut(ctx, "palette", "mod+mod+K")
	if domain.CodeOf(err) != domain.CodeUnknownShortcut {
		t.Errorf("err = %v, want a refusal", err)
	}

	// "Reset all" empties the lot.
	reset, err := uc.ResetShortcuts(ctx)
	if err != nil {
		t.Fatalf("ResetShortcuts: %v", err)
	}
	if len(reset.Shortcuts) != 0 {
		t.Errorf("shortcuts = %v after the reset, want none changed", reset.Shortcuts)
	}
}

func TestSyncScopesRoundTrip(t *testing.T) {
	uc, store, _ := newUseCase()
	ctx := context.Background()

	// A fresh installation lets both out: that is what the app has always done, and a switch nobody
	// touched keeps it.
	fresh, err := uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !fresh.SyncScopes.Runs || !fresh.SyncScopes.Secrets {
		t.Errorf("fresh scopes = %+v, want both on", fresh.SyncScopes)
	}

	off := false
	saved, err := uc.SetSyncScopes(ctx, SyncScopesPatch{Secrets: &off})
	if err != nil {
		t.Fatalf("SetSyncScopes: %v", err)
	}
	if !saved.SyncScopes.Runs || saved.SyncScopes.Secrets {
		t.Errorf("scopes = %+v, want the secret values kept home", saved.SyncScopes)
	}
	if store[domain.SettingSyncScopes] == "" {
		t.Error("nothing was written down")
	}

	// A patch that says nothing about a scope leaves it where it stands.
	again, err := uc.SetSyncScopes(ctx, SyncScopesPatch{Runs: &off})
	if err != nil {
		t.Fatalf("SetSyncScopes: %v", err)
	}
	if again.SyncScopes.Runs || again.SyncScopes.Secrets {
		t.Errorf("scopes = %+v, want the untouched switch kept", again.SyncScopes)
	}
}

// The extension sends its rules with no host list at all when none is picked, and the window draws
// that list row by row: what is kept is an empty list, never the absence of one.
func TestFiltersWithoutHostsAreKeptAsAnEmptyList(t *testing.T) {
	uc, store, _ := newUseCase()
	ctx := context.Background()

	saved, err := uc.SetCaptureFilters(ctx, domain.CaptureFilters{Static: true})
	if err != nil {
		t.Fatalf("SetCaptureFilters: %v", err)
	}
	if saved.CaptureFilters.Hosts == nil {
		t.Error("filters answered with no host list, want an empty one")
	}
	if got := store[domain.SettingCaptureFilters]; !strings.Contains(got, `"hosts":[]`) {
		t.Errorf("stored filters = %s, want the empty list written down", got)
	}
}

func TestCaptureFiltersRoundTrip(t *testing.T) {
	uc, _, _ := newUseCase()
	ctx := context.Background()

	// A fresh installation skips static files, which is what the extension did before anybody could
	// say otherwise.
	fresh, err := uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !fresh.CaptureFilters.Static || fresh.CaptureFilters.Analytics || fresh.CaptureFilters.JSON ||
		len(fresh.CaptureFilters.Hosts) != 0 {
		t.Errorf("fresh filters = %+v, want only static files skipped", fresh.CaptureFilters)
	}

	given := domain.CaptureFilters{
		Hosts:     []string{"api.example.com", "example.com"},
		Static:    true,
		Analytics: true,
		JSON:      true,
	}
	saved, err := uc.SetCaptureFilters(ctx, given)
	if err != nil {
		t.Fatalf("SetCaptureFilters: %v", err)
	}
	if !reflect.DeepEqual(saved.CaptureFilters, given) {
		t.Errorf("saved filters = %+v, want what was given", saved.CaptureFilters)
	}

	// And they survive the read that the window makes on its own.
	again, err := uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !reflect.DeepEqual(again.CaptureFilters, given) {
		t.Errorf("filters read back = %+v, want what was written", again.CaptureFilters)
	}
}

// The engine is what a password travels to, and a window is not: what the caller gets back says one
// is kept and never what it is.
func TestSetProxyStoresAndApplies(t *testing.T) {
	uc, store, notifier, outbound := newUseCaseWithOutbound()

	saved, err := uc.SetProxy(context.Background(), ProxyPatch{
		Mode:     ptr(domain.ProxyManual),
		Host:     ptr("proxy.corp"),
		Port:     ptr(8080),
		User:     ptr("maria"),
		Password: ptr("s3cret"),
		Bypass:   ptr([]string{"api.internal", "  "}),
	})
	if err != nil {
		t.Fatalf("SetProxy: %v", err)
	}

	if outbound.net.Proxy.Mode != domain.ProxyManual || outbound.net.Proxy.Password != "s3cret" {
		t.Errorf("the engine was given %+v, want the stored proxy with its password", outbound.net.Proxy)
	}
	if len(outbound.net.Proxy.Bypass) != 1 || outbound.net.Proxy.Bypass[0] != "api.internal" {
		t.Errorf("bypass = %v, want the blank entry dropped", outbound.net.Proxy.Bypass)
	}
	if saved.Proxy.Password != "" || !saved.Proxy.HasPassword {
		t.Errorf("snapshot proxy = %+v, want hasPassword and no value", saved.Proxy)
	}
	if store[domain.SettingProxy] == "" {
		t.Error("nothing was stored")
	}
	if len(notifier.seen) != 1 || notifier.seen[0].topic != TopicChanged {
		t.Errorf("published %+v, want one settings:changed", notifier.seen)
	}
}

// A password already stored is hidden the same way, whatever wrote it: the row is plain text in the
// database, and that is the only place the value is allowed to sit.
func TestSnapshotHidesTheStoredProxyPassword(t *testing.T) {
	uc, store, _ := newUseCase()
	store[domain.SettingProxy] = `{"mode":"manual","host":"proxy.corp","port":8080,` +
		`"user":"maria","password":"s3cret","bypass":[]}`

	got, err := uc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.Proxy.Password != "" || !got.Proxy.HasPassword {
		t.Errorf("snapshot proxy = %+v, want no value and hasPassword", got.Proxy)
	}
	if got.Proxy.Host != "proxy.corp" || got.Proxy.Port != 8080 {
		t.Errorf("the rest of the proxy was lost: %+v", got.Proxy)
	}
}

// Reveal is the eye, and the only call that hands the value over — nothing on a fresh database.
func TestRevealProxyPassword(t *testing.T) {
	uc, _, _, _ := newUseCaseWithOutbound()
	ctx := context.Background()

	if held, err := uc.RevealProxyPassword(ctx); err != nil || held != "" {
		t.Errorf("RevealProxyPassword = %q, %v; want nothing on a fresh database", held, err)
	}
	if _, err := uc.SetProxy(ctx, ProxyPatch{Password: ptr("s3cret")}); err != nil {
		t.Fatalf("SetProxy: %v", err)
	}
	held, err := uc.RevealProxyPassword(ctx)
	if err != nil || held != "s3cret" {
		t.Errorf("RevealProxyPassword = %q, %v; want the stored value", held, err)
	}
}

// An address the engine could not dial is refused where it is entered: a mode this build does not
// know, a URL where a host belongs, and a port no socket has.
func TestSetProxyRefusesABadAddress(t *testing.T) {
	uc, store, _, _ := newUseCaseWithOutbound()
	ctx := context.Background()

	refused := []ProxyPatch{
		{Mode: ptr(domain.ProxyMode("auto"))},
		{Mode: ptr(domain.ProxyManual), Host: ptr("http://proxy.corp")},
		{Mode: ptr(domain.ProxyManual), Port: ptr(70000)},
	}
	for _, patch := range refused {
		if _, err := uc.SetProxy(ctx, patch); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("%+v was accepted: %v", patch, err)
		}
	}
	if store[domain.SettingProxy] != "" {
		t.Error("a refused proxy was stored anyway")
	}
}

// Both switches are what the engine is told and what the window reads back.
func TestSetCertificatesApplies(t *testing.T) {
	uc, _, _, outbound := newUseCaseWithOutbound()

	saved, err := uc.SetCertificates(context.Background(), CertificatesPatch{
		Verify:         ptr(false),
		TrustLocalhost: ptr(true),
	})
	if err != nil {
		t.Fatalf("SetCertificates: %v", err)
	}
	if outbound.net.Certificates.Verify || !outbound.net.Certificates.TrustLocalhost {
		t.Errorf("the engine was given %+v, want what was set", outbound.net.Certificates)
	}
	if saved.Certificates.Verify {
		t.Errorf("snapshot certificates = %+v, want verification off", saved.Certificates)
	}
}

// A switch is not a rewrite: the authorities somebody added stay where they are when a switch
// beside them moves, and a snapshot never hands nil to a window that draws a list from it.
func TestASwitchKeepsTheAddedAuthorities(t *testing.T) {
	uc, _, _, outbound := newUseCaseWithOutbound()
	if _, err := uc.AddRootCertificate(context.Background(), "corp.pem",
		selfSigned(t, "Schemo Test CA")); err != nil {
		t.Fatalf("AddRootCertificate: %v", err)
	}

	saved, err := uc.SetCertificates(context.Background(), CertificatesPatch{Verify: ptr(false)})
	if err != nil {
		t.Fatalf("SetCertificates: %v", err)
	}
	if len(saved.Certificates.Roots) != 1 {
		t.Fatalf("roots = %+v, want the one that was added", saved.Certificates.Roots)
	}
	if len(outbound.net.Certificates.Roots) != 1 {
		t.Errorf("the engine was given %+v, want the authority too", outbound.net.Certificates)
	}
}

// The name the window draws is the certificate's own, and the same authority added twice is one
// row: a file chosen again is not a second trust.
func TestAddRootCertificateNamesAndDeduplicates(t *testing.T) {
	uc, _, _, outbound := newUseCaseWithOutbound()

	root := selfSigned(t, "Schemo Test CA")
	saved, err := uc.AddRootCertificate(context.Background(), "corp.pem", root)
	if err != nil {
		t.Fatalf("AddRootCertificate: %v", err)
	}
	if len(saved.Certificates.Roots) != 1 || saved.Certificates.Roots[0].Name != "Schemo Test CA" {
		t.Fatalf("roots = %+v, want one named after its subject", saved.Certificates.Roots)
	}

	again, err := uc.AddRootCertificate(context.Background(), "other.pem", root)
	if err != nil {
		t.Fatalf("AddRootCertificate: %v", err)
	}
	if len(again.Certificates.Roots) != 1 {
		t.Errorf("roots = %+v, want the same one and no second", again.Certificates.Roots)
	}
	if len(outbound.net.Certificates.Roots) != 1 {
		t.Errorf("the engine was given %+v, want one authority", outbound.net.Certificates)
	}
}

// Nothing is trusted that is not a certificate: a file of something else would be a row in the
// window and nothing on the wire.
func TestAddRootCertificateRefusesWhatIsNotOne(t *testing.T) {
	uc, store, _, _ := newUseCaseWithOutbound()

	for _, contents := range []string{"", "not a certificate at all"} {
		_, err := uc.AddRootCertificate(context.Background(), "notes.pem", contents)
		if !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("%q = %v, want ErrNotAllowed", contents, err)
		}
	}
	if store[domain.SettingCertificates] != "" {
		t.Error("a refused file was stored anyway")
	}
}

// Removing is by the name the window draws, and a name that is not there leaves the rest alone.
func TestRemoveRootCertificate(t *testing.T) {
	uc, _, _, outbound := newUseCaseWithOutbound()
	if _, err := uc.AddRootCertificate(context.Background(), "corp.pem",
		selfSigned(t, "Schemo Test CA")); err != nil {
		t.Fatalf("AddRootCertificate: %v", err)
	}

	saved, err := uc.RemoveRootCertificate(context.Background(), "Schemo Test CA")
	if err != nil {
		t.Fatalf("RemoveRootCertificate: %v", err)
	}
	if len(saved.Certificates.Roots) != 0 {
		t.Errorf("roots = %+v, want none", saved.Certificates.Roots)
	}
	if len(outbound.net.Certificates.Roots) != 0 {
		t.Errorf("the engine was given %+v, want none", outbound.net.Certificates)
	}

	// A name that is not there is one already gone, not an error.
	if _, err := uc.RemoveRootCertificate(context.Background(), "Schemo Test CA"); err != nil {
		t.Errorf("removing a name that is gone = %v, want nothing", err)
	}
}

// selfSigned is a certificate that names itself, which is what the row is titled with. It is
// generated rather than pasted: a real one would be a page of base64 nobody can read, and this
// leaves the subject the test asserts on in front of the reader.
func selfSigned(t *testing.T, common string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: common},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// Restore is what the app does before the window exists: the engine is told what was stored, so the
// first request already leaves the way the settings say.
func TestRestoreAppliesWhatIsStored(t *testing.T) {
	uc, store, _, outbound := newUseCaseWithOutbound()
	store[domain.SettingProxy] = `{"mode":"off","bypass":[]}`
	store[domain.SettingCertificates] = `{"verify":false,"trustLocalhost":false}`

	if err := uc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if outbound.net.Proxy.Mode != domain.ProxyOff || outbound.net.Certificates.Verify {
		t.Errorf("the engine was given %+v, want what was stored", outbound.net)
	}
}

// The two numbers are clamped rather than refused: a timeout somebody typed by accident is not a
// reason to leave the screen with an error on it, and a chain longer than the limit is a loop.
func TestSetRequestsClampsAndApplies(t *testing.T) {
	uc, _, _, outbound := newUseCaseWithOutbound()

	saved, err := uc.SetRequests(context.Background(), RequestsPatch{
		Timeout:         ptr(5000),
		FollowRedirects: ptr(false),
		MaxRedirects:    ptr(500),
		UserAgent:       ptr("  Schemo/test  "),
	})
	if err != nil {
		t.Fatalf("SetRequests: %v", err)
	}

	if outbound.net.Requests.Timeout != domain.MaxRequestTimeout {
		t.Errorf("timeout = %d, want it clamped", outbound.net.Requests.Timeout)
	}
	if outbound.net.Requests.MaxRedirects != domain.MaxRequestRedirect {
		t.Errorf("redirects = %d, want them clamped", outbound.net.Requests.MaxRedirects)
	}
	if outbound.net.Requests.FollowRedirects {
		t.Error("the engine was told to follow redirects after being told not to")
	}
	if outbound.net.Requests.UserAgent != "Schemo/test" {
		t.Errorf("user agent = %q, want it trimmed", outbound.net.Requests.UserAgent)
	}
	if saved.Requests.UserAgent != "Schemo/test" {
		t.Errorf("snapshot = %+v, want what was set", saved.Requests)
	}
	// And what was not in the patch is what it was before it.
	if !saved.Requests.Cookies || !saved.Requests.AcceptEncoding {
		t.Errorf("snapshot = %+v, want the other switches untouched", saved.Requests)
	}
}

// A whole address pasted into one field comes apart into the fields it names, and says nothing
// about the ones it does not.
func TestParseProxyAddressReadsTheWholeLine(t *testing.T) {
	uc, _, _ := newUseCase()

	patch, err := uc.ParseProxyAddress("http://a.petrov:s3cret@proxy.corp:3128")
	if err != nil {
		t.Fatalf("ParseProxyAddress: %v", err)
	}
	if patch.Protocol == nil || *patch.Protocol != domain.ProxyHTTP {
		t.Errorf("protocol = %v, want http", patch.Protocol)
	}
	if patch.Host == nil || *patch.Host != "proxy.corp" {
		t.Errorf("host = %v, want the host alone", patch.Host)
	}
	if patch.Port == nil || *patch.Port != 3128 {
		t.Errorf("port = %v, want the port", patch.Port)
	}
	if patch.User == nil || *patch.User != "a.petrov" || patch.Password == nil ||
		*patch.Password != "s3cret" {
		t.Errorf("credentials = %v/%v, want them read apart", patch.User, patch.Password)
	}

	// A line with no scheme at all is a host and a port, which is what somebody pastes most often.
	bare, err := uc.ParseProxyAddress("proxy.corp:8080")
	if err != nil {
		t.Fatalf("ParseProxyAddress: %v", err)
	}
	if bare.Host == nil || *bare.Host != "proxy.corp" || bare.Port == nil || *bare.Port != 8080 {
		t.Errorf("bare address = %+v, want the host and the port", bare)
	}
}

// And a line that is not an address is refused where it is read, before anything is stored.
func TestParseProxyAddressRefusesWhatIsNotOne(t *testing.T) {
	uc, _, _ := newUseCase()

	for _, raw := range []string{"", "   ", "http://"} {
		if _, err := uc.ParseProxyAddress(raw); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("%q = %v, want ErrNotAllowed", raw, err)
		}
	}
	if _, err := uc.ParseProxyAddress("ftp://proxy.corp"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Error("a protocol this build cannot speak was accepted")
	}
}

// Nothing is sent with a protocol version this build cannot ask for.
func TestSetRequestsRefusesAnUnknownVersion(t *testing.T) {
	uc, store, _, _ := newUseCaseWithOutbound()

	_, err := uc.SetRequests(context.Background(), RequestsPatch{
		HTTPVersion: ptr(domain.HTTPVersion("http3")),
	})
	if !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("unknown version = %v, want ErrNotAllowed", err)
	}
	if store[domain.SettingRequests] != "" {
		t.Error("a refused version was stored anyway")
	}
}

// Every window is the same window in the same clothes: the choice is stored, and the broadcast is
// how the others hear about it.
func TestSetAppearanceStoresAndPublishes(t *testing.T) {
	uc, store, notifier := newUseCase()

	saved, err := uc.SetAppearance(context.Background(), AppearancePatch{
		Accent:   ptr(domain.AccentPurple),
		MonoFont: ptr(domain.MonoCascadia),
	})
	if err != nil {
		t.Fatalf("SetAppearance: %v", err)
	}

	if saved.Appearance.Accent != domain.AccentPurple {
		t.Errorf("accent = %q, want what was set", saved.Appearance.Accent)
	}
	if store[domain.SettingAppearance] == "" {
		t.Error("nothing was stored")
	}
	if len(notifier.seen) == 0 {
		t.Error("the other windows were not told")
	}
}

// The code's size is a measurement somebody nudges: a step past either end stops at the end rather
// than leaving the screen with an error on it.
func TestSetAppearanceClampsTheCodeSize(t *testing.T) {
	uc, _, _ := newUseCase()

	small, err := uc.SetAppearance(context.Background(), AppearancePatch{MonoSize: ptr(1)})
	if err != nil {
		t.Fatalf("SetAppearance: %v", err)
	}
	if small.Appearance.MonoSize != domain.MinMonoSize {
		t.Errorf("code size = %d, want %d", small.Appearance.MonoSize, domain.MinMonoSize)
	}

	big, err := uc.SetAppearance(context.Background(), AppearancePatch{MonoSize: ptr(99)})
	if err != nil {
		t.Fatalf("SetAppearance: %v", err)
	}
	if big.Appearance.MonoSize != domain.MaxMonoSize {
		t.Errorf("code size = %d, want %d", big.Appearance.MonoSize, domain.MaxMonoSize)
	}
}

// An accent the stylesheet has no rule for would be drawn as the blue it falls back to, so it is
// refused where it is read.
func TestSetAppearanceRefusesAnUnknownAccent(t *testing.T) {
	uc, store, _ := newUseCase()

	_, err := uc.SetAppearance(context.Background(), AppearancePatch{
		Accent: ptr(domain.Accent("pink")),
	})
	if !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("unknown accent = %v, want ErrNotAllowed", err)
	}
	if store[domain.SettingAppearance] != "" {
		t.Error("a refused accent was stored anyway")
	}
}

// A blob written by a build whose palette had a family this one does not is read into the default
// rather than handed to the stylesheet to look up and not find.
func TestSnapshotReadsAnUnknownAccentAsTheDefault(t *testing.T) {
	uc, store, _ := newUseCase()
	store[domain.SettingAppearance] = `{"accent":"teal","size":"medium",
		"monoFont":"system","monoSize":13}`

	got, err := uc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got.Appearance.Accent != domain.AccentBlue {
		t.Errorf("accent = %q, want the default", got.Appearance.Accent)
	}
}

// An authority reissued under the same subject takes the place of the one it replaces: the row is
// drawn by its name, and two rows of one name would be one row the window cannot take away alone.
func TestAReissuedAuthorityReplacesTheOldOne(t *testing.T) {
	uc, _, _, outbound := newUseCaseWithOutbound()
	if _, err := uc.AddRootCertificate(context.Background(), "corp.pem",
		selfSigned(t, "Schemo Test CA")); err != nil {
		t.Fatalf("AddRootCertificate: %v", err)
	}
	fresh := selfSigned(t, "Schemo Test CA")

	saved, err := uc.AddRootCertificate(context.Background(), "corp-new.pem", fresh)
	if err != nil {
		t.Fatalf("AddRootCertificate: %v", err)
	}
	if len(saved.Certificates.Roots) != 1 {
		t.Fatalf("roots = %+v, want one", saved.Certificates.Roots)
	}
	if saved.Certificates.Roots[0].PEM != fresh {
		t.Error("the row kept the authority it replaces rather than the one that arrived")
	}
	if len(outbound.net.Certificates.Roots) != 1 {
		t.Errorf("the engine was given %+v, want one authority", outbound.net.Certificates)
	}
}

// noApply is the door that takes every answer. These tests are about what is written down; whether
// the server itself can be opened is what TestARefusedDoorLeavesNothingBehind watches.
func noApply(domain.MCPSettings) error { return nil }

// A server that is switched on is one somebody can call, which means it has a token from the first
// moment: a server nobody could reach is not one that was turned on.
func TestTurningTheMCPOnMintsAToken(t *testing.T) {
	uc, store, _ := newUseCase()
	ctx := context.Background()

	fresh, err := uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if fresh.MCP.Enabled || fresh.MCP.Token != "" {
		t.Errorf("fresh MCP = %+v, want it off and without a token", fresh.MCP)
	}

	on := true
	saved, err := uc.SetMCP(ctx, MCPPatch{Enabled: &on}, noApply)
	if err != nil {
		t.Fatalf("SetMCP: %v", err)
	}
	if !saved.MCP.Enabled || !saved.MCP.HasToken {
		t.Fatalf("MCP = %+v, want it on with a token", saved.MCP)
	}
	if saved.MCP.Token != "" {
		t.Errorf("snapshot MCP = %+v, want the fact and not the value", saved.MCP)
	}
	if store[domain.SettingMCP] == "" {
		t.Error("nothing was written down")
	}
	if saved.MCP.Port != domain.DefaultMCPPort {
		t.Errorf("port = %d, want the default one", saved.MCP.Port)
	}

	// Switching it off keeps the token: the door is closed rather than re-keyed.
	off := false
	parked, err := uc.SetMCP(ctx, MCPPatch{Enabled: &off}, noApply)
	if err != nil {
		t.Fatalf("SetMCP: %v", err)
	}
	if !parked.MCP.HasToken {
		t.Error("turning the server off reissued its token")
	}
}

// Reissuing a token is a new value, and the one place it has to arrive is the guard: a snapshot
// never carries it, so a rotation that only wrote the row would leave the server on the old one.
func TestReissuingATokenChangesItAndKeepsTheServerOn(t *testing.T) {
	uc, _, _ := newUseCase()
	ctx := context.Background()

	on := true
	if _, err := uc.SetMCP(ctx, MCPPatch{Enabled: &on}, noApply); err != nil {
		t.Fatalf("SetMCP: %v", err)
	}

	var keyed string
	after, err := uc.RotateMCPToken(ctx, func(token string) { keyed = token })
	if err != nil {
		t.Fatalf("RotateMCPToken: %v", err)
	}
	if !strings.HasPrefix(keyed, domain.MCPTokenPrefix) {
		t.Fatalf("the guard was given %q, want the fresh token", keyed)
	}
	held, err := uc.MCPSettings(ctx)
	if err != nil {
		t.Fatalf("MCPSettings: %v", err)
	}
	if held.Token != keyed {
		t.Error("the guard and the row disagree about the token")
	}
	if after.MCP.Token != "" || !after.MCP.HasToken {
		t.Errorf("snapshot MCP = %+v, want the fact and not the value", after.MCP)
	}
	if !after.MCP.Enabled {
		t.Error("reissuing a token turned the server off")
	}
}

// The value reaches the eye through one call and nowhere else: the row is where it lives, and a
// fresh installation has nothing to hand over.
func TestRevealMCPToken(t *testing.T) {
	uc, _, _ := newUseCase()
	ctx := context.Background()

	if held, err := uc.RevealMCPToken(ctx); err != nil || held != "" {
		t.Errorf("RevealMCPToken = %q, %v; want nothing on a fresh database", held, err)
	}

	on := true
	if _, err := uc.SetMCP(ctx, MCPPatch{Enabled: &on}, noApply); err != nil {
		t.Fatalf("SetMCP: %v", err)
	}
	stored, err := uc.MCPSettings(ctx)
	if err != nil {
		t.Fatalf("MCPSettings: %v", err)
	}
	held, err := uc.RevealMCPToken(ctx)
	if err != nil {
		t.Fatalf("RevealMCPToken: %v", err)
	}
	if held == "" || held != stored.Token {
		t.Errorf("RevealMCPToken = %q, want the stored value", held)
	}

	// The section draws a token it can recognise without holding: the fact, and the tail of it.
	shot, err := uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if want := stored.Token[len(stored.Token)-4:]; shot.MCP.TokenTail != want {
		t.Errorf("tail = %q, want %q", shot.MCP.TokenTail, want)
	}
}

// A port nothing may listen on is refused before anything is stored: the row would otherwise be
// written down and the server silently absent.
func TestABadPortIsRefused(t *testing.T) {
	uc, store, _ := newUseCase()
	ctx := context.Background()

	bad := 80
	_, err := uc.SetMCP(ctx, MCPPatch{Port: &bad}, noApply)
	if domain.CodeOf(err) != domain.CodeUnknownPort {
		t.Fatalf("SetMCP(port=80) = %v, want a refusal", err)
	}
	if store[domain.SettingMCP] != "" {
		t.Error("a refused port was written down")
	}
}

// A door that will not open leaves nothing behind: the row goes back to what it said and no window
// is told, because a section drawing a server that answers is the worst way to be wrong about who
// may call this app.
func TestARefusedDoorLeavesNothingBehind(t *testing.T) {
	uc, store, notifier, _ := newUseCaseWithOutbound()
	ctx := context.Background()

	on := true
	if _, err := uc.SetMCP(ctx, MCPPatch{Enabled: &on, Port: ptr(5360)}, noApply); err != nil {
		t.Fatalf("SetMCP: %v", err)
	}
	before, told := store[domain.SettingMCP], len(notifier.seen)

	refused := errors.New("the port is somebody else's")
	taken := 5361
	_, err := uc.SetMCP(ctx, MCPPatch{Port: &taken}, func(domain.MCPSettings) error {
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("SetMCP with a door that refuses = %v, want the refusal itself", err)
	}
	if store[domain.SettingMCP] != before {
		t.Errorf("stored MCP = %q, want the row put back as it was", store[domain.SettingMCP])
	}
	if len(notifier.seen) != told {
		t.Error("a change that never happened was announced")
	}
}

// What was stored is read back the way the server reads it, and a row that says a server is on
// without a token it could check is read as off.
func TestMCPReadsBackWhatWasStored(t *testing.T) {
	uc, store, _ := newUseCase()
	ctx := context.Background()

	on := true
	patch := MCPPatch{Enabled: &on, Mode: mode(domain.MCPRead), Ask: boolOf(false)}
	if _, err := uc.SetMCP(ctx, patch, noApply); err != nil {
		t.Fatalf("SetMCP: %v", err)
	}
	held, err := uc.MCPSettings(ctx)
	if err != nil {
		t.Fatalf("MCPSettings: %v", err)
	}
	if held.Mode != domain.MCPRead || held.Ask {
		t.Errorf("MCP = %+v, want the mode and the asking as stored", held)
	}

	// A token that went missing leaves the server off rather than open: without it the guard refuses
	// every call, and a server that answers nobody is not one to draw as running.
	rolled := domain.MCPSettings{Enabled: true, Port: domain.DefaultMCPPort, Mode: domain.MCPSend}
	raw, _ := json.Marshal(rolled)
	store[domain.SettingMCP] = string(raw)
	held, err = uc.MCPSettings(ctx)
	if err != nil {
		t.Fatalf("MCPSettings: %v", err)
	}
	if held.Enabled {
		t.Error("a server without a token was read as on")
	}
}

func mode(m domain.MCPMode) *domain.MCPMode { return &m }
func boolOf(v bool) *bool                   { return &v }

// A scale that was written down as a percentage step — a build between two shapes of this setting
// wrote those — reads as the nearest word: the size a person chose is not a setting to change
// behind their back.
func TestAPercentStepReadsAsTheNearestWord(t *testing.T) {
	uc, store, _ := newUseCase()
	store[domain.SettingAppearance] = `{"accent":"blue","density":"regular","size":"110",` +
		`"monoFont":"system","monoSize":13}`

	held, err := uc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if held.Appearance.Size != domain.SizeLarge {
		t.Errorf("size = %q, want the large size", held.Appearance.Size)
	}
}

// The welcome's own state is kept as a set of steps: marking one adds it, marking it again does not
// double it, and a name this build does not know is dropped rather than stored — the window draws a
// count against the list it knows, and a foreign name would make that count a lie.
func TestOnboardingStepsAreASetTheDomainKnows(t *testing.T) {
	uc, _, _ := newUseCase()
	ctx := context.Background()

	fresh, err := uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if fresh.Onboarding.Seen || fresh.Onboarding.Dismissed || len(fresh.Onboarding.Done) != 0 {
		t.Errorf("fresh onboarding = %+v, want nothing walked", fresh.Onboarding)
	}

	marked := []string{string(domain.StepSend), string(domain.StepSave), string(domain.StepSend)}
	saved, err := uc.SetOnboarding(ctx, OnboardingPatch{Seen: boolOf(true), Done: &marked})
	if err != nil {
		t.Fatalf("SetOnboarding: %v", err)
	}
	if !saved.Onboarding.Seen {
		t.Error("the overview was not remembered as offered")
	}
	want := []string{string(domain.StepSend), string(domain.StepSave)}
	if !reflect.DeepEqual(saved.Onboarding.Done, want) {
		t.Errorf("Done = %v, want %v", saved.Onboarding.Done, want)
	}

	// A step that is not one of ours never lands, and a patch that says nothing keeps what stands.
	foreign := []string{string(domain.StepEnv), "teleport"}
	held, err := uc.SetOnboarding(ctx, OnboardingPatch{Done: &foreign})
	if err != nil {
		t.Fatalf("SetOnboarding: %v", err)
	}
	if !reflect.DeepEqual(held.Onboarding.Done, []string{string(domain.StepEnv)}) {
		t.Errorf("Done = %v, want only the step we know", held.Onboarding.Done)
	}
	if !held.Onboarding.Seen {
		t.Error("a patch about the steps dropped what the overview was told")
	}

	// Dismissing the checklist is its own word, and it survives the read the window makes.
	dismissed, err := uc.SetOnboarding(ctx, OnboardingPatch{Dismissed: boolOf(true)})
	if err != nil {
		t.Fatalf("SetOnboarding: %v", err)
	}
	if !dismissed.Onboarding.Dismissed {
		t.Error("putting the checklist away was not remembered")
	}
	again, err := uc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !reflect.DeepEqual(again.Onboarding, dismissed.Onboarding) {
		t.Errorf("read back %+v, want %+v", again.Onboarding, dismissed.Onboarding)
	}
}
