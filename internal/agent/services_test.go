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
	mask := domain.SecretMask
	if _, err := tools.UpdateRequest(ctx, RequestPatch{IDInput: id, RequestFields: RequestFields{Body: &mask}}); err == nil {
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
		Name: &name, Environment: &env.ID,
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
	workspace := WorkspaceInput{Workspace: "other"}
	created, err := tools.CreateEnvironment(ctx, EnvironmentInput{WorkspaceInput: workspace, Name: "Testing"})
	if err != nil {
		t.Fatal(err)
	}
	variable, err := tools.SetVariable(ctx, VariableInput{WorkspaceInput: workspace, Environment: created.ID, Name: "token", Value: "first-secret", Secret: true})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := tools.SetVariable(ctx, VariableInput{WorkspaceInput: workspace, Environment: created.ID, Name: "token", Value: "replacement-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != variable.ID {
		t.Fatal("setting an existing variable created a duplicate")
	}
	state, err := tools.Environments(ctx, workspace)
	if err != nil {
		t.Fatal(err)
	}
	value := state.Environments[0].Vars[0]
	if value.Value != "" || value.Kind != domain.VariableSecret || !value.HasValue {
		t.Fatalf("secret metadata = %+v", value)
	}
	if _, err := tools.SetVariable(ctx, VariableInput{WorkspaceInput: workspace, Environment: created.ID, Name: "token", Value: domain.SecretMask}); err == nil {
		t.Fatal("a secret mask overwrote the value")
	}
	if _, err := tools.DeleteVariable(ctx, VariableIDInput{WorkspaceInput: WorkspaceInput{Workspace: domain.WorkspacePersonalID}, Environment: created.ID, ID: variable.ID}); err == nil {
		t.Fatal("a foreign variable was deleted")
	}
	if err := tools.editEnvironment(ctx, workspace.Workspace, created.ID, func(env *domain.Environment) { env.Readonly = true }); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.SetVariable(ctx, VariableInput{WorkspaceInput: workspace, Environment: created.ID, Name: "token", Value: "blocked"}); err == nil {
		t.Fatal("MCP edited a read-only environment")
	}
	if _, err := tools.DeleteEnvironment(ctx, IDInput{WorkspaceInput: workspace, ID: created.ID}); err != nil {
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
