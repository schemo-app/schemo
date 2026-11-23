package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"schemo/internal/convert"
	"schemo/internal/domain"
	"schemo/internal/request"
	"schemo/internal/workspace"
)

func TestTwoDevicesShareACollection(t *testing.T) {
	cloud := newFakeCloud(t)
	laptop, desktop := newDevice(t), newDevice(t)
	laptop.signIn(cloud, "ann")
	desktop.signIn(cloud, "ann")

	created, err := laptop.Collections.CreateCollection(t.Context(), "Billing", "", "")
	if err != nil {
		t.Fatal(err)
	}
	laptop.sync()
	desktop.sync()

	tree, err := desktop.Collections.Tree(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 || tree[0].ID != created.ID || tree[0].Name != "Billing" {
		t.Fatalf("desktop tree = %+v", tree)
	}
	if laptop.keyring.len() != 1 {
		t.Fatalf("keyring holds %d entries, want the refresh token only", laptop.keyring.len())
	}
}

func TestReorderedCollectionsKeepTheirRequestsForOtherMembers(t *testing.T) {
	cloud := newFakeCloud(t)
	owner, member := newDevice(t), newDevice(t)
	owner.signIn(cloud, "ann")
	ctx := t.Context()
	first, err := owner.Collections.CreateCollection(ctx, "First", "", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := owner.Collections.CreateCollection(ctx, "Second", "", "")
	if err != nil {
		t.Fatal(err)
	}
	folder, err := owner.Collections.CreateCollection(ctx, "Folder", "", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	const count = 205
	for i := range count {
		draft := domain.NewDraft()
		draft.URL = fmt.Sprintf("https://example.test/items/%d", i)
		if _, err := owner.Drafts.SaveAsNode(ctx, folder.ID, fmt.Sprintf("Item %d", i), draft); err != nil {
			t.Fatal(err)
		}
	}
	owner.sync()
	cloud.mu.Lock()
	for _, workspace := range cloud.workspaces {
		workspace.members["bob"] = true
	}
	cloud.mu.Unlock()
	member.signIn(cloud, "bob")
	member.sync()
	check := func(receiver *device, parent, other string) {
		t.Helper()
		workspaces, err := receiver.Store.SyncWorkspaces(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, workspace := range workspaces {
			tree, err := receiver.Collections.TreeOf(ctx, workspace.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(tree) == 0 {
				continue
			}
			if len(tree) != 2 {
				t.Fatalf("member tree has %d roots, want 2", len(tree))
			}
			if tree[0].ID != parent || tree[1].ID != other {
				t.Fatalf("member roots = %s, %s, want %s, %s", tree[0].ID, tree[1].ID, parent, other)
			}
			if len(tree[0].Children) != 1 || tree[0].Children[0].ID != folder.ID {
				t.Fatalf("member collection %s is missing folder %s", parent, folder.ID)
			}
			if got := len(tree[0].Children[0].Items); got != count {
				t.Fatalf("member folder has %d requests, want %d", got, count)
			}
			for i, row := range tree[0].Children[0].Items {
				node, err := receiver.Collections.Node(ctx, row.ID)
				if err != nil || node.URL != fmt.Sprintf("https://example.test/items/%d", i) {
					t.Fatalf("member request %d = %+v, %v", i, node, err)
				}
			}
			return
		}
		t.Fatal("member did not receive the shared collections")
	}
	check(member, first.ID, second.ID)
	if err := owner.Collections.Move(ctx, second.ID, "", 0); err != nil {
		t.Fatal(err)
	}
	if err := owner.Collections.Move(ctx, folder.ID, second.ID, 0); err != nil {
		t.Fatal(err)
	}
	owner.sync()
	member.sync()
	check(member, second.ID, first.ID)

	fresh := newDevice(t)
	fresh.signIn(cloud, "bob")
	fresh.sync()
	check(fresh, second.ID, first.ID)
}

func TestInspectorSuggestsASavedRequest(t *testing.T) {
	t.Parallel()
	device := newDevice(t)
	ctx := t.Context()

	const template = "https://api.example.test/products/:id"
	draft := domain.NewDraft()
	draft.URL = template
	saved, err := device.Drafts.SaveAsNewCollection(ctx, "Catalog", "Read product", draft)
	if err != nil {
		t.Fatalf("save the request: %v", err)
	}
	opened, err := device.Inspector.Open(`{"data":[{"type":"products","id":"p-101"}]}`, "application/json")
	if err != nil {
		t.Fatalf("open the response: %v", err)
	}

	const path = "$.data[0].id"
	want := domain.Suggestion{
		NodeID: saved.ID,
		Name:   "Read product",
		Method: "GET",
		URL:    template,
		Filled: "https://api.example.test/products/p-101",
	}
	got, err := device.Inspector.Suggest(ctx, opened.ID, path)
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("Suggest(%q) = %+v, %v, want [%+v]", path, got, err, want)
	}
}

func TestASavedRequestSendsWithASecretAndExportsWithoutIt(t *testing.T) {
	got := make(chan string, 1)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	device := newDevice(t)
	ctx := t.Context()
	if _, err := device.Environments.SetInScope(ctx, domain.EnvScope{}, "token", "s3cret", true); err != nil {
		t.Fatal(err)
	}
	draft := domain.NewDraft()
	draft.URL = api.URL + "/invoices"
	draft.Auth = domain.Auth{Type: domain.AuthBearer, Fields: map[string]string{"token": "{{token}}"}}
	node, err := device.Drafts.SaveAsNewCollection(ctx, "Billing", "Invoices", draft)
	if err != nil {
		t.Fatal(err)
	}

	saved, err := device.Drafts.OpenNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := device.Drafts.Send(ctx, saved.Draft); err != nil {
		t.Fatal(err)
	}
	device.eventually("record", func() bool { return device.events.count(request.TopicRequestFinished) == 1 })
	select {
	case header := <-got:
		if header != "Bearer s3cret" {
			t.Fatalf("server saw %q", header)
		}
	default:
		t.Fatalf("server saw nothing: %+v", device.events.last(request.TopicRequestFinished))
	}

	exported, err := device.Transfer.ExportText(ctx, node.CollectionID, string(convert.FormatSchemo), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(exported, "s3cret") || !strings.Contains(exported, "{{token}}") {
		t.Fatalf("export = %s", exported)
	}

	// The exported document comes back as a second collection with the same request.
	preview, err := device.Transfer.PreviewText(ctx, string(convert.FormatSchemo), exported, "")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := device.Transfer.Apply(ctx, preview.Token, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Requests != 1 || len(summary.Tree) != 2 {
		t.Fatalf("import summary = %+v", summary)
	}
}

func TestAnAgentNeverSeesASecretValue(t *testing.T) {
	device := newDevice(t)
	ctx := t.Context()
	if _, err := device.Environments.SetInScope(ctx, domain.EnvScope{}, "token", "s3cret", true); err != nil {
		t.Fatal(err)
	}
	draft := domain.NewDraft()
	draft.URL = "https://api.example/invoices"
	draft.Headers = []domain.Row{{Name: "X-Key", Value: "{{token}}", Enabled: true}}
	node, err := device.Drafts.SaveAsNewCollection(ctx, "Billing", "Invoices", draft)
	if err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	settings := domain.DefaultMCP()
	settings.Enabled, settings.Port, settings.Token, settings.AllWorkspaces = true, port, "agent-token", true
	if err := device.MCP.Apply(settings); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = device.MCP.Shutdown(context.Background()) })

	client := sdk.NewClient(&sdk.Implementation{Name: "agent", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:             fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		HTTPClient:           &http.Client{Transport: bearer("agent-token")},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	for name, args := range map[string]any{
		"requests":     map[string]any{"request": node.ID},
		"environments": map[string]any{"workspace": device.active()},
	} {
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		answer, _ := json.Marshal(result)
		if result.IsError || strings.Contains(string(answer), "s3cret") {
			t.Fatalf("%s answered %s", name, answer)
		}
	}
}

type bearer string

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(b))
	return http.DefaultTransport.RoundTrip(r)
}

// A collection run goes out through the same draft and history as the window's own send: tokens
// filled from the collection's variables, every request recorded, the run written down.
func TestACollectionRunSendsEveryRequestAndRecordsIt(t *testing.T) {
	var hits atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("v") != "2" {
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer api.Close()

	device := newDevice(t)
	ctx := t.Context()
	draft := domain.NewDraft()
	draft.URL = api.URL + "/a?v={{version}}"
	first, err := device.Drafts.SaveAsNewCollection(ctx, "API", "A", draft)
	if err != nil {
		t.Fatal(err)
	}
	variables := []domain.Variable{{ID: "v", Name: "version", Value: "2", Enabled: true}}
	if err := device.Collections.Update(ctx, first.CollectionID,
		workspace.CollectionPatch{Variables: &variables}); err != nil {
		t.Fatal(err)
	}
	if _, err := device.Collections.CreateNode(ctx, workspace.RequestDraft{
		CollectionID: first.CollectionID, Name: "B", Method: "GET", URL: api.URL + "/b?v={{version}}",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := device.Runs.Run(ctx, first.CollectionID, ""); err != nil {
		t.Fatal(err)
	}
	device.eventually("run", func() bool { return device.events.count(request.TopicRunFinished) == 1 })
	run, err := device.Runs.Last(ctx, first.CollectionID, "")
	if err != nil || run == nil {
		t.Fatalf("Last = %v, %v", run, err)
	}
	if run.Passed != 2 || hits.Load() != 2 {
		t.Fatalf("run = %+v, server hits = %d; want both passed", run, hits.Load())
	}
	for _, row := range run.Results {
		if row.RecordID == "" {
			t.Errorf("row %+v has no record", row)
		}
	}
}
