package desktop

// The events the desktop itself publishes: a window opens a tab or takes in a collection somebody
// published, the menu asks the settings' updates page for a check or the sample to be put in, a row
// of the system's bar names the row of the window's own menu it stands for, and the theme or the
// language moves. What each of them carries is registered in `transport/wails/events.go`, which is
// where the window's whole event list can be read.
const (
	EventOpenTab       = "open-tab"
	EventImportLink    = "import-link"
	EventSettingsTab   = "settings-tab"
	EventUpdateChanged = "update-changed"
	EventUpdateCheck   = "update-check"
	EventLoadSample    = "load-sample"
	EventMenuCommand   = "menu-command"
)
