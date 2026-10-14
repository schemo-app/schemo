// Package wails is the window's contract with the app: one bound service per feature, the events
// they publish, and the ports this layer answers on behalf of other features. The application
// those services belong to — its windows, its menu, its tray — is `transport/desktop`.
package wails

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/domain"
	"schemo/internal/transport/desktop"
	"schemo/internal/transport/extension"
	"schemo/internal/transport/mcp"
	"schemo/internal/usecase/account"
	"schemo/internal/usecase/collection"
	"schemo/internal/usecase/record"
	"schemo/internal/usecase/settings"
	"schemo/internal/usecase/sync"
	"schemo/internal/usecase/update"
	"schemo/internal/usecase/workspace"
)

// Event names registered for typed frontend event broadcasting.
const (
	eventCaptureState        = "capture-state"
	eventCaptureDisconnected = "capture-disconnected"
)

// init registers all typed application events with the Wails framework at startup.
func init() {
	application.RegisterEvent[extension.CaptureState](eventCaptureState)
	application.RegisterEvent[application.Void](eventCaptureDisconnected)
	application.RegisterEvent[mcp.Status](mcp.TopicChanged)
	application.RegisterEvent[mcp.ConfirmRequest](mcp.TopicConfirm)
	application.RegisterEvent[int](desktop.EventOpenTab)
	application.RegisterEvent[application.Void](desktop.EventImportLink)
	application.RegisterEvent[string](desktop.EventSettingsTab)
	application.RegisterEvent[*update.Status](desktop.EventUpdateChanged)
	application.RegisterEvent[application.Void](desktop.EventUpdateCheck)
	application.RegisterEvent[application.Void](desktop.EventLoadSample)
	application.RegisterEvent[string](desktop.EventMenuCommand)
	application.RegisterEvent[settings.ThemeChanged](settings.TopicThemeChanged)
	application.RegisterEvent[settings.LanguageChanged](settings.TopicLanguageChanged)
	application.RegisterEvent[domain.Settings](settings.TopicChanged)
	application.RegisterEvent[workspace.Changed](workspace.TopicChanged)
	application.RegisterEvent[account.Status](account.TopicChanged)
	application.RegisterEvent[sync.Status](sync.TopicChanged)
	application.RegisterEvent[domain.Record](record.TopicRecordAdded)
	application.RegisterEvent[record.HistoryCleared](record.TopicHistoryCleared)
	application.RegisterEvent[record.RequestFinished](record.TopicRequestFinished)
	application.RegisterEvent[record.RequestFailed](record.TopicRequestFailed)
	application.RegisterEvent[collection.RunProgress](collection.TopicRunProgress)
	application.RegisterEvent[domain.CollectionRun](collection.TopicRunFinished)
}

// bus implements the domain Notifier interface to broadcast events across Wails host windows.
type bus struct {
	host *desktop.Host
}

// newBus initializes and returns a new event bus instance tied to the specified host.
func newBus(host *desktop.Host) *bus {
	return &bus{host: host}
}

// Publish broadcasts a topic and payload to all connected frontend clients via the host.
func (b *bus) Publish(topic string, payload any) {
	b.host.Broadcast(topic, payload)
}
