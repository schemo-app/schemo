package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"schemo/internal/convert"
	"schemo/internal/convert/schemofile"
	"schemo/internal/domain"
	"schemo/internal/request"
	"schemo/internal/workspace"
)

func TestImportInsomniaFolderDetectsBothFormats(t *testing.T) {
	t.Parallel()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/products") && r.URL.Query().Get("access_key") != "folder-credential" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer api.Close()
	const yaml = `type: collection.insomnia.rest/5.0
name: Storefront
collection:
  - name: Products
    meta: {id: fld_products, description: Product requests}
    environment: {credential: folder-credential}
    confidentialKeys: [credential]
    children:
      - name: Create product
        meta: {description: Adds a product}
        method: POST
        url: "{{ _.base_url }}/products"
        parameters:
          - {name: page, value: 1}
          - {name: hidden, value: true, disabled: true}
        body:
          mimeType: application/json
          text: '{"product":"lamp"}'
        authentication:
          type: apikey
          addTo: query
          key: access_key
          value: "{{ _.credential }}"
environments:
  name: Base Environment
  confidentialKeys: [credential]
  data:
    base_url: API_URL/storefront
    credential: test-credential
  subEnvironments:
    - name: Staging
      data: {base_url: API_URL/staging}
`
	const json = `{"_type":"export","__export_format":4,"resources":[
		{"_id":"wrk_legacy","_type":"workspace","name":"Legacy"},
		{"_id":"req_list","_type":"request","parentId":"wrk_legacy","name":"List","url":"{{ _.base_url }}/list"},
		{"_id":"__BASE_ENVIRONMENT_ID__","_type":"environment","parentId":"wrk_legacy","name":"Base Environment",
		 "data":{"base_url":"API_URL/legacy"}}
	]}`
	storefrontData := strings.ReplaceAll(yaml, "API_URL", api.URL)
	legacyData := strings.ReplaceAll(json, "API_URL", api.URL)
	path := t.TempDir()
	writeImportFile(t, filepath.Join(path, "01-storefront.json"), storefrontData)
	writeImportFile(t, filepath.Join(path, "02-legacy.yml"), legacyData)
	writeImportFile(t, filepath.Join(path, "unrelated.yaml"), "openapi: 3.1.0\n")
	writeImportFile(t, filepath.Join(path, "ignored.txt"), yaml)

	d := newDevice(t)
	ctx := t.Context()
	workspaceEnv, err := d.Environments.Create(ctx, workspace.EnvironmentInput{Name: "Window"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Environments.SetInScope(ctx, domain.EnvScope{Environment: workspaceEnv},
		"base_url", "http://127.0.0.1:1/window", false); err != nil {
		t.Fatal(err)
	}
	preview, err := d.Transfer.PreviewPath(ctx, "insomnia", path)
	if err != nil {
		t.Fatal(err)
	}
	if preview.File != filepath.Base(path) || preview.Size != len(storefrontData)+len(legacyData) ||
		preview.Collections != 2 || preview.Folders != 1 || preview.Requests != 2 || preview.Environments != 3 {
		t.Fatalf("PreviewPath = %+v, want the two exports combined", preview)
	}
	tree, err := d.Collections.Tree(ctx)
	if err != nil || len(tree) != 0 {
		t.Fatalf("before Apply: Tree = %v, %v, want empty", tree, err)
	}
	summary, err := d.Transfer.Apply(ctx, preview.Token, "", "", convert.ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Tree) != 2 || summary.Tree[0].Name != "Storefront" || summary.Tree[1].Name != "Legacy" {
		t.Fatalf("Apply = %+v, want Storefront followed by Legacy", summary)
	}
	collection, err := d.Collections.Full(ctx, summary.Tree[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(collection.Environments) != 2 || len(collection.Children) != 1 || len(collection.Children[0].Items) != 1 {
		t.Fatal("Full = missing collection environments or nested request")
	}
	node := collection.Children[0].Items[0]
	if node.Method != "POST" || node.Description != "Adds a product" ||
		node.URL != "{{base_url}}/products?page=1" || node.Body != `{"product":"lamp"}` ||
		len(node.Params) != 2 || node.Params[1].Enabled || node.Auth == nil ||
		node.Auth.Type != domain.AuthAPIKey || node.Auth.Fields["place"] != "query" ||
		node.Auth.Fields["value"] != "{{credential}}" {
		t.Errorf("Full = %+v, want the imported HTTP request and its settings", node)
	}
	state, err := d.Environments.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Environments) != 1 || state.ActiveID != workspaceEnv || len(state.CollectionEnvironments) != 4 {
		t.Fatal("State must keep the workspace environment and the imported collection environments in their scopes")
	}
	base, ok := state.Find(state.CollectionActive[collection.ID])
	if !ok || !base.Base || len(base.Vars) != 2 || base.Vars[1].Name != "credential" ||
		base.Vars[1].Kind != domain.VariableSecret || !base.Vars[1].HasValue || base.Vars[1].Value != "" {
		t.Fatal("State must select the base and withhold its confidential value")
	}

	storefront, err := d.Drafts.OpenNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := d.Drafts.OpenNode(ctx, summary.Tree[1].Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	assertURL := func(draft domain.Draft, path string) {
		t.Helper()
		record := send(t, d, draft)
		if record.Status != http.StatusOK || !strings.HasPrefix(record.URL, api.URL+path) ||
			strings.Contains(record.URL, "test-credential") || strings.Contains(record.URL, "folder-credential") {
			t.Errorf("Send = status %d, URL %q, want %s with any credential masked", record.Status, record.URL, path)
		}
	}
	resolved, err := d.Drafts.Preview(ctx, storefront.Draft)
	if err != nil || resolved.Variables["credential"].Source != "folder" ||
		resolved.Variables["credential"].Kind != domain.VariableSecret || resolved.Variables["credential"].Value != "" {
		t.Fatal("Preview must show the folder's secret override without exposing its value")
	}
	assertURL(storefront.Draft, "/storefront/products")
	assertURL(legacy.Draft, "/legacy/list")

	var stagingID string
	for _, env := range state.CollectionEnvironments {
		if env.CollectionID == collection.ID && env.Name == "Staging" {
			stagingID = env.ID
		}
	}
	if stagingID == "" {
		t.Fatal("State = missing the Staging environment")
	}
	if err := d.Environments.Activate(ctx, stagingID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scripts.SaveScripts(ctx, collection.ID, &domain.Scripts{
		Post: `pm.environment.set("last_base", pm.environment.get("base_url")); console.log(pm.environment.get("credential"))`,
	}); err != nil {
		t.Fatal(err)
	}
	assertURL(storefront.Draft, "/staging/products")
	assertURL(legacy.Draft, "/legacy/list")

	if _, err := d.Runs.Run(ctx, collection.ID, ""); err != nil {
		t.Fatal(err)
	}
	d.eventually("collection run", func() bool { return d.events.count(request.TopicRunFinished) == 1 })
	run, err := d.Runs.Last(ctx, collection.ID, "")
	if err != nil || run == nil || run.Environment != "Staging" || run.Passed != 1 {
		t.Fatalf("Last = %v, %v, want the successful Staging run", run, err)
	}
	reports, err := d.Scripts.Runs(ctx, run.Results[0].RecordID)
	if err != nil || len(reports) != 1 || !reports[0].OK || len(reports[0].Logs) != 1 ||
		reports[0].Logs[0].Message != domain.SecretMask {
		t.Fatal("Runs must mask confidential values in script output")
	}
	fromStaging := d.Environments.InEnvironment(d.active(), stagingID)
	if value, ok, err := fromStaging.Variable(ctx, domain.ScopeEnvironment, "last_base"); err != nil ||
		!ok || value != api.URL+"/staging" {
		t.Errorf("Staging's last_base = %q, %v, want the scoped script value", value, err)
	}
	if _, ok, err := d.Environments.Variable(ctx, domain.ScopeEnvironment, "last_base"); err != nil || ok {
		t.Errorf("Workspace's last_base present = %v, %v, want absent", ok, err)
	}
	exported, err := d.Transfer.ExportText(ctx, collection.ID, "schemo")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(exported, "test-credential") || strings.Contains(exported, "folder-credential") {
		t.Fatal("ExportText must withhold confidential collection variables")
	}
	reopened, err := schemofile.Read([]byte(exported))
	if err != nil || len(reopened.Environments) != 0 || len(reopened.Collections) != 1 ||
		len(reopened.Collections[0].Environments) != 2 || !reopened.Collections[0].Environments[0].Base {
		t.Fatal("The Schemo file must travel with the collection's own selectable environments")
	}
}

func TestImportHAROverEightMiB(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "large.har")
	data := `{"log":{"entries":[{"request":{"method":"GET","url":"https://example.test/first"},` +
		`"response":{"content":{"text":"` + strings.Repeat("x", 9<<20) + `"}}},` +
		`{"request":{"method":"POST","url":"https://example.test/last","postData":{"text":"kept"}}}],"version":"1.2"}}`
	writeImportFile(t, path, data)

	d := newDevice(t)
	ctx := t.Context()
	preview, err := d.Transfer.PreviewPath(ctx, "har", path)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Size != len(data) || preview.Requests != 2 {
		t.Fatalf("PreviewPath = %+v, want both requests in a %d-byte archive", preview, len(data))
	}
	summary, err := d.Transfer.Apply(ctx, preview.Token, "", "", convert.ImportOptions{})
	if err != nil || summary.Requests != 2 {
		t.Fatalf("Apply = %d requests, %v, want two requests", summary.Requests, err)
	}
}

func writeImportFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
