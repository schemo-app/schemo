package desktop

// The events the desktop itself publishes: a window opens a tab or takes in a published collection,
// the menu asks the updates page for a check, a row of the system's bar names the window menu row
// it stands for, and the theme or language moves. Payloads are registered in
// `internal/app/events.go`, which lists every window event.
const (
	EventOpenTab       = "open-tab"
	EventImportLink    = "import-link"
	EventSettingsTab   = "settings-tab"
	EventUpdateChanged = "update-changed"
	EventUpdateCheck   = "update-check"
	EventMenuCommand   = "menu-command"
)
