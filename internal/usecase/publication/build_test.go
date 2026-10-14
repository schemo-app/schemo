package publication

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"

	"schemo/internal/domain"
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
	uc := newTestUseCase(&fakeStore{}, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tree}

	snapshot, report, err := uc.Build(context.Background(), "space-1", tree.ID, "env-1", "en")
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
	uc := newTestUseCase(&fakeStore{}, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tree}

	_, _, err := uc.Build(context.Background(), "space-1", "collection-1", "env-1", "en")

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
	tree.Description = "Everything the shop sells."
	tree.Docs = domain.Docs{
		Authorization: "Send the key in the header.",
		Errors:        "A refused order answers 409.",
	}

	snapshot, _ := treeWith(t, tree)

	want := snapshotv1.Prose{
		Lead:   "Everything the shop sells.",
		Auth:   "Send the key in the header.",
		Errors: "A refused order answers 409.",
	}
	if snapshot.Prose != want {
		t.Errorf("prose = %+v, want %+v", snapshot.Prose, want)
	}
	if snapshot.Collection.Description != snapshot.Prose.Lead {
		t.Errorf("the description is %q and the lead %q, want the one text",
			snapshot.Collection.Description, snapshot.Prose.Lead)
	}
}

// The notes are written in the same editor as everything else and go through the same mask: the
// name of a secret is part of the secret, and prose is no place for it to stop being true.
func TestASecretWrittenIntoProseIsMasked(t *testing.T) {
	tree := published()
	tree.Variables = []domain.Variable{{
		Name: "API_TOKEN", Value: "s3cr3t", Kind: domain.VariableSecret, Enabled: true,
	}}
	tree.Docs.Errors = "A refused call answers 401 — check {{API_TOKEN}}."

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
	uc := newTestUseCase(&fakeStore{}, &fakeServer{}, fakeSession{})
	uc.environments = fakeEnvironments{state: domain.EnvState{
		Environments: []domain.Environment{
			{ID: "env-1", Name: "Production", Vars: addressed("https://api.example.com")},
			{ID: "env-2", Name: "Blank"},
			{ID: "env-3", Name: "Staging", Vars: addressed("https://staging.example.com")},
		},
		ActiveID: "env-1",
	}}

	snapshot, _, err := uc.Build(context.Background(), "space-1", "collection-1", "env-3", "en")
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
	store := &fakeStore{}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	tree := catalogue()
	uc.collections = fakeCollections{tree: tree}
	ctx := context.Background()

	if _, err := uc.Publish(ctx, "space-1", "collection-1", "env-1", domain.VisibilityUnlisted,
		"en", "", true); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	tree.Items = []domain.CollectionNode{
		{ID: "node-1", Name: "List all products", Method: "GET", URL: "{{baseUrl}}/v1/products"},
		{ID: "node-3", Name: "Create product", Method: "POST", URL: "{{baseUrl}}/v1/products"},
	}
	uc.collections = fakeCollections{tree: tree}

	snapshot, _, err := uc.Build(ctx, "space-1", "collection-1", "env-1", "en")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(snapshot.Changes) != 1 {
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
	store := &fakeStore{}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: catalogue()}
	ctx := context.Background()

	if _, err := uc.Publish(ctx, "space-1", "collection-1", "env-1", domain.VisibilityUnlisted,
		"en", "", true); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	snapshot, _, err := uc.Build(ctx, "space-1", "collection-1", "env-1", "en")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(snapshot.Changes) != 0 {
		t.Errorf("changes = %+v, want nothing said about a page nobody moved", snapshot.Changes)
	}
}

// A page's history grows at the front: the release a publish writes stands above the ones before
// it, and what was said about the page last time is still said under it.
func TestAPagesHistoryGrowsAtTheFront(t *testing.T) {
	store := &fakeStore{}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	tree := catalogue()
	uc.collections = fakeCollections{tree: tree}
	ctx := context.Background()

	publish := func() {
		t.Helper()
		if _, err := uc.Publish(ctx, "space-1", "collection-1", "env-1",
			domain.VisibilityUnlisted, "en", "", true); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}
	publish()

	tree.Items = tree.Items[:1]
	uc.collections = fakeCollections{tree: tree}
	publish()

	tree.Items[0].Name = "List all products"
	uc.collections = fakeCollections{tree: tree}
	publish()

	held, _ := store.find("collection-1")
	if len(held.Changes) != 2 {
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

	held := appended(history, []domain.PageItem{{Sign: signChanged}}, time.UnixMilli(maxReleases))

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
