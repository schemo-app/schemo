package desktop

import "github.com/wailsapp/wails/v3/pkg/application"

// MenuLabels are the native menu's words, sent by the window so there is only one catalogue.
type MenuLabels struct {
	About       string `json:"about"`
	File        string `json:"file"`
	Settings    string `json:"settings"`
	CloseWindow string `json:"closeWindow"`
	// SettingsKey and UpdateKey are the commands' accelerators ("CmdOrCtrl+,"): "" keeps the
	// default and "none" means unassigned.
	SettingsKey  string `json:"settingsKey"`
	CheckUpdates string `json:"checkUpdates"`
	UpdateKey    string `json:"updateKey"`
}

// EnglishMenuLabels label the menu built before any window has sent its language.
func EnglishMenuLabels() MenuLabels {
	return MenuLabels{
		About:        "About",
		File:         "File",
		Settings:     "Settings…",
		CheckUpdates: "Check for updates…",
		CloseWindow:  "Close Window",
	}
}

// MenuRow is a row of the window's drawn menu, mirrored in the system bar; picking it hands its ID
// back to the window.
type MenuRow struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Accel    string `json:"accel"`
	Checked  bool   `json:"checked"`
	Disabled bool   `json:"disabled"`
}

// MenuGroup is one submenu, addressed by ID ("file", "edit", "view", "go", "help"); sections are
// split where the window drew a rule. Rows join the system's own Edit menu.
type MenuGroup struct {
	ID       string      `json:"id"`
	Label    string      `json:"label"`
	Sections [][]MenuRow `json:"sections"`
}

// BuildMenu builds the native menu where the system draws the title bar. It is rebuilt whenever the
// window sends labels or rows; groups follow macOS order and empty ones are skipped.
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

// ApplyMenu rebuilds the native menu in the window's language.
func (h *Host) ApplyMenu(labels MenuLabels) {
	h.applyMenu(&labels, nil)
}

// ApplyWindowMenu takes the rows of the main window's own menu: what the platforms whose titlebar
// it draws draw themselves is what the system's bar carries where the system has one.
func (h *Host) ApplyWindowMenu(groups []MenuGroup) {
	h.applyMenu(nil, &groups)
}

// applyMenu rebuilds the bar from labels and rows; a nil half keeps what was there, since the settings
// window sends labels without rows.
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
