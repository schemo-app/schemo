package desktop

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The `schemo://` links the OS hands the app, and the second launch that carries one.

// importLink is the link a published page offers: the reader's own address, asked to take the
// collection standing at it.
const importLink = "import"

func (h *Host) HandleURLOpen(rawURL string) {
	h.FocusMain()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	if page := importedPage(parsed); page != "" {
		h.RequestImport(page)
		return
	}
	if tabID, ok := tabFromURL(parsed); ok {
		h.OpenTab(tabID)
	}
}

func (h *Host) OnSecondInstance(data application.SecondInstanceData) {
	for _, arg := range data.Args {
		if strings.HasPrefix(arg, deepLinkScheme+"://") {
			h.HandleURLOpen(arg)
			return
		}
	}
	h.FocusMain()
}

// importedPage is the page an import link names (`schemo://import?url=…`), and an empty string for
// every other link. The address is handed on as it arrived; whether this app may read it is for the
// reading side to say.
func importedPage(parsed *url.URL) string {
	if parsed.Host != importLink {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get("url"))
}

func tabFromURL(u *url.URL) (int, bool) {
	value := u.Query().Get("tab")
	if value == "" {
		return 0, false
	}
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
