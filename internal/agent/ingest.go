package agent

import (
	"context"
	"log"
	"strings"

	"schemo/internal/desktop"
	"schemo/internal/domain"
	"schemo/internal/request"
	"schemo/internal/workspace"
)

// Ingest handles data received from the extension.
type Ingest interface {
	Captured(req CapturedRequest)
	StateChanged(state CaptureState)
	ClientGone()
	FocusRequested(req FocusRequest)
	FiltersChanged(filters domain.CaptureFilters)
}

type nopIngest struct{}

func (nopIngest) Captured(CapturedRequest)             {}
func (nopIngest) StateChanged(CaptureState)            {}
func (nopIngest) ClientGone()                          {}
func (nopIngest) FocusRequested(FocusRequest)          {}
func (nopIngest) FiltersChanged(domain.CaptureFilters) {}

// Events the window hears from the extension.
const (
	EventCaptureState        = "capture-state"
	EventCaptureDisconnected = "capture-disconnected"
)

// capture is the app's side of Ingest: captures become history history, popup filters become
// saved settings, and state changes reach the window.
type capture struct {
	host     *desktop.Host
	history  *request.History
	settings *workspace.Settings
}

func NewCapture(
	host *desktop.Host,
	history *request.History,
	settings *workspace.Settings,
) Ingest {
	return capture{host: host, history: history, settings: settings}
}

// FiltersChanged stores filters edited in the extension popup; windows get the settings change.
func (c capture) FiltersChanged(filters domain.CaptureFilters) {
	filters.Hosts = domain.OrEmpty(filters.Hosts)
	patch := map[string]any{"captureFilters": map[string]any{
		"hosts": filters.Hosts, "static": filters.Static, "analytics": filters.Analytics, "json": filters.JSON,
	}}
	if _, err := c.settings.Save(context.Background(), patch); err != nil {
		log.Printf("[capture] storing filters: %v", err)
	}
}

// The window hears about this as the same event, and the same record, as one this app sent.
func (c capture) Captured(req CapturedRequest) {
	responseHeaders := domain.PairsFromMap(req.ResponseHeaders)

	if _, err := c.history.Ingest(context.Background(), request.IngestInput{
		Source:          domain.SourceBrowser,
		Method:          req.Method,
		URL:             req.URL,
		Status:          req.Status,
		StatusText:      req.StatusText,
		ContentType:     headerValue(responseHeaders, "Content-Type"),
		RequestHeaders:  domain.PairsFromMap(req.RequestHeaders),
		ResponseHeaders: responseHeaders,
		RequestBody:     req.RequestBody,
		ResponseBody:    req.ResponseBody,
		DurationMs:      req.DurationMs,
		StartedAt:       req.StartedAt,
		WaitMs:          req.WaitMs,
		DownloadMs:      req.DownloadMs,
		TabID:           req.TabID,
		TabTitle:        req.TabTitle,
		TabURL:          req.TabURL,
		FavIconURL:      req.FavIconURL,
	}); err != nil {
		// A capture that cannot be stored is a gap in the list, not a reason to drop the socket:
		// the extension cannot retry it and the next request is already on its way. The address is
		// left out of the line, since a captured URL carries whatever the page had in it.
		log.Printf("[capture] storing a request: %v", err)
	}
}

func (c capture) StateChanged(state CaptureState) {
	c.host.Emit(EventCaptureState, state)
}

func (c capture) ClientGone() {
	c.host.Emit(EventCaptureDisconnected)
}

func (c capture) FocusRequested(req FocusRequest) {
	c.host.FocusMain()
	if req.Tab > 0 {
		c.host.OpenTab(req.Tab)
	}
}

// headerValue reads one name out of headers, ignoring case: the extension reports the names the
// browser used, which are lower-case.
func headerValue(headers []domain.HeaderPair, name string) string {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}
