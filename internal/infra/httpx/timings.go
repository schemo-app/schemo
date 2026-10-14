package httpx

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"

	"schemo/internal/domain"
)

// timeline is what a client trace learns about one request: when each phase began and how long it
// took. The trace's hooks may run on several goroutines at once — a dual-stack dial starts two
// connections — so everything they write, the start times included, is behind one lock.
type timeline struct {
	mu    sync.Mutex
	began time.Time

	dnsAt, connectAt, tlsAt, wroteAt, firstByteAt time.Time
	dns, connect, handshake, wait                 *time.Duration
}

func newTimeline(began time.Time) *timeline { return &timeline{began: began} }

// trace is the client trace that fills the timeline in.
func (t *timeline) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart:          func(httptrace.DNSStartInfo) { t.enter(&t.dnsAt) },
		DNSDone:           func(httptrace.DNSDoneInfo) { t.leave(&t.dns, &t.dnsAt) },
		ConnectStart:      func(_, _ string) { t.enter(&t.connectAt) },
		ConnectDone:       func(_, _ string, _ error) { t.leave(&t.connect, &t.connectAt) },
		TLSHandshakeStart: func() { t.enter(&t.tlsAt) },
		TLSHandshakeDone:  func(tls.ConnectionState, error) { t.leave(&t.handshake, &t.tlsAt) },
		WroteRequest:      func(httptrace.WroteRequestInfo) { t.enter(&t.wroteAt) },
		GotFirstResponseByte: func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			now := time.Now()
			// A request that never reports being written is waited for from the moment it began.
			ready := t.wroteAt
			if ready.IsZero() {
				ready = t.began
			}
			spend(&t.wait, ready, now)
			t.firstByteAt = now
		},
	}
}

func (t *timeline) enter(at *time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	*at = time.Now()
}

func (t *timeline) leave(into **time.Duration, from *time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	spend(into, *from, time.Now())
}

// fill writes what the trace measured into the answer. A phase nothing reported stays absent rather
// than zero: a connection that was reused did not take no time to dial, it did not dial.
func (t *timeline) fill(res *domain.Response, end time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	res.DurationUs = end.Sub(t.began).Microseconds()
	res.DNSUs = microseconds(t.dns)
	res.ConnectUs = microseconds(t.connect)
	res.TLSUs = microseconds(t.handshake)
	res.WaitUs = microseconds(t.wait)
	res.DownloadUs = between(t.firstByteAt, end)
}

// spend adds the time from start to end to a phase, unless one of the two was never reported.
func spend(field **time.Duration, start, end time.Time) {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return
	}
	if *field == nil {
		*field = new(time.Duration)
	}
	**field += end.Sub(start)
}

func between(start, end time.Time) *int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return nil
	}
	us := end.Sub(start).Microseconds()
	return &us
}

func microseconds(spent *time.Duration) *int64 {
	if spent == nil {
		return nil
	}
	us := spent.Microseconds()
	return &us
}
