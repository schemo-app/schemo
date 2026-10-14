package httpx

import (
	"context"
	"errors"
	"io"
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
	cfg   Config
	plain *http.Transport
	local *http.Transport
}

func newNetTransport(cfg Config) *netTransport {
	held := &netTransport{cfg: cfg}
	held.rebuild()
	return held
}

func (t *netTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.RLock()
	held := t.forHost(req.URL.Hostname())
	t.mu.RUnlock()
	return held.RoundTrip(req)
}

func (t *netTransport) forHost(host string) *http.Transport {
	if t.local != nil && isLocalHost(host) {
		return t.local
	}
	return t.plain
}

func (t *netTransport) settings() Config {
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
	}
}

func (e *Engine) sendClient(cfg Config, hops *hopJar) *http.Client {
	stop := redirectPolicy(cfg.Requests)
	client := &http.Client{
		Transport:     e.net,
		Timeout:       time.Duration(cfg.Requests.Timeout) * time.Second,
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
	cfg      Config
	ids      platform.IDGen
}

func NewEngine(cfg Config, ids platform.IDGen) *Engine {
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

type Spec struct {
	ID        string
	Method    string
	URL       string
	Headers   []domain.HeaderPair
	Body      string
	Workspace string
	Jar       Jar
	Digest    *domain.DigestCredentials
}

func (e *Engine) newRequest(ctx context.Context, spec Spec, cfg Config) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, spec.Method, complete(spec.URL),
		strings.NewReader(spec.Body))
	if err != nil {
		return nil, err
	}
	if agent := firstNonEmpty(cfg.Requests.UserAgent, cfg.UserAgent); agent != "" {
		req.Header.Set("User-Agent", agent)
	}
	for _, h := range spec.Headers {
		if strings.TrimSpace(h.Name) != "" {
			req.Header.Add(h.Name, h.Value)
		}
	}
	return req, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// CompleteURL is the address a typed one becomes before it is sent, which is what the Host header
// is read off: the editor draws a request's own headers before a send, and the rule has to be this
// one rather than a second guess at it.
func (e *Engine) CompleteURL(raw string) string {
	return complete(raw)
}

// WireDefaults is the two headers the engine decides about every request on its own: the name the
// app goes by, and whether the server is allowed to compress. Read from the same config a send
// reads, so a preview cannot disagree with what goes out.
func (e *Engine) WireDefaults() (userAgent string, acceptEncoding bool) {
	cfg := e.net.settings()
	return firstNonEmpty(cfg.Requests.UserAgent, cfg.UserAgent), cfg.Requests.AcceptEncoding
}

func (e *Engine) answerChallenge(
	ctx context.Context,
	client *http.Client,
	spec Spec,
	cfg Config,
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
	// The wire gets the answer itself; what is handed back is the scheme and the mask. An answer
	// authenticates the request it was made for, so a record or an export that kept it whole would
	// carry a credential to replay — and the set of headers below is exactly where that copy is made.
	return sent, []domain.HeaderPair{
		{Name: "Authorization", Value: digest.Prefix + domain.SecretMask},
	}, nil
}

func (e *Engine) Do(ctx context.Context, spec Spec) *domain.Response {
	res := &domain.Response{}
	start := time.Now()

	id := spec.ID
	if id == "" {
		id = e.ids()
	}

	line := newTimeline(start)

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	reqCtx = httptrace.WithClientTrace(reqCtx, line.trace())

	e.mu.Lock()
	e.inflight[id] = cancel
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.inflight, id)
		e.mu.Unlock()
	}()

	cfg := e.net.settings()

	req, err := e.newRequest(reqCtx, spec, cfg)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	var hops *hopJar
	if spec.Jar != nil {
		hops = &hopJar{jar: spec.Jar, workspace: spec.Workspace, ctx: reqCtx}
	}
	client := e.sendClient(cfg, hops)

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			res.Cancelled = true
		} else {
			res.Error = err.Error()
		}
		return res
	}

	if spec.Digest != nil && resp.StatusCode == http.StatusUnauthorized {
		answered, sent, answerErr := e.answerChallenge(reqCtx, client, spec, cfg, resp)
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

	data, truncated, err := readCapped(resp.Body, e.cfg.MaxBodyBytes)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	end := time.Now()

	line.fill(res, end)
	res.Status = resp.StatusCode
	res.StatusText = resp.Status
	res.FinalURL = resp.Request.URL.String()
	res.ContentType = resp.Header.Get("Content-Type")
	res.Headers = headerPairs(resp.Header)
	res.Body = string(data)
	res.BodyTruncated = truncated
	return res
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
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	slices.Sort(names)

	out := make([]domain.HeaderPair, 0, len(names))
	for _, name := range names {
		for _, value := range header[name] {
			out = append(out, domain.HeaderPair{Name: name, Value: value})
		}
	}
	return out
}
