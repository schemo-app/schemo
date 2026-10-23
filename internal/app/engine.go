package app

import (
	"context"
	"sync"

	"schemo/internal/domain"
	"schemo/internal/request"
)

type engineExecutor struct {
	engine  *request.Engine
	jar     request.EngineJar
	cookies *cookiePolicy
}

var _ request.Executor = engineExecutor{}

func (e engineExecutor) Execute(ctx context.Context, req request.Request) (*domain.Response, error) {
	var jar request.EngineJar
	if e.cookies.sending() {
		jar = e.jar
	}
	return e.engine.Do(ctx, request.Spec{
		ID:        req.ID,
		Method:    req.Method,
		URL:       req.URL,
		Headers:   req.Headers,
		Body:      req.Body,
		Digest:    req.Digest,
		Workspace: req.Workspace,
		Jar:       jar,
	}), nil
}

func (e engineExecutor) Cancel(id string) bool {
	return e.engine.Cancel(id)
}

type outbound struct {
	engine  *request.Engine
	cookies *cookiePolicy
}

func (o outbound) Apply(net domain.Network) {
	o.engine.Apply(net)
	o.cookies.set(net.Requests.Cookies)
}

// cookiePolicy keeps the cookie switch in memory rather than reading the setting: the draft asks on
// every preparation, so a read there would be a database read per keystroke.
type cookiePolicy struct {
	mu sync.RWMutex
	on bool
}

// On until the settings say otherwise: the answer before they are read must not be the one that
// changes behaviour.
func newCookiePolicy() *cookiePolicy {
	return &cookiePolicy{on: true}
}

func (p *cookiePolicy) set(on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.on = on
}

func (p *cookiePolicy) sending() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.on
}

// cookieSwitch is the jar as the setting leaves it: the same jar while cookies are sent, and one
// that answers nothing when they are not.
type cookieSwitch struct {
	cookies *request.Cookies
	policy  *cookiePolicy
}

var _ request.CookieHeaders = cookieSwitch{}

func (c cookieSwitch) CookieHeader(ctx context.Context, workspace, rawURL string) (string, error) {
	if !c.policy.sending() {
		return "", nil
	}
	return c.cookies.CookieHeader(ctx, workspace, rawURL)
}
