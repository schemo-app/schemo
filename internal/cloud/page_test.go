package cloud

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"schemo/internal/domain"
	"strings"
	"testing"
	"time"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
)

// catalogue is a collection of two requests, which is the least a changelog has anything to say
// about: one request to keep, and one to leave out.
func catalogue() domain.Collection {
	return domain.Collection{
		ID:   "collection-1",
		Name: "Example API",
		Items: []domain.CollectionNode{
			{ID: "node-1", Name: "List products", Method: "GET", URL: "{{baseUrl}}/v1/products"},
			{ID: "node-2", Name: "Delete product", Method: "DELETE",
				URL: "{{baseUrl}}/v1/products/{{id}}"},
		},
	}
}

// addressed is the one variable every environment has: the address its requests stand on.
func addressed(base string) []domain.Variable {
	return []domain.Variable{{Name: domain.BaseURLVariable, Value: base}}
}

// expanded is the document a publish carried, opened: a page is stored compressed, and a test that
// searched the bytes would be searching noise.
func expanded(t *testing.T, packed []byte) string {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatalf("opening the document: %v", err)
	}
	defer reader.Close()
	document, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading the document: %v", err)
	}
	return string(document)
}

// noted reports whether the walk said something about a piece of the collection.
func noted(report Report, code domain.Code) bool {
	for _, problem := range report.Problems {
		if problem.Code == code {
			return true
		}
	}
	return false
}

// treeWith is the document of one collection, built as a publish would build it.
func treeWith(t *testing.T, tree domain.Collection) (snapshotv1.Snapshot, Report) {
	t.Helper()
	publications := newPublications(newStore(t), &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tree}

	snapshot, report, err := publications.Build(context.Background(), "space-1", tree.ID, "env-1", "en")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return snapshot, report
}

// What a page shows of a request is written field by field, so an id of the tree has nowhere to
// arrive: the names a page uses are derived from them one way, and a claim staked with a real id
// is not one a stranger can read.
func TestAPageNamesItsNodesWithoutTheirIds(t *testing.T) {
	document, _ := json.Marshal(mustBuild(t, published()))

	for _, held := range []string{"collection-1", "node-1"} {
		if bytes.Contains(document, []byte(held)) {
			t.Errorf("the document carries the id %q", held)
		}
	}
	if !bytes.Contains(document, []byte(PageID("collection-1", "node-1"))) {
		t.Error("the document carries no name of its own for the request")
	}
}

// The one thing a page may not do is carry a value the app already hides, and hiding it is not a
// setting: a scope read together means a variable written in terms of a hidden one is hidden too.
func TestAVariableWrittenInTermsOfAHiddenOneIsHidden(t *testing.T) {
	hidden := hiddenNames([]domain.Variable{
		{Name: "API_TOKEN", Value: "s3cr3t", Kind: domain.VariableSecret},
		{Name: "SEARCH_URL", Value: "{{baseUrl}}/v1/search?key={{API_TOKEN}}"},
	}, []domain.Variable{
		{Name: "SHOP_ID", Value: "12"},
	})

	if hidden["SEARCH_URL"] != domain.HiddenLinked {
		t.Errorf("SEARCH_URL is %q, want it hidden for what it is written in terms of",
			hidden["SEARCH_URL"])
	}
	if _, held := hidden["SHOP_ID"]; held {
		t.Error("SHOP_ID is hidden, want an ordinary variable carried")
	}
}

// A secret nobody marked is the one that would otherwise be published by accident, so a name that
// reads like a credential's is enough on its own — however the name is spelled.
func TestANameThatReadsLikeACredentialIsHidden(t *testing.T) {
	for _, name := range []string{"SUSPICIOUS_TOKEN", "apiKey", "api-key", "Db.Password"} {
		hidden := hiddenNames([]domain.Variable{{Name: name, Value: "x", Enabled: true}})
		if hidden[name] != domain.HiddenLooksSecret {
			t.Errorf("%s is %q, want it hidden for its name alone", name, hidden[name])
		}
	}
}

// A token names a variable, so the name of a hidden one is part of what is hidden: a page read by a
// stranger is no place for `{{API_TOKEN}}`, whatever it would have resolved to.
func TestASecretsNameIsMaskedWhereItIsUsed(t *testing.T) {
	hidden := map[string]domain.HiddenReason{"API_TOKEN": domain.HiddenSecret}

	masked := maskText("https://api.example.com/v1?key={{API_TOKEN}}", hidden)

	if strings.Contains(masked, "API_TOKEN") {
		t.Errorf("masked = %q, want the name gone as well as the value", masked)
	}
	if !strings.Contains(masked, domain.SecretMask) {
		t.Errorf("masked = %q, want the app's own mask in its place", masked)
	}
}

// A body kept as a file is a path on somebody's machine rather than a fact about the request, so
// the page leaves it out whole and says so.
func TestABinaryBodyIsLeftOutWithAProblem(t *testing.T) {
	tree := published()
	tree.Items[0].BodyKind = domain.BodyBinary
	tree.Items[0].BodyFile = `C:\Users\someone\Pictures\photo.png`

	snapshot, report := treeWith(t, tree)
	document, _ := json.Marshal(snapshot)

	if snapshot.Collection.Groups[0].Endpoints[0].Body != nil {
		t.Error("the endpoint carries a body, want none for one kept as a file")
	}
	if bytes.Contains(document, []byte(`C:\Users`)) {
		t.Error("the document carries a path of somebody's machine")
	}
	if !noted(report, domain.CodeBodyTypeUnsupported) {
		t.Errorf("problems = %+v, want the body noted", report.Problems)
	}
}

// A parameter named like a credential keeps its name and its description and loses its example: an
// example of a key is a key.
func TestAnExampleOfACredentialIsLeftOut(t *testing.T) {
	tree := published()
	tree.Items[0].Params = []domain.Row{{
		Name: "api_key", Value: "live-1234567890", Description: "the key to send", Enabled: true,
	}}

	snapshot, _ := treeWith(t, tree)

	held := snapshot.Collection.Groups[0].Endpoints[0].Parameters[0]
	if held.Example != "" {
		t.Errorf("example = %q, want none beside a credential's name", held.Example)
	}
	if held.Description != "the key to send" {
		t.Errorf("description = %q, want what the author wrote", held.Description)
	}
}

// A page draws the levels of a collection side by side, so a folder inside a folder is headed by
// its own name: the folder it was written in is no part of what it is called.
func TestAFolderInsideAFolderIsHeadedByItsOwnName(t *testing.T) {
	tree := published()
	tree.Children = []domain.Collection{{
		ID:   "collection-2",
		Name: "Admin",
		Items: []domain.CollectionNode{{
			ID: "node-2", Name: "List users", Method: "GET", URL: "{{baseUrl}}/v1/admin/users",
		}},
	}}

	snapshot, _ := treeWith(t, tree)

	headings := []string{}
	for _, group := range snapshot.Collection.Groups {
		headings = append(headings, group.Name)
	}
	want := []string{"Example API", "Admin"}
	if strings.Join(headings, ", ") != strings.Join(want, ", ") {
		t.Errorf("headings = %v, want %v", headings, want)
	}
}

// The base address is shown once, in a card of its own, so a row carries the path without it —
// whether the address was written absolutely or in terms of the base variable.
func TestABaseAddressIsTakenOffThePath(t *testing.T) {
	for _, address := range []string{
		"{{baseUrl}}/v1/products",
		"https://api.example.com/v1/products",
	} {
		tree := published()
		tree.Items[0].URL = address

		snapshot, _ := treeWith(t, tree)
		held := snapshot.Collection.Groups[0].Endpoints[0].Path
		if held != "/v1/products" {
			t.Errorf("%s is drawn as %q, want the path alone", address, held)
		}
	}
}

// An address this app does not speak is refused rather than drawn as a link a reader would click.
func TestAnAddressOfAnotherProtocolIsRefused(t *testing.T) {
	tree := published()
	tree.Items[0].URL = "file:///etc/passwd"
	publications := newPublications(newStore(t), &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tree}

	_, _, err := publications.Build(context.Background(), "space-1", "collection-1", "env-1", "en")

	if domain.CodeOf(err) != domain.CodeProtocolUnsupported {
		t.Fatalf("err = %v, want the address refused", err)
	}
}

// An update of the application, or a reader in another language, is not a change to a collection:
// the digest drops both, which is what lets a device say a page is behind without publishing.
func TestADigestIgnoresTheBuildAndTheLanguage(t *testing.T) {
	base := snapshotv1.Snapshot{
		Format: snapshotv1.Format, Version: snapshotv1.Version,
		Collection: snapshotv1.Collection{Name: "Example API"},
	}
	_, first, err := Encode(base)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	other := base
	other.Generator = snapshotv1.Generator{Name: "Schemo", Version: "9.9.9"}
	other.Locale = "ru"
	_, second, err := Encode(other)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if first != second {
		t.Errorf("digests %q and %q differ, want the same document", first, second)
	}
}

// What the digest does notice is the collection, which is the whole of what a publish carries.
func TestADigestNoticesAChangedCollection(t *testing.T) {
	base := snapshotv1.Snapshot{
		Format: snapshotv1.Format, Version: snapshotv1.Version,
		Collection: snapshotv1.Collection{Name: "Example API"},
	}
	_, first, err := Encode(base)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	other := base
	other.Collection.Name = "Example API v2"
	_, second, err := Encode(other)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if first == second {
		t.Error("digests are equal, want an edited collection to be a different document")
	}
}

// mustBuild is a document built for a test that only reads it back.
func mustBuild(t *testing.T, tree domain.Collection) snapshotv1.Snapshot {
	t.Helper()
	snapshot, _ := treeWith(t, tree)
	return snapshot
}

// What an author wrote for a page goes to the page's own blocks, and the collection's description
// is its lead: it is written once and read in both places, and a page whose lead differed would
// be a page about another collection.
func TestTheAuthorsWordsAreThePagesProse(t *testing.T) {
	tree := published()
	tree.Description = "Everything the shop sells.\n\n## Authorization\nSend the key in the header.\n\n## Errors\nA refused order answers 409."

	snapshot, _ := treeWith(t, tree)

	want := snapshotv1.Prose{
		Lead:   "Everything the shop sells.",
		Auth:   "Send the key in the header.",
		Errors: "A refused order answers 409.",
	}
	if snapshot.Prose != want {
		t.Errorf("prose = %+v, want %+v", snapshot.Prose, want)
	}
	if snapshot.Collection.Description != tree.Description {
		t.Errorf("description = %q, want the complete Markdown document", snapshot.Collection.Description)
	}
}

// The notes are written in the same editor as everything else and go through the same mask: the
// name of a secret is part of the secret, and prose is no place for it to stop being true.
func TestASecretWrittenIntoProseIsMasked(t *testing.T) {
	tree := published()
	tree.Variables = []domain.Variable{{
		Name: "API_TOKEN", Value: "s3cr3t", Kind: domain.VariableSecret, Enabled: true,
	}}
	tree.Description = "## Errors\nA refused call answers 401 — check {{API_TOKEN}}."

	snapshot, _ := treeWith(t, tree)
	document, _ := json.Marshal(snapshot)

	for _, secret := range []string{"API_TOKEN", "s3cr3t"} {
		if bytes.Contains(document, []byte(secret)) {
			t.Errorf("the document carries %q in the prose: %q", secret, snapshot.Prose.Errors)
		}
	}
}

// The addresses a page may be called from are every environment that stands somewhere, and the one
// the page was built against stands first, because that is the address a reader is handed. An
// environment with no address of its own is left out rather than named with nothing beside it.
func TestThePageNamesEveryEnvironmentItMayBeCalledFrom(t *testing.T) {
	publications := newPublications(newStore(t), &fakePublicationServer{}, publicationCaller{})
	publications.environments = fakeEnvironments{state: domain.EnvState{
		Environments: []domain.Environment{
			{ID: "env-1", Name: "Production", Vars: addressed("https://api.example.com")},
			{ID: "env-2", Name: "Blank"},
			{ID: "env-3", Name: "Staging", Vars: addressed("https://staging.example.com")},
		},
		ActiveID: "env-1",
	}}

	snapshot, _, err := publications.Build(context.Background(), "space-1", "collection-1", "env-3", "en")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	held := []string{}
	for _, environment := range snapshot.Environments {
		held = append(held, environment.Name+" "+environment.BaseURL)
	}
	want := []string{"Staging https://staging.example.com", "Production https://api.example.com"}
	if strings.Join(held, ", ") != strings.Join(want, ", ") {
		t.Errorf("environments = %v, want %v", held, want)
	}
}

// What a page says has moved at it is read off the shape the last publish put up: a request that
// arrived, one that changed while staying the same request, and one that went. The three are said
// in the order the page draws them, and a request that is gone is said last, where it has a place.
func TestAPublishSaysWhatMovedAtThePage(t *testing.T) {
	store := newStore(t)
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	tree := catalogue()
	publications.collections = fakeCollections{tree: tree}
	ctx := context.Background()

	if _, err := publications.publish(ctx, publishing("collection-1")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	tree.Items = []domain.CollectionNode{
		{ID: "node-1", Name: "List all products", Method: "GET", URL: "{{baseUrl}}/v1/products"},
		{ID: "node-3", Name: "Create product", Method: "POST", URL: "{{baseUrl}}/v1/products"},
	}
	publications.collections = fakeCollections{tree: tree}

	snapshot, _, err := publications.Build(ctx, "space-1", "collection-1", "env-1", "en")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(snapshot.Changes) != 2 {
		t.Fatalf("changes = %+v, want the one release", snapshot.Changes)
	}
	held := []string{}
	for _, item := range snapshot.Changes[0].Items {
		held = append(held, item.Sign+" "+item.Name)
	}
	want := []string{"~ List all products", "+ Create product", "- Delete product"}
	if strings.Join(held, ", ") != strings.Join(want, ", ") {
		t.Errorf("changes = %v, want %v", held, want)
	}
}

// A page nobody moved says nothing: a publish that carries what the last one carried adds no entry,
// and the history a device holds is carried exactly as it was.
func TestAPageNobodyMovedGainsNoRelease(t *testing.T) {
	store := newStore(t)
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: catalogue()}
	ctx := context.Background()

	if _, err := publications.publish(ctx, publishing("collection-1")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	snapshot, _, err := publications.Build(ctx, "space-1", "collection-1", "env-1", "en")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(snapshot.Changes) != 1 || snapshot.Changes[0].Revision != 1 || snapshot.Changes[0].Items[0].Fields[0] != "initial:2" {
		t.Errorf("changes = %+v, want nothing said about a page nobody moved", snapshot.Changes)
	}
}

// A page's history grows at the front: the release a publish writes stands above the ones before
// it, and what was said about the page last time is still said under it.
func TestAPagesHistoryGrowsAtTheFront(t *testing.T) {
	store := newStore(t)
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	tree := catalogue()
	publications.collections = fakeCollections{tree: tree}
	ctx := context.Background()

	publish := func() {
		t.Helper()
		if _, err := publications.publish(ctx, publishing("collection-1")); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}
	publish()

	tree.Items = tree.Items[:1]
	publications.collections = fakeCollections{tree: tree}
	publish()

	tree.Items[0].Name = "List all products"
	publications.collections = fakeCollections{tree: tree}
	publish()

	held, _ := store.find("collection-1")
	if len(held.Changes) != 3 {
		t.Fatalf("changes = %+v, want the two releases", held.Changes)
	}
	said := []string{held.Changes[0].Items[0].Sign, held.Changes[1].Items[0].Sign}
	if strings.Join(said, ", ") != "~, -" {
		t.Errorf("signs = %v, want the newest release first", said)
	}
}

// A page's history is a window and not a journal: a release older than the last two hundred falls
// off the end, which is the ceiling the server holds a document to and will not draw past.
func TestAnOlderReleaseFallsOffTheEnd(t *testing.T) {
	history := make([]domain.PageRelease, 0, maxReleases)
	for at := range maxReleases {
		history = append(history, domain.PageRelease{
			At:    time.UnixMilli(int64(at)),
			Items: []domain.PageItem{{Sign: signAdded}},
		})
	}

	held := appended(history, []domain.PageItem{{Sign: signChanged}}, time.UnixMilli(maxReleases), maxReleases+1)

	if len(held) != maxReleases {
		t.Fatalf("history is %d releases, want %d", len(held), maxReleases)
	}
	if held[0].At != time.UnixMilli(maxReleases) {
		t.Errorf("the newest release is %v, want the one just written", held[0].At)
	}
	if held[len(held)-1].At != history[len(history)-2].At {
		t.Errorf("the oldest release is %v, want the one before the oldest", held[len(held)-1].At)
	}
}

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
			Description: "Where the products are\n\n## Authorization\nSend a token.\n\n## Errors\n400 means the body is wrong.",
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
	if collection.Name != "Example API" || collection.Description != "Where the products are\n\n## Authorization\nSend a token.\n\n## Errors\n400 means the body is wrong." {
		t.Errorf("collection = %q, %q, want the page's own title and lead",
			collection.Name, collection.Description)
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

// A page's document is refused when it is another version, not a document at all, or too large
// packed or unpacked; a size refusal names the limit in MB.
func TestADocumentThatCannotBeReadIsRefused(t *testing.T) {
	for _, c := range []struct {
		what  string
		bytes []byte
		code  domain.Code
		limit string
	}{
		{"another version", compressed(t, `{"format":"schemo.collection-snapshot","version":2,"collection":{"name":"X"}}`), domain.CodeSnapshotVersion, ""},
		{"not a document", []byte("<!doctype html><html><body>Hello</body></html>"), domain.CodeNotSchemo, ""},
		{"too large packed", make([]byte, maxPackedBytes+1), domain.CodeFileTooLarge, "4"},
		{"too large unpacked", compressed(t, strings.Repeat("a", maxUnpackedBytes+1)), domain.CodeFileTooLarge, "16"},
	} {
		_, err := AsDocument(c.bytes)
		failure := domain.AsFailure(err)
		if failure == nil || failure.Code != c.code || failure.Args["limit"] != c.limit {
			t.Errorf("%s: err = %v, want %s (limit %q)", c.what, err, c.code, c.limit)
		}
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

func TestDescriptionSections(t *testing.T) {
	tests := []struct {
		name, markdown string
		want           snapshotv1.Prose
	}{
		{
			name:     "sections retain nested Markdown and leave other sections in the lead",
			markdown: "Intro.\n\n## **Authorization**\nSend a token.\n\n### Headers\n`Authorization`\n\n## Environments\nLocal and production.\n\n## Errors\n- 400: invalid\n\n## Rate limits\n60 requests.",
			want: snapshotv1.Prose{
				Lead: "Intro.\n\n## Environments\nLocal and production.",
				Auth: "Send a token.\n\n### Headers\n`Authorization`", Errors: "- 400: invalid", Limits: "60 requests.",
			},
		},
		{
			name:     "fenced and quoted headings do not create sections",
			markdown: "```md\n## Errors\nexample\n```\n\n> ## Limits\n> example\n\n### Authorization\nNested.",
			want:     snapshotv1.Prose{Lead: "```md\n## Errors\nexample\n```\n\n> ## Limits\n> example\n\n### Authorization\nNested."},
		},
		{
			name:     "formatted links code and multiline headings retain their visible titles",
			markdown: "## [**Auth**](https://example.com)\nSend a token.\n\n## `Errors`\nBad request.\n\nRate\nlimits\n------\n60 requests.",
			want:     snapshotv1.Prose{Auth: "Send a token.", Errors: "Bad request.", Limits: "60 requests."},
		},
		{
			name:     "setext headings and CRLF are parsed as Markdown",
			markdown: "Intro.\r\n\r\nErrors\r\n------\r\nBad request.\r\n\r\n## Limits\r\n60 requests.\r\n",
			want:     snapshotv1.Prose{Lead: "Intro.", Errors: "Bad request.", Limits: "60 requests."},
		},
		{
			name:     "duplicate sections retain their content and H1 closes a section",
			markdown: "## Errors\nFirst.\n\n## Errors\nSecond.\n\n# More\nOther text.",
			want:     snapshotv1.Prose{Lead: "# More\nOther text.", Errors: "First.\n\nSecond."},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := descriptionProse(test.markdown); got != test.want {
				t.Errorf("prose = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestLocalizedDescriptionHeadings(t *testing.T) {
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			raw, err := os.ReadFile("../../frontend/src/i18n/locales/" + locale + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var catalog struct {
				Collections map[string]string `json:"collections"`
			}
			if err := json.Unmarshal(raw, &catalog); err != nil {
				t.Fatal(err)
			}
			var document strings.Builder
			for _, key := range []string{"docsAuthorization", "docsErrors", "docsLimits"} {
				document.WriteString("## " + catalog.Collections[key] + "\n" + key + "\n\n")
			}
			want := snapshotv1.Prose{Auth: "docsAuthorization", Errors: "docsErrors", Limits: "docsLimits"}
			if got := descriptionProse(document.String()); got != want {
				t.Errorf("prose = %+v, want %+v", got, want)
			}
		})
	}
}
