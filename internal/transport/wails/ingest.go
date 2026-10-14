package wails

import (
	"context"
	"log"
	"strings"

	"schemo/internal/domain"
	"schemo/internal/transport/desktop"
	"schemo/internal/transport/extension"
	"schemo/internal/usecase/record"
	"schemo/internal/usecase/settings"
)

// Deliberately not a bound service: Wails binds every exported method of a service, and these are
// callbacks rather than an API the frontend may call.
type captureIngest struct {
	host     *desktop.Host
	records  *record.UseCase
	settings *settings.UseCase
}

func newCaptureIngest(
	host *desktop.Host,
	records *record.UseCase,
	settings *settings.UseCase,
) extension.Ingest {
	return captureIngest{host: host, records: records, settings: settings}
}

// FiltersChanged stores filters edited in the extension popup; windows get the settings change.
func (c captureIngest) FiltersChanged(filters domain.CaptureFilters) {
	if _, err := c.settings.SetCaptureFilters(context.Background(), filters); err != nil {
		log.Printf("[capture] storing filters: %v", err)
	}
}

// The window hears about this as the same event, and the same record, as one this app sent.
func (c captureIngest) Captured(req extension.CapturedRequest) {
	responseHeaders := domain.PairsFromMap(req.ResponseHeaders)

	if _, err := c.records.Ingest(context.Background(), record.IngestInput{
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
		// the extension has no way to retry it, and the next request is already on its way.
		// The address is left out of the line: a captured URL carries whatever the page had in it.
		log.Printf("[capture] storing a request: %v", err)
	}
}

func (c captureIngest) StateChanged(state extension.CaptureState) {
	c.host.Emit(eventCaptureState, state)
}

func (c captureIngest) ClientGone() {
	c.host.Emit(eventCaptureDisconnected)
}

func (c captureIngest) FocusRequested(req extension.FocusRequest) {
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
