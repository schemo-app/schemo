package openapi

import (
	"errors"
	"os"
	"slices"
	"strings"
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

// read is the fixture as the collection it holds, with the document's skips beside it.
func read(t *testing.T, name string) (domain.Collection, domain.Document) {
	t.Helper()
	doc, err := Import(fixture(t, name))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(doc.Collections) != 1 {
		t.Fatalf("collections = %d, want one", len(doc.Collections))
	}
	return doc.Collections[0], doc
}

func node(t *testing.T, collection domain.Collection, name string) domain.CollectionNode {
	t.Helper()
	for _, folder := range collection.Children {
		for _, one := range folder.Items {
			if one.Name == name {
				return one
			}
		}
	}
	for _, one := range collection.Items {
		if one.Name == name {
			return one
		}
	}
	t.Fatalf("no request named %q in %+v", name, collection)
	return domain.CollectionNode{}
}

// operationAt is the request of the pair given: a request a document left unnamed carries the same
// name as every other such request, and the method with the address is what names it.
func operationAt(
	t *testing.T, collection domain.Collection, method, url string,
) domain.CollectionNode {
	t.Helper()
	for _, folder := range collection.Children {
		for _, one := range folder.Items {
			if one.Method == method && one.URL == url {
				return one
			}
		}
	}
	for _, one := range collection.Items {
		if one.Method == method && one.URL == url {
			return one
		}
	}
	t.Fatalf("no %s request at %q in %+v", method, url, collection)
	return domain.CollectionNode{}
}

func variable(t *testing.T, collection domain.Collection, name string) domain.Variable {
	t.Helper()
	for _, one := range collection.Variables {
		if one.Name == name {
			return one
		}
	}
	t.Fatalf("no variable named %q in %+v", name, collection.Variables)
	return domain.Variable{}
}

// An operation is a request: the name the document gives it, its address against the declared
// server, and the parameters it requires written into that address.
func TestImportReadsAnOperationAsARequest(t *testing.T) {
	collection, _ := read(t, "example.json")
	if collection.Name != "Example API" || collection.Description != "Products and the cart." {
		t.Errorf("collection = %+v, want the document's own name", collection)
	}

	list := node(t, collection, "listProducts")
	if list.Method != "GET" {
		t.Errorf("method = %q, want GET", list.Method)
	}
	if list.URL != "{{baseUrl}}/products?page[size]=25" {
		t.Errorf("url = %q, want the required parameter in the address", list.URL)
	}
	if len(list.Params) != 2 || !list.Params[0].Enabled || list.Params[0].Value != "25" {
		t.Errorf("params = %+v, want the required one enabled and the optional one parked", list.Params)
	}
	if list.Params[1].Enabled || list.Params[1].Value != "offers" {
		t.Errorf("param = %+v, want the optional one carrying its first enum", list.Params[1])
	}
	if len(list.Headers) != 1 || !list.Headers[0].Enabled || list.Headers[0].Value != "example" {
		t.Errorf("headers = %+v, want the required header with its default", list.Headers)
	}
	// The folders stand in the order the walk met their tags — the same order Hoppscotch makes them
	// in — and the paths are walked sorted, so `cart` comes first here.
	if len(collection.Children) != 2 || collection.Children[0].Name != "cart" ||
		collection.Children[1].Name != "products" {
		t.Errorf("children = %+v, want a folder per tag where the walk met it", collection.Children)
	}
}

// Folders nest when the document's tags show a hierarchy — a tag that is the beginning of another,
// or two that begin the same way — and stay plain names otherwise, which is the rule Hoppscotch
// reads by. A tag the document describes stands as a folder even with no operation in it.
func TestImportNestsTagsOnlyWhenTheDocumentShowsAHierarchy(t *testing.T) {
	nested := []byte(`{"openapi": "3.1.0", "info": {"title": "Deep"},
		"tags": [{"name": "api/users", "description": "The people"}, {"name": "api/orders"},
		         {"name": "sandbox", "description": "Playground"}],
		"paths": {
			"/users": {"get": {"tags": ["api/users"], "responses": {"200": {"description": "OK"}}}},
			"/orders": {"get": {"tags": ["api/orders"], "responses": {"200": {"description": "OK"}}}}}}`)

	doc, err := Import(nested)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	collection := doc.Collections[0]
	if len(collection.Children) != 2 || collection.Children[0].Name != "api" ||
		collection.Children[1].Name != "sandbox" || collection.Children[1].Description != "Playground" {
		t.Fatalf("children = %+v, want the nested pair and the described empty folder",
			collection.Children)
	}
	api := collection.Children[0]
	if len(api.Children) != 2 || api.Children[0].Name != "users" ||
		api.Children[0].Description != "The people" || api.Children[1].Name != "orders" {
		t.Fatalf("children = %+v, want the two halves with their description", api.Children)
	}
	if len(api.Children[0].Items) != 1 || len(api.Children[1].Items) != 1 {
		t.Errorf("items = %+v, want a request in each nested folder", api.Children)
	}

	// A lone `x/y` shows no hierarchy, so nothing is split: the folder keeps the tag as its name.
	flat := []byte(`{"openapi": "3.1.0", "info": {"title": "Flat"},
		"paths": {"/a": {"get": {"tags": ["x/y"], "responses": {"200": {"description": "OK"}}}}}}`)
	doc, err = Import(flat)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got := doc.Collections[0].Children[0].Name; got != "x/y" {
		t.Errorf("folder = %q, want the tag as one plain name where nothing nests", got)
	}
}

// What a document declares an operation answers with is read as the examples beside the request:
// the name is what it describes the answer as, the status is the code, and the body is the example
// it shows or the skeleton its schema makes.
func TestImportReadsResponsesAsExamples(t *testing.T) {
	document := []byte(`{"openapi": "3.1.0", "info": {"title": "Answers"},
		"paths": {"/a": {"get": {"responses": {
			"200": {"description": "The list", "content": {"application/json": {
				"example": {"ok": true}}}},
			"404": {"description": "Not here", "content": {"application/json": {
				"schema": {"type": "object", "properties": {"error": {"type": "string"}}}}}}}}}}}`)

	doc, err := Import(document)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	responses := doc.Collections[0].Items[0].Responses
	if len(responses) != 2 {
		t.Fatalf("responses = %+v, want the two declared answers", responses)
	}
	if responses[0].Name != "The list" || responses[0].Status != 200 ||
		responses[0].Body != `{"ok":true}` {
		t.Errorf("response = %+v, want the example it shows", responses[0])
	}
	if responses[1].Name != "Not here" || responses[1].Status != 404 ||
		!strings.Contains(responses[1].Body, `"error": "string"`) {
		t.Errorf("response = %+v, want the skeleton its schema makes", responses[1])
	}
	if len(responses[0].Headers) != 1 || responses[0].Headers[0].Value != "application/json" {
		t.Errorf("headers = %+v, want the media type as the content type", responses[0].Headers)
	}
}

// The server is the variable every address is written against, and what the document does not carry
// — a token — is declared switched off so a person can see what to fill in.
func TestImportDeclaresWhatTheDocumentLeavesOpen(t *testing.T) {
	collection, _ := read(t, "example.json")
	base := variable(t, collection, "baseUrl")
	if base.Value != "https://api.example.com/v1" || !base.Enabled {
		t.Errorf("baseUrl = %+v, want the server's address", base)
	}
	if token := variable(t, collection, "token"); token.Value != "" || token.Enabled {
		t.Errorf("token = %+v, want a switched-off variable for the token", token)
	}
}

// A path parameter is the request's own and not a variable: the address names the slot with `:name`
// and the request carries the row that fills it, with what the document says about it.
func TestImportReadsAPathParameterAsTheRequestsOwnRow(t *testing.T) {
	collection, _ := read(t, "example.json")

	for _, one := range collection.Variables {
		if one.Name == "id" {
			t.Errorf("variable = %+v, want no variable for a path parameter", one)
		}
	}
	one := node(t, collection, "One product")
	if one.URL != "{{baseUrl}}/products/:id" {
		t.Errorf("url = %q, want the path parameter as a `:name`", one.URL)
	}
	if len(one.PathParams) != 1 || one.PathParams[0].Name != "id" || !one.PathParams[0].Enabled {
		t.Errorf("path params = %+v, want the row of the parameter", one.PathParams)
	}
}

func TestImportReadsWhatTheDocumentSaysOfAPathParameter(t *testing.T) {
	document := []byte(`{"openapi": "3.1.0", "info": {"title": "PIM"},
	  "paths": {"/groups/{attribute-group}/items/{item.id}": {"get": {"summary": "One item",
	    "parameters": [
	      {"name": "attribute-group", "in": "path", "required": true, "example": "colors",
	       "description": "The group", "schema": {"type": "string"}},
	      {"name": "item.id", "in": "path", "required": true, "schema": {"type": "string"}}
	    ], "responses": {"200": {"description": "ok"}}}}}}`)
	doc, err := Import(document)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	one := doc.Collections[0].Items[0]
	if one.URL != "{{baseUrl}}/groups/:attribute-group/items/:item_id" {
		t.Errorf("url = %q, want every name written as an address can carry it", one.URL)
	}
	want := []domain.Row{
		{Name: "attribute-group", Value: "colors", Enabled: true, Description: "The group"},
		{Name: "item_id", Enabled: true},
	}
	if !slices.Equal(one.PathParams, want) {
		t.Errorf("path params = %+v, want %+v", one.PathParams, want)
	}
}

// A `:name` reaches a whole segment, so a name that shares its segment with other text is left a
// variable, and the parameter of a segment of its own is still the request's row.
func TestImportKeepsAPathNameInsideASegmentAVariable(t *testing.T) {
	document := []byte(`{"openapi": "3.1.0", "info": {"title": "Files"},
	  "paths": {"/api/v{version}/files/{name}.{ext}/{id}": {"get": {"summary": "One file",
	    "parameters": [
	      {"name": "version", "in": "path", "required": true, "schema": {"type": "string"}},
	      {"name": "name", "in": "path", "required": true, "schema": {"type": "string"}},
	      {"name": "ext", "in": "path", "required": true, "schema": {"type": "string"}},
	      {"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}
	    ], "responses": {"200": {"description": "ok"}}}}}}`)
	doc, err := Import(document)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	collection := doc.Collections[0]
	one := collection.Items[0]
	if want := "{{baseUrl}}/api/v{{version}}/files/{{name}}.{{ext}}/:id"; one.URL != want {
		t.Errorf("url = %q, want %q", one.URL, want)
	}
	if len(one.PathParams) != 1 || one.PathParams[0].Name != "id" {
		t.Errorf("path params = %+v, want the row of the segment of its own only", one.PathParams)
	}
	for _, name := range []string{"version", "name", "ext"} {
		if v := variable(t, collection, name); v.Enabled || v.Value != "" {
			t.Errorf("variable %s = %+v, want it declared, empty and off", name, v)
		}
	}
}

// A server written as one hole is not an address: the document says where the address goes without
// knowing it, so the variable is left empty and off rather than sent as `<<baseUrl>>` on the wire.
func TestImportLeavesAServerThatIsOnlyAHoleEmpty(t *testing.T) {
	document := []byte(`{"openapi": "3.1.0", "info": {"title": "PIM"},
		"servers": [{"url": "<<baseUrl>>"}],
		"paths": {"/attributes": {"get": {"responses": {"200": {"description": "OK"}}}}}}`)

	doc, err := Import(document)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	collection := doc.Collections[0]
	if base := variable(t, collection, "baseUrl"); base.Value != "" || base.Enabled {
		t.Errorf("baseUrl = %+v, want a switched-off variable for a server nobody named", base)
	}
	if url := collection.Items[0].URL; url != "{{baseUrl}}/attributes" {
		t.Errorf("url = %q, want the address written against the variable", url)
	}
}

// A body is what the document describes: the media type this app is for, or the form it can still
// compose by hand.
func TestImportBuildsBodiesFromSchemas(t *testing.T) {
	collection, _ := read(t, "example.json")
	created := node(t, collection, "Create a product")
	if created.BodyKind != domain.BodyJSON {
		t.Fatalf("kind = %q, want a JSON body", created.BodyKind)
	}
	// The placeholders are written the way Hoppscotch writes them: a person sees what the document
	// expects, not an empty body.
	if created.Body != "{\n  \"name\": \"string\",\n  \"price\": 0\n}" {
		t.Errorf("body = %q, want the skeleton of the schema", created.Body)
	}

	added := operationAt(t, collection, "POST", "{{baseUrl}}/cart/items")
	if added.BodyKind != domain.BodyRaw || added.Body != "sku=" {
		t.Errorf("body = %+v, want the form written as text", added)
	}
}

// What a document declares about authorization becomes this app's own: a bearer scheme points at a
// variable, an operation that asks for nothing is `none`, and a scheme with no shape here is
// counted rather than guessed at.
func TestImportReadsAuthorization(t *testing.T) {
	collection, doc := read(t, "example.json")
	if collection.Auth == nil || collection.Auth.Type != domain.AuthBearer ||
		collection.Auth.Answer("token") != "{{token}}" {
		t.Errorf("auth = %+v, want the document's bearer scheme", collection.Auth)
	}

	removed := operationAt(t, collection, "DELETE", "{{baseUrl}}/products/:id")
	if removed.Auth == nil || removed.Auth.Type != domain.AuthNone {
		t.Errorf("auth = %+v, want none where the document asked for none", removed.Auth)
	}
	// A request that says nothing inherits, which is a nil auth here.
	if list := node(t, collection, "listProducts"); list.Auth != nil {
		t.Errorf("auth = %+v, want nothing where the document said nothing", list.Auth)
	}
	// An OAuth 2.0 scheme arrives as the grant the document declares, with the addresses it runs
	// against: a flow this app can hold is not one to drop.
	if health := node(t, collection, "Health"); health.Auth == nil ||
		health.Auth.Type != domain.AuthOAuth2 ||
		health.Auth.Answer("grant") != "implicit" ||
		health.Auth.Answer("authUrl") != "https://example.com/auth" {
		t.Errorf("auth = %+v, want the document's implicit grant", health.Auth)
	}

	var schemes int
	for _, skip := range doc.Skipped {
		if skip.Code == domain.CodeSkippedAuthScheme {
			schemes = skip.Count
		}
	}
	if schemes != 0 {
		t.Errorf("skipped = %+v, want no scheme skipped", doc.Skipped)
	}
}

// A scheme offering several flows is read as the first one Hoppscotch reads: authorization code,
// implicit, password, client credentials. What the requirement asks the scheme with — its scopes —
// is what the grant carries, and the client pair arrives as variables a person fills in once.
func TestImportReadsTheFirstOAuthFlowTheDocumentOffers(t *testing.T) {
	document := []byte(`{"openapi": "3.1.0", "info": {"title": "Auth"},
		"components": {"securitySchemes": {"oauth": {
			"type": "oauth2",
			"flows": {
				"clientCredentials": {"tokenUrl": "https://example.com/cc"},
				"password": {"tokenUrl": "https://example.com/pw"},
				"implicit": {"authorizationUrl": "https://example.com/imp"},
				"authorizationCode": {"authorizationUrl": "https://example.com/authorize",
					"tokenUrl": "https://example.com/token"}}}}},
		"security": [{"oauth": ["read", "write"]}],
		"paths": {"/a": {"get": {"responses": {"200": {"description": "OK"}}}}}}`)

	doc, err := Import(document)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	collection := doc.Collections[0]
	if collection.Auth == nil || collection.Auth.Type != domain.AuthOAuth2 {
		t.Fatalf("auth = %+v, want the oauth2 scheme", collection.Auth)
	}
	if collection.Auth.Answer("grant") != "authorization_code" ||
		collection.Auth.Answer("authUrl") != "https://example.com/authorize" ||
		collection.Auth.Answer("tokenUrl") != "https://example.com/token" {
		t.Errorf("auth = %+v, want the authorization code flow", collection.Auth)
	}
	if collection.Auth.Answer("scope") != "read write" {
		t.Errorf("scope = %q, want the requirement's scopes", collection.Auth.Answer("scope"))
	}
	if id := variable(t, collection, "clientId"); id.Enabled || id.Value != "" {
		t.Errorf("clientId = %+v, want the switched-off variable the scheme points at", id)
	}
	if got := collection.Auth.Answer("clientId"); got != "{{clientId}}" {
		t.Errorf("clientId = %q, want the variable indirection", got)
	}
}

// A generator may write a security entry as a bare array of scheme names — `["bearerAuth"]` — where
// the specification asks for an object. The entry names no scheme the document declares, so the
// document reads with no authorization — which is what Hoppscotch makes of it too, and not a
// refusal: the rest of the document is still a document.
func TestImportLeavesASecurityEntryWrittenAsAnArrayWithoutAScheme(t *testing.T) {
	collection, _ := read(t, "bare-security.json")
	if collection.Auth != nil {
		t.Errorf("auth = %+v, want no scheme from a bare array entry", collection.Auth)
	}
	if list := node(t, collection, "List products"); list.Method != "GET" {
		t.Errorf("request = %+v, want the document's operation read anyway", list)
	}
}

// YAML and JSON are one document: the same file in the two notations reads the same way.
func TestImportReadsYAMLAsJSON(t *testing.T) {
	fromJSON, _ := read(t, "example.json")
	fromYAML, _ := read(t, "example.yaml")

	if len(fromYAML.Children) != len(fromJSON.Children) ||
		len(fromYAML.Variables) != len(fromJSON.Variables) {
		t.Fatalf("YAML = %d folders and %d variables, JSON = %d and %d",
			len(fromYAML.Children), len(fromYAML.Variables), len(fromJSON.Children), len(fromJSON.Variables))
	}
	for index, folder := range fromJSON.Children {
		other := fromYAML.Children[index]
		if folder.Name != other.Name || len(folder.Items) != len(other.Items) {
			t.Fatalf("folder %d: %+v against %+v", index, folder, other)
		}
		for at, one := range folder.Items {
			two := other.Items[at]
			if one.Name != two.Name || one.URL != two.URL || one.Body != two.Body ||
				one.BodyKind != two.BodyKind {
				t.Errorf("request %q: %+v against %+v", one.Name, one, two)
			}
		}
	}
}

// A request with no tag stands at the top level: it named no folder, and one would be a folder
// named after nothing.
func TestImportLeavesUntaggedRequestsAtTheTop(t *testing.T) {
	collection, _ := read(t, "example.json")
	if len(collection.Items) != 1 || collection.Items[0].Name != "Health" {
		t.Errorf("items = %+v, want the untagged request at the top", collection.Items)
	}
}

func TestImportRefusesWhatIsNotADocument(t *testing.T) {
	for name, data := range map[string]string{
		"not json":       `curl -X GET https://example.com`,
		"another format": `{"info": {"name": "Storefront"}, "item": []}`,
		"no version":     `{"info": {"title": "Something"}}`,
	} {
		if _, err := Import([]byte(data)); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("%s: Import = %v, want ErrNotAllowed", name, err)
		}
	}
}

// A document with no operations is refused: an import that wrote an empty collection would be an
// import that said it did something.
func TestImportRefusesADocumentWithNothingInIt(t *testing.T) {
	empty := []byte(`{"openapi": "3.1.0", "info": {"title": "Empty"}, "paths": {}}`)
	if _, err := Import(empty); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("Import = %v, want ErrNotAllowed", err)
	}
}

// A tag the document declares and no operation uses is not written: a folder with nothing in it is
// a row that opens onto nothing, and the declaration is a category rather than a request group.
func TestImportLeavesAnUnusedTagOut(t *testing.T) {
	document := []byte(`{"openapi": "3.1.0", "info": {"title": "Thin"},
		"tags": [{"name": "empty"}],
		"paths": {"/a": {"get": {"responses": {"200": {"description": "OK"}}}}}}`)

	doc, err := Import(document)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	collection := doc.Collections[0]
	if len(collection.Children) != 0 {
		t.Errorf("children = %+v, want no folder for a tag nobody used", collection.Children)
	}
	if len(collection.Items) != 1 || collection.Items[0].URL != "{{baseUrl}}/a" {
		t.Errorf("items = %+v, want the untagged request at the top", collection.Items)
	}
}

// A document that names an operation nowhere leaves it under the plain placeholder — the name
// Hoppscotch writes, and not a name invented out of the path.
func TestImportLeavesAnUnnamedOperationUnderThePlaceholderName(t *testing.T) {
	document := []byte(`{"openapi": "3.1.0", "info": {"title": "Thin"},
		"paths": {"/a": {"get": {"responses": {"200": {"description": "OK"}}}}}}`)

	doc, err := Import(document)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if name := doc.Collections[0].Items[0].Name; name != "Untitled Request" {
		t.Errorf("name = %q, want the placeholder", name)
	}
}

// The document's title is the name a person reads, and the identifier is the generator's: a slug is
// not a name, so the title wins wherever the document has both.
func TestImportNamesARequestByTheDocumentTitle(t *testing.T) {
	document := []byte(`{"openapi": "3.1.0", "info": {"title": "PIM"},
		"paths": {"/a": {"get": {"operationId": "_", "summary": "List products",
			"responses": {"200": {"description": "OK"}}}}}}`)

	doc, err := Import(document)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if name := doc.Collections[0].Items[0].Name; name != "List products" {
		t.Errorf("name = %q, want the document's own title", name)
	}
}

// The spec spells the api-key scheme `apiKey`, and a document is read by what it says rather than
// by the case it was typed in.
func TestImportReadsAnAPIKeyScheme(t *testing.T) {
	document := []byte(`{"openapi": "3.1.0", "info": {"title": "Keys"},
		"components": {"securitySchemes": {"key": {"type": "apiKey", "name": "X-Key", "in": "header"}}},
		"security": [{"key": []}],
		"paths": {"/a": {"get": {"responses": {"200": {"description": "OK"}}}}}}`)

	doc, err := Import(document)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	auth := doc.Collections[0].Auth
	if auth == nil || auth.Type != domain.AuthAPIKey || auth.Answer("key") != "X-Key" ||
		auth.Answer("place") != domain.PlaceHeader {
		t.Errorf("auth = %+v, want the api-key scheme the document declared", auth)
	}
	if len(doc.Skipped) != 0 {
		t.Errorf("skips = %+v, want nothing skipped", doc.Skipped)
	}
}
