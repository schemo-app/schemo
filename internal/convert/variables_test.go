package convert

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// A variables file is told apart by what it holds, not by its name.
func TestParseVariablesDetectsTheFormat(t *testing.T) {
	for _, test := range []struct {
		name, data   string
		first, value string
		secret       string
	}{
		{
			name: "postman environment", data: postmanEnvironment,
			first: "baseUrl", value: "https://st.example.com", secret: "apiKey",
		},
		{
			name:  "postman globals",
			data:  `{"name":"Globals","_postman_variable_scope":"globals","values":[{"key":"region","value":"eu"}]}`,
			first: "region", value: "eu",
		},
		{
			name: "insomnia v4",
			data: `{"_type":"export","resources":[` +
				`{"_id":"env_1","_type":"environment","parentId":"wrk_1","data":{"auth":{"token":"{{ _.key }}"}}},` +
				`{"_id":"env_2","_type":"environment","parentId":"env_1","isPrivate":true,"data":{"pin":"1"}}]}`,
			first: "auth.token", value: "{{key}}", secret: "pin",
		},
		{
			name: "insomnia v5",
			data: "type: environment.insomnia.rest/5.0\nname: Shared\nenvironments:\n  name: Base\n" +
				"  data: {region: eu}\n  subEnvironments:\n    - name: Prod\n      meta: {isPrivate: true}\n      data: {pin: '1'}\n",
			first: "region", value: "eu", secret: "pin",
		},
		{
			name: "schemo",
			data: `{"schemo":"1.0","environments":[{"name":"Prod","variables":[{"name":"region","value":"eu","enabled":true}],` +
				`"blocks":[{"collection":0,"variables":[{"name":"baseUrl","value":"https://prod.example","enabled":true}]}]}]}`,
			first: "region", value: "eu",
		},
		{
			name: "dotenv", data: "\xef\xbb\xbf# local\nBASE_URL=https://local.example\nexport API_TOKEN=\"t-1\"\n",
			first: "BASE_URL", value: "https://local.example", secret: "API_TOKEN",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			vars, err := parseVariables([]byte(test.data))
			if err != nil {
				t.Fatalf("parseVariables: %v", err)
			}
			if vars[0].Name != test.first || vars[0].Value != test.value || vars[0].Position != 1 {
				t.Errorf("first = %+v, want %s = %q", vars[0], test.first, test.value)
			}
			if test.secret == "" {
				return
			}
			for _, one := range vars {
				if one.Name == test.secret && one.Kind == domain.VariableSecret {
					return
				}
			}
			t.Errorf("vars = %+v, want %s secret", vars, test.secret)
		})
	}
}

func TestParseVariablesRefusesWhatHoldsNone(t *testing.T) {
	for name, data := range map[string]string{
		"unknown json": `{"info":{"name":"A collection"}}`,
		"broken json":  `{"_type":`,
		"no values":    "# nothing here\n\n",
		"other yaml":   "openapi: 3.1.0\n",
	} {
		if _, err := parseVariables([]byte(data)); err == nil || domain.CodeOf(err) == "" {
			t.Errorf("%s: parseVariables = %v, want a refusal", name, err)
		}
	}
}

// A globals file writes the globals scope, and the summary carries the list as it is now.
func TestApplyWritesGlobals(t *testing.T) {
	h := newHarness()
	h.files.content = []byte(`{"name":"Globals","_postman_variable_scope":"globals","values":[{"key":"region","value":"eu"}]}`)
	ctx := context.Background()

	preview, err := h.transfer.previewFile(ctx, FormatPostman, "workspace.postman_globals.json")
	if err != nil {
		t.Fatalf("previewFile: %v", err)
	}
	if preview.Globals != 1 || preview.Environments != 0 || preview.Collections != 0 {
		t.Fatalf("preview = %+v, want one global", preview)
	}
	summary, err := h.transfer.Apply(ctx, preview.Token, "", "", "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(h.environments.scopes) != 1 || h.environments.scopes[0] != (domain.EnvScope{}) ||
		h.environments.vars[0][0].Name != "region" {
		t.Errorf("imported = %+v %+v, want region into the globals", h.environments.scopes, h.environments.vars)
	}
	if summary.Globals != 1 || summary.EnvState == nil {
		t.Errorf("summary = %+v, want the globals counted and the list", summary)
	}
}

// Every export format reads the blocks of the exported level's top collection only.
func TestExportUsesTheBlocksOfTheTopCollection(t *testing.T) {
	h := newHarness()
	h.ledgerTree()
	h.collections.full = domain.Collection{
		ID: "admin", Name: "Admin",
		Variables: []domain.Variable{{Name: "baseUrl", Value: "https://api.example", Enabled: true}},
		Items:     []domain.CollectionNode{{Name: "List", Method: "GET", URL: "{{baseUrl}}/users"}},
	}
	h.environments.state = domain.EnvState{Environments: []domain.Environment{
		{
			ID: "prod", Name: "Prod", Color: "red",
			Vars: []domain.Variable{
				{Name: "baseUrl", Value: "https://all.example", Enabled: true},
				{Name: "region", Value: "eu", Enabled: true},
			},
			Collections: map[string][]domain.Variable{"ledger": {
				{Name: "baseUrl", Value: "https://prod.example", Enabled: true},
			}},
		},
		{ID: "other", Name: "Other", Collections: map[string][]domain.Variable{"elsewhere": {
			{Name: "baseUrl", Value: "https://other.example", Enabled: true},
		}}},
	}}
	ctx := context.Background()

	openapi, err := h.transfer.export(ctx, "admin", FormatOpenAPI, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(openapi.Data)
	if !strings.Contains(text, "https://prod.example") || !strings.Contains(text, "description: Prod") ||
		strings.Contains(text, "other.example") || strings.Contains(text, "all.example") {
		t.Errorf("openapi = %s, want the Prod block's baseUrl as a server", text)
	}

	insomnia, err := h.transfer.export(ctx, "admin", FormatInsomnia, false)
	if err != nil {
		t.Fatal(err)
	}
	text = string(insomnia.Data)
	if !strings.Contains(text, `"baseUrl": "https://prod.example"`) || !strings.Contains(text, `"region": "eu"`) ||
		strings.Contains(text, "Other") {
		t.Errorf("insomnia = %s, want Prod with its block over its shared values", text)
	}
}

// A folder of exports joins the documents, so a block keeps pointing at its own collection.
func TestReadFolderShiftsBlocksToTheirCollection(t *testing.T) {
	h := newHarness()
	h.transfer.files = platform.NewFileReader()
	dir := t.TempDir()
	for name, data := range map[string]string{
		"a.yaml": "type: collection.insomnia.rest/5.0\nname: A\ncollection:\n  - {name: One, method: GET, url: https://a.example}\n" +
			"environments:\n  name: Base\n  subEnvironments:\n    - {name: Dev, data: {k: a}}\n",
		"b.yaml": "type: collection.insomnia.rest/5.0\nname: B\ncollection:\n  - {name: Two, method: GET, url: https://b.example}\n" +
			"environments:\n  name: Base\n  subEnvironments:\n    - {name: Dev, data: {k: b}}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	document, _, err := h.transfer.readPath(t.Context(), FormatInsomnia, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Collections) != 2 || len(document.Environments) != 2 {
		t.Fatalf("document = %+v, want two collections and two drafts", document)
	}
	for index, draft := range document.Environments {
		if draft.Blocks[0].Collection != index ||
			draft.Blocks[0].Variables[0].Value != strings.ToLower(document.Collections[index].Name) {
			t.Errorf("draft %d = %+v, want a block for collection %d", index, draft, index)
		}
	}
}
