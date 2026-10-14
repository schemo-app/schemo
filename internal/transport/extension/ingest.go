package extension

import "schemo/internal/domain"

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
