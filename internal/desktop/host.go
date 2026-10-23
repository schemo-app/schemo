package desktop

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

const (
	deepLinkScheme = platform.Scheme

	windowMain        = "main"
	windowAbout       = "about"
	windowSettings    = "settings"
	windowUpdate      = "update"
	aboutWidth        = 336
	aboutHeightBarred = 439
	aboutHeightInset  = 415
	updateWidth       = 560
	updateHeight      = 488
	settingsWidth     = 1160
	settingsHeight    = 760
	settingsMinWidth  = 820
	settingsMinHeight = 560
	// The window the app is opened in, which is the one size nobody chose: everything below it
	// belongs to a window opened on purpose.
	mainWidth     = 1280
	mainHeight    = 820
	mainMinWidth  = 900
	mainMinHeight = 600
)

var singleInstanceKey = [32]byte([]byte("schemo-v3-single-instance-key-01"))

type Host struct {
	mu                 sync.Mutex
	app                *application.App
	mainWin            *application.WebviewWindow
	ready              atomic.Bool
	appName            string
	labels             MenuLabels
	groups             []MenuGroup
	theme              atomic.Value
	language           atomic.Value
	appearance         atomic.Value
	tinted             atomic.Bool
	appliedAppearance  atomic.Value
	pendingTab         int
	pendingLink        string
	pendingUpdate      *parkedUpdate
	pendingUpdateCheck bool
	restartPath        string
}

func NewHost() *Host {
	host := &Host{}
	host.theme.Store("")
	host.language.Store("")
	host.appearance.Store(domain.DefaultAppearance())
	return host
}

func (h *Host) SetLanguage(language domain.Language) {
	h.language.Store(string(language))
}

// SetAppearance is the second of the choices a window is told rather than shown: the accent, scale
// and code font go on the address of every window opened after this, so a window that shows a
// picture and a version draws itself without asking the settings.
func (h *Host) SetAppearance(appearance domain.Appearance) {
	h.appearance.Store(appearance)
}

func (h *Host) SetAppName(name string) {
	h.mu.Lock()
	h.appName = name
	h.mu.Unlock()
}

func (h *Host) windowTitle() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.appName
}

func (h *Host) Attach(app *application.App) {
	h.mu.Lock()
	h.app = app
	h.mu.Unlock()
}

func (h *Host) App() *application.App {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.app
}

func (h *Host) SetMainWindow(w *application.WebviewWindow) {
	h.mu.Lock()
	h.mainWin = w
	h.mu.Unlock()
}

func (h *Host) MainWindow() *application.WebviewWindow {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.mainWin
}

func (h *Host) MarkReady() {
	h.mu.Lock()
	h.ready.Store(true)
	tab, u := h.pendingTab, h.pendingUpdate
	h.pendingTab, h.pendingUpdate = 0, nil
	h.mu.Unlock()

	if u != nil {
		h.PublishUpdate(u.status, u.available)
	}
	if tab > 0 {
		h.Emit(EventOpenTab, tab)
	}
}

func (h *Host) Emit(name string, data ...any) {
	if !h.ready.Load() {
		return
	}
	if win := h.MainWindow(); win != nil {
		win.EmitEvent(name, data...)
	}
}

func (h *Host) Broadcast(name string, data ...any) {
	if app := h.App(); app != nil {
		app.Event.Emit(name, data...)
	}
}

// SetHeaderHeight applies the main window's measured header height to its native controls.
func (h *Host) SetHeaderHeight(height float64) error {
	if height <= 0 || math.IsNaN(height) || math.IsInf(height, 0) {
		return errors.New("invalid header height")
	}
	window := h.MainWindow()
	if !h.ready.Load() || window == nil {
		return errors.New("main window is not ready")
	}
	setWindowHeaderHeight(window, height)
	return nil
}
