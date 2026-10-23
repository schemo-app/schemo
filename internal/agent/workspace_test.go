package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"schemo/internal/domain"
)

type fakeWorkspaceTools struct {
	WorkspaceTools
	calls atomic.Int32
}

type fakeAccountTools struct {
	fakeTools
	changed atomic.Bool
	sends   atomic.Int32
}

func (f *fakeAccountTools) Workspaces(ctx context.Context) ([]Workspace, error) {
	if f.changed.Load() {
		return []Workspace{{ID: "new-account"}}, nil
	}
	return f.fakeTools.Workspaces(ctx)
}

func (f *fakeAccountTools) Request(ctx context.Context, id string) (Request, error) {
	if id == "previous-request" {
		return Request{ID: id, Workspace: "previous-account", Name: "Private"}, nil
	}
	return f.fakeTools.Request(ctx, id)
}

func (f *fakeAccountTools) Requests(ctx context.Context, id string) ([]Request, error) {
	if id == "previous-collection" {
		return []Request{{ID: "previous-request", Workspace: "previous-account", Name: "Private"}}, nil
	}
	return f.fakeTools.Requests(ctx, id)
}

func (f *fakeAccountTools) Response(ctx context.Context, id string) (Response, error) {
	if id == "previous-record" {
		return Response{Workspace: "previous-account", Body: "Private"}, nil
	}
	return f.fakeTools.Response(ctx, id)
}

func (f *fakeAccountTools) Send(ctx context.Context, id, agent string) (Response, error) {
	f.sends.Add(1)
	return f.fakeTools.Send(ctx, id, agent)
}

func TestAllWorkspacesDoesNotExposeAnotherAccountsCachedIDs(t *testing.T) {
	port := freePort(t)
	held := on(port)
	tools := &fakeAccountTools{}
	start(t, tools, held, newRecorder())
	session := dial(t, port, held.Token, "Account agent")
	for _, test := range []struct {
		tool string
		args map[string]any
	}{
		{"requests", map[string]any{"request": "previous-request"}},
		{"history", map[string]any{"record": "previous-record"}},
		{"collections", map[string]any{"workspace": "previous-account"}},
		{"send_request", map[string]any{"request": "previous-request"}},
	} {
		if out := call(t, session, test.tool, test.args); !out.IsError {
			t.Errorf("%s exposed a previous account's object", test.tool)
		}
	}
	result := call(t, session, "requests", map[string]any{"collection": "previous-collection"})
	var out requestsOut
	if err := json.Unmarshal([]byte(structured(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Requests) != 0 {
		t.Fatal("a collection listing exposed previous account requests")
	}
	if tools.sends.Load() != 0 {
		t.Fatal("an inaccessible request reached the sender")
	}
}

func TestApprovedSendRechecksAccountAndPermissions(t *testing.T) {
	for _, change := range []string{"account", "sending", "scope"} {
		t.Run(change, func(t *testing.T) {
			port := freePort(t)
			held := on(port)
			held.Ask = true
			tools := &fakeAccountTools{}
			bus := newRecorder()
			server := start(t, tools, held, bus)
			session := dial(t, port, held.Token, "Approval agent")
			type outcome struct {
				result *sdk.CallToolResult
				err    error
			}
			done := make(chan outcome, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			go func() {
				result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "send_request", Arguments: map[string]any{"request": "r-1"}})
				done <- outcome{result, err}
			}()
			var ask ConfirmRequest
			select {
			case ask = <-bus.confirms:
			case <-ctx.Done():
				t.Fatal("approval was never requested")
			}
			switch change {
			case "account":
				tools.changed.Store(true)
			case "sending":
				held.Mode = domain.MCPRead
			case "scope":
				held.AllWorkspaces, held.Workspaces = false, []string{}
			}
			if err := server.Apply(held); err != nil {
				t.Fatal(err)
			}
			server.Answer(ask.ID, true)
			select {
			case got := <-done:
				if got.err == nil && (got.result == nil || !got.result.IsError) {
					t.Fatal("approval bypassed changed account or permissions")
				}
			case <-ctx.Done():
				t.Fatal("approved send did not return")
			}
			if tools.sends.Load() != 0 {
				t.Fatal("a revoked send reached the sender")
			}
		})
	}
}

func (f *fakeWorkspaceTools) CreateCollection(_ context.Context, in CollectionInput) (IDOutput, error) {
	f.calls.Add(1)
	return IDOutput{ID: in.Workspace + "/" + in.Name}, nil
}

func TestEditingToolsRequireOptInAndRecheckWorkspacePolicy(t *testing.T) {
	port := freePort(t)
	held := on(port)
	backend := &fakeWorkspaceTools{}
	server := start(t, &fakeTools{WorkspaceTools: backend}, held, newRecorder())
	session := dial(t, port, held.Token, "Workspace agent")
	count := func() int {
		t.Helper()
		n := 0
		for _, err := range session.Tools(context.Background(), nil) {
			if err != nil {
				t.Fatal(err)
			}
			n++
		}
		return n
	}
	if got := count(); got != 7 {
		t.Fatalf("default tools = %d, want 7", got)
	}
	held.Write, held.AllWorkspaces, held.Workspaces = true, false, []string{"s-personal"}
	if err := server.Apply(held); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 20 {
		t.Fatalf("editing tools = %d, want 20", got)
	}
	for _, workspace := range []string{"s-team", "missing"} {
		if out := call(t, session, "create_collection", map[string]any{"workspace": workspace, "name": "API"}); !out.IsError {
			t.Fatalf("create in %q unexpectedly succeeded", workspace)
		}
	}
	if backend.calls.Load() != 0 {
		t.Fatal("a denied workspace reached the backend")
	}
	if out := call(t, session, "create_collection", map[string]any{"workspace": "s-personal", "name": "API"}); out.IsError {
		t.Fatalf("allowed create failed: %+v", out)
	}
	if backend.calls.Load() != 1 {
		t.Fatal("allowed edit did not reach the backend")
	}
	// An already discovered tool must enforce the new policy even before the list is refreshed.
	held.Write = false
	server.policy.Store(&held)
	if out := call(t, session, "create_collection", map[string]any{"workspace": "s-personal", "name": "API"}); !out.IsError {
		t.Fatal("a cached tool bypassed revoked edit access")
	}
	if backend.calls.Load() != 1 {
		t.Fatal("a revoked edit reached the backend")
	}
	if err := server.Apply(held); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 7 {
		t.Fatalf("tools after revocation = %d, want 7", got)
	}
	held.Mode, held.Write = domain.MCPRead, true
	if err := server.Apply(held); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 19 {
		t.Fatalf("editing without sending = %d tools, want 19", got)
	}
}
