package desktop

import (
	"schemo/internal/usecase/update"
)

// What the app tells a window that is already open: which tab to show, that there is a collection
// to take in, and that a release is waiting. Each waits for the page to be able to take it — a tab
// and a release parked until the window is ready, a link until the window comes asking for it.

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

// RequestImport opens the window's import dialog on a page somebody published. Both halves are
// needed: the event reaches a window that is already open, the flag one that is being created right
// now — a link is usually what started this app, so that is the half that carries it.
//
// The flag is cleared by TakeImportLink alone, never here: that is what makes the two paths open
// the dialog exactly once between them, however they interleave.
func (h *Host) RequestImport(page string) {
	h.mu.Lock()
	h.pendingLink = page
	h.mu.Unlock()

	h.Broadcast(EventImportLink)
}

// TakeImportLink reports and clears the page a link parked. Destructive on purpose: it is called
// from both the mount and the event handler, and only the first caller may open the dialog, or an
// old link would be followed again the next time the window draws.
func (h *Host) TakeImportLink() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	page := h.pendingLink
	h.pendingLink = ""
	return page
}

// PublishUpdate tells every window that the update state moved, parked until the page can take one.
// It is published on a check and on a skip, not only when a release turns up: a skip has to reach
// the windows that are showing the release just as much as a find does, and the About window is a
// document of its own that cannot see the update window's click.
func (h *Host) PublishUpdate(u *update.Status) {
	h.mu.Lock()
	if !h.ready.Load() {
		h.pendingUpdate = u
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.Emit(EventUpdateChanged, u)
}

// RequestUpdateCheck opens the settings' updates page and asks it for a check. Both halves are
// needed: the event reaches a page that is already open, the flag one that is being created right
// now — a page that does not exist yet has nothing listening, and an open one never mounts again.
//
// The flag is cleared by TakeUpdateCheckRequest alone, never here: that is what makes the two
// paths deliver exactly one check between them, however they interleave.
func (h *Host) RequestUpdateCheck() {
	h.mu.Lock()
	h.pendingUpdateCheck = true
	h.mu.Unlock()

	// Events go to every window in this version of Wails; only the updates page listens.
	h.Broadcast(EventUpdateCheck)
	h.ShowSettings("updates")
}

// RequestLoadSample asks the main window to put the sample request in. The menu is the system's and
// the window is the side that knows how to compose one and where it is kept — so the item says what
// is wanted rather than doing it.
func (h *Host) RequestLoadSample() {
	h.Broadcast(EventLoadSample)
}

// TakeUpdateCheckRequest reports and clears a check parked by RequestUpdateCheck. Destructive on
// purpose: it is called from both the mount and the event handler, and only the first caller may
// act on it, or an old request would fire a check the next time the page opens.
func (h *Host) TakeUpdateCheckRequest() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	pending := h.pendingUpdateCheck
	h.pendingUpdateCheck = false
	return pending
}
