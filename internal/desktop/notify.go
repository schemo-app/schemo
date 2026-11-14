package desktop

// Notifications for open windows: a tab to show, a link to import, a release. Each is parked until the
// page can take it.

// OpenTab switches the rail to the extension's tab, parking the request if the window can't
// take events yet. The latest request wins: an earlier tab doesn't need reopening.
func (h *Host) OpenTab(tab int) {
	h.mu.Lock()
	if !h.ready.Load() {
		h.pendingTab = tab
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.Emit(EventOpenTab, tab)
}

// RequestImport opens the import dialog on a page: the event reaches an open window, the flag one
// being created. Only TakeImportLink clears the flag, so the dialog opens exactly once.
func (h *Host) RequestImport(page string) {
	h.mu.Lock()
	h.pendingLink = page
	h.mu.Unlock()

	h.Broadcast(EventImportLink)
}

// TakeImportLink returns and clears the parked link, so only the first of mount and event opens it.
func (h *Host) TakeImportLink() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	page := h.pendingLink
	h.pendingLink = ""
	return page
}

// PublishUpdate tells every window the update state moved, including skips, which the About window
// cannot otherwise see.
func (h *Host) PublishUpdate(status UpdateStatus, show bool) {
	h.mu.Lock()
	if !h.ready.Load() {
		if pending := h.pendingUpdate; pending != nil && pending.status.Latest == status.Latest && status.Available {
			show = show || pending.show
		}
		h.pendingUpdate = &parkedUpdate{status: status, show: show}
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.Broadcast(EventUpdateChanged, status)
	if show {
		h.ShowUpdate()
	}
}

// parkedUpdate is an update published before the page could take it.
type parkedUpdate struct {
	status UpdateStatus
	show   bool
}

// RequestUpdateCheck asks the updates page for a check: the event reaches an open page, the flag one
// being created. Only TakeUpdateCheckRequest clears the flag, so exactly one check runs.
func (h *Host) RequestUpdateCheck() {
	h.mu.Lock()
	h.pendingUpdateCheck = true
	h.mu.Unlock()

	// Events go to every window in this version of Wails; only the updates page listens.
	h.Broadcast(EventUpdateCheck)
	h.ShowSettings("updates")
}

// TakeUpdateCheckRequest returns and clears the parked check, so only the first of mount and event runs it.
func (h *Host) TakeUpdateCheckRequest() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	pending := h.pendingUpdateCheck
	h.pendingUpdateCheck = false
	return pending
}
