package insomnia

import (
	"errors"
	"os"
	"testing"

	"schemo/internal/domain"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return data
}

func read(t *testing.T, name string) domain.Document {
	t.Helper()
	doc, err := Import(fixture(t, name))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return doc
}

func only(t *testing.T, doc domain.Document) domain.Collection {
	t.Helper()
	if len(doc.Collections) != 1 {
		t.Fatalf("collections = %d, want one", len(doc.Collections))
	}
	return doc.Collections[0]
}

func node(t *testing.T, from domain.Collection, name string) domain.CollectionNode {
	t.Helper()
	for _, one := range from.Items {
		if one.Name == name {
			return one
		}
	}
	for _, child := range from.Children {
		for _, one := range child.Items {
			if one.Name == name {
				return one
			}
		}
	}
	t.Fatalf("no request named %q in %+v", name, from)
	return domain.CollectionNode{}
}

func variable(t *testing.T, vars []domain.Variable, name string) domain.Variable {
	t.Helper()
	for _, one := range vars {
		if one.Name == name {
			return one
		}
	}
	t.Fatalf("no variable named %q in %+v", name, vars)
	return domain.Variable{}
}

func skipped(doc domain.Document, code domain.Code) int {
	for _, one := range doc.Skipped {
		if one.Code == code {
			return one.Count
		}
	}
	return 0
}

// A workspace is a collection: its groups become folders, its requests become requests, and both
// keep the order the export wrote them in.
func TestImportReadsAWorkspaceAsACollection(t *testing.T) {
	collection := only(t, read(t, "export_v4.json"))
	if collection.Name != "Storefront" || collection.Description != "The shop's own API." {
		t.Errorf("collection = %+v, want the workspace's own name", collection)
	}

	level := collection.Level()
	if len(level) != 3 {
		t.Fatalf("level = %d entries, want three", len(level))
	}
	if level[0].Collection == nil || level[0].Collection.Name != "Products" {
		t.Errorf("entry 0 = %+v, want the Products folder", level[0])
	}
	if level[1].Collection == nil || level[1].Collection.Name != "Cart" {
		t.Errorf("entry 1 = %+v, want the Cart folder", level[1])
	}
	if level[2].Node == nil || level[2].Node.Name != "One item" {
		t.Errorf("entry 2 = %+v, want the request in its turn", level[2])
	}

	list := node(t, collection, "List products")
	if list.Method != "GET" || list.Description != "Everything on the shelf." {
		t.Errorf("list = %+v, want the file's method and description", list)
	}
	if list.URL != "{{base_url}}/products?page[size]=25" {
		t.Errorf("url = %q, want the template rewritten and the enabled parameter in it", list.URL)
	}
	if len(list.Headers) != 2 || !list.Headers[0].Enabled || list.Headers[0].Name != "Accept" {
		t.Errorf("headers = %+v, want the Accept header on", list.Headers)
	}
	if list.Headers[1].Enabled {
		t.Errorf("headers = %+v, want the disabled one parked", list.Headers)
	}
	if len(list.Params) != 2 || !list.Params[0].Enabled || list.Params[0].Value != "25" {
		t.Errorf("params = %+v, want the enabled one first", list.Params)
	}
	if list.Params[1].Enabled || list.Params[1].Value != "offers" {
		t.Errorf("params = %+v, want the disabled one parked", list.Params)
	}
}

// A body is what its media type says: json is json, a form is written as the text it would send,
// and a file field keeps its name while the file itself stays out and is counted.
func TestImportReadsBodiesByTheirMediaType(t *testing.T) {
	doc := read(t, "export_v4.json")
	collection := only(t, doc)

	created := node(t, collection, "Create a product")
	if created.BodyKind != domain.BodyJSON || created.Body != "{\n  \"name\": \"{{product}}\"\n}" {
		t.Errorf("body = %+v, want the template rewritten inside json", created)
	}

	uploaded := node(t, collection, "Attach a receipt")
	if uploaded.BodyKind != domain.BodyForm || len(uploaded.Form) != 2 {
		t.Fatalf("form = %+v, want the two fields", uploaded.Form)
	}
	if !uploaded.Form[0].Enabled || uploaded.Form[0].Value != "paid" {
		t.Errorf("field = %+v, want the note as it was", uploaded.Form[0])
	}
	if !uploaded.Form[1].File || uploaded.Form[1].Value != "" {
		t.Errorf("field = %+v, want the file marked with no value", uploaded.Form[1])
	}
	if count := skipped(doc, domain.CodeSkippedBinaryFile); count != 1 {
		t.Errorf("skips = %+v, want the file counted", doc.Skipped)
	}
}

// Authorization becomes this app's own: a bearer scheme is a bearer scheme whatever it is
// templated with, and a scheme with no shape here is `none` rather than the collection's — the
// file asked for it, and inheriting would send credentials it never named.
func TestImportReadsAuthorization(t *testing.T) {
	doc := read(t, "export_v4.json")
	collection := only(t, doc)

	created := node(t, collection, "Create a product")
	if created.Auth == nil || created.Auth.Type != domain.AuthBearer ||
		created.Auth.Answer("token") != "{{api_key}}" || created.Auth.Answer("prefix") != "Bearer" {
		t.Errorf("auth = %+v, want the bearer scheme over the templated key", created.Auth)
	}
	if list := node(t, collection, "List products"); list.Auth != nil {
		t.Errorf("auth = %+v, want nothing where the file declared nothing", list.Auth)
	}

	// An OAuth 2.0 scheme arrives as the flow the file leaves out: the authorization code one.
	uploaded := node(t, collection, "Attach a receipt")
	if uploaded.Auth == nil || uploaded.Auth.Type != domain.AuthOAuth2 ||
		uploaded.Auth.Answer("grant") != "authorization_code" ||
		uploaded.Auth.Answer("tokenUrl") != "https://auth.storefront.example/token" {
		t.Errorf("auth = %+v, want the oauth2 scheme with its token url", uploaded.Auth)
	}
	if count := skipped(doc, domain.CodeSkippedAuthScheme); count != 0 {
		t.Errorf("skips = %+v, want no scheme skipped", doc.Skipped)
	}
}

// A path parameter is the request's own: the address keeps its `:name`, and what the file kept
// beside the url is the value of the row that fills it. It is no variable.
func TestImportReadsPathParametersAsTheRequestsRows(t *testing.T) {
	collection := only(t, read(t, "export_v4.json"))
	one := node(t, collection, "One item")
	if one.URL != "{{base_url}}/items/:item_id" {
		t.Errorf("url = %q, want the path parameter kept as a `:name`", one.URL)
	}
	if len(one.PathParams) != 1 || one.PathParams[0].Name != "item_id" ||
		one.PathParams[0].Value != "it_1" || !one.PathParams[0].Enabled {
		t.Errorf("path params = %+v, want the value the file kept", one.PathParams)
	}
	for _, one := range collection.Variables {
		if one.Name == "item_id" {
			t.Errorf("variable = %+v, want no variable for a path parameter", one)
		}
	}
}

// Environments come with their collections. Insomnia's base environment applies beside whichever
// one is selected, and this app has no such second scope — so its values are also folded into every
// environment that does not set them, and the base itself is written too.
func TestImportReadsEnvironments(t *testing.T) {
	doc := read(t, "export_v4.json")
	if len(doc.Environments) != 2 {
		t.Fatalf("environments = %+v, want the base and Dev", doc.Environments)
	}

	base := doc.Environments[0]
	if base.Name != "Base Environment" {
		t.Fatalf("first = %q, want the base environment", base.Name)
	}
	url := variable(t, base.Variables, "base_url")
	if url.Value != "https://api.storefront.example/v1" {
		t.Errorf("base_url = %+v, want the address", url)
	}

	dev := doc.Environments[1]
	if dev.Name != "Dev" {
		t.Fatalf("second = %q, want Dev", dev.Name)
	}
	if token := variable(t, dev.Variables, "dev_token"); token.Value != "tok_dev" ||
		token.Kind != domain.VariableSecret {
		t.Errorf("dev_token = %+v, want the environment's own value", token)
	}
	inherited := variable(t, dev.Variables, "base_url")
	if inherited.Value != "https://api.storefront.example/v1" {
		t.Errorf("base_url = %+v, want the base's value folded in", inherited)
	}
	// Insomnia does not mark a variable as a secret; the name is the only thing left to read.
	if key := variable(t, dev.Variables, "api_key"); key.Kind != domain.VariableSecret {
		t.Errorf("api_key = %+v, want a secret by its name", key)
	}
}

func TestImportRefusesWhatIsNotAnExport(t *testing.T) {
	for name, data := range map[string]string{
		"not json": `curl -X GET https://example.com`,
		"no type":  `{"hello": "world"}`,
	} {
		if _, err := Import([]byte(data)); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("%s: Import = %v, want ErrNotAllowed", name, err)
		}
	}
	// v5 is a different shape, and the underscore is how v4 marks itself: a file without it is
	// refused rather than read as another version of itself.
	other := []byte(`{"type": "export", "resources": [{"id": "r1", "type": "request"}]}`)
	if _, err := Import(other); domain.CodeOf(err) != domain.CodeNotInsomnia {
		t.Errorf("code = %q, want notInsomniaExport", domain.CodeOf(err))
	}
}

// An export that holds a workspace and nothing else is refused: writing an empty collection would
// be an import that said it did something.
func TestImportRefusesAnExportWithNothingInIt(t *testing.T) {
	for name, data := range map[string]string{
		"workspace only": `{"_type": "export", "__export_format": 4, "resources": [` +
			`{"_id": "wrk_1", "parentId": null, "name": "Empty", "_type": "workspace"}]}`,
		"no resources": `{"_type": "export", "resources": []}`,
	} {
		if _, err := Import([]byte(data)); domain.CodeOf(err) != domain.CodeNothingToImport {
			t.Errorf("%s: Import = %v, want nothingToImport", name, err)
		}
	}
}
