package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// The fake below is the port as the tests need it: a handful of workspaces and requests that never
// change, and a send that is recorded rather than performed.
type fakeTools struct {
	WorkspaceTools
	mu   sync.Mutex
	sent []sent
}

func (f *fakeTools) Workspaces(context.Context) ([]Workspace, error) {
	return []Workspace{
		{ID: "s-personal", Name: "Personal", Kind: "personal"},
		{ID: "s-team", Name: "Studio", Kind: "team"},
	}, nil
}

func (f *fakeTools) Collections(_ context.Context, workspaceID string) ([]Collection, error) {
	if workspaceID == "s-personal" {
		return []Collection{{ID: "c-1", Workspace: "s-personal", Name: "Users", Requests: 1}}, nil
	}
	return []Collection{{ID: "c-2", Workspace: "s-team", Name: "Orders"}}, nil
}

func (f *fakeTools) Requests(_ context.Context, collectionID string) ([]Request, error) {
	if collectionID != "c-1" {
		return []Request{}, nil
	}
	return []Request{{
		ID: "r-1", Workspace: "s-personal", Collection: "c-1", Name: "List users",
		Method: "GET", URL: "https://api.example/users?token=••••",
	}}, nil
}

func (f *fakeTools) Request(_ context.Context, id string) (Request, error) {
	if id != "r-1" {
		return Request{}, domain.ErrNotFound
	}
	return Request{
		ID: "r-1", Workspace: "s-personal", Collection: "c-1", Name: "List users",
		Method: "GET", URL: "https://api.example/users",
		Headers: []Header{{Name: "Authorization", Value: "••••"}},
	}, nil
}

func (f *fakeTools) History(_ context.Context, workspaceID string, _ int) ([]HistoryRow, error) {
	if workspaceID != "s-personal" {
		return []HistoryRow{}, nil
	}
	return []HistoryRow{{
		ID: "rec-1", Workspace: "s-personal", Method: "GET", URL: "https://api.example/users",
		Status: 200, StartedAt: 1000,
	}}, nil
}

func (f *fakeTools) Response(_ context.Context, recordID string) (Response, error) {
	if recordID != "rec-1" {
		return Response{}, domain.ErrNotFound
	}
	return Response{Status: 200, Body: `{"data":[]}`, Workspace: "s-personal"}, nil
}

func (f *fakeTools) Send(_ context.Context, requestID, agent string) (Response, error) {
	if requestID != "r-1" {
		return Response{}, domain.ErrNotFound
	}
	f.mu.Lock()
	f.sent = append(f.sent, sent{id: requestID, agent: agent})
	f.mu.Unlock()
	return Response{Status: 201, Body: `{"data":{"id":"1"}}`, Workspace: "s-personal"}, nil
}

// sent is one call: which request, and whose doing it was.
type sent struct {
	id    string
	agent string
}

func (f *fakeTools) sentIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.sent))
	for _, one := range f.sent {
		out = append(out, one.id)
	}
	return out
}

// sentBy is the agent the last send was attributed to.
func (f *fakeTools) sentBy() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return ""
	}
	return f.sent[len(f.sent)-1].agent
}

// recorder is the bus as a test sees it: everything published, and the questions kept apart so a
// test can wait for one without polling.
type recorder struct {
	mu       sync.Mutex
	topics   []string
	confirms chan ConfirmRequest
}

func newRecorder() *recorder { return &recorder{confirms: make(chan ConfirmRequest, 4)} }

func (r *recorder) Publish(topic string, payload any) {
	r.mu.Lock()
	r.topics = append(r.topics, topic)
	r.mu.Unlock()
	if topic == TopicConfirm {
		r.confirms <- payload.(ConfirmRequest)
	}
}

// any is whether a topic was published at all — what the window follows to redraw itself.
func (r *recorder) any(topic string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, seen := range r.topics {
		if seen == topic {
			return true
		}
	}
	return false
}

// freePort is a port nothing is listening on at the moment it is asked.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

// bearer is how a client sends the token: the same header the config the person copies carries.
type bearer struct {
	token string
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func start(t *testing.T, tools Tools, held domain.MCPSettings, bus *recorder) *MCP {
	t.Helper()
	server := NewMCP(tools, bus, platform.BuildInfo{
		Name: "Schemo", Slug: "schemo", Version: "0.0-test",
	})
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	if err := server.Apply(held); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return server
}

// dial connects a real MCP client — the protocol as an agent speaks it — through the token.
func dial(t *testing.T, port int, token, name string) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: name, Title: name, Version: "1"}, nil)
	transport := &sdk.StreamableClientTransport{
		Endpoint:             fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		HTTPClient:           &http.Client{Transport: bearer{token: token}},
		DisableStandaloneSSE: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connecting %s: %v", name, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// on is a server turned on with the settings a test is about, running on a port of its own.
func on(port int) domain.MCPSettings {
	return domain.MCPSettings{
		Enabled: true, Port: port, Token: "sk-sch-testtoken", Mode: domain.MCPSend,
		AllWorkspaces: true, Workspaces: []string{},
	}
}

func call(t *testing.T, session *sdk.ClientSession, name string, args any) *sdk.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("calling %s: %v", name, err)
	}
	return result
}

func TestARightTokenIsTheOnlyWayIn(t *testing.T) {
	port := freePort(t)
	start(t, &fakeTools{}, on(port), newRecorder())

	post := func(token string) int {
		body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":` +
			`{"protocolVersion":"2025-06-18","capabilities":{},` +
			`"clientInfo":{"name":"probe","version":"1"}}}`
		req, err := http.NewRequest(http.MethodPost,
			fmt.Sprintf("http://127.0.0.1:%d/mcp", port), strings.NewReader(body))
		if err != nil {
			t.Fatalf("building the request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("posting: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := post(""); code != http.StatusUnauthorized {
		t.Errorf("without a token the answer was %d, want 401", code)
	}
	if code := post("sk-sch-wrong"); code != http.StatusUnauthorized {
		t.Errorf("with a wrong token the answer was %d, want 401", code)
	}
	if code := post("sk-sch-testtoken"); code == http.StatusUnauthorized {
		t.Error("the right token was refused")
	}
}

// The scheme of an Authorization header is case-insensitive, so a client that writes `bearer` is
// let in as one that writes `Bearer` is (RFC 9110 §11.1).
func TestTheBearerSchemeIsReadWhateverItsCase(t *testing.T) {
	port := freePort(t)
	start(t, &fakeTools{}, on(port), newRecorder())

	post := func(header string) int {
		req, err := http.NewRequest(http.MethodPost,
			fmt.Sprintf("http://127.0.0.1:%d/mcp", port), strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("building the request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", header)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("posting: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	for _, header := range []string{"bearer sk-sch-testtoken", "BEARER sk-sch-testtoken", "Bearer   sk-sch-testtoken"} {
		if code := post(header); code == http.StatusUnauthorized {
			t.Errorf("%q was refused", header)
		}
	}
	for _, header := range []string{"Basic sk-sch-testtoken", "sk-sch-testtoken", "Bearer"} {
		if code := post(header); code != http.StatusUnauthorized {
			t.Errorf("%q was answered %d, want 401", header, code)
		}
	}
}

// The question a refused client reads says where the token comes from: "wrong token" alone leaves
// somebody configuring a client with nowhere to look.
func TestARefusalSaysWhereTheTokenIs(t *testing.T) {
	port := freePort(t)
	start(t, &fakeTools{}, on(port), newRecorder())

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		"application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("posting: %v", err)
	}
	defer resp.Body.Close()

	var refusal struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&refusal); err != nil {
		t.Fatalf("reading the refusal: %v", err)
	}
	if !strings.Contains(refusal.Error.Message, "Settings") {
		t.Errorf("the refusal reads %q, want it to name where the token is", refusal.Error.Message)
	}
}

// The token can be reissued while the app runs: the old one stops working at once, and the new one
// works without a restart.
func TestAReissuedTokenReplacesTheOldOneAtOnce(t *testing.T) {
	port := freePort(t)
	server := start(t, &fakeTools{}, on(port), newRecorder())

	session := dial(t, port, "sk-sch-testtoken", "Claude Desktop")
	call(t, session, "workspaces", map[string]any{})

	server.Rotate("sk-sch-second")
	if agents := server.Status().Agents; len(agents) != 0 {
		t.Errorf("agents after the reissue = %+v, want nobody", agents)
	}

	post := func(token string) int {
		req, _ := http.NewRequest(http.MethodPost,
			fmt.Sprintf("http://127.0.0.1:%d/mcp", port), strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("posting: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := post("sk-sch-testtoken"); code != http.StatusUnauthorized {
		t.Errorf("the old token was answered %d, want 401", code)
	}
	if code := post("sk-sch-second"); code == http.StatusUnauthorized {
		t.Error("the new token was refused")
	}
}

// A call is what introduces an agent: the window promises a row after the first call, and a
// connection that only opened the door is not somebody the person needs to hear about.
func TestAnAgentAppearsAfterItsFirstCall(t *testing.T) {
	port := freePort(t)
	bus := newRecorder()
	server := start(t, &fakeTools{}, on(port), bus)

	dial(t, port, "sk-sch-testtoken", "Claude Desktop")
	if agents := server.Status().Agents; len(agents) != 0 {
		t.Fatalf("agents before any call = %+v, want nobody", agents)
	}

	session := dial(t, port, "sk-sch-testtoken", "Cursor")
	call(t, session, "workspaces", map[string]any{})

	agents := server.Status().Agents
	if len(agents) != 1 {
		t.Fatalf("agents = %+v, want one", agents)
	}
	if agents[0].Name != "Cursor" || agents[0].Today != 1 {
		t.Errorf("agent = %+v, want Cursor with one call", agents[0])
	}
	// The window is told by an event rather than by polling: a row that appeared in the status and
	// nowhere else is a row the section would never draw.
	if !bus.any(TopicChanged) {
		t.Error("the call published no change event")
	}
}

func TestDisconnectingAnAgentForgetsIt(t *testing.T) {
	port := freePort(t)
	server := start(t, &fakeTools{}, on(port), newRecorder())

	session := dial(t, port, "sk-sch-testtoken", "Cursor")
	call(t, session, "workspaces", map[string]any{})

	agents := server.Status().Agents
	if len(agents) != 1 {
		t.Fatalf("agents = %+v, want one", agents)
	}
	if !server.Disconnect(agents[0].ID) {
		t.Fatal("Disconnect answered that there was nobody")
	}
	if left := server.Status().Agents; len(left) != 0 {
		t.Errorf("agents after disconnecting = %+v, want nobody", left)
	}
}

// In "reading only" the send tool is not offered at all — an agent is not handed a tool it may not
// use — and a direct call is refused as well.
func TestReadingOnlyOffersNoSending(t *testing.T) {
	port := freePort(t)
	held := on(port)
	held.Mode = domain.MCPRead
	server := start(t, &fakeTools{}, held, newRecorder())

	session := dial(t, port, "sk-sch-testtoken", "Cursor")
	for tool, err := range session.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("listing the tools: %v", err)
		}
		if tool.Name == "send_request" {
			t.Error("the send tool was offered by a server in reading-only mode")
		}
	}

	// A direct call is refused as well: the tool is not there, and the client is told.
	params := &sdk.CallToolParams{Name: "send_request", Arguments: map[string]any{"request": "r-1"}}
	if _, err := session.CallTool(context.Background(), params); err == nil {
		t.Error("a send was allowed by a server in reading-only mode")
	}

	// Flipping the switch mid-flight offers it, and calls it: the policy is read per call.
	held.Mode = domain.MCPSend
	if err := server.Apply(held); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	result := call(t, session, "send_request", map[string]any{"request": "r-1"})
	if result.IsError {
		t.Errorf("a send was refused after sending was allowed: %+v", result.Content)
	}
}

// Workspaces the person kept the agent out of are out of the listing, out of a by-id read and out of a
// send — one policy, applied everywhere.
func TestWorkspacesThePersonKeptHomeAreNotReachable(t *testing.T) {
	port := freePort(t)
	held := on(port)
	held.AllWorkspaces = false
	held.Workspaces = []string{"s-personal"}
	server := start(t, &fakeTools{}, held, newRecorder())

	session := dial(t, port, "sk-sch-testtoken", "Cursor")

	result := call(t, session, "workspaces", map[string]any{})
	if !strings.Contains(structured(t, result), "s-personal") ||
		strings.Contains(structured(t, result), "s-team") {
		t.Errorf("workspaces = %s, want only the one that was allowed", structured(t, result))
	}

	if result := call(t, session, "collections", map[string]any{"workspace": "s-team"}); !result.IsError {
		t.Error("a collection of a kept-home workspace was listed")
	}
	if result := call(t, session, "send_request", map[string]any{"request": "r-1"}); result.IsError {
		t.Error("a send in an allowed workspace was refused")
	}
	if len(server.Status().Agents) != 1 {
		t.Errorf("agents = %+v, want the one that called", server.Status().Agents)
	}
}

// With "ask before sending" on, a send waits for the window: nothing goes out until the person
// allows it, and a no leaves the request unsent.
func TestASendWaitsForThePersonWhenTheyAskedToBeAsked(t *testing.T) {
	port := freePort(t)
	tools := &fakeTools{}
	bus := newRecorder()
	held := on(port)
	held.Ask = true
	server := start(t, tools, held, bus)

	session := dial(t, port, "sk-sch-testtoken", "Claude Desktop")

	type outcome struct {
		result *sdk.CallToolResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := session.CallTool(ctx,
			&sdk.CallToolParams{Name: "send_request", Arguments: map[string]any{"request": "r-1"}})
		done <- outcome{result: result, err: err}
	}()

	var ask ConfirmRequest
	select {
	case ask = <-bus.confirms:
	case <-time.After(5 * time.Second):
		t.Fatal("the window was never asked")
	}
	if ask.Agent != "Claude Desktop" || ask.Method != "GET" || ask.Name != "List users" {
		t.Errorf("the question was %+v, want the agent and the request named", ask)
	}
	if len(tools.sentIDs()) != 0 {
		t.Fatal("the request went out before the person answered")
	}

	server.Answer(ask.ID, true)
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("the send after a yes: %v", got.err)
		}
		if got.result.IsError {
			t.Fatalf("the send after a yes was an error: %+v", got.result.Content)
		}
		if sent := tools.sentIDs(); len(sent) != 1 || sent[0] != "r-1" {
			t.Errorf("sent = %+v, want the one request", sent)
		}
		// The send carries the name of whoever asked for it: that is what the record in the history
		// is marked with, and there is no other place the difference is written down.
		if by := tools.sentBy(); by != "Claude Desktop" {
			t.Errorf("the send was attributed to %q, want the agent that asked", by)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the send never finished")
	}
}

func TestASendThePersonRefusedDoesNotGoOut(t *testing.T) {
	port := freePort(t)
	tools := &fakeTools{}
	bus := newRecorder()
	held := on(port)
	held.Ask = true
	server := start(t, tools, held, bus)

	session := dial(t, port, "sk-sch-testtoken", "Cursor")

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := session.CallTool(ctx,
			&sdk.CallToolParams{Name: "send_request", Arguments: map[string]any{"request": "r-1"}})
		if err == nil && !result.IsError {
			done <- errors.New("the send was not refused")
			return
		}
		done <- nil
	}()

	var ask ConfirmRequest
	select {
	case ask = <-bus.confirms:
	case <-time.After(5 * time.Second):
		t.Fatal("the window was never asked")
	}
	server.Answer(ask.ID, false)

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the answer never reached the agent")
	}
	if sent := tools.sentIDs(); len(sent) != 0 {
		t.Errorf("sent = %+v, want nothing", sent)
	}
}

// A server that is off opens nothing: the port is not even bound.
func TestAServerThatIsOffListensToNothing(t *testing.T) {
	port := freePort(t)
	held := on(port)
	held.Enabled = false
	server := start(t, &fakeTools{}, held, newRecorder())

	if status := server.Status(); status.Running || status.Busy {
		t.Errorf("status = %+v, want a server that is off", status)
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("the port was already taken: %v", err)
	}
	_ = listener.Close()
}

// A port that belongs to something else is a state, not a failure: the settings are kept, the
// window is told, and nothing of the app's is broken.
func TestAPortThatIsTakenIsDrawnRatherThanRefused(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupying a port: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	server := start(t, &fakeTools{}, on(port), newRecorder())
	status := server.Status()
	if status.Running || !status.Busy {
		t.Fatalf("status = %+v, want it drawn as busy", status)
	}

	// The port becomes free: the next apply takes it.
	_ = listener.Close()
	if err := server.Apply(on(port)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if status := server.Status(); !status.Running || status.Busy {
		t.Errorf("status = %+v, want it running", status)
	}
}

// The reads an agent is offered are masked before they leave the app — the tool's own answer
// carries the mask, not a secret.
func TestAReadCarriesTheMaskAndNotTheValue(t *testing.T) {
	port := freePort(t)
	start(t, &fakeTools{}, on(port), newRecorder())

	session := dial(t, port, "sk-sch-testtoken", "Cursor")

	listed := structured(t, call(t, session, "requests", map[string]any{"collection": "c-1"}))
	if !strings.Contains(listed, "••••") {
		t.Errorf("the listing carries no mask: %s", listed)
	}

	one := structured(t, call(t, session, "requests", map[string]any{"request": "r-1"}))
	if !strings.Contains(one, "••••") || strings.Contains(one, "Bearer") {
		t.Errorf("a single request = %s, want it masked", one)
	}
}

// structured is the answer as an agent reads it — the JSON the tool put in content.
func structured(t *testing.T, result *sdk.CallToolResult) string {
	t.Helper()
	var text string
	for _, content := range result.Content {
		if piece, ok := content.(*sdk.TextContent); ok {
			text += piece.Text
		}
	}
	return text
}
