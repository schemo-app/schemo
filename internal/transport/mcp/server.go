package mcp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// Event topics. One says the server's own state moved — running, busy on its port, who has called —
// and the other is a question the window has to answer before a send goes out.
const (
	TopicChanged = "mcp:changed"
	TopicConfirm = "mcp:confirm"
)

// stopTimeout bounds the graceful stop: an agent's call in flight is given a moment to finish, and
// a client that holds the socket open does not hold the app's exit.
const stopTimeout = 2 * time.Second

// sessionIdle is how long a client's session is kept after its last request. Long enough for a
// conversation, short enough that closed laptops do not accumulate.
const sessionIdle = 30 * time.Minute

// Status is what the window draws: whether the server is listening and where, whether it was asked
// to run and could not, and who has called.
type Status struct {
	Agents  []Agent `json:"agents"`
	Port    int     `json:"port"`
	Running bool    `json:"running"`
	Busy    bool    `json:"busy"`
}

// Server is the MCP server of one app instance: an HTTP listener on the loopback interface, the
// protocol behind a token, and the policy of what an agent may do. It is started and stopped by
// Apply, which is the only way its state changes, so what is stored in the settings and what is
// listening cannot disagree for longer than one call.
type Server struct {
	tools    Tools
	notifier domain.Notifier
	build    platform.BuildInfo

	mcp      *sdk.Server
	guard    *guard
	agents   *registry
	confirms *confirmations

	policy atomic.Pointer[domain.MCPSettings]

	mu       sync.Mutex
	http     *http.Server
	listener net.Listener
	// listenPort is the port the listener is actually on, which is not the configured one until a
	// listener is opened: a change of port is what the two disagreeing means.
	listenPort int
	port       int
	busy       bool
}

// NewServer is the server as an object: nothing listens until Apply says so.
func NewServer(tools Tools, notifier domain.Notifier, build platform.BuildInfo) *Server {
	s := &Server{tools: tools, notifier: notifier, build: build}
	s.confirms = newConfirmations()
	s.agents = newRegistry(s.publishChanged)
	s.mcp = sdk.NewServer(&sdk.Implementation{
		Name:    build.Slug,
		Title:   build.Name,
		Version: build.Version,
	}, &sdk.ServerOptions{
		Instructions: "This is the person's Schemo desktop client: their own spaces, saved " +
			"requests and history on their own machine. Sending makes real calls with the " +
			"person's environment and secrets, and is confirmed in the app when they ask for it.",
	})
	s.registerReadTools()

	mux := http.NewServeMux()
	mux.Handle("/mcp", sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return s.mcp },
		&sdk.StreamableHTTPOptions{
			// Plain JSON answers rather than an event stream: this server never pushes anything of
			// its own, and a response that arrives whole is one fewer thing to keep open.
			JSONResponse:   true,
			SessionTimeout: sessionIdle,
		}))
	s.guard = newGuard("", mux)
	return s
}

// Apply is the whole of the server's state: the policy for the next call, and whether and where it
// listens. It never fails on a port that is taken — the window draws that state, and a settings row
// that refused to be saved because a number is in use would be the wrong half of the app failing.
func (s *Server) Apply(settings domain.MCPSettings) error {
	s.policy.Store(&settings)
	s.guard.set(settings.Token)
	s.setSendTool(settings.Mode == domain.MCPSend)

	s.mu.Lock()
	switch {
	case !settings.Enabled:
		s.stopLocked()
	case s.listener != nil && s.listenPort == settings.Port:
		// Already where it should be: the token and the policy above are the only things that moved.
	default:
		s.stopLocked()
		s.startLocked(settings.Port)
	}
	s.port = settings.Port
	s.mu.Unlock()

	// The event is published outside the lock: Status reads it, and a lock held across its own
	// reader is a deadlock rather than a state.
	s.publishChanged()
	return nil
}

// Rotate takes a fresh token and drops everyone: every agent holding the old one is no longer an
// agent of this server, and a connection kept through it is kept by a stranger.
func (s *Server) Rotate(token string) {
	s.guard.set(token)
	s.agents.reset()
	s.publishChanged()
}

// Disconnect ends one agent's sessions and forgets it. The token stays: the client is welcome back
// with the same introduction, and its row returns when it calls again.
func (s *Server) Disconnect(id string) bool {
	forgotten := s.agents.forget(id)
	if forgotten {
		s.publishChanged()
	}
	return forgotten
}

// Answer delivers the window's decision on a send that is waiting for it.
func (s *Server) Answer(id string, allow bool) { s.confirms.answer(id, allow) }

// Shutdown stops the listener and everything that was speaking through it.
func (s *Server) Shutdown(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	return nil
}

func (s *Server) startLocked(port int) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		// The address is loopback by construction; the way this fails is a port that belongs to
		// something else, and that is what the window is told.
		s.busy = true
		log.Printf("[mcp] cannot listen on 127.0.0.1:%d: %v", port, err)
		return
	}
	s.busy = false
	s.listener = listener
	s.listenPort = port
	s.http = &http.Server{
		Handler:           s.guard,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(log.Writer(), "[mcp] ", log.LstdFlags),
	}
	log.Printf("[mcp] listening on 127.0.0.1:%d", port)
	go func(srv *http.Server) {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[mcp] serving: %v", err)
		}
	}(s.http)
}

// stopLocked ends the listener and the sessions. An agent's row does not outlive the connection it
// was made through: a server that is off has nobody connected to it, and that is what it must say.
func (s *Server) stopLocked() {
	s.busy = false
	if s.http == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	if err := s.http.Shutdown(ctx); err != nil {
		_ = s.http.Close()
	}
	s.http, s.listener = nil, nil
	s.agents.reset()
}

// policyNow is the policy this call runs under. A call without an applied policy is a call that
// cannot happen — nothing listens then — and the empty answer denies everything, which is the
// right way for an impossible one to fall.
func (s *Server) policyNow() domain.MCPSettings {
	if held := s.policy.Load(); held != nil {
		return *held
	}
	return domain.MCPSettings{}
}

// Status is the server as the window draws it.
func (s *Server) Status() Status {
	s.mu.Lock()
	running := s.listener != nil
	port, busy := s.port, s.busy
	s.mu.Unlock()
	return Status{Running: running, Port: port, Busy: busy, Agents: s.agents.list()}
}

func (s *Server) publishChanged() { s.notifier.Publish(TopicChanged, s.Status()) }

func (s *Server) publishConfirm(ask ConfirmRequest) {
	s.notifier.Publish(TopicConfirm, ask)
}
