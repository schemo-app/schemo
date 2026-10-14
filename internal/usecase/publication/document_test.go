package publication

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"

	"schemo/internal/domain"
)

// shipped is a document as a page serves it: the writer's own bytes, because the reader is the
// other half of the writer and a scenario that built its own bytes would be testing a shape
// neither side writes.
func shipped(t *testing.T, snapshot snapshotv1.Snapshot) []byte {
	t.Helper()
	snapshot.Format = snapshotv1.Format
	snapshot.Version = snapshotv1.Version
	data, _, err := Encode(snapshot)
	if err != nil {
		t.Fatalf("encoding the document: %v", err)
	}
	return data
}

// compressed is any bytes as a page serves them, for a document written by hand: the reader is
// handed what travels, and what travels is compressed.
func compressed(t *testing.T, raw string) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write([]byte(raw)); err != nil {
		t.Fatalf("compressing the document: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("compressing the document: %v", err)
	}
	return out.Bytes()
}

// What a page shows of a request is what an import has to hold: its address written against the
// variable the page named an environment with, its parameters, its body and its examples. This is
// the whole promise of the reader — a page is not a one-way door.
func TestARequestOfAPageComesBackAsThisAppsOwn(t *testing.T) {
	document, err := AsDocument(shipped(t, snapshotv1.Snapshot{
		Collection: snapshotv1.Collection{
			Name:        "Example API",
			Description: "Where the products are",
			Groups: []snapshotv1.Group{{Name: "Example API", Endpoints: []snapshotv1.Endpoint{{
				Name:        "Create a product",
				Method:      "POST",
				Path:        "/v1/products",
				Description: "Writes one.",
				Parameters: []snapshotv1.Parameter{
					{Name: "page", In: "query", Example: "2"},
					{Name: "id", In: "path", Example: "42"},
				},
				Body: &snapshotv1.Body{
					Fields:  []snapshotv1.Field{{Name: "sku", Type: "string"}},
					Example: `{"sku":"abc"}`,
				},
				Responses: []snapshotv1.Response{{Status: 201, Name: "Created", Body: `{"id":"1"}`}},
			}}}},
			Variables: []snapshotv1.Variable{{Key: "shop", Value: "north"}},
		},
		Prose: snapshotv1.Prose{Auth: "Send a token.", Errors: "400 means the body is wrong."},
	}))
	if err != nil {
		t.Fatalf("AsDocument: %v", err)
	}

	if len(document.Collections) != 1 {
		t.Fatalf("collections = %d, want the one the page documents", len(document.Collections))
	}
	collection := document.Collections[0]
	if collection.Name != "Example API" || collection.Description != "Where the products are" {
		t.Errorf("collection = %q, %q, want the page's own title and lead",
			collection.Name, collection.Description)
	}
	if collection.Docs.Authorization != "Send a token." ||
		collection.Docs.Errors != "400 means the body is wrong." {
		t.Errorf("docs = %+v, want the notes the author wrote for the page", collection.Docs)
	}
	if len(collection.Variables) != 1 || collection.Variables[0].Name != "shop" ||
		collection.Variables[0].Value != "north" {
		t.Errorf("variables = %+v, want the one the page may show", collection.Variables)
	}

	if len(collection.Items) != 1 {
		t.Fatalf("items = %d, want the request of the collection's own level", len(collection.Items))
	}
	node := collection.Items[0]
	if node.URL != "{{baseUrl}}/v1/products" {
		t.Errorf("url = %q, want the path written against the page's own variable", node.URL)
	}
	if node.Method != "POST" || node.Name != "Create a product" || node.Description != "Writes one." {
		t.Errorf("node = %+v, want the request as the page shows it", node)
	}
	if len(node.Params) != 1 || node.Params[0].Name != "page" || node.Params[0].Value != "2" {
		t.Errorf("params = %+v, want the query row with its example", node.Params)
	}
	if len(node.PathParams) != 1 || node.PathParams[0].Name != "id" {
		t.Errorf("pathParams = %+v, want the path row where the page says it is read", node.PathParams)
	}
	if node.BodyKind != domain.BodyJSON || node.Body != `{"sku":"abc"}` {
		t.Errorf("body = %q (%s), want the example the page showed", node.Body, node.BodyKind)
	}
	if len(node.Responses) != 1 || node.Responses[0].Status != 201 {
		t.Errorf("responses = %+v, want the saved answer with its status", node.Responses)
	}
	if len(node.Responses) == 1 && node.Responses[0].Body != `{"id":"1"}` {
		t.Errorf("response body = %q, want the example the page showed", node.Responses[0].Body)
	}
}

// A page flattens a tree to one heading per level, and the heading it names after the collection
// is the level the collection itself stands on: the rest of them are folders, or what a person
// imports would be a collection of folders of one collection.
func TestTheHeadingsOfAPageComeBackAsFolders(t *testing.T) {
	document, err := AsDocument(shipped(t, snapshotv1.Snapshot{
		Collection: snapshotv1.Collection{
			Name: "Example API",
			Groups: []snapshotv1.Group{
				{Name: "Example API", Endpoints: []snapshotv1.Endpoint{
					{Name: "List products", Method: "GET", Path: "/v1/products"},
				}},
				{Name: "Orders", Endpoints: []snapshotv1.Endpoint{
					{Name: "Place an order", Method: "POST", Path: "/v1/orders"},
					{Name: "Read an order", Method: "GET", Path: "/v1/orders/{id}"},
				}},
			},
		},
	}))
	if err != nil {
		t.Fatalf("AsDocument: %v", err)
	}

	collection := document.Collections[0]
	if len(collection.Items) != 1 || collection.Items[0].Name != "List products" {
		t.Errorf("items = %+v, want the collection's own requests where they were", collection.Items)
	}
	if len(collection.Children) != 1 {
		t.Fatalf("children = %d, want one folder for the one heading that is not the collection",
			len(collection.Children))
	}
	folder := collection.Children[0]
	if folder.Name != "Orders" || len(folder.Items) != 2 {
		t.Errorf("folder = %q with %d requests, want Orders with the two under it",
			folder.Name, len(folder.Items))
	}
	if folder.Position <= collection.Items[0].Position {
		t.Errorf("positions = folder %d after request %d, want the page's own order kept",
			folder.Position, collection.Items[0].Position)
	}
}

// The addresses a page was written against are what an imported collection resolves its requests
// with: without them a request taken off a page would stand nowhere.
func TestTheAddressesOfAPageComeBackAsEnvironments(t *testing.T) {
	document, err := AsDocument(shipped(t, snapshotv1.Snapshot{
		Collection: snapshotv1.Collection{
			Name: "Example API",
			Groups: []snapshotv1.Group{{
				Name:      "Example API",
				Endpoints: []snapshotv1.Endpoint{{Name: "List", Path: "/v1"}},
			}},
		},
		Environments: []snapshotv1.Environment{
			{Name: "Production", BaseURL: "https://api.example.com"},
			{Name: "Staging", BaseURL: "https://staging.example.com"},
			{Name: "Nowhere"},
		},
	}))
	if err != nil {
		t.Fatalf("AsDocument: %v", err)
	}

	if len(document.Environments) != 2 {
		t.Fatalf("environments = %d, want the two that stand somewhere", len(document.Environments))
	}
	first := document.Environments[0]
	if first.Name != "Production" {
		t.Errorf("name = %q, want the page's own name for it", first.Name)
	}
	if len(first.Variables) != 1 {
		t.Fatalf("variables = %+v, want the one address an environment is", first.Variables)
	}
	held := first.Variables[0]
	if held.Name != domain.BaseURLVariable || held.Value != "https://api.example.com" {
		t.Errorf("variable = %q = %q, want the base address the page named", held.Name, held.Value)
	}
}

// A document written by another build is refused rather than read as if it were this one: the
// fields this build knows may mean something else in it, and a collection that quietly came out
// wrong is worse than a message.
func TestADocumentOfAnotherVersionIsRefused(t *testing.T) {
	newer := `{"format":"schemo.collection-snapshot","version":2,"collection":{"name":"X"}}`

	_, err := AsDocument(compressed(t, newer))
	if code := domain.CodeOf(err); code != domain.CodeSnapshotVersion {
		t.Errorf("code = %q, want %q for a document of another version", code,
			domain.CodeSnapshotVersion)
	}
}

// A document is expanded under a bound rather than after one: a page that serves a bomb has already
// filled memory by the time its unpacked length is known, and it is this side that pays for it.
func TestADocumentThatExpandsWithoutEndIsRefused(t *testing.T) {
	bomb := compressed(t, strings.Repeat("a", maxUnpackedBytes+1))

	_, err := AsDocument(bomb)
	failure := domain.AsFailure(err)
	if failure == nil || failure.Code != domain.CodeFileTooLarge {
		t.Fatalf("err = %v, want a refusal of a document past the bound", err)
	}
	if failure.Args["limit"] != "16" {
		t.Errorf("limit = %q, want the bound that was crossed", failure.Args["limit"])
	}
}

// What a page sends is bounded before it is expanded as well: the compressed bytes are the ones a
// server chose, and a bound that only guards the expansion would read them all first.
func TestAPagePastThePackedBoundIsRefused(t *testing.T) {
	_, err := AsDocument(make([]byte, maxPackedBytes+1))
	failure := domain.AsFailure(err)
	if failure == nil || failure.Code != domain.CodeFileTooLarge {
		t.Fatalf("err = %v, want a refusal of a document past the bound", err)
	}
	if failure.Args["limit"] != "4" {
		t.Errorf("limit = %q, want the bound that was crossed", failure.Args["limit"])
	}
}

// Bytes that are not a page's document are refused as one thing rather than three: what arrived is
// a page somebody pointed the app at, and whether it was gzipped, parseable or of this format is
// not something a person acts on differently.
func TestBytesThatAreNotAPagesDocumentAreRefused(t *testing.T) {
	_, err := AsDocument([]byte("<!doctype html><html><body>Hello</body></html>"))
	if code := domain.CodeOf(err); code != domain.CodeNotSchemo {
		t.Errorf("code = %q, want %q for what is not a document", code, domain.CodeNotSchemo)
	}
}

// A page is read one path further in than it stands: the address a link carries is where a person
// reads the collection, and the bytes are served from the document beside it.
func TestAPageIsReadAtTheDocumentBesideIt(t *testing.T) {
	address, err := DocumentAddress("https://pages.example/petstore/")
	if err != nil {
		t.Fatalf("DocumentAddress: %v", err)
	}
	want := "https://pages.example/petstore/snapshot.json"
	if address != want {
		t.Errorf("address = %q, want %q", address, want)
	}
}

// A link opens whatever it names, and a document is not something to go and fetch from a scheme
// this app has no business reading.
func TestALinkToAnotherSchemeIsRefused(t *testing.T) {
	for _, address := range []string{"ftp://pages.example/petstore", "file:///tmp/petstore",
		"petstore", ""} {
		if _, err := DocumentAddress(address); domain.CodeOf(err) != domain.CodeURLUnreachable {
			t.Errorf("DocumentAddress(%q) = %v, want a refusal to fetch it", address, err)
		}
	}
}
