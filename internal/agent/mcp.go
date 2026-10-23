// Package agent serves the app to outside clients: the MCP server for AI agents and the bridge
// for the browser extension (native messaging relay and unix socket).
package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
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

// MCPStatus is what the window draws: whether the server is listening and where, whether it was asked
// to run and could not, and who has called.
type MCPStatus struct {
	Agents  []Agent `json:"agents"`
	Port    int     `json:"port"`
	Running bool    `json:"running"`
	Busy    bool    `json:"busy"`
}

// MCP is the MCP server of one app instance: an HTTP listener on the loopback interface, the
// protocol behind a token, and the policy of what an agent may do. Apply is the only way its state
// changes, so the stored settings and what is listening cannot disagree for longer than one call.
type MCP struct {
	tools    Tools
	notifier domain.Notifier
	build    platform.BuildInfo

	mcp      *sdk.Server
	guard    *guard
	agents   *agentRegistry
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

// NewMCP is the server as an object: nothing listens until Apply says so.
func NewMCP(tools Tools, notifier domain.Notifier, build platform.BuildInfo) *MCP {
	s := &MCP{tools: tools, notifier: notifier, build: build}
	s.confirms = newConfirmations()
	s.agents = newAgentRegistry(s.publishChanged)
	s.mcp = sdk.NewServer(&sdk.Implementation{
		Name:    build.Slug,
		Title:   build.Name,
		Version: build.Version,
	}, &sdk.ServerOptions{
		Instructions: "Schemo is a desktop HTTP client. Discover workspace ids with workspaces. " +
			"Read collections, requests, environments and history. Editing tools are offered only " +
			"when enabled; they save data but do not send requests. Use {{variables}} instead of " +
			"credentials and omit masked fields when editing. Sending uses stored authorization " +
			"and variables, makes real network calls, and may require approval in the app.",
	})
	s.registerReadTools()
	s.registerWorkspaceReads()

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
// listens. It never fails on a port that is taken: the window draws that state, and a settings row
// that refused to save because a number is in use would be the wrong half of the app failing.
//
//wails:ignore
func (s *MCP) Apply(settings domain.MCPSettings) error {
	s.policy.Store(&settings)
	s.guard.set(settings.Token)
	s.setSendTool(settings.Mode == domain.MCPSend)
	s.setWriteTools(settings.Write)

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
//
//wails:ignore
func (s *MCP) Rotate(token string) {
	s.guard.set(token)
	s.agents.reset()
	s.publishChanged()
}

// Disconnect ends one agent's sessions and forgets it. The token stays: the client is welcome back
// with the same introduction, and its row returns when it calls again.
func (s *MCP) Disconnect(id string) bool {
	forgotten := s.agents.forget(id)
	if forgotten {
		s.publishChanged()
	}
	return forgotten
}

// Answer delivers the window's decision on a send that is waiting for it.
func (s *MCP) Answer(id string, allow bool) { s.confirms.answer(id, allow) }

// Shutdown stops the listener and everything that was speaking through it.
//
//wails:ignore
func (s *MCP) Shutdown(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	return nil
}

// ServiceShutdown is Wails' hook for Shutdown: a client holding a call open does not hold the
// app's exit.
func (s *MCP) ServiceShutdown() error { return s.Shutdown(context.Background()) }

func (s *MCP) startLocked(port int) {
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
	go func(server *http.Server) {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[mcp] serving: %v", err)
		}
	}(s.http)
}

// stopLocked ends the listener and the sessions. An agent's row does not outlive the connection it
// was made through: a server that is off has nobody connected to it, and that is what it must say.
func (s *MCP) stopLocked() {
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

// policyNow is the policy this call runs under. A call without an applied policy cannot happen,
// since nothing listens then, and the empty answer denies everything, which is the right way for an
// impossible call to fall.
func (s *MCP) policyNow() domain.MCPSettings {
	if held := s.policy.Load(); held != nil {
		return *held
	}
	return domain.MCPSettings{}
}

// Status is the server as the window draws it.
func (s *MCP) Status() MCPStatus {
	s.mu.Lock()
	running := s.listener != nil
	port, busy := s.port, s.busy
	s.mu.Unlock()
	return MCPStatus{Running: running, Port: port, Busy: busy, Agents: s.agents.list()}
}

func (s *MCP) publishChanged() { s.notifier.Publish(TopicChanged, s.Status()) }

func (s *MCP) publishConfirm(ask ConfirmRequest) {
	s.notifier.Publish(TopicConfirm, ask)
}

// Tools is what the server can ask the app. It is a port of small questions rather than the use
// cases: the answers are already shaped for an agent (masked where a value would be a secret, named
// rather than addressed), so the policy of what an agent may see lives where the policy lives.
type Tools interface {
	WorkspaceTools
	Workspaces(ctx context.Context) ([]Workspace, error)
	Collections(ctx context.Context, workspaceID string) ([]Collection, error)
	Requests(ctx context.Context, collectionID string) ([]Request, error)
	Request(ctx context.Context, id string) (Request, error)
	History(ctx context.Context, workspaceID string, limit int) ([]HistoryRow, error)
	Response(ctx context.Context, recordID string) (Response, error)
	// Send makes a real call with the person's environment (inherited variables and authorization
	// resolved by the app) and returns what came back. The agent's name travels with it, so the
	// history record says whose doing the send was.
	Send(ctx context.Context, requestID string, agent string) (Response, error)
}

// Workspace is one workspace as an agent addresses it.
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Collection is a collection or a folder; ParentID is empty at the top of a workspace.
type Collection struct {
	ID          string       `json:"id"`
	Workspace   string       `json:"workspace"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Parent      string       `json:"parent,omitempty"`
	Requests    int          `json:"requests"`
	Auth        *domain.Auth `json:"auth,omitempty"`
}

// Request includes saved authorization. Protected variable values remain masked and references
// stay unresolved; inline API credentials are part of the saved request.
type Request struct {
	ID          string       `json:"id"`
	Workspace   string       `json:"workspace"`
	Collection  string       `json:"collection"`
	Name        string       `json:"name"`
	Method      string       `json:"method"`
	URL         string       `json:"url"`
	Headers     []Header     `json:"headers,omitempty"`
	Body        string       `json:"body,omitempty"`
	BodyKind    string       `json:"bodyKind,omitempty"`
	Params      []Header     `json:"params,omitempty"`
	Description string       `json:"description,omitempty"`
	Environment string       `json:"environment,omitempty"`
	Auth        *domain.Auth `json:"auth,omitempty"`
}

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HistoryRow is one record of the history, without bodies.
type HistoryRow struct {
	ID         string `json:"id"`
	Workspace  string `json:"workspace"`
	Method     string `json:"method"`
	URL        string `json:"url"`
	Status     int    `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	StartedAt  int64  `json:"startedAt"`
}

// Response is what came back. Workspace is not drawn: it is what the server checks a by-id read
// against, and the agent already knows which workspace it asked about.
type Response struct {
	Record     string   `json:"record,omitempty"`
	Status     int      `json:"status,omitempty"`
	StatusText string   `json:"statusText,omitempty"`
	DurationMs int64    `json:"durationMs,omitempty"`
	Headers    []Header `json:"headers,omitempty"`
	Body       string   `json:"body,omitempty"`
	Truncated  bool     `json:"truncated,omitempty"`
	Error      string   `json:"error,omitempty"`
	Workspace  string   `json:"-"`
}
