package insomnia

import (
	"testing"

	"schemo/internal/domain"
)

// first is the fixture's Storefront workspace; the second one is there for the block indexes.
func first(t *testing.T, doc domain.Document) domain.Collection {
	t.Helper()
	if len(doc.Collections) == 0 || doc.Collections[0].Name != "Storefront" {
		t.Fatalf("collections = %+v, want Storefront first", doc.Collections)
	}
	return doc.Collections[0]
}

// The base and folder environments become collection variables, the first value of a name winning;
// data is flattened with dots and every scalar is text.
func TestImportReadsBaseAndFolderEnvironmentsAsCollectionVariables(t *testing.T) {
	doc := read(t, "export_v4.json")
	collection := first(t, doc)

	want := []struct{ name, value string }{
		{"api_key", "sk_live_1"},
		{"auth.retries", "3"},
		{"auth.token", "{{api_key}}"},
		{"base_url", "https://api.storefront.example/v1"},
		{"debug", "true"},
		{"nothing", ""},
		{"request_id", "{% uuid 'v4' %}"},
		{"page_size", "25"},
	}
	if len(collection.Variables) != len(want) {
		t.Fatalf("variables = %+v, want %d", collection.Variables, len(want))
	}
	for index, one := range want {
		got := collection.Variables[index]
		if got.Name != one.name || got.Value != one.value || got.Kind != domain.VariableText ||
			!got.Enabled || got.Position != index+1 {
			t.Errorf("variable %d = %+v, want %s = %q", index, got, one.name, one.value)
		}
	}
	for _, child := range collection.Children {
		if len(child.Variables) != 0 {
			t.Errorf("folder %q = %+v, want no variables of its own", child.Name, child.Variables)
		}
	}
	if count := skipped(doc, domain.CodeSkippedVariableClash); count != 1 {
		t.Errorf("clashes = %d, want the folder's base_url counted", count)
	}
	if count := skipped(doc, domain.CodeSkippedTemplateTag); count != 1 {
		t.Errorf("template tags = %d, want the uuid tag counted", count)
	}
}

// A sub environment is a workspace environment with a block for its own collection; a private one
// holds secrets only.
func TestImportReadsSubEnvironmentsAsBlocks(t *testing.T) {
	doc := read(t, "export_v4.json")
	if len(doc.Collections) != 2 || doc.Collections[1].Name != "Admin" {
		t.Fatalf("collections = %+v, want Storefront and Admin", doc.Collections)
	}
	if len(doc.Environments) != 3 {
		t.Fatalf("environments = %+v, want Dev, Mine and Admin's Dev", doc.Environments)
	}

	dev := doc.Environments[0]
	if dev.Name != "Dev" || dev.Color != "#7d69cb" || len(dev.Variables) != 0 || len(dev.Blocks) != 1 {
		t.Fatalf("dev = %+v, want a block and the color", dev)
	}
	if block := dev.Blocks[0]; block.Collection != 0 || len(block.Variables) != 1 ||
		block.Variables[0].Name != "dev_token" || block.Variables[0].Kind != domain.VariableText {
		t.Errorf("dev block = %+v, want dev_token for Storefront", block)
	}

	mine := doc.Environments[1]
	if mine.Name != "Mine" || len(mine.Blocks) != 1 ||
		variable(t, mine.Blocks[0].Variables, "password").Kind != domain.VariableSecret {
		t.Errorf("mine = %+v, want the private environment's values secret", mine)
	}

	admin := doc.Environments[2]
	if admin.Name != "Dev" || len(admin.Blocks) != 1 || admin.Blocks[0].Collection != 1 ||
		variable(t, admin.Blocks[0].Variables, "base_url").Value != "https://admin.dev.example" {
		t.Errorf("admin dev = %+v, want a block for the second collection", admin)
	}
	if url := variable(t, doc.Collections[1].Variables, "base_url"); url.Value != "https://admin.example" {
		t.Errorf("admin base_url = %+v, want its own base environment", url)
	}
}

// The environments window reads every environment of an export, base first.
func TestVariablesReadsEveryEnvironment(t *testing.T) {
	vars, err := Variables(fixture(t, "export_v4.json"))
	if err != nil {
		t.Fatal(err)
	}
	if url := variable(t, vars, "base_url"); url.Value != "https://api.storefront.example/v1" {
		t.Errorf("base_url = %+v, want the first value", url)
	}
	if token := variable(t, vars, "dev_token"); token.Value != "tok_dev" {
		t.Errorf("dev_token = %+v", token)
	}
	if password := variable(t, vars, "password"); password.Kind != domain.VariableSecret {
		t.Errorf("password = %+v, want a secret", password)
	}
}

const v5Collection = `type: collection.insomnia.rest/5.0
name: Shop
collection:
  - name: Items
    meta: {id: fld_items}
    environment: {region: eu}
    children:
      - name: List
        method: GET
        url: "{{ _.base_url }}/items"
environments:
  name: Base Environment
  confidentialKeys: [key]
  data:
    base_url: https://shop.example
    key: k-1
  subEnvironments:
    - name: Staging
      color: "#ff0000"
      data: {base_url: https://staging.shop.example}
    - name: Private
      meta: {isPrivate: true}
      data: {token: t-1}
`

// v5 reads the same way; a confidential base value stays a name only, as collections hold no secrets.
func TestImportReadsV5Environments(t *testing.T) {
	doc, err := Import([]byte(v5Collection))
	if err != nil {
		t.Fatal(err)
	}
	collection := only(t, doc)
	if key := variable(t, collection.Variables, "key"); key.Value != "" || key.Kind != domain.VariableText {
		t.Errorf("key = %+v, want the confidential value left out", key)
	}
	if region := variable(t, collection.Variables, "region"); region.Value != "eu" {
		t.Errorf("region = %+v, want the folder's value on the collection", region)
	}
	if count := skipped(doc, domain.CodeSkippedSecretValue); count != 1 {
		t.Errorf("secret skips = %d, want one", count)
	}
	if len(doc.Environments) != 2 {
		t.Fatalf("environments = %+v, want Staging and Private", doc.Environments)
	}
	staging := doc.Environments[0]
	if staging.Name != "Staging" || staging.Color != "#ff0000" || staging.Blocks[0].Collection != 0 ||
		variable(t, staging.Blocks[0].Variables, "base_url").Value != "https://staging.shop.example" {
		t.Errorf("staging = %+v", staging)
	}
	if token := variable(t, doc.Environments[1].Blocks[0].Variables, "token"); token.Kind != domain.VariableSecret {
		t.Errorf("token = %+v, want the private environment's value secret", token)
	}
}

// An environment file belongs to no collection: its base is the globals, its subs apply to all.
func TestImportReadsAV5EnvironmentFile(t *testing.T) {
	doc, err := Import([]byte(`type: environment.insomnia.rest/5.0
name: Shared
environments:
  name: Base Environment
  data: {region: eu}
  subEnvironments:
    - name: Prod
      data: {base_url: https://prod.example}
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Collections) != 0 || len(doc.Globals) != 1 || doc.Globals[0].Name != "region" {
		t.Fatalf("document = %+v, want the base as globals", doc)
	}
	if len(doc.Environments) != 1 || len(doc.Environments[0].Blocks) != 0 ||
		variable(t, doc.Environments[0].Variables, "base_url").Value != "https://prod.example" {
		t.Errorf("environments = %+v, want Prod for all collections", doc.Environments)
	}
}

// The collection's variables are the base environment and environments hang off it; dotted names
// nest so `{{ _.auth.token }}` reads them, and come back flat.
func TestExportWritesTheBaseEnvironment(t *testing.T) {
	collection := storefront()
	collection.Variables = []domain.Variable{
		{Name: "auth.token", Value: "t-1", Kind: domain.VariableText, Enabled: true},
		{Name: "region", Value: "eu", Kind: domain.VariableText, Enabled: true},
		{Name: "off", Value: "x", Kind: domain.VariableText, Enabled: false},
	}
	environments := []domain.EnvironmentDraft{{Name: "Dev", Color: "#00ff00", Variables: []domain.Variable{
		{Name: "region", Value: "us", Kind: domain.VariableText, Enabled: true},
	}}}
	data := exported(t, collection, environments, false)

	base := resourceRow(t, data, "environment", "Base Environment")
	if at(t, base, "parentId") != "wrk_1" || at(t, base, "data", "auth", "token") != "t-1" {
		t.Errorf("base = %+v, want the collection's values nested under the workspace", base)
	}
	if _, held := base["data"].(map[string]any)["off"]; held {
		t.Errorf("base = %+v, want the disabled value left out", base)
	}
	dev := resourceRow(t, data, "environment", "Dev")
	if at(t, dev, "parentId") != base["_id"] || at(t, dev, "color") != "#00ff00" {
		t.Errorf("dev = %+v, want a sub environment of the base with its color", dev)
	}

	back := roundTrip(t, collection, environments, false)
	if token := variable(t, only(t, back).Variables, "auth.token"); token.Value != "t-1" {
		t.Errorf("auth.token = %+v, want it back flat", token)
	}
	if region := variable(t, back.Environments[0].Blocks[0].Variables, "region"); region.Value != "us" {
		t.Errorf("region = %+v, want Dev's value in its block", region)
	}
}
