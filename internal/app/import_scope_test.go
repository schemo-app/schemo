package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"schemo/internal/convert"
	"schemo/internal/convert/schemofile"
	"schemo/internal/domain"
	"schemo/internal/workspace"
)

func TestImportedServersUseTheirCollectionSelection(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer api.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer other.Close()

	d := newDevice(t)
	ctx := t.Context()
	windowID, err := d.Environments.Create(ctx, workspace.EnvironmentInput{Name: "Window"})
	if err != nil {
		t.Fatal(err)
	}
	setBase := func(id, address string) {
		t.Helper()
		if _, err := d.Environments.SetInScope(ctx, domain.EnvScope{Environment: id}, domain.BaseURLVariable, address, false); err != nil {
			t.Fatal(err)
		}
	}
	setBase(windowID, api.URL+"/window")
	sharedID, err := d.Environments.Create(ctx, workspace.EnvironmentInput{Name: "Shared"})
	if err != nil {
		t.Fatal(err)
	}
	setBase(sharedID, other.URL+"/shared")
	if err := d.Environments.Activate(ctx, windowID); err != nil {
		t.Fatal(err)
	}

	parent, err := d.Collections.CreateCollection(ctx, "Parent", "", "")
	if err != nil {
		t.Fatal(err)
	}
	windowRequest, err := d.Drafts.SaveAsNode(ctx, parent.ID, "Workspace request", domain.Draft{
		Method: "GET", URL: "{{baseUrl}}/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	variables := []domain.Variable{{Name: domain.BaseURLVariable, Value: other.URL + "/parent", Enabled: true}}
	folder, err := d.Collections.CreateCollection(ctx, "Folder", "", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Collections.Update(ctx, folder.ID, workspace.CollectionPatch{Variables: &variables}); err != nil {
		t.Fatal(err)
	}

	spec := fmt.Sprintf(`{
		"openapi":"3.1.0","info":{"title":"First"},
		"servers":[{"url":%q},{"url":%q,"description":"Staging"}],
		"paths":{
			"/main":{"get":{"responses":{"204":{"description":"OK"}}}},
			"/special":{"servers":[{"url":%q}],"get":{"responses":{"202":{"description":"OK"}}},
				"post":{"servers":[{"url":%q}],"responses":{"204":{"description":"OK"}}}}
		}}`, api.URL+"/first", other.URL+"/staging", other.URL+"/path", api.URL+"/operation")
	legacy := fmt.Sprintf(`{
		"swagger":"2.0","info":{"title":"Second"},"host":%q,"schemes":["http"],"basePath":"/second",
		"paths":{"/main":{"get":{"responses":{"202":{"description":"OK"}}}}}}`, strings.TrimPrefix(other.URL, "http://"))

	applyImport(t, d, "openapi", spec, folder.ID, convert.ImportOptions{})
	applyImport(t, d, "openapi", legacy, "", convert.ImportOptions{})
	tree, err := d.Collections.Tree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, second := tree[0].Children[0].Children[0], tree[1]
	read := func(node domain.CollectionNode) domain.Draft {
		t.Helper()
		opened, err := d.Drafts.OpenNode(ctx, node.ID)
		if err != nil {
			t.Fatal(err)
		}
		return opened.Draft
	}
	main, special, operation := read(first.Items[0]), read(first.Items[1]), read(first.Items[2])
	secondMain := read(second.Items[0])
	check := func(draft domain.Draft, address string, status int) {
		t.Helper()
		rec := send(t, d, draft)
		if rec.URL != address || rec.Status != status {
			t.Errorf("Send URL = %q, status = %d, want %q, %d", rec.URL, rec.Status, address, status)
		}
	}
	check(main, api.URL+"/first/main", http.StatusNoContent)
	check(special, other.URL+"/path/special", http.StatusAccepted)
	check(operation, api.URL+"/operation/special", http.StatusNoContent)
	check(secondMain, other.URL+"/second/main", http.StatusAccepted)
	workspaceDraft := read(windowRequest)
	check(workspaceDraft, api.URL+"/window/workspace", http.StatusNoContent)

	state, err := d.Environments.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var staging string
	for _, env := range state.CollectionEnvironments {
		if env.CollectionID == first.ID && env.Name == "Staging" {
			staging = env.ID
		}
	}
	if staging == "" {
		t.Fatal("missing Staging environment")
	}
	if err := d.Environments.SelectCollection(ctx, first.ID, staging); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Environments.Create(ctx, workspace.EnvironmentInput{Name: "QA", CollectionID: first.ID}); err != nil {
		t.Fatal(err)
	}
	if err := d.Environments.Activate(ctx, sharedID); err != nil {
		t.Fatal(err)
	}
	check(workspaceDraft, other.URL+"/shared/workspace", http.StatusAccepted)
	check(main, other.URL+"/staging/main", http.StatusAccepted)
	check(secondMain, other.URL+"/second/main", http.StatusAccepted)
	if err := d.Environments.SelectCollection(ctx, first.ID, sharedID); err == nil {
		t.Fatal("SelectCollection accepted a workspace environment")
	}
	if err := d.Environments.SelectCollection(ctx, first.ID, ""); err != nil {
		t.Fatal(err)
	}
	check(main, api.URL+"/first/main", http.StatusNoContent)
	held, err := d.Environments.State(ctx)
	if err != nil || held.ActiveID != sharedID || len(held.Environments) != 2 {
		t.Fatal("imports and collection selections must leave the workspace selection and list intact")
	}
}

func TestImportOpenAPIRelativeServer(t *testing.T) {
	t.Parallel()
	const spec = `{
		"openapi":"3.1.0","info":{"title":"Relative"},
		"servers":[{"url":"../api"}],"paths":{"/ping":{"get":{"responses":{"200":{"description":"OK"}}}}}}`
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/spec":
			http.Redirect(w, r, "/docs/v1/spec.json", http.StatusFound)
		case "/docs/v1/spec.json":
			_, _ = w.Write([]byte(spec))
		}
	}))
	defer api.Close()
	d := newDevice(t)
	ctx := t.Context()

	preview, err := d.Transfer.PreviewText(ctx, "openapi", spec, "relative.json")
	if err != nil || !preview.NeedsServer {
		t.Fatalf("PreviewText must ask for the origin of a local spec: %v", err)
	}
	if _, err := d.Transfer.Apply(ctx, preview.Token, "", "", convert.ImportOptions{ServerOrigin: "invalid"}); domain.CodeOf(err) != domain.CodeImportServer {
		t.Fatalf("Apply with invalid origin = %v", err)
	}
	local, err := d.Transfer.Apply(ctx, preview.Token, "", "", convert.ImportOptions{ServerOrigin: api.URL})
	if err != nil {
		t.Fatal(err)
	}

	preview, err = d.Transfer.PreviewUrl(ctx, "openapi", api.URL+"/spec")
	if err != nil || preview.NeedsServer {
		t.Fatalf("PreviewUrl must use the redirected document URL: %v", err)
	}
	remote, err := d.Transfer.Apply(ctx, preview.Token, "", "", convert.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range []struct {
		node domain.CollectionNode
		url  string
	}{{local.Tree[0].Items[0], api.URL + "/api/ping"}, {remote.Tree[1].Items[0], api.URL + "/docs/api/ping"}} {
		opened, err := d.Drafts.OpenNode(ctx, item.node.ID)
		if err != nil {
			t.Fatal(err)
		}
		preview, err := d.Drafts.Preview(ctx, opened.Draft)
		if err != nil || len(preview.Missing) != 0 || preview.Variables[domain.BaseURLVariable].Source != "collection" {
			t.Fatalf("Preview does not show the resolved collection variable: %v", err)
		}
		if rec := send(t, d, opened.Draft); rec.URL != item.url || rec.Status != http.StatusOK {
			t.Errorf("Send URL = %q, status = %d, want %q, 200", rec.URL, rec.Status, item.url)
		}
	}
}

func TestImportEnvironmentDestinationsAndExportRoundTrip(t *testing.T) {
	t.Parallel()
	const postman = `{
		"info":{"name":"Example"},
		"variable":[{"key":"baseUrl","value":"https://collection.example"}],
		"item":[{"name":"Ping","request":{"method":"GET","url":"{{baseUrl}}/ping"}}]
	}`
	const profile = `{"name":"Dev","_postman_variable_scope":"environment",
		"values":[{"key":"baseUrl","value":"https://dev.example","enabled":true}]}`
	const legacy = `{"schemo":"1.0","collections":[{"name":"Legacy","requests":[{"name":"Ping","url":"{{baseUrl}}/ping"}]}],
		"environments":[{"name":"Bundled","variables":[{"name":"baseUrl","value":"https://legacy.example","enabled":true}]}]}`
	d := newDevice(t)
	ctx := t.Context()
	if err := d.Environments.SaveGlobals(ctx, []domain.Variable{{
		ID: "global", Name: domain.BaseURLVariable, Value: "https://global.example", Enabled: true,
	}}); err != nil {
		t.Fatal(err)
	}

	result := applyImport(t, d, "postman", postman, "", convert.ImportOptions{})
	collection := result.Tree[0]
	assertValue := func(node domain.CollectionNode, want string) {
		t.Helper()
		opened, err := d.Drafts.OpenNode(ctx, node.ID)
		if err != nil {
			t.Fatal(err)
		}
		preview, err := d.Drafts.Preview(ctx, opened.Draft)
		if err != nil || preview.Variables[domain.BaseURLVariable].Value != want {
			t.Errorf("resolved baseUrl = %q, %v, want %q", preview.Variables[domain.BaseURLVariable].Value, err, want)
		}
	}
	assertValue(collection.Items[0], "https://collection.example")
	owned := applyImport(t, d, "postman", profile, collection.ID, convert.ImportOptions{EnvironmentTarget: "collection"})
	if len(owned.EnvState.Environments) != 0 || len(owned.EnvState.CollectionEnvironments) != 1 {
		t.Fatal("chosen collection must own the separately imported environment")
	}
	assertValue(collection.Items[0], "https://collection.example")
	if err := d.Environments.SelectCollection(ctx, collection.ID, owned.EnvState.CollectionEnvironments[0].ID); err != nil {
		t.Fatal(err)
	}
	assertValue(collection.Items[0], "https://dev.example")
	workspace := applyImport(t, d, "postman", profile, "", convert.ImportOptions{EnvironmentTarget: "workspace"})
	if len(workspace.EnvState.Environments) != 1 || workspace.EnvState.ActiveID != "" {
		t.Fatal("workspace import must add a reusable environment without activating it")
	}

	exported, err := d.Transfer.ExportText(ctx, collection.ID, "schemo")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := schemofile.Read([]byte(exported))
	if err != nil || len(doc.Environments) != 0 || len(doc.Collections) != 1 || len(doc.Collections[0].Environments) != 1 {
		t.Fatal("collection export must exclude the workspace environment and include the owned environment")
	}
	copied := applyImport(t, d, "schemo", exported, "", convert.ImportOptions{})
	assertValue(copied.Tree[1].Items[0], "https://collection.example")
	if len(copied.EnvState.Environments) != 1 {
		t.Fatal("round trip added an unrelated workspace environment")
	}

	skipped := applyImport(t, d, "schemo", legacy, "", convert.ImportOptions{})
	assertValue(skipped.Tree[2].Items[0], "https://global.example")
	if skipped.Environments != 0 {
		t.Fatal("legacy workspace environments should be skipped by default")
	}
	attached := applyImport(t, d, "schemo", legacy, "", convert.ImportOptions{EnvironmentTarget: "collection"})
	var bundledID string
	for _, env := range attached.EnvState.CollectionEnvironments {
		if env.CollectionID == attached.Tree[3].ID && env.Name == "Bundled" {
			bundledID = env.ID
		}
	}
	if bundledID == "" {
		t.Fatal("legacy environment was not attached")
	}
	if err := d.Environments.SelectCollection(ctx, attached.Tree[3].ID, bundledID); err != nil {
		t.Fatal(err)
	}
	assertValue(attached.Tree[3].Items[0], "https://legacy.example")
	if len(attached.EnvState.Environments) != 1 {
		t.Fatal("attaching legacy environments changed the workspace list")
	}
}

func applyImport(t *testing.T, d *device, format, source, parent string, options convert.ImportOptions) convert.Summary {
	t.Helper()
	preview, err := d.Transfer.PreviewText(t.Context(), format, source, "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.Transfer.Apply(t.Context(), preview.Token, parent, "", options)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
