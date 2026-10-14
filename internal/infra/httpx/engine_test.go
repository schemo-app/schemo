package httpx

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

func newTestEngine(t *testing.T, cfg Config) *Engine {
	t.Helper()
	return NewEngine(cfg, platform.NewIDGen())
}

func send(
	engine *Engine,
	method, url string,
	headers map[string]string,
	body string,
) *domain.Response {
	return engine.Do(context.Background(), Spec{
		Method:  method,
		URL:     url,
		Headers: domain.PairsFromMap(headers),
		Body:    body,
	})
}

func inflight(engine *Engine) int {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return len(engine.inflight)
}

func TestARedirectedAnswerReportsWhereItCameFrom(t *testing.T) {
	landing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "landed")
	}))
	defer landing.Close()

	start := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, landing.URL+"/here", http.StatusFound)
	}))
	defer start.Close()

	res := send(newTestEngine(t, Config{}), http.MethodGet, start.URL, nil, "")
	if res.Error != "" {
		t.Fatalf("send: %s", res.Error)
	}
	if res.FinalURL != landing.URL+"/here" {
		t.Errorf("final url = %q, want the address the answer came from", res.FinalURL)
	}
	// A request that was not moved reports its own address, because there is nothing else to report.
	plain := send(newTestEngine(t, Config{}), http.MethodGet, landing.URL, nil, "")
	if plain.FinalURL != landing.URL {
		t.Errorf("final url = %q, want the address that was asked", plain.FinalURL)
	}
}

func TestTimings(t *testing.T) {
	t.Run("plain http", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write([]byte("ok"))
		}))
		defer srv.Close()

		res := send(newTestEngine(t, Config{}), http.MethodGet, srv.URL, nil, "")
		if res.Error != "" {
			t.Fatalf("send failed: %s", res.Error)
		}
		if res.TLSUs != nil {
			t.Errorf("tlsUs = %d on a plaintext server, want no phase at all", *res.TLSUs)
		}
		// Not asserted as positive: a connect to localhost takes a fraction of a millisecond.
		if res.ConnectUs == nil || *res.ConnectUs > res.DurationUs {
			t.Errorf("connectUs = %v, want a phase inside the %dus total", res.ConnectUs, res.DurationUs)
		}
		// The wait starts when the connection is ready — there is no TLS handshake to wait for.
		if res.WaitUs == nil || *res.WaitUs < 20_000 {
			t.Errorf("waitUs = %v, want at least the 20ms the handler sleeps", res.WaitUs)
		}
		assertPhasesWithinTotal(t, res)
	})

	t.Run("tls", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		defer srv.Close()

		res := send(newTestEngine(t, Config{SkipTLSVerify: true}), http.MethodGet, srv.URL, nil, "")
		if res.Error != "" {
			t.Fatalf("send failed: %s", res.Error)
		}
		if res.TLSUs == nil || *res.TLSUs <= 0 {
			t.Errorf("tlsUs = %v, want the handshake measured", res.TLSUs)
		}
		assertPhasesWithinTotal(t, res)
	})

	t.Run("a repeat dials again", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write([]byte("ok"))
		}))
		defer srv.Close()

		engine := newTestEngine(t, Config{})
		if res := send(engine, http.MethodGet, srv.URL, nil, ""); res.Error != "" {
			t.Fatalf("first send failed: %s", res.Error)
		}
		second := send(engine, http.MethodGet, srv.URL, nil, "")

		if second.Error != "" {
			t.Fatalf("second send failed: %s", second.Error)
		}
		if second.ConnectUs == nil {
			t.Error("connectUs is absent on a repeat, want the connection dialled again")
		}
		if second.DNSUs != nil || second.TLSUs != nil {
			t.Errorf("dns/tls = %v/%v against a literal address over plain http, want none",
				second.DNSUs, second.TLSUs)
		}
		if second.WaitUs == nil || *second.WaitUs < 20_000 {
			t.Errorf("waitUs = %v, want at least the 20ms the handler sleeps", second.WaitUs)
		}
		assertPhasesWithinTotal(t, second)
	})

	t.Run("every hop of a redirect", func(t *testing.T) {
		landing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write([]byte("landed"))
		}))
		defer landing.Close()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(20 * time.Millisecond)
			http.Redirect(w, r, landing.URL, http.StatusFound)
		}))
		defer srv.Close()

		res := send(newTestEngine(t, Config{}), http.MethodGet, srv.URL, nil, "")
		if res.Error != "" {
			t.Fatalf("send failed: %s", res.Error)
		}
		if res.WaitUs == nil || *res.WaitUs < 40_000 {
			t.Errorf("waitUs = %v, want both hops' waits", res.WaitUs)
		}
		assertPhasesWithinTotal(t, res)
	})
}

func TestKeepingConnectionsReusesThem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	engine := newTestEngine(t, Config{KeepConnections: true})
	if res := send(engine, http.MethodGet, srv.URL, nil, ""); res.Error != "" {
		t.Fatalf("first send failed: %s", res.Error)
	}
	second := send(engine, http.MethodGet, srv.URL, nil, "")

	if second.Error != "" {
		t.Fatalf("second send failed: %s", second.Error)
	}
	if second.ConnectUs != nil {
		t.Errorf("connectUs = %v on a reused connection, want none — nothing was dialled",
			*second.ConnectUs)
	}
	if second.WaitUs == nil {
		t.Error("waitUs is absent: the request did happen, only the dial did not")
	}
}

func TestAFailedRequestHasNoPhases(t *testing.T) {
	res := send(newTestEngine(t, Config{Timeout: 2 * time.Second}), http.MethodGet,
		"http://127.0.0.1:1/", nil, "")

	if res.Error == "" {
		t.Fatal("a request to a dead port reported no error")
	}
	if res.WaitUs != nil || res.DownloadUs != nil {
		t.Errorf("wait/download = %v/%v for a request that never reached a server, want none",
			res.WaitUs, res.DownloadUs)
	}
	if res.DurationUs < 0 {
		t.Errorf("durationUs = %d, want the attempt timed", res.DurationUs)
	}
}

func assertPhasesWithinTotal(t *testing.T, res *domain.Response) {
	t.Helper()
	var sum int64
	for _, us := range []*int64{res.DNSUs, res.ConnectUs, res.TLSUs, res.WaitUs, res.DownloadUs} {
		if us != nil {
			sum += *us
		}
	}
	if sum > res.DurationUs {
		t.Errorf("phases sum to %dus, more than the %dus total", sum, res.DurationUs)
	}
	if res.DurationUs <= 0 {
		t.Errorf("durationUs = %d, want a positive total", res.DurationUs)
	}
}

func TestTLSVerificationIsOnByDefault(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	res := send(newTestEngine(t, Config{}), http.MethodGet, srv.URL, nil, "")
	if res.Error == "" {
		t.Error("a self-signed certificate was accepted with verification on")
	}

	relaxed := send(newTestEngine(t, Config{SkipTLSVerify: true}), http.MethodGet, srv.URL, nil, "")
	if relaxed.Error != "" {
		t.Errorf("SkipTLSVerify did not take effect: %s", relaxed.Error)
	}
}

func TestTrustLocalhostTakesASelfSignedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	trusted := send(newTestEngine(t, Config{TrustLocalhost: true}), http.MethodGet, srv.URL, nil, "")
	if trusted.Error != "" {
		t.Errorf("a local self-signed certificate was refused: %s", trusted.Error)
	}
}

func TestTrustLocalhostIsOnlyAboutThisMachine(t *testing.T) {
	trusting := newNetTransport(Config{TrustLocalhost: true})
	if trusting.forHost("127.0.0.1") == trusting.forHost("example.com") {
		t.Error("a local name and another host were given the same transport")
	}
	strict := newNetTransport(Config{})
	if strict.forHost("127.0.0.1") != strict.forHost("example.com") {
		t.Error("the local exception is on without anybody asking for it")
	}
}

func TestRedirects(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("final"))
	}))
	defer final.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer first.Close()

	t.Run("followed by default", func(t *testing.T) {
		res := send(newTestEngine(t, Config{}), http.MethodGet, first.URL, nil, "")
		if res.Status != http.StatusOK || res.Body != "final" {
			t.Errorf("got %d %q, want the redirect target", res.Status, res.Body)
		}
	})

	t.Run("stopped when asked", func(t *testing.T) {
		asked := Config{Requests: domain.DefaultRequests()}
		asked.Requests.FollowRedirects = false
		res := send(newTestEngine(t, asked), http.MethodGet, first.URL, nil, "")
		if res.Status != http.StatusFound {
			t.Errorf("status = %d, want the 302 itself", res.Status)
		}
		if got := headerValues(res, "Location"); len(got) != 1 || got[0] != final.URL {
			t.Errorf("Location = %v, want the target", got)
		}
	})

	t.Run("a loop ends with the redirect limit", func(t *testing.T) {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, srv.URL, http.StatusFound)
		}))
		defer srv.Close()

		short := Config{Requests: domain.DefaultRequests()}
		short.Requests.MaxRedirects = 3
		res := send(newTestEngine(t, short), http.MethodGet, srv.URL, nil, "")
		if !strings.Contains(res.Error, "3 redirects") {
			t.Errorf("error = %q, want the redirect limit named", res.Error)
		}
	})
}

func TestRepeatedHeadersBothSurvive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.Header().Add("Set-Cookie", "c=3")
		w.Header().Set("X-One", "1")
	}))
	defer srv.Close()

	res := send(newTestEngine(t, Config{}), http.MethodGet, srv.URL, nil, "")
	cookies := headerValues(res, "Set-Cookie")
	if len(cookies) != 3 {
		t.Fatalf("Set-Cookie = %v, want all three", cookies)
	}
	for i, want := range []string{"a=1", "b=2", "c=3"} {
		if cookies[i] != want {
			t.Errorf("cookie %d = %q, want %q", i, cookies[i], want)
		}
	}
	if len(res.Headers) != 6 {
		t.Errorf("headers = %v, want every line kept", res.Headers)
	}
	names := make([]string, 0, len(res.Headers))
	for _, h := range res.Headers {
		names = append(names, h.Name)
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("header names %v are not sorted, so the order would vary between sends", names)
	}
}

func TestBodyCap(t *testing.T) {
	const cap int64 = 1024
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", int(cap)+500)))
	}))
	defer srv.Close()

	res := send(newTestEngine(t, Config{MaxBodyBytes: cap}), http.MethodGet, srv.URL, nil, "")
	if !res.BodyTruncated {
		t.Error("bodyTruncated = false, want the cap reported")
	}
	if int64(len(res.Body)) != cap {
		t.Errorf("body length = %d, want exactly the %d-byte cap", len(res.Body), cap)
	}

	small := send(newTestEngine(t, Config{}), http.MethodGet, srv.URL, nil, "")
	if small.BodyTruncated {
		t.Error("a body under the default cap was reported as truncated")
	}
	if int64(len(small.Body)) != cap+500 {
		t.Errorf("body length = %d, want the whole body", len(small.Body))
	}
}

func TestCancelStopsOneRequest(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()
	defer close(release)

	engine := newTestEngine(t, Config{})

	done := make(chan *domain.Response, 1)
	go func() {
		done <- engine.Do(context.Background(), Spec{ID: "slow", Method: http.MethodGet, URL: srv.URL})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		engine.mu.Lock()
		_, running := engine.inflight["slow"]
		engine.mu.Unlock()
		if running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the request never registered itself")
		}
		time.Sleep(time.Millisecond)
	}

	if !engine.Cancel("slow") {
		t.Fatal("Cancel reported nothing in flight")
	}

	select {
	case res := <-done:
		if !res.Cancelled || res.Error != "" {
			t.Errorf("res = %+v, want the cancellation flag and no text", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the cancelled request never returned")
	}

	if engine.Cancel("slow") {
		t.Error("the id is still in flight after the request returned")
	}
	if n := inflight(engine); n != 0 {
		t.Errorf("%d attempts are still in flight, want none", n)
	}
}

// TestConcurrentRequestsCancelIndependently is what the single global cancel could not do.
func TestConcurrentRequestsCancelIndependently(t *testing.T) {
	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(3)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started.Done()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
	}))
	defer srv.Close()
	defer close(release)

	engine := newTestEngine(t, Config{})

	results := make(chan string, 3)
	for _, id := range []string{"a", "b", "c"} {
		go func(id string) {
			res := engine.Do(context.Background(), Spec{ID: id, Method: http.MethodGet, URL: srv.URL})
			if res.Cancelled {
				results <- id + ":cancelled"
				return
			}
			results <- id + ":answered"
		}(id)
	}

	started.Wait()
	if !engine.Cancel("b") {
		t.Fatal("cancel b found nothing in flight")
	}
	if n := inflight(engine); n != 3 {
		t.Fatalf("%d attempts are in flight, want all three", n)
	}
	if !engine.Cancel("a") || !engine.Cancel("c") {
		t.Fatal("an attempt could not be cancelled on its own")
	}

	got := map[string]bool{}
	for i := 0; i < 3; i++ {
		select {
		case r := <-results:
			got[r] = true
		case <-time.After(3 * time.Second):
			t.Fatal("a request never returned")
		}
	}
	for _, id := range []string{"a", "b", "c"} {
		if !got[id+":cancelled"] {
			t.Errorf("%s was not cancelled: %v", id, got)
		}
	}
}

type fakeJar struct {
	answers map[string]string
	asked   []string
	applied []string
}

func (f *fakeJar) CookieHeader(_ context.Context, _, rawURL string) (string, error) {
	f.asked = append(f.asked, rawURL)
	return f.answers[rawURL], nil
}

func (f *fakeJar) Apply(
	_ context.Context,
	_, rawURL string,
	headers []domain.HeaderPair,
) []domain.CookieWrite {
	f.applied = append(f.applied, rawURL)
	out := []domain.CookieWrite{}
	for _, header := range headers {
		if !strings.EqualFold(header.Name, "Set-Cookie") {
			continue
		}
		name, _, _ := strings.Cut(header.Value, "=")
		out = append(out, domain.CookieWrite{Name: name, State: domain.CookieAdded})
	}
	return out
}

func redirectPair(t *testing.T, hops *[]string) (start, next string) {
	t.Helper()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hops = append(*hops, r.Header.Get("Cookie"))
		w.Header().Add("Set-Cookie", "final=1")
		_, _ = w.Write([]byte("final"))
	}))
	t.Cleanup(second.Close)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hops = append(*hops, r.Header.Get("Cookie"))
		w.Header().Add("Set-Cookie", "hop=1")
		http.Redirect(w, r, second.URL, http.StatusFound)
	}))
	t.Cleanup(first.Close)
	return first.URL, second.URL
}

func TestARedirectCarriesTheJarsAnswerForItsOwnAddress(t *testing.T) {
	t.Run("the jar has an answer", func(t *testing.T) {
		var hops []string
		start, next := redirectPair(t, &hops)
		jar := &fakeJar{answers: map[string]string{next: "from=jar"}}

		res := newTestEngine(t, Config{}).Do(context.Background(), Spec{
			Method: http.MethodGet, URL: start, Workspace: "personal", Jar: jar,
			Headers: []domain.HeaderPair{{Name: "Cookie", Value: "first=1"}},
		})
		if res.Error != "" {
			t.Fatalf("send: %s", res.Error)
		}
		if len(hops) != 2 {
			t.Fatalf("hops = %v, want the request and the one it was sent to", hops)
		}
		if hops[0] != "first=1" {
			t.Errorf("first hop carried %q, want what the request was prepared with", hops[0])
		}
		if hops[1] != "from=jar" {
			t.Errorf("second hop carried %q, want this address's own cookies and no other", hops[1])
		}
		if len(jar.asked) != 1 || jar.asked[0] != next {
			t.Errorf("jar asked about %v, want the address of the hop it had not answered for", jar.asked)
		}
	})

	t.Run("the jar has none", func(t *testing.T) {
		var hops []string
		start, _ := redirectPair(t, &hops)

		res := newTestEngine(t, Config{}).Do(context.Background(), Spec{
			Method: http.MethodGet, URL: start, Workspace: "personal", Jar: &fakeJar{},
			Headers: []domain.HeaderPair{{Name: "Cookie", Value: "first=1"}},
		})
		if res.Error != "" {
			t.Fatalf("send: %s", res.Error)
		}
		if hops[1] != "" {
			t.Errorf("second hop carried %q, want nothing: no jar cookie reaches it", hops[1])
		}
	})
}

func TestEveryAnswerOfARedirectGoesIntoTheJar(t *testing.T) {
	var hops []string
	start, next := redirectPair(t, &hops)
	jar := &fakeJar{}

	res := newTestEngine(t, Config{}).Do(context.Background(), Spec{
		Method: http.MethodGet, URL: start, Workspace: "personal", Jar: jar,
	})
	if res.Error != "" {
		t.Fatalf("send: %s", res.Error)
	}
	if len(jar.applied) != 2 || jar.applied[0] != start || jar.applied[1] != next {
		t.Fatalf("jar written under %v, want both addresses in the order they answered", jar.applied)
	}
	if len(res.CookieWrites) != 2 || res.CookieWrites[0].Name != "hop" ||
		res.CookieWrites[1].Name != "final" {
		t.Errorf("writes = %+v, want both answers, the first one first", res.CookieWrites)
	}
}

func TestASendWithoutAJarWritesNothing(t *testing.T) {
	var hops []string
	start, _ := redirectPair(t, &hops)

	res := send(newTestEngine(t, Config{}), http.MethodGet, start, nil, "")
	if res.Status != http.StatusOK {
		t.Fatalf("status = %d, want the redirect followed", res.Status)
	}
	if len(res.CookieWrites) != 0 {
		t.Errorf("writes = %+v, want none without a jar", res.CookieWrites)
	}
}

func TestProxyIsUsed(t *testing.T) {
	var seen atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Add(1)
		_, _ = w.Write([]byte("proxied"))
	}))
	defer proxy.Close()

	res := send(newTestEngine(t, Config{Proxy: manualProxy(t, proxy.URL)}), http.MethodGet,
		"http://example.com/articles", nil, "")
	if res.Error != "" {
		t.Fatalf("send through the proxy failed: %s", res.Error)
	}
	if seen.Load() == 0 {
		t.Error("the proxy never saw the request")
	}
	if res.Body != "proxied" {
		t.Errorf("body = %q, want the proxy's own answer", res.Body)
	}
}

func manualProxy(t *testing.T, rawURL string) domain.ProxySettings {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parsing the proxy URL: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parsing the proxy port: %v", err)
	}
	return domain.ProxySettings{
		Mode:     domain.ProxyManual,
		Protocol: domain.ProxyHTTP,
		Host:     parsed.Hostname(),
		Port:     port,
	}
}

func TestProbeReadsWhatTheProxyAnswers(t *testing.T) {
	tunnel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer tunnel.Close()

	engine := newTestEngine(t, Config{})
	engine.Apply(domain.Network{Proxy: manualProxy(t, tunnel.URL)})
	opened := engine.Probe(context.Background())
	if !opened.OK || opened.Error != "" || opened.NeedsAuth {
		t.Errorf("probe = %+v, want a tunnel that opened", opened)
	}

	walled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer walled.Close()

	engine.Apply(domain.Network{Proxy: manualProxy(t, walled.URL)})
	refused := engine.Probe(context.Background())
	if refused.OK || !refused.NeedsAuth || refused.Status != http.StatusProxyAuthRequired {
		t.Errorf("probe = %+v, want the proxy asking for credentials", refused)
	}
}

func TestProbeSaysWhenNothingAnswers(t *testing.T) {
	engine := newTestEngine(t, Config{})
	engine.Apply(domain.Network{Proxy: domain.ProxySettings{
		Mode: domain.ProxyManual, Protocol: domain.ProxyHTTP, Host: "127.0.0.1", Port: 1,
	}})

	probe := engine.Probe(context.Background())
	if probe.OK || probe.Error == "" {
		t.Errorf("probe = %+v, want a failure with its reason", probe)
	}
}

func TestProxyEnvironmentNamesTheVariables(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.corp:3128")

	entries := newTestEngine(t, Config{}).ProxyEnvironment()
	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want the three names", entries)
	}
	if entries[0].Key != "HTTP_PROXY" || entries[0].Value != "http://proxy.corp:3128" {
		t.Errorf("HTTP_PROXY = %+v, want what the environment holds", entries[0])
	}
	if entries[1].Value != "" {
		t.Errorf("HTTPS_PROXY = %+v, want it unset", entries[1])
	}
}

func TestProxyBypassGoesDirect(t *testing.T) {
	held := domain.ProxySettings{
		Mode:   domain.ProxyManual,
		Host:   "proxy.corp",
		Port:   8080,
		Bypass: []string{"api.internal", "10.0.0.0/8"},
	}
	decide := proxyOf(held)

	through, err := decide(httptest.NewRequest(http.MethodGet, "http://example.com/a", nil))
	if err != nil || through == nil || through.Host != "proxy.corp:8080" {
		t.Errorf("example.com went to %v (%v), want the proxy", through, err)
	}
	for _, address := range []string{"http://api.internal/a", "http://10.4.5.6/a"} {
		direct, err := decide(httptest.NewRequest(http.MethodGet, address, nil))
		if err != nil || direct != nil {
			t.Errorf("%s went to %v (%v), want no proxy at all", address, direct, err)
		}
	}
}

func TestProxyOffUsesNothing(t *testing.T) {
	if decide := proxyOf(domain.ProxySettings{Mode: domain.ProxyOff}); decide != nil {
		t.Error("off returned a decision, want none at all")
	}
}

func TestRequestHeadersAreSent(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer srv.Close()

	engine := newTestEngine(t, Config{})
	engine.Do(context.Background(), Spec{
		Method: http.MethodPost,
		URL:    srv.URL,
		Body:   `{"a":1}`,
		Headers: []domain.HeaderPair{
			{Name: "X-Second", Value: "2"},
			{Name: "X-First", Value: "1"},
			{Name: "X-Repeated", Value: "a"},
			{Name: "X-Repeated", Value: "b"},
			{Name: "  ", Value: "dropped"},
		},
	})

	if got.Get("X-First") != "1" || got.Get("X-Second") != "2" {
		t.Errorf("headers did not arrive: %v", got)
	}
	if values := got.Values("X-Repeated"); len(values) != 2 {
		t.Errorf("X-Repeated = %v, want both values", values)
	}
	if got.Get("Content-Length") != strconv.Itoa(len(`{"a":1}`)) {
		t.Errorf("content-length = %q, want the body length", got.Get("Content-Length"))
	}
}

func TestBadURLIsReportedNotPanicked(t *testing.T) {
	res := send(newTestEngine(t, Config{}), http.MethodGet, "not a url at all", nil, "")
	if res.Error == "" {
		t.Error("a malformed URL produced no error")
	}
	if res.Cancelled || res.Status != 0 {
		t.Errorf("res = %+v, want a plain failure", res)
	}
}

// The Content-Type is chosen a layer up, where the body's format and the user's header are both
// visible: an engine that helped would be a second opinion that could see neither.
// headerValues is every value one header carries: a redirect names its target, and a session
// sets more than one cookie.
func headerValues(res *domain.Response, name string) []string {
	var out []string
	for _, h := range res.Headers {
		if h.Name == name {
			out = append(out, h.Value)
		}
	}
	return out
}

func TestTheEngineSendsTheBodyItIsGiven(t *testing.T) {
	var (
		got  string
		kind string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got, kind = string(body), r.Header.Get("Content-Type")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	engine := newTestEngine(t, Config{})
	// A body a form would produce, sent with no Content-Type at all: it must arrive byte for byte.
	multipart := "--BOUNDARY\r\nContent-Disposition: form-data; name=\"a\"\r\n\r\n1\r\n" +
		"--BOUNDARY--\r\n"
	res := send(engine, http.MethodPost, srv.URL, nil, multipart)
	if res.Error != "" {
		t.Fatalf("send failed: %s", res.Error)
	}
	if got != multipart {
		t.Errorf("body = %q, want the one the engine was given", got)
	}
	if kind != "" {
		t.Errorf("Content-Type = %q, want the engine to have invented none", kind)
	}

	res = send(engine, http.MethodPost, srv.URL,
		map[string]string{"Content-Type": "application/vnd.api+json"}, "{}")
	if res.Error != "" {
		t.Fatalf("send failed: %s", res.Error)
	}
	if kind != "application/vnd.api+json" {
		t.Errorf("Content-Type = %q, want the header the caller wrote", kind)
	}
}

func TestAnAddedAuthorityIsTrustedAsWell(t *testing.T) {
	base := x509.NewCertPool()
	known, knownLeaf := certificateAuthority(t)
	base.AddCert(parseCertificate(t, known))

	authority, leafPEM := certificateAuthority(t)
	pool := rootPool(base, []domain.Root{{Name: "corp", PEM: authority}})

	for name, leafPEM := range map[string]string{
		"the one that was already there": knownLeaf,
		"the added one":                  leafPEM,
	} {
		if _, err := parseCertificate(t, leafPEM).Verify(x509.VerifyOptions{Roots: pool}); err != nil {
			t.Errorf("%s was not trusted: %v", name, err)
		}
	}
}

func TestThePoolTravelsWithTheTransport(t *testing.T) {
	authority, _ := certificateAuthority(t)

	built := tlsConfigFor(Config{Roots: []domain.Root{{Name: "corp", PEM: authority}}})
	if built == nil || built.RootCAs == nil {
		t.Fatal("no pool was built for the added authority")
	}
	if built.InsecureSkipVerify {
		t.Error("adding an authority turned verification off")
	}
	if tlsConfigFor(Config{}) != nil {
		t.Error("a TLS configuration was built with no reason for one")
	}
}

func parseCertificate(t *testing.T, contents string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(contents))
	if block == nil {
		t.Fatal("that is not a PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	return cert
}

func certificateAuthority(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Schemo Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("authority: %v", err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("authority: %v", err)
	}

	leafDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "api.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("leaf: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})),
		string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}))
}
