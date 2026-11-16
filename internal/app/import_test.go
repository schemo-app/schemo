package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"schemo/internal/domain"
)

func TestImportInsomniaFolderDetectsBothFormats(t *testing.T) {
	t.Parallel()
	const yaml = `type: collection.insomnia.rest/5.0
name: Storefront
collection:
  - name: Products
    meta: {id: fld_products, description: Product requests}
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
    base_url: https://example.test
    credential: test-credential
  subEnvironments:
    - name: Staging
      data: {base_url: https://staging.example.test}
`
	const json = `{"_type":"export","__export_format":4,"resources":[
		{"_id":"wrk_legacy","_type":"workspace","name":"Legacy"},
		{"_id":"req_list","_type":"request","parentId":"wrk_legacy","name":"List","url":"https://example.test/list"}
	]}`
	path := t.TempDir()
	writeImportFile(t, filepath.Join(path, "01-storefront.json"), yaml)
	writeImportFile(t, filepath.Join(path, "02-legacy.yml"), json)
	writeImportFile(t, filepath.Join(path, "unrelated.yaml"), "openapi: 3.1.0\n")
	writeImportFile(t, filepath.Join(path, "ignored.txt"), yaml)

	d := newDevice(t)
	ctx := t.Context()
	preview, err := d.Transfer.PreviewPath(ctx, "insomnia", path)
	if err != nil {
		t.Fatal(err)
	}
	if preview.File != filepath.Base(path) || preview.Size != len(yaml)+len(json) ||
		preview.Collections != 2 || preview.Folders != 1 || preview.Requests != 2 || preview.Environments != 2 {
		t.Fatalf("PreviewPath = %+v, want the two exports combined", preview)
	}
	tree, err := d.Collections.Tree(ctx)
	if err != nil || len(tree) != 0 {
		t.Fatalf("before Apply: Tree = %v, %v, want empty", tree, err)
	}
	summary, err := d.Transfer.Apply(ctx, preview.Token, "", "")
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
	if len(collection.Variables) != 1 || collection.Variables[0].Name != "base_url" ||
		len(collection.Children) != 1 || len(collection.Children[0].Items) != 1 {
		t.Fatal("Full = missing public base variable or nested request")
	}
	request := collection.Children[0].Items[0]
	if request.Method != "POST" || request.Description != "Adds a product" ||
		request.URL != "{{base_url}}/products?page=1" || request.Body != `{"product":"lamp"}` ||
		len(request.Params) != 2 || request.Params[1].Enabled || request.Auth == nil ||
		request.Auth.Type != domain.AuthAPIKey || request.Auth.Fields["place"] != "query" ||
		request.Auth.Fields["value"] != "{{credential}}" {
		t.Errorf("Full = %+v, want the imported HTTP request and its settings", request)
	}
	if summary.EnvState == nil || len(summary.EnvState.Environments) != 2 {
		t.Fatal("Apply = missing Insomnia environments")
	}
	for _, environment := range summary.EnvState.Environments {
		if len(environment.Vars) != 2 || environment.Vars[1].Name != "credential" ||
			environment.Vars[1].Kind != domain.VariableSecret || environment.Vars[1].Value != "" {
			t.Errorf("environment %q: confidential data must be masked", environment.Name)
		}
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
	summary, err := d.Transfer.Apply(ctx, preview.Token, "", "")
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
