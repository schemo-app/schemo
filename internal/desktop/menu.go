package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// MenuLabels are the few words the native menu needs. They arrive from the window because the
// system draws the menu and only the page knows the language; a second catalogue here would drift
// from the one the rest of the app reads.
type MenuLabels struct {
	About       string `json:"about"`
	File        string `json:"file"`
	Settings    string `json:"settings"`
	CloseWindow string `json:"closeWindow"`
	// SettingsKey and UpdateKey are the accelerators the two commands wear, in this platform's own
	// words ("CmdOrCtrl+,"). The window resolves what a command is bound to and the system draws
	// the menu, so they meet here: "" is a window that has not said yet (the default stands) and
	// "none" is an unassigned command.
	SettingsKey  string `json:"settingsKey"`
	CheckUpdates string `json:"checkUpdates"`
	UpdateKey    string `json:"updateKey"`
}

// EnglishMenuLabels is what the bar carries before a window has said what language it is in: the
// menu is built before any window exists, and a bar with no words is worse than one in the wrong
// language for a second. The first window that answers replaces the whole set.
func EnglishMenuLabels() MenuLabels {
	return MenuLabels{
		About:        "About",
		File:         "File",
		Settings:     "Settings…",
		CheckUpdates: "Check for updates…",
		CloseWindow:  "Close Window",
	}
}

// MenuRow is one line of the menu the window drew for itself, sent so the system's bar can carry
// the same lines. Go draws the line and hands its ID back when it is picked; what the row does is
// the window's, so a row with no command of its own (a theme, a zoom) is no different here.
type MenuRow struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Accel    string `json:"accel"`
	Checked  bool   `json:"checked"`
	Disabled bool   `json:"disabled"`
}

// MenuGroup is one submenu of that menu. Its ID names the submenu of the bar the rows land in
// ("file", "edit", "view", "go", "help") and its sections are the rows, cut where the window drew a
// rule. Where the system's own menu is the working one (Edit) the rows are added to it; a group the
// system carries whole is not sent.
type MenuGroup struct {
	ID       string      `json:"id"`
	Label    string      `json:"label"`
	Sections [][]MenuRow `json:"sections"`
}

// BuildMenu is the native menu for the platforms that do not draw their own title bar. It is built
// again whenever the window sends new labels or rows, which is why it takes them rather than
// reading them.
//
// The application menu has about, the update check and the settings above services and hiding, with
// quit at the foot. Below it is macOS's own order (File, Edit, View, Go, Window, Help); groups are
// addressed by ID so the window's order is kept, and a group with no rows is not drawn.
func BuildMenu(host *Host, name string, labels MenuLabels, groups []MenuGroup) *application.Menu {
	menu := application.NewMenu()

	appMenu := menu.AddSubmenu(name)
	appMenu.Add(labels.About).OnClick(func(*application.Context) { host.ShowAbout() })
	// The check is the settings' updates page's own: the item opens that page and asks it to
	// run one.
	check := appMenu.Add(labels.CheckUpdates).OnClick(func(*application.Context) {
		host.RequestUpdateCheck()
	})
	accelerate(check, labels.UpdateKey, "CmdOrCtrl+U")
	appMenu.AddSeparator()
	settings := appMenu.Add(labels.Settings).OnClick(func(*application.Context) {
		host.ShowSettings("")
	})
	accelerate(settings, labels.SettingsKey, "CmdOrCtrl+,")
	appMenu.AddSeparator()
	appMenu.AddRole(application.ServicesMenu)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Hide)
	appMenu.AddRole(application.HideOthers)
	appMenu.AddRole(application.UnHide)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Quit)

	byID := make(map[string]MenuGroup, len(groups))
	for _, group := range groups {
		byID[group.ID] = group
	}

	if group, ok := byID["file"]; ok {
		file := menu.AddSubmenu(group.Label)
		addRows(file, group.Sections, host)
	}

	// The stock Edit and Window menus bring the standard roles with them: their items work wherever
	// text is, and the window's rows are added beside them rather than drawn instead.
	menu.AddRole(application.EditMenu)
	if group, ok := byID["edit"]; ok {
		addRows(submenuOf(menu, application.EditMenu), group.Sections, host)
	}

	if group, ok := byID["view"]; ok {
		addRows(menu.AddSubmenu(group.Label), group.Sections, host)
	}
	if group, ok := byID["go"]; ok {
		addRows(menu.AddSubmenu(group.Label), group.Sections, host)
	}

	menu.AddRole(application.WindowMenu)
	if window := submenuOf(menu, application.WindowMenu); window != nil {
		window.AddSeparator()
		// ⌘W closes a tab, so the window's own Close moves to ⇧⌘W, added to the system's Window
		// menu rather than one of ours: macOS augments that menu and its items are worth keeping.
		closeWindow := window.Add(labels.CloseWindow).OnClick(func(*application.Context) {
			host.CloseMainWindow()
		})
		closeWindow.SetAccelerator("Shift+CmdOrCtrl+W")
	}

	if group, ok := byID["help"]; ok {
		addRows(menu.AddSubmenu(group.Label), group.Sections, host)
	}

	return menu
}

// addRows draws a group's rows where they belong: one section after another with a rule between
// them, and a rule never inside one — the window's drawn menu cuts its sections the same way.
func addRows(menu *application.Menu, sections [][]MenuRow, host *Host) {
	if menu == nil {
		return
	}
	for index, section := range sections {
		if index > 0 {
			menu.AddSeparator()
		}
		for _, row := range section {
			item := addRow(menu, row)
			item.OnClick(func(*application.Context) {
				host.Emit(EventMenuCommand, row.ID)
			})
			item.SetEnabled(!row.Disabled)
			if row.Accel != "" {
				item.SetAccelerator(row.Accel)
			}
		}
	}
}

// addRow draws one row: a checked one is made as a checkbox, because that is what the bar reads a
// mark from — a plain line keeps none, whatever state it is given afterwards.
func addRow(menu *application.Menu, row MenuRow) *application.MenuItem {
	if row.Checked {
		return menu.AddCheckbox(row.Label, true)
	}
	return menu.Add(row.Label)
}

// submenuOf is the menu a role stands for, or nil for a role the platform did not take.
func submenuOf(menu *application.Menu, role application.Role) *application.Menu {
	item := menu.FindByRole(role)
	if item == nil {
		return nil
	}
	return item.GetSubmenu()
}

// accelerate gives a menu item the combination its command wears: the one the window resolved, or
// the default for a window that has not said, or none at all for a command somebody unassigned.
func accelerate(item *application.MenuItem, key, fallback string) {
	switch key {
	case "none":
	case "":
		item.SetAccelerator(fallback)
	default:
		item.SetAccelerator(key)
	}
}

// ApplyMenu rebuilds the native menu in the language the window sent. Go cannot resolve "system"
// and has nowhere to keep a second catalogue, so the menu is built twice: with the fallback at
// startup and again here.
func (h *Host) ApplyMenu(labels MenuLabels) {
	h.applyMenu(&labels, nil)
}

// ApplyWindowMenu takes the rows of the main window's own menu: what the platforms whose titlebar
// it draws draw themselves is what the system's bar carries where the system has one.
func (h *Host) ApplyWindowMenu(groups []MenuGroup) {
	h.applyMenu(nil, &groups)
}

// applyMenu rebuilds the bar from the two halves the window describes: the words, which arrive with
// the language, and the rows, which arrive with the state. A window that has not sent one leaves
// the half it has (the settings window changes the language without having tabs to draw rows from).
func (h *Host) applyMenu(labels *MenuLabels, groups *[]MenuGroup) {
	h.mu.Lock()
	if labels != nil {
		h.labels = *labels
	}
	if groups != nil {
		h.groups = *groups
	}
	app, title := h.app, h.appName
	words, rows := h.labels, h.groups
	h.mu.Unlock()

	if app == nil || UseCustomTitlebar() {
		return
	}
	app.Menu.Set(BuildMenu(h, title, words, rows))
}

func (h *Host) FocusMain() {
	if win := h.MainWindow(); win != nil {
		bringToFront(win)
	}
}
