package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/agent"
	"schemo/internal/cloud"
	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/request"
	"schemo/internal/workspace"
)

// init registers all typed application events with the Wails framework at startup.
func init() {
	application.RegisterEvent[agent.CaptureState](agent.EventCaptureState)
	application.RegisterEvent[application.Void](agent.EventCaptureDisconnected)
	application.RegisterEvent[agent.MCPStatus](agent.TopicChanged)
	application.RegisterEvent[string](agent.TopicDataChanged)
	application.RegisterEvent[agent.ConfirmRequest](agent.TopicConfirm)
	application.RegisterEvent[int](desktop.EventOpenTab)
	application.RegisterEvent[application.Void](desktop.EventImportLink)
	application.RegisterEvent[desktop.FileDrop](desktop.EventFilesDropped)
	application.RegisterEvent[string](desktop.EventSettingsTab)
	application.RegisterEvent[*desktop.UpdateStatus](desktop.EventUpdateChanged)
	application.RegisterEvent[application.Void](desktop.EventUpdateCheck)
	application.RegisterEvent[string](desktop.EventMenuCommand)
	application.RegisterEvent[domain.Changed](domain.TopicChanged)
	application.RegisterEvent[workspace.Changed](workspace.TopicChanged)
	application.RegisterEvent[cloud.AccountStatus](cloud.TopicAccountChanged)
	application.RegisterEvent[cloud.SyncStatus](cloud.TopicSyncChanged)
	application.RegisterEvent[domain.Record](request.TopicRecordAdded)
	application.RegisterEvent[request.HistoryCleared](request.TopicHistoryCleared)
	application.RegisterEvent[request.RequestFinished](request.TopicRequestFinished)
	application.RegisterEvent[request.RequestFailed](request.TopicRequestFailed)
	application.RegisterEvent[request.RunProgress](request.TopicRunProgress)
	application.RegisterEvent[domain.CollectionRun](request.TopicRunFinished)
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
