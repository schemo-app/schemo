package request

import (
	"cmp"
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptrace"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/icholy/digest"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

type netTransport struct {
	mu    sync.RWMutex
	cfg   EngineConfig
	plain *http.Transport
	local *http.Transport
}

type transportPolicy struct {
	cfg   EngineConfig
	plain *http.Transport
	local *http.Transport
}

func newNetTransport(cfg EngineConfig) *netTransport {
	held := &netTransport{cfg: cfg}
	held.rebuild()
	return held
}

func (t *netTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.snapshot().RoundTrip(req)
}

func (p transportPolicy) RoundTrip(req *http.Request) (*http.Response, error) {
	transport := p.forHost(req.URL.Hostname())
	res, err := transport.RoundTrip(req)
	if err != nil {
		return nil, explained(err, transport, req)
	}
	return res, nil
}

func (p transportPolicy) forHost(host string) *http.Transport {
	if p.local != nil && domain.LocalName(host) {
		return p.local
	}
	return p.plain
}

func (t *netTransport) snapshot() transportPolicy {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return transportPolicy{cfg: t.cfg, plain: t.plain, local: t.local}
}

func (t *netTransport) settings() EngineConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cfg
}

func (t *netTransport) apply(net domain.Network) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cfg.Proxy = net.Proxy
	t.cfg.Requests = net.Requests
	t.cfg.SkipTLSVerify = !net.Certificates.Verify
	t.cfg.TrustLocalhost = net.Certificates.TrustLocalhost
	t.cfg.Roots = net.Certificates.Roots
	t.rebuild()
}

func (t *netTransport) rebuild() {
	if t.plain != nil {
		t.plain.CloseIdleConnections()
	}
	if t.local != nil {
		t.local.CloseIdleConnections()
	}
	t.plain = newTransport(t.cfg)
	t.local = nil
	if t.cfg.TrustLocalhost && !t.cfg.SkipTLSVerify {
		relaxed := t.cfg
		relaxed.SkipTLSVerify = true
		t.local = newTransport(relaxed)
		// A waived certificate is for this machine only, so the dial is checked to land here; proxies
		// are off for it.
		t.local.Proxy = nil
		t.local.DialContext = loopbackDial
	}
}

func (p transportPolicy) client(hops *hopJar) *http.Client {
	stop := redirectPolicy(p.cfg.Requests)
	client := &http.Client{
		Transport:     p,
		Timeout:       time.Duration(p.cfg.Requests.Timeout) * time.Second,
		CheckRedirect: stop,
	}
	if hops == nil {
		return client
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := stop(req, via); err != nil {
			return err
		}
		if moved := req.Response; moved != nil {
			hops.write(moved.Request.URL.String(), headerPairs(moved.Header))
		}
		hops.carry(req)
		return nil
	}
	return client
}

type Engine struct {
	mu       sync.Mutex
	client   *http.Client
	net      *netTransport
	inflight map[string]context.CancelFunc
	cfg      EngineConfig
	ids      platform.IDGen
}

func NewEngine(cfg EngineConfig, ids platform.IDGen) *Engine {
	cfg = cfg.withDefaults()
	engine := &Engine{
		cfg:      cfg,
		net:      newNetTransport(cfg),
		inflight: map[string]context.CancelFunc{},
		ids:      ids,
	}
	engine.client = &http.Client{
		Timeout:       cfg.Timeout,
		Transport:     engine.net,
		CheckRedirect: redirectPolicy(cfg.Requests),
	}
	return engine
}

func (e *Engine) Apply(net domain.Network) {
	e.net.apply(net)
}

func (e *Engine) Client() *http.Client { return e.client }

// Spec is the request the engine sends.
type Spec = Request

func (e *Engine) newRequest(ctx context.Context, spec Spec, cfg EngineConfig) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, spec.Method, complete(spec.URL),
		strings.NewReader(spec.Body))
	if err != nil {
		return nil, err
	}
	if agent := cmp.Or(cfg.Requests.UserAgent, cfg.UserAgent); agent != "" {
		req.Header.Set("User-Agent", agent)
	}
	for _, h := range spec.Headers {
		if strings.TrimSpace(h.Name) != "" {
			req.Header.Add(h.Name, h.Value)
		}
	}
	return req, nil
}

// CompleteURL is the address a typed one becomes before sending, shared with the editor's preview.
func (e *Engine) CompleteURL(raw string) string {
	return complete(raw)
}

// WireDefaults are the User-Agent and compression from the send config, so a preview matches.
func (e *Engine) WireDefaults() (userAgent string, acceptEncoding bool) {
	cfg := e.net.settings()
	return cmp.Or(cfg.Requests.UserAgent, cfg.UserAgent), cfg.Requests.AcceptEncoding
}

func (e *Engine) answerChallenge(
	ctx context.Context,
	client *http.Client,
	spec Spec,
	cfg EngineConfig,
	refused *http.Response,
) (*http.Response, []domain.HeaderPair, error) {
	challenge, err := digest.FindChallenge(refused.Header)
	if err != nil {
		return nil, nil, err
	}
	retry, err := e.newRequest(ctx, spec, cfg)
	if err != nil {
		return nil, nil, err
	}
	credentials, err := digest.Digest(challenge, digest.Options{
		Method:   spec.Method,
		URI:      retry.URL.RequestURI(),
		GetBody:  retry.GetBody,
		Count:    1,
		Username: spec.Digest.Username,
		Password: spec.Digest.Password,
	})
	if err != nil {
		return nil, nil, err
	}
	answer := credentials.String()
	retry.Header.Set("Authorization", answer)

	sent, err := client.Do(retry)
	if err != nil {
		return nil, nil, err
	}
	// The wire gets the credential; the record gets the scheme and a mask, so nothing replayable is stored.
	return sent, []domain.HeaderPair{
		{Name: "Authorization", Value: digest.Prefix + domain.SecretMask},
	}, nil
}

func (e *Engine) Do(ctx context.Context, spec Spec) *domain.Response {
	start := time.Now()

	id := spec.ID
	if id == "" {
		id = e.ids()
	}

	reqCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	defer e.track(id, func() { cancel(context.Canceled) })()

	policy := e.net.snapshot()
	defer e.net.release(policy)

	switch spec.Protocol {
	case domain.ProtocolGRPC:
		rpcCtx, finish := requestDeadline(reqCtx, policy.cfg.Requests.Timeout)
		defer finish()
		return e.callRPC(rpcCtx, spec, policy.cfg, start)
	case domain.ProtocolWebSocket:
		return e.webSocket(reqCtx, spec, policy, start)
	case domain.ProtocolSSE:
		return e.eventStream(reqCtx, spec, policy, start, cancel)
	default:
		return e.doHTTP(reqCtx, spec, policy, start, nil)
	}
}

type readHTTPResponse func(*http.Response, *domain.Response, func()) bool

func (e *Engine) doHTTP(
	ctx context.Context, spec Spec, policy transportPolicy, start time.Time, read readHTTPResponse,
) *domain.Response {
	res := &domain.Response{}
	line := newTimeline(start)
	ctx = httptrace.WithClientTrace(ctx, line.trace())
	req, err := e.newRequest(ctx, spec, policy.cfg)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	var hops *hopJar
	if spec.Jar != nil {
		hops = &hopJar{jar: spec.Jar, workspace: spec.Workspace, ctx: ctx}
	}
	client := policy.client(hops)

	resp, err := client.Do(req)
	if err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return stopped(res, cause)
		}
		return stopped(res, err)
	}

	if spec.Digest != nil && resp.StatusCode == http.StatusUnauthorized {
		answered, sent, answerErr := e.answerChallenge(ctx, client, spec, policy.cfg, resp)
		if answerErr == nil {
			resp.Body.Close()
			resp = answered
			res.SentHeaders = sent
		}
	}
	defer resp.Body.Close()
	if hops != nil {
		hops.write(resp.Request.URL.String(), headerPairs(resp.Header))
		res.CookieWrites = hops.writes
	}

	res.Status = resp.StatusCode
	res.StatusText = resp.Status
	res.FinalURL = resp.Request.URL.String()
	res.ContentType = resp.Header.Get("Content-Type")
	res.Headers = headerPairs(resp.Header)

	if read != nil && read(resp, res, func() { line.fill(res, time.Now()) }) {
		return res
	}
	data, truncated, err := readCapped(resp.Body, e.cfg.MaxBodyBytes)
	if err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return stopped(res, cause)
		}
		return stopped(res, err)
	}
	end := time.Now()

	line.fill(res, end)
	res.Body = string(data)
	res.BodyTruncated = truncated
	return res
}

func requestDeadline(ctx context.Context, seconds int) (context.Context, context.CancelFunc) {
	if seconds == 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
}

func (t *netTransport) release(policy transportPolicy) {
	if current := t.snapshot(); current.plain == policy.plain {
		return
	}
	policy.plain.CloseIdleConnections()
	if policy.local != nil {
		policy.local.CloseIdleConnections()
	}
}

// stopped is the response of a request that ended without an answer: one the user cancelled is
// reported as that, whenever it was stopped, and anything else as the error it was.
func stopped(res *domain.Response, err error) *domain.Response {
	if errors.Is(err, context.Canceled) {
		res.Cancelled = true
	} else {
		res.Error = err.Error()
	}
	return res
}

// track makes a request stoppable by its id until the returned function is called.
func (e *Engine) track(id string, cancel context.CancelFunc) (untrack func()) {
	e.mu.Lock()
	e.inflight[id] = cancel
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		delete(e.inflight, id)
		e.mu.Unlock()
	}
}

func (e *Engine) Cancel(id string) bool {
	e.mu.Lock()
	cancel, ok := e.inflight[id]
	e.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

func readCapped(body io.Reader, limit int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

func headerPairs(header http.Header) []domain.HeaderPair {
	out := make([]domain.HeaderPair, 0, len(header))
	for _, name := range slices.Sorted(maps.Keys(header)) {
		for _, value := range header[name] {
			out = append(out, domain.HeaderPair{Name: name, Value: value})
		}
	}
	return out
}
