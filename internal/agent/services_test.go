package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"schemo/internal/db"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/internal/request"
	"schemo/internal/workspace"
)

// A captured request is kept as the browser sent it — that is what makes it worth having — so the
// agent's surface is the one place it must not read the value out again. An address is where a
// page's own token sits and a header is where a session comes back; both are masked by value.
func TestAgentReadsNoSecretOutOfACapture(t *testing.T) {
	const secret = "captured-token-value"

	store := dbtest.New(t)
	ctx := context.Background()

	// A global secret is what both readers below are given to find: it applies whatever environment
	// is active, which is the one thing a history row cannot say about itself.
	if err := store.SaveVariable(ctx, domain.WorkspacePersonalID, domain.EnvScope{}, domain.Variable{
		ID: "var-1", Name: "token", Value: secret, Kind: domain.VariableSecret, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}

	records := request.NewHistory(request.HistoryDeps{Store: store, Scope: store, Notifier: quiet{},
		IDs: platform.NewIDGen(), Build: platform.BuildInfo{Name: "Schemo"}})

	captured, err := records.Ingest(ctx, request.IngestInput{
		Method:          "GET",
		URL:             "https://api.example.com/things?token=" + secret,
		Status:          200,
		ResponseHeaders: []domain.HeaderPair{{Name: "Set-Cookie", Value: "session=" + secret}},
		ResponseBody:    strings.Repeat(".", maxAgentBody-4) + secret + strings.Repeat(".", 32),
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	// The store holds what the browser sent: hiding a value is the reader's business, and a capture
	// written down masked would be one that cannot be replayed.
	if !strings.Contains(captured.URL, secret) {
		t.Fatalf("stored url = %q, want the address as it arrived", captured.URL)
	}

	tools := appTools{
		history:      records,
		environments: workspace.NewEnvironments(store, store, platform.NewIDGen()),
	}

	rows, err := tools.History(ctx, domain.WorkspacePersonalID, 10)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want the one capture", rows)
	}
	if strings.Contains(rows[0].URL, secret) {
		t.Errorf("agent's url = %q, want the secret masked", rows[0].URL)
	}

	answer, err := tools.Response(ctx, captured.ID)
	if err != nil {
		t.Fatalf("Response: %v", err)
	}
	if len(answer.Headers) != 1 {
		t.Fatalf("headers = %+v, want the one the server sent", answer.Headers)
	}
	if strings.Contains(answer.Headers[0].Value, secret) {
		t.Errorf("agent's header = %q, want the secret masked", answer.Headers[0].Value)
	}
	if !answer.Truncated || len(answer.Body) > maxAgentBody {
		t.Fatal("a large response was not bounded and marked as truncated")
	}
	if strings.Contains(answer.Body, "capt") {
		t.Fatal("truncating a response disclosed the prefix of a secret")
	}
}

func workspaceTools(t *testing.T) (appTools, *db.Store) {
	t.Helper()
	store := dbtest.New(t)
	ctx := context.Background()
	if err := store.SaveWorkspace(ctx, domain.NewWorkspace("other", "Other", domain.WorkspacePersonal, "", 1)); err != nil {
		t.Fatal(err)
	}
	return appTools{
		collections:  workspace.NewCollections(store, store, platform.NewIDGen()),
		environments: workspace.NewEnvironments(store, store, platform.NewIDGen()),
	}, store
}

func TestMCPEditsStayInTheirWorkspaceAndPreserveSavedRequestFields(t *testing.T) {
	tools, _ := workspaceTools(t)
	ctx := context.Background()
	workspace := WorkspaceInput{Workspace: "other"}
	created, err := tools.CreateCollection(ctx, CollectionInput{WorkspaceInput: workspace, Name: "API"})
	if err != nil {
		t.Fatal(err)
	}
	changedName, invalidDescription := "Changed", strings.Repeat("x", 8001)
	if _, err := tools.UpdateCollection(ctx, CollectionPatch{
		IDInput: IDInput{WorkspaceInput: workspace, ID: created.ID}, Name: &changedName, Description: &invalidDescription,
	}); err == nil {
		t.Fatal("an invalid collection description was accepted")
	}
	unchanged, err := tools.GetCollection(ctx, IDInput{WorkspaceInput: workspace, ID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Collection.Name != "API" {
		t.Fatal("an invalid compound edit partially renamed the collection")
	}
	name, address, description := "List", "https://example.test", "## Responses"
	request, err := tools.CreateRequest(ctx, RequestInput{
		WorkspaceInput: workspace, Collection: created.ID,
		RequestFields: RequestFields{Name: &name, URL: &address, Description: &description},
	})
	if err != nil {
		t.Fatal(err)
	}
	node, err := tools.collections.Node(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	node.Auth = &domain.Auth{Type: domain.AuthBearer, Fields: map[string]string{"token": "private-token"}}
	node.Headers = []domain.Row{{ID: "header", Name: "Authorization", Value: "Bearer literal-token", Enabled: true}}
	if err := tools.collections.InWorkspace("other").SaveNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	newName := "Renamed"
	id := IDInput{WorkspaceInput: workspace, ID: request.ID}
	if _, err := tools.UpdateRequest(ctx, RequestPatch{IDInput: id, RequestFields: RequestFields{Name: &newName}}); err != nil {
		t.Fatal(err)
	}
	stored, err := tools.collections.Node(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Auth.Answer("token") != "private-token" || stored.Headers[0].Value != "Bearer literal-token" || stored.Description != description {
		t.Fatal("a partial edit overwrote omitted request fields")
	}
	read, err := tools.Request(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(read)
	if !strings.Contains(string(raw), "literal-token") || !strings.Contains(string(raw), "private-token") {
		t.Fatal("MCP withheld saved inline API credentials")
	}
	if _, err := tools.DeleteRequest(ctx, IDInput{WorkspaceInput: WorkspaceInput{Workspace: domain.WorkspacePersonalID}, ID: request.ID}); err == nil {
		t.Fatal("a request was deleted through another workspace")
	}
	if _, err := tools.MoveRequest(ctx, MoveInput{IDInput: id, Parent: "missing"}); err == nil {
		t.Fatal("a request moved into a missing collection")
	}
	selected, err := tools.collections.Tree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 0 {
		t.Fatal("MCP switched or wrote into the active desktop workspace")
	}
	mask, err := json.Marshal(domain.SecretMask)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tools.UpdateRequest(ctx, RequestPatch{IDInput: id, RequestFields: RequestFields{Body: mask}}); err == nil {
		t.Fatal("a masked value was accepted as editable content")
	}
	if _, err := tools.DeleteCollection(ctx, IDInput{WorkspaceInput: workspace, ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.collections.Node(ctx, request.ID); err == nil {
		t.Fatal("deleting a collection left its request behind")
	}
}

func TestMCPAuthorizationCanBeEditedWhileSecretVariableValuesStayProtected(t *testing.T) {
	tools, _ := workspaceTools(t)
	ctx := context.Background()
	workspace := WorkspaceInput{Workspace: "other"}
	env, err := tools.CreateEnvironment(ctx, EnvironmentInput{WorkspaceInput: workspace, Name: "Testing"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tools.SetVariable(ctx, VariableInput{WorkspaceInput: workspace, Environment: env.ID, Name: "credential", Value: "protected-value", Secret: true}); err != nil {
		t.Fatal(err)
	}
	level, err := tools.CreateCollection(ctx, CollectionInput{WorkspaceInput: workspace, Name: "Authorized"})
	if err != nil {
		t.Fatal(err)
	}
	levelID := IDInput{WorkspaceInput: workspace, ID: level.ID}
	auth := &domain.Auth{Type: domain.AuthBearer, Fields: map[string]string{"token": "{{credential}}"}}
	if _, err := tools.UpdateCollection(ctx, CollectionPatch{IDInput: levelID, Auth: auth}); err != nil {
		t.Fatal(err)
	}
	collection, err := tools.GetCollection(ctx, levelID)
	if err != nil {
		t.Fatal(err)
	}
	if collection.Collection.Auth.Answer("token") != "{{credential}}" {
		t.Fatal("collection authorization lost its variable reference")
	}
	name := "Read credentials"
	created, err := tools.CreateRequest(ctx, RequestInput{WorkspaceInput: workspace, Collection: level.ID, RequestFields: RequestFields{
		Name: &name,
		Auth: &domain.Auth{Type: domain.AuthBearer, Fields: map[string]string{"token": "protected-value"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	read, err := tools.Request(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Auth.Answer("token") != domain.SecretMask {
		t.Fatal("MCP exposed a protected variable value in saved authorization")
	}
	patch := RequestPatch{IDInput: IDInput{WorkspaceInput: workspace, ID: created.ID}, RequestFields: RequestFields{Auth: read.Auth}}
	if _, err := tools.UpdateRequest(ctx, patch); err == nil {
		t.Fatal("masked authorization was persisted")
	}
	patch.Auth = &domain.Auth{Type: domain.AuthBasic, Fields: map[string]string{"username": "demo", "password": "inline-password"}}
	if _, err := tools.UpdateRequest(ctx, patch); err != nil {
		t.Fatal(err)
	}
	read, err = tools.Request(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Auth.Type != domain.AuthBasic || read.Auth.Answer("password") != "inline-password" {
		t.Fatal("inline authorization did not round-trip")
	}
	patch.Auth = &domain.Auth{Type: domain.AuthInherit}
	if _, err := tools.UpdateRequest(ctx, patch); err != nil {
		t.Fatal(err)
	}
	read, err = tools.Request(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Auth.Type != domain.AuthInherit {
		t.Fatal("authorization did not return to inheritance")
	}
}

func TestMCPEnvironmentEditsNeverReturnOrDowngradeSecrets(t *testing.T) {
	tools, _ := workspaceTools(t)
	ctx := context.Background()
	scope := WorkspaceInput{Workspace: "other"}
	created, err := tools.CreateEnvironment(ctx, EnvironmentInput{WorkspaceInput: scope, Name: "Testing"})
	if err != nil {
		t.Fatal(err)
	}
	variable, err := tools.SetVariable(ctx, VariableInput{WorkspaceInput: scope, Environment: created.ID, Name: "token", Value: "first-secret", Secret: true})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := tools.SetVariable(ctx, VariableInput{WorkspaceInput: scope, Environment: created.ID, Name: "token", Value: "replacement-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != variable.ID {
		t.Fatal("setting an existing variable created a duplicate")
	}
	state, err := tools.Environments(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	value := state.Environments[0].Vars[0]
	if value.Value != "" || value.Kind != domain.VariableSecret || !value.HasValue {
		t.Fatalf("secret metadata = %+v", value)
	}
	if _, err := tools.SetVariable(ctx, VariableInput{WorkspaceInput: scope, Environment: created.ID, Name: "token", Value: domain.SecretMask}); err == nil {
		t.Fatal("a secret mask overwrote the value")
	}
	if _, err := tools.DeleteVariable(ctx, VariableIDInput{WorkspaceInput: WorkspaceInput{Workspace: domain.WorkspacePersonalID}, Environment: created.ID, ID: variable.ID}); err == nil {
		t.Fatal("a foreign variable was deleted")
	}
	readonly := true
	if err := tools.environments.InWorkspace(scope.Workspace).Update(ctx, created.ID, workspace.EnvironmentPatch{Readonly: &readonly}); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.SetVariable(ctx, VariableInput{WorkspaceInput: scope, Environment: created.ID, Name: "token", Value: "blocked"}); err == nil {
		t.Fatal("MCP edited a read-only environment")
	}
	if _, err := tools.DeleteEnvironment(ctx, IDInput{WorkspaceInput: scope, ID: created.ID}); err != nil {
		t.Fatal(err)
	}
}

func TestMCPWritesRespectWorkspaceRolesAndRollback(t *testing.T) {
	tools, store := workspaceTools(t)
	ctx := context.Background()
	workspace := WorkspaceInput{Workspace: "other"}
	created, err := tools.CreateCollection(ctx, CollectionInput{WorkspaceInput: workspace, Name: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Link(ctx, workspace.Workspace, domain.RemoteWorkspace{ID: "remote", Access: domain.WorkspaceAccess{RoleName: "viewer"}}, "account"); err != nil {
		t.Fatal(err)
	}
	name := "Forbidden"
	if _, err := tools.UpdateCollection(ctx, CollectionPatch{IDInput: IDInput{WorkspaceInput: workspace, ID: created.ID}, Name: &name}); err == nil {
		t.Fatal("MCP bypassed workspace read-only access")
	}
	if _, err := tools.CreateEnvironment(ctx, EnvironmentInput{WorkspaceInput: workspace, Name: "Forbidden"}); err == nil {
		t.Fatal("MCP created an environment in a read-only workspace")
	}
	got, err := tools.GetCollection(ctx, IDInput{WorkspaceInput: workspace, ID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Collection.Name != "Original" {
		t.Fatal("a rejected edit was not rolled back")
	}
}

// quiet is a Notifier nobody listens to.
type quiet struct{}

func (quiet) Publish(string, any) {}

type requestBodyTools struct{ appTools }

func (requestBodyTools) Workspaces(context.Context) ([]Workspace, error) {
	return []Workspace{{ID: "other", Name: "Other", Kind: "personal"}}, nil
}

func TestMCPRequestBodies(t *testing.T) {
	tools, _ := workspaceTools(t)
	ctx := context.Background()
	collection, err := tools.CreateCollection(ctx, CollectionInput{WorkspaceInput: WorkspaceInput{Workspace: "other"}, Name: "API"})
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	settings := on(port)
	settings.Write = true
	start(t, requestBodyTools{tools}, settings, newRecorder())
	session := dial(t, port, settings.Token, "JSON agent")
	created := call(t, session, "create_request", map[string]any{
		"workspace": "other", "collection": collection.ID, "name": "Create role",
		"method": "POST", "url": "https://example.test/roles", "bodyKind": "json",
		"body": json.RawMessage(`{"code":"admin","permissions":["read"],"id":9007199254740993}`),
	})
	if created.IsError {
		t.Fatalf("create_request: %+v", created)
	}
	var id IDOutput
	if err := json.Unmarshal([]byte(structured(t, created)), &id); err != nil {
		t.Fatal(err)
	}
	check := func(want string, kind domain.BodyKind) {
		t.Helper()
		node, err := tools.collections.Node(ctx, id.ID)
		if err != nil {
			t.Fatal(err)
		}
		if node.Body != want || node.BodyKind != kind {
			t.Errorf("saved body = %q (%s), want %q (%s)", node.Body, node.BodyKind, want, kind)
		}
	}
	check(`{"code":"admin","permissions":["read"],"id":9007199254740993}`, domain.BodyJSON)
	for _, test := range []struct {
		name      string
		fields    map[string]any
		want      string
		kind      domain.BodyKind
		wantError bool
	}{
		{"array with saved kind", map[string]any{"body": json.RawMessage(`["admin",{"enabled":true}]`)}, `["admin",{"enabled":true}]`, domain.BodyJSON, false},
		{"serialized JSON text", map[string]any{"body": `{"code":"admin"}`}, `{"code":"admin"}`, domain.BodyJSON, false},
		{"template", map[string]any{"body": `{"code":"admin","permissions":[{{perms}}]}`}, `{"code":"admin","permissions":[{{perms}}]}`, domain.BodyJSON, false},
		{"omitted", map[string]any{"name": "Renamed"}, `{"code":"admin","permissions":[{{perms}}]}`, domain.BodyJSON, false},
		{"null preserves", map[string]any{"body": nil}, `{"code":"admin","permissions":[{{perms}}]}`, domain.BodyJSON, false},
		{"clear", map[string]any{"body": ""}, "", domain.BodyJSON, false},
		{"number", map[string]any{"body": json.RawMessage(`9007199254740993`)}, "9007199254740993", domain.BodyJSON, false},
		{"boolean", map[string]any{"body": false}, "false", domain.BodyJSON, false},
		{"literal null", map[string]any{"body": "null"}, "null", domain.BodyJSON, false},
		{"JSON string literal", map[string]any{"body": `"hello"`}, `"hello"`, domain.BodyJSON, false},
		{"masked object", map[string]any{"body": map[string]any{"token": domain.SecretMask}}, `"hello"`, domain.BodyJSON, true},
		{"raw text", map[string]any{"bodyKind": "raw", "body": "hello"}, "hello", domain.BodyRaw, false},
		{"object requires json", map[string]any{"body": map[string]any{"code": "admin"}}, "hello", domain.BodyRaw, true},
		{"XML text", map[string]any{"bodyKind": "xml", "body": "<code>admin</code>"}, "<code>admin</code>", domain.BodyXML, false},
		{"array requires json", map[string]any{"bodyKind": "xml", "body": []string{"admin"}}, "<code>admin</code>", domain.BodyXML, true},
		{"switch to json", map[string]any{"bodyKind": "json", "body": map[string]any{"code": "admin"}}, `{"code":"admin"}`, domain.BodyJSON, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.fields["workspace"], test.fields["id"] = "other", id.ID
			out := call(t, session, "update_request", test.fields)
			if out.IsError != test.wantError {
				t.Fatalf("update_request(%v) = %+v, want error %v", test.fields, out, test.wantError)
			}
			check(test.want, test.kind)
		})
	}
}

func TestMCPFormAndBinaryBodies(t *testing.T) {
	tools, _ := workspaceTools(t)
	ctx := context.Background()
	scope := WorkspaceInput{Workspace: "other"}
	collection, err := tools.CreateCollection(ctx, CollectionInput{WorkspaceInput: scope, Name: "Forms"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tools.SetVariable(ctx, VariableInput{WorkspaceInput: scope, Name: "token", Value: "private-form-token", Secret: true}); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	settings := on(port)
	settings.Write = true
	start(t, requestBodyTools{tools}, settings, newRecorder())
	session := dial(t, port, settings.Token, "Form agent")
	created := call(t, session, "create_request", map[string]any{
		"workspace": "other", "collection": collection.ID, "name": "Form request",
		"method": "POST", "url": "https://example.test/form", "bodyKind": "form",
		"form": []Header{{Name: "tag", Value: "one"}, {Name: "tag", Value: "{{tag}}"}, {Name: "token", Value: "private-form-token"}},
	})
	if created.IsError {
		t.Fatalf("create form: %s", structured(t, created))
	}
	var id IDOutput
	if err := json.Unmarshal([]byte(structured(t, created)), &id); err != nil {
		t.Fatal(err)
	}
	node, err := tools.collections.Node(ctx, id.ID)
	if err != nil {
		t.Fatal(err)
	}
	if node.BodyKind != domain.BodyForm || len(node.Form) != 3 || node.Form[0].ID == "" || node.Form[1].Value != "{{tag}}" {
		t.Fatalf("created form = %+v", node.Form)
	}
	node.Form = append(node.Form,
		domain.FormRow{ID: "upload", Name: "upload", File: true, Src: "/not-read/upload.bin", Enabled: true},
		domain.FormRow{ID: "disabled", Name: "disabled", Value: "kept"},
	)
	if err := tools.collections.InWorkspace("other").SaveNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	read := call(t, session, "requests", map[string]any{"request": id.ID})
	var result requestsOut
	if err := json.Unmarshal([]byte(structured(t, read)), &result); err != nil {
		t.Fatal(err)
	}
	if result.Request == nil {
		t.Fatalf("request read: %s", structured(t, read))
	}
	request := result.Request
	if len(request.Form) != 3 || request.Form[2].Value != domain.SecretMask || strings.Contains(structured(t, read), "/not-read/") {
		t.Fatalf("read form did not mask secrets or exclude file parts: %+v", request.Form)
	}
	update := func(fields map[string]any, wantError bool) domain.CollectionNode {
		t.Helper()
		fields["workspace"], fields["id"] = "other", id.ID
		out := call(t, session, "update_request", fields)
		if out.IsError != wantError {
			t.Fatalf("update = %s, want error %v", structured(t, out), wantError)
		}
		node, err := tools.collections.Node(ctx, id.ID)
		if err != nil {
			t.Fatal(err)
		}
		return node
	}
	node = update(map[string]any{"bodyKind": "form", "name": "Renamed"}, false)
	if len(node.Form) != 5 || node.Form[3].Src != "/not-read/upload.bin" {
		t.Fatal("repeating form mode erased saved parts")
	}
	node = update(map[string]any{"form": []Header{{Name: "code", Value: "admin"}}}, false)
	if len(node.Form) != 3 || node.Form[0].ID != "upload" || node.Form[1].Enabled || node.Form[2].Value != "admin" {
		t.Fatalf("form replacement lost protected parts: %+v", node.Form)
	}
	node = update(map[string]any{"form": []Header{{Name: "token", Value: domain.SecretMask}}}, true)
	if len(node.Form) != 3 || node.Form[2].Value != "admin" {
		t.Fatal("masked form edit changed saved data")
	}
	update(map[string]any{"body": "ignored text"}, true)
	node = update(map[string]any{"form": []Header{}}, false)
	if len(node.Form) != 2 || node.Form[0].ID != "upload" || node.Form[1].ID != "disabled" {
		t.Fatal("clearing text form fields erased other parts")
	}
	node = update(map[string]any{"bodyKind": "binary"}, false)
	if len(node.Form) != 0 {
		t.Fatal("switching body mode retained form fields")
	}
	node.BodyFile = "/not-read/saved.bin"
	if err := tools.collections.InWorkspace("other").SaveNode(ctx, node); err != nil {
		t.Fatal(err)
	}
	for _, fields := range []map[string]any{{"name": "Binary"}, {"bodyKind": "binary"}} {
		node = update(fields, false)
		if node.BodyFile != "/not-read/saved.bin" {
			t.Fatal("partial edit erased the saved binary file")
		}
	}
	update(map[string]any{"body": "ignored bytes"}, true)
	update(map[string]any{"form": []Header{{Name: "code", Value: "admin"}}}, true)
	node = update(map[string]any{"bodyKind": "raw", "body": "text"}, false)
	if node.BodyFile != "" || node.Body != "text" {
		t.Fatal("switching to raw did not clear the binary file")
	}
}
