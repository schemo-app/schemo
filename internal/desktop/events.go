package desktop

// Events the desktop publishes; payloads are registered in internal/app/events.go.
const (
	EventOpenTab       = "open-tab"
	EventImportLink    = "import-link"
	EventFilesDropped  = "files-dropped"
	EventSettingsTab   = "settings-tab"
	EventUpdateChanged = "update-changed"
	EventUpdateCheck   = "update-check"
	EventMenuCommand   = "menu-command"
)
