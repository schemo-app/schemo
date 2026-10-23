package openapi

import (
	"errors"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"schemo/internal/domain"
)

// reimport is one collection through the document and back, as the collection the reader builds.
func reimport(t *testing.T, collection domain.Collection) domain.Collection {
	t.Helper()
	data, err := Export(collection)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	doc, err := Import(data)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(doc.Collections) != 1 {
		t.Fatalf("collections = %d, want one", len(doc.Collections))
	}
	return doc.Collections[0]
}

// A request comes back as it left: its address against the server variable, its method, its name,
// its headers and its body. The query rows return parked and out of the address, as examples.
func TestExportRoundTripsRequests(t *testing.T) {
	collection := domain.Collection{
		Name:        "Storefront",
		Description: "Products and the cart.",
		Variables: []domain.Variable{
			{Name: "baseUrl", Value: "https://api.example.com/v1",
				Kind: domain.VariableText, Enabled: true},
		},
		Items: []domain.CollectionNode{
			{
				Name: "Create a product", Method: "POST", URL: "{{baseUrl}}/products?page[size]=25",
				Params: []domain.Row{
					{Name: "page[size]", Value: "25", Enabled: true, Description: "Page size"},
					{Name: "include", Value: "offers", Enabled: false},
				},
				Headers: []domain.Row{
					{Name: "X-Tenant", Value: "example", Enabled: true, Description: "Which tenant"},
					{Name: "X-Parked", Value: "no", Enabled: false},
				},
				Body: `{"name":"Widget","price":9}`, BodyKind: domain.BodyJSON,
			},
			{
				Name: "One product", Method: "GET", URL: "{{baseUrl}}/products/:id",
				PathParams: []domain.Row{
					{Name: "id", Value: "7", Enabled: true, Description: "Which product"},
				},
			},
		},
	}

	back := reimport(t, collection)

	if back.Name != "Storefront" || back.Description != "Products and the cart." {
		t.Errorf("collection = %+v, want the name and description it was exported with", back)
	}
	base := variable(t, back, "baseUrl")
	if base.Value != "https://api.example.com/v1" || !base.Enabled {
		t.Errorf("baseUrl = %+v, want the server's address", base)
	}

	created := node(t, back, "Create a product")
	if created.Method != "POST" || created.URL != "{{baseUrl}}/products" {
		t.Errorf("request = %+v, want its method and an address without the optional query", created)
	}
	if len(created.Params) != 1 || created.Params[0].Name != "page[size]" ||
		created.Params[0].Value != "25" || created.Params[0].Enabled ||
		created.Params[0].Description != "Page size" {
		t.Errorf("params = %+v, want the enabled row back parked with its value and description",
			created.Params)
	}
	if len(created.Headers) != 1 || created.Headers[0].Name != "X-Tenant" ||
		created.Headers[0].Value != "example" || created.Headers[0].Enabled ||
		created.Headers[0].Description != "Which tenant" {
		t.Errorf("headers = %+v, want the enabled header back parked with its value and description",
			created.Headers)
	}
	if created.BodyKind != domain.BodyJSON || created.Body != `{"name":"Widget","price":9}` {
		t.Errorf("body = %q of kind %q, want the JSON body back", created.Body, created.BodyKind)
	}

	one := node(t, back, "One product")
	if one.Method != "GET" || one.URL != "{{baseUrl}}/products/:id" {
		t.Errorf("request = %+v, want the path parameter back", one)
	}
	if len(one.PathParams) != 1 || one.PathParams[0].Value != "7" ||
		one.PathParams[0].Description != "Which product" {
		t.Errorf("path params = %+v, want the value and the description back", one.PathParams)
	}
	for _, one := range back.Variables {
		if one.Name == "id" {
			t.Errorf("variable = %+v, want a path parameter that is no variable", one)
		}
	}
}

// A variable in a path is a variable still: the document keeps it as written, and the reader finds
// it as one rather than as a parameter of the request.
func TestExportKeepsAVariableInAPathAsAVariable(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Items: []domain.CollectionNode{
			{Name: "Versioned", Method: "GET", URL: "{{baseUrl}}/{{version}}/products"},
		},
	}
	back := reimport(t, collection)
	one := node(t, back, "Versioned")
	if one.URL != "{{baseUrl}}/{{version}}/products" || len(one.PathParams) != 0 {
		t.Errorf("request = %+v, want the variable kept in the address and no path parameter", one)
	}
}

// A folder becomes the tag its requests carry, and comes back as the folder it was with the request
// inside it.
func TestExportWritesFoldersAsTags(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Variables: []domain.Variable{
			{Name: "baseUrl", Value: "https://api.example.com/v1",
				Kind: domain.VariableText, Enabled: true},
		},
		Children: []domain.Collection{
			{
				Name: "products",
				Items: []domain.CollectionNode{
					{Name: "List products", Method: "GET", URL: "{{baseUrl}}/products"},
				},
			},
		},
	}

	back := reimport(t, collection)

	if len(back.Items) != 0 {
		t.Errorf("items = %+v, want the request inside its folder and not at the top", back.Items)
	}
	if len(back.Children) != 1 || back.Children[0].Name != "products" {
		t.Fatalf("children = %+v, want the folder back under its own name", back.Children)
	}
	if len(back.Children[0].Items) != 1 || back.Children[0].Items[0].Name != "List products" {
		t.Errorf("items = %+v, want the request inside the folder", back.Children[0].Items)
	}
}

// The document's own authorization is the collection's own, and an operation that asks for nothing
// says so. The token stays a variable the reader declares, so nothing worth hiding is written down.
func TestExportRoundTripsAuthorization(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Auth: &domain.Auth{Type: domain.AuthBearer, Fields: map[string]string{
			"prefix": "Bearer", "token": "{{token}}",
		}},
		Variables: []domain.Variable{
			{Name: "baseUrl", Value: "https://api.example.com/v1",
				Kind: domain.VariableText, Enabled: true},
		},
		Items: []domain.CollectionNode{
			{Name: "List products", Method: "GET", URL: "{{baseUrl}}/products"},
			{Name: "Open status", Method: "GET", URL: "{{baseUrl}}/status",
				Auth: &domain.Auth{Type: domain.AuthNone, Fields: map[string]string{}}},
		},
	}

	back := reimport(t, collection)

	if back.Auth == nil || back.Auth.Type != domain.AuthBearer ||
		back.Auth.Answer("token") != "{{token}}" {
		t.Errorf("auth = %+v, want the bearer scheme back", back.Auth)
	}
	if token := variable(t, back, "token"); token.Enabled {
		t.Errorf("token = %+v, want the switched-off variable the scheme points at", token)
	}

	open := node(t, back, "Open status")
	if open.Auth == nil || open.Auth.Type != domain.AuthNone {
		t.Errorf("auth = %+v, want none where the request asked for none", open.Auth)
	}
	if list := node(t, back, "List products"); list.Auth != nil {
		t.Errorf("auth = %+v, want nothing where the request inherits the collection's", list.Auth)
	}
}

// A collection with nothing in it writes a document all the same — its name, its version — and
// importing that document back is refused: there would be no request to write.
func TestExportOfAnEmptyCollection(t *testing.T) {
	data, err := Export(domain.Collection{Name: "Storefront"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		t.Fatalf("the export is not YAML: %v", err)
	}
	if tree["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v, want 3.1.0", tree["openapi"])
	}

	if _, err := Import(data); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("Import = %v, want ErrNotAllowed for a document with nothing to import", err)
	}
}

// The format requires a version for the document and an answer for every operation: the export
// writes the ones it can — a plain version, and the plain success.
func TestExportWritesTheVersionAndTheSuccessTheFormatDemands(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Items: []domain.CollectionNode{
			{Name: "List products", Method: "GET", URL: "{{baseUrl}}/products"},
		},
	}
	data, err := Export(collection)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		t.Fatalf("the export is not YAML: %v", err)
	}

	info, _ := tree["info"].(map[string]any)
	if info["version"] != "1.0.0" {
		t.Errorf("info.version = %v, want the version the format requires", info["version"])
	}
	paths, _ := tree["paths"].(map[string]any)
	item, _ := paths["/products"].(map[string]any)
	op, _ := item["get"].(map[string]any)
	responses, _ := op["responses"].(map[string]any)
	if _, ok := responses["200"]; !ok {
		t.Errorf("responses = %v, want the success the format requires", responses)
	}
}

// A row named for a header the document states elsewhere — the authorization, the content type,
// the accept — is left out of the parameters, and the token in it with it.
func TestExportLeavesHeadersTheDocumentStatesElsewhereOut(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Items: []domain.CollectionNode{
			{
				Name: "List products", Method: "GET", URL: "{{baseUrl}}/products",
				Headers: []domain.Row{
					{Name: "Authorization", Value: "Bearer s3cret-token", Enabled: true},
					{Name: "Content-Type", Value: "application/json", Enabled: true},
					{Name: "X-Tenant", Value: "example", Enabled: true},
				},
			},
		},
	}
	data, err := Export(collection)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if strings.Contains(string(data), "s3cret-token") {
		t.Errorf("the export carries the token:\n%s", data)
	}

	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		t.Fatalf("the export is not YAML: %v", err)
	}
	paths, _ := tree["paths"].(map[string]any)
	item, _ := paths["/products"].(map[string]any)
	op, _ := item["get"].(map[string]any)
	parameters, _ := op["parameters"].([]any)
	if len(parameters) != 1 {
		t.Fatalf("parameters = %v, want only the header the document has nowhere else to state",
			parameters)
	}
	if one, _ := parameters[0].(map[string]any); one["name"] != "X-Tenant" {
		t.Errorf("parameter = %v, want X-Tenant", parameters[0])
	}
}

// A request's own scheme carries the base name where the collection declared none of its kind —
// numbered only when that name is taken — and an api-key field nobody filled keeps a name the
// format demands.
func TestExportNamesARequestsSchemeAfterItsBaseName(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Items: []domain.CollectionNode{
			{Name: "List products", Method: "GET", URL: "{{baseUrl}}/products",
				Auth: &domain.Auth{Type: domain.AuthBasic, Fields: map[string]string{}}},
			{Name: "List orders", Method: "GET", URL: "{{baseUrl}}/orders",
				Auth: &domain.Auth{Type: domain.AuthAPIKey, Fields: map[string]string{}}},
		},
	}
	data, err := Export(collection)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		t.Fatalf("the export is not YAML: %v", err)
	}

	components, _ := tree["components"].(map[string]any)
	schemes, _ := components["securitySchemes"].(map[string]any)
	if _, ok := schemes["basicAuth"]; !ok {
		t.Errorf("schemes = %v, want the request's scheme under its base name", schemes)
	}
	key, _ := schemes["apiKey"].(map[string]any)
	if key == nil || key["name"] != "api_key" {
		t.Errorf("apiKey = %v, want a name the format demands", key)
	}
}

// The examples a request carries are what the document answers with, and the trip through it is
// whole: the name, the status, the content type and the body as the example held it.
func TestExportWritesExamplesAsResponses(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Items: []domain.CollectionNode{{
			Name: "List products", Method: "GET", URL: "{{baseUrl}}/products",
			Responses: []domain.ResponseExample{{
				Name: "The list", Status: 200, Body: `{"data":[]}`,
				Headers: []domain.HeaderPair{{Name: "Content-Type", Value: "application/json"}},
			}},
		}},
	}

	back := reimport(t, collection)

	one := back.Items[0]
	if len(one.Responses) != 1 || one.Responses[0].Name != "The list" ||
		one.Responses[0].Status != 200 || one.Responses[0].Body != `{"data":[]}` {
		t.Fatalf("responses = %+v, want the example back", one.Responses)
	}
	if len(one.Responses[0].Headers) != 1 ||
		one.Responses[0].Headers[0].Value != "application/json" {
		t.Errorf("headers = %+v, want the content type kept", one.Responses[0].Headers)
	}
}

// A folder above another folder — one holding no request of its own — comes back whole: the tag is
// the folder's whole path, and the document says its tags nest. A folder that holds nothing and
// says nothing about itself is named by no tag a reader can turn back into a folder, so it is the
// one shape the trip does not keep.
func TestExportWritesTheNestingBack(t *testing.T) {
	collection := domain.Collection{
		Name: "Storefront",
		Variables: []domain.Variable{
			{Name: "baseUrl", Value: "https://api.example.com/v1",
				Kind: domain.VariableText, Enabled: true},
		},
		Children: []domain.Collection{
			{Name: "empty"},
			{Name: "v2", Children: []domain.Collection{
				{Name: "products", Items: []domain.CollectionNode{
					{Name: "List products", Method: "GET", URL: "{{baseUrl}}/products"},
				}},
			}},
		},
	}

	back := reimport(t, collection)

	if len(back.Children) != 1 || back.Children[0].Name != "v2" {
		t.Fatalf("children = %+v, want the folder the request's own sits in", back.Children)
	}
	v2 := back.Children[0]
	if len(v2.Children) != 1 || v2.Children[0].Name != "products" {
		t.Fatalf("children = %+v, want the nesting back", v2.Children)
	}
	if len(v2.Children[0].Items) != 1 || v2.Children[0].Items[0].Name != "List products" {
		t.Errorf("items = %+v, want the request two folders down", v2.Children[0].Items)
	}
}
