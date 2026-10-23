package convert

// The transfer feature over fakes: what a source read stays here for one Apply, and everything the
// fakes were handed is read back by the test rather than by a database.

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"

	"schemo/internal/cloud"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// postmanFile is a small real Postman v2.1 collection: two requests at the top level and one folder
// holding a third, which is what the preview's counters are read off.
const postmanFile = `{
  "info": {
    "name": "Ledger",
    "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
  },
  "item": [
    {
      "name": "List users",
      "request": {"method": "GET", "url": {"raw": "https://api.example.com/users"}}
    },
    {
      "name": "Create user",
      "request": {"method": "POST", "url": {"raw": "https://api.example.com/users"}}
    },
    {
      "name": "Admin",
      "item": [
        {
          "name": "Ping",
          "request": {"method": "GET", "url": {"raw": "https://api.example.com/ping"}}
        }
      ]
    }
  ]
}`

// postmanEnvironment is an environment export rather than a collection, told apart by the scope the
// file writes; its second value is the secret the export may or may not carry.
const postmanEnvironment = `{
  "name": "Staging",
  "_postman_variable_scope": "environment",
  "values": [
    {"key": "baseUrl", "value": "https://st.example.com", "enabled": true, "type": "default"},
    {"key": "apiKey", "value": "s3cret", "enabled": true, "type": "secret"}
  ]
}`

// fakeCollections stands in for the tree: it records what Import was handed and answers Full,
// FullOf, TreeOf and Requests from what a test seeded.
type fakeCollections struct {
	imported []domain.Collection
	parentID string
	// importedTree is the tree Import answers with, so a test sees what the summary carried back.
	importedTree []domain.Collection
	full         domain.Collection
	fullID       string
	trees        map[string][]domain.Collection
	requests     []domain.ResolvedRequest
}

func (f *fakeCollections) Import(_ context.Context, collections []domain.Collection,
	parentID string) ([]domain.Collection, error) {
	f.imported = collections
	f.parentID = parentID
	return f.importedTree, nil
}

func (f *fakeCollections) Full(_ context.Context, id string) (domain.Collection, error) {
	f.fullID = id
	return f.full, nil
}

func (f *fakeCollections) FullOf(_ context.Context, _, _ string) (domain.Collection, error) {
	return f.full, nil
}

func (f *fakeCollections) TreeOf(_ context.Context, workspace string) ([]domain.Collection, error) {
	return f.trees[workspace], nil
}

func (f *fakeCollections) Requests(_ context.Context, _, _ string) ([]domain.ResolvedRequest, error) {
	return f.requests, nil
}

// fakeEnvironments answers the list a test seeded and records what was imported and which secret
// values were asked for, since reading a value is the one thing an export must be asked to do.
type fakeEnvironments struct {
	drafts   []domain.EnvironmentDraft
	state    domain.EnvState
	revealed []string
	reveal   string
}

func (f *fakeEnvironments) ImportState(_ context.Context,
	draft domain.EnvironmentDraft) (domain.EnvState, error) {
	f.drafts = append(f.drafts, draft)
	return f.state, nil
}

func (f *fakeEnvironments) State(context.Context) (domain.EnvState, error) {
	return f.state, nil
}

func (f *fakeEnvironments) Reveal(_ context.Context, variableID string) (string, error) {
	f.revealed = append(f.revealed, variableID)
	return f.reveal, nil
}

// fakeWorkspaces answers the workspaces a test seeded, the way the copy source lists them.
type fakeWorkspaces struct{ state domain.WorkspaceState }

func (f *fakeWorkspaces) Snapshot(context.Context) (domain.WorkspaceState, error) {
	return f.state, nil
}

// fakeFiles answers one picked file's bytes: reading a file is what the preview over it is about.
type fakeFiles struct{ content []byte }

func (f *fakeFiles) Read(string) ([]byte, error) { return f.content, nil }

// fakeRemote answers one URL's bytes and records the address it was asked for: a page is read at
// the address of its document rather than at the one a person pasted, and that is worth saying.
type fakeRemote struct {
	content []byte
	address string
}

func (f *fakeRemote) Fetch(_ context.Context, address string) ([]byte, error) {
	f.address = address
	return f.content, nil
}

// fakeCommands reads a paste as the nodes a test seeded and writes a marker per request, so the
// joined text says which requests were written and in what order.
type fakeCommands struct {
	nodes []domain.CollectionNode
	asked []bool
}

func (f *fakeCommands) Read(string) ([]domain.CollectionNode, error) { return f.nodes, nil }

func (f *fakeCommands) Curl(_ context.Context, request domain.ResolvedRequest,
	secrets bool) (string, error) {
	f.asked = append(f.asked, secrets)
	return "curl " + request.Node.URL, nil
}

// harness is the service over the six fakes, so a test can say which side of a call it is looking
// at — what the window got back, or what the dependency was handed.
type harness struct {
	transfer     *Transfer
	collections  *fakeCollections
	environments *fakeEnvironments
	workspaces   *fakeWorkspaces
	files        *fakeFiles
	remote       *fakeRemote
	commands     *fakeCommands
}

func newHarness() *harness {
	all := &harness{
		collections:  &fakeCollections{},
		environments: &fakeEnvironments{},
		workspaces:   &fakeWorkspaces{},
		files:        &fakeFiles{},
		remote:       &fakeRemote{},
		commands:     &fakeCommands{},
	}
	all.transfer = NewTransfer(all.collections, all.environments, all.workspaces, all.files, all.remote,
		all.commands, platform.NewIDGen(), nil)
	return all
}

// The dialog says what a file holds before the person commits, so the counters are the document's
// own — a folder counts with everything inside it, because a copy would take it all.
func TestPreviewCountsWhatAFileHolds(t *testing.T) {
	h := newHarness()
	h.files.content = []byte(postmanFile)

	preview, err := h.transfer.previewFile(context.Background(), FormatPostman, "ledger.postman.json")
	if err != nil {
		t.Fatalf("PreviewFile: %v", err)
	}

	if preview.Name != "Ledger" {
		t.Errorf("name = %q, want the collection's own name", preview.Name)
	}
	if preview.Size != len(postmanFile) {
		t.Errorf("size = %d, want the payload's length %d", preview.Size, len(postmanFile))
	}
	if preview.Collections != 1 || preview.Requests != 3 || preview.Folders != 1 {
		t.Errorf("counts = %d collections, %d requests, %d folders, want 1, 3, 1",
			preview.Collections, preview.Requests, preview.Folders)
	}
	if preview.Token == "" {
		t.Error("token is empty, want one for the apply to name this read")
	}
	if len(preview.Skipped) != 0 {
		t.Errorf("skipped = %+v, want nothing left out of this file", preview.Skipped)
	}
}

// A page is asked for the document it stands on rather than for the address a person pasted: what
// they have is where the page is read, and the collection is served one path further in.
func TestAPageIsReadAtTheDocumentBesideIt(t *testing.T) {
	h := newHarness()
	h.remote.content = pageDocument(t)

	preview, err := h.transfer.previewUrl(context.Background(), FormatPage,
		"https://pages.example/petstore")
	if err != nil {
		t.Fatalf("PreviewUrl: %v", err)
	}

	if h.remote.address != "https://pages.example/petstore/snapshot.json" {
		t.Errorf("fetched %q, want the page's own document", h.remote.address)
	}
	if preview.Name != "Petstore" {
		t.Errorf("name = %q, want the collection the page documents", preview.Name)
	}
	if preview.Requests != 1 || preview.Environments != 1 {
		t.Errorf("counts = %d requests, %d environments, want 1 and 1",
			preview.Requests, preview.Environments)
	}
}

// pageDocument is a page's own bytes as a server serves them, written by the publish side: what
// the import reads is what a publish writes, and a hand-made document would prove neither half.
func pageDocument(t *testing.T) []byte {
	t.Helper()
	packed, _, err := cloud.Encode(snapshotv1.Snapshot{
		Format:  snapshotv1.Format,
		Version: snapshotv1.Version,
		Collection: snapshotv1.Collection{
			Name: "Petstore",
			Groups: []snapshotv1.Group{{Name: "Petstore", Endpoints: []snapshotv1.Endpoint{
				{Name: "List pets", Method: "GET", Path: "/pets"},
			}}},
		},
		Environments: []snapshotv1.Environment{
			{Name: "Production", BaseURL: "https://api.example.com"},
		},
	})
	if err != nil {
		t.Fatalf("encoding the page's document: %v", err)
	}
	return packed
}

// Apply writes what the preview read and consumes the token: applying the same read twice would
// write the file twice, which is what the token being good once is for.
func TestApplyWritesWhatThePreviewRead(t *testing.T) {
	h := newHarness()
	h.files.content = []byte(postmanFile)
	h.collections.importedTree = []domain.Collection{{ID: "stored", Name: "Ledger"}}
	ctx := context.Background()

	preview, err := h.transfer.previewFile(ctx, FormatPostman, "ledger.postman.json")
	if err != nil {
		t.Fatalf("PreviewFile: %v", err)
	}

	summary, err := h.transfer.Apply(ctx, preview.Token, "", "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(h.collections.imported) != 1 {
		t.Fatalf("imported = %d collections, want the document's one", len(h.collections.imported))
	}
	written := h.collections.imported[0]
	if written.Name != "Ledger" {
		t.Errorf("imported name = %q, want the document's own", written.Name)
	}
	if len(written.Items) != 2 || written.Items[0].Name != "List users" ||
		written.Items[1].Name != "Create user" {
		t.Errorf("imported requests = %+v, want the two at the top level", written.Items)
	}
	if len(written.Children) != 1 || written.Children[0].Name != "Admin" ||
		len(written.Children[0].Items) != 1 || written.Children[0].Items[0].Name != "Ping" {
		t.Errorf("imported children = %+v, want the folder with the request inside it",
			written.Children)
	}
	if len(summary.Tree) != 1 || summary.Tree[0].ID != "stored" {
		t.Errorf("tree = %+v, want the tree the fake answered", summary.Tree)
	}
	if summary.Collections != 1 || summary.Requests != 3 || summary.Name != "Ledger" {
		t.Errorf("summary = %+v, want the counts of what was written", summary)
	}

	_, err = h.transfer.Apply(ctx, preview.Token, "", "")
	if domain.CodeOf(err) != domain.CodeImportExpired {
		t.Errorf("second Apply = %v, want %v", err, domain.CodeImportExpired)
	}
}

// A paste of commands has no name of its own, so the window's wording fills the gap; a document
// that names its collection keeps that name instead.
func TestApplyFillsTheNameTheDocumentLeftEmpty(t *testing.T) {
	h := newHarness()
	h.commands.nodes = []domain.CollectionNode{
		{Method: "GET", URL: "https://api.example.com/users"},
		{Method: "POST", URL: "https://api.example.com/users"},
	}
	ctx := context.Background()

	preview, err := h.transfer.previewText(ctx, FormatCurl, "curl https://api.example.com/users")
	if err != nil {
		t.Fatalf("PreviewText: %v", err)
	}
	if preview.Name != "" {
		t.Fatalf("preview name = %q, want none from a paste of commands", preview.Name)
	}

	if _, err := h.transfer.Apply(ctx, preview.Token, "", "Imported from cURL"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(h.collections.imported) != 1 ||
		h.collections.imported[0].Name != "Imported from cURL" {
		t.Errorf("imported = %+v, want the name the window filled in", h.collections.imported)
	}

	h.files.content = []byte(postmanFile)
	preview, err = h.transfer.previewFile(ctx, FormatPostman, "ledger.postman.json")
	if err != nil {
		t.Fatalf("PreviewFile: %v", err)
	}
	if _, err := h.transfer.Apply(ctx, preview.Token, "", "Imported from cURL"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if name := h.collections.imported[0].Name; name != "Ledger" {
		t.Errorf("imported name = %q, want the document's own, not the window's wording", name)
	}
}

// An environment file writes its environment and stops there: the variables arrive as the file
// describes them, and the list keeps the active environment it had — importing is not switching.
func TestApplyImportsEnvironmentsWithoutActivating(t *testing.T) {
	h := newHarness()
	h.files.content = []byte(postmanEnvironment)
	h.environments.state = domain.EnvState{
		Environments: []domain.Environment{{ID: "staging", Name: "Staging"}},
		ActiveID:     "previous",
	}
	ctx := context.Background()

	preview, err := h.transfer.previewFile(ctx, FormatPostman, "staging.postman_environment.json")
	if err != nil {
		t.Fatalf("PreviewFile: %v", err)
	}
	if preview.Environments != 1 || preview.Collections != 0 {
		t.Fatalf("preview = %+v, want the one environment and no collection", preview)
	}

	summary, err := h.transfer.Apply(ctx, preview.Token, "", "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(h.collections.imported) != 0 {
		t.Errorf("imported %d collections, want an environment file to write none",
			len(h.collections.imported))
	}
	if len(h.environments.drafts) != 1 {
		t.Fatalf("imported %d environments, want the document's one", len(h.environments.drafts))
	}
	draft := h.environments.drafts[0]
	if draft.Name != "Staging" {
		t.Errorf("imported name = %q, want the file's own", draft.Name)
	}
	if len(draft.Variables) != 2 || draft.Variables[0].Name != "baseUrl" ||
		draft.Variables[1].Name != "apiKey" {
		t.Fatalf("variables = %+v, want the environment's two", draft.Variables)
	}
	if draft.Variables[1].Kind != domain.VariableSecret {
		t.Errorf("apiKey kind = %q, want the secret the file marks", draft.Variables[1].Kind)
	}
	if summary.Environments != 1 || summary.EnvState == nil ||
		summary.EnvState.ActiveID != "previous" {
		t.Errorf("summary = %+v, want the imported environment and the list as it was", summary)
	}
}

// A document holding neither half is refused by the readers, so the apply that would write nothing
// cannot be reached; an apply of a read this service never made is refused as expired.
func TestApplyRefusesAnEmptyDocument(t *testing.T) {
	h := newHarness()
	ctx := context.Background()

	if _, err := h.transfer.Apply(ctx, "never-issued", "", ""); domain.CodeOf(err) !=
		domain.CodeImportExpired {
		t.Errorf("unknown token = %v, want %v", err, domain.CodeImportExpired)
	}

	h.remote.content = []byte(`{"schemo":"1.0"}`)
	_, err := h.transfer.previewUrl(ctx, FormatSchemo, "https://example.com/empty.json")
	if domain.CodeOf(err) != domain.CodeNothingToImport {
		t.Errorf("empty document = %v, want %v", err, domain.CodeNothingToImport)
	}
}

// The source that copies from another workspace draws what a copy would take, so each workspace carries its
// collections with the size of the whole subtree — and the open workspace is marked, not left out.
func TestWorkspacesCountsWhatACopyWouldTake(t *testing.T) {
	h := newHarness()
	h.workspaces.state = domain.WorkspaceState{
		Workspaces: []domain.Workspace{
			{ID: "other", Name: "Shared"},
			{ID: "personal", Name: "Personal"},
		},
		ActiveID: "personal",
	}
	h.collections.trees = map[string][]domain.Collection{
		"other": {{
			ID:   "payments",
			Name: "Payments",
			Items: []domain.CollectionNode{
				{Name: "Charge", Method: "POST"},
				{Name: "Refund", Method: "POST"},
			},
			Children: []domain.Collection{{
				ID:    "refunds",
				Name:  "Refunds",
				Items: []domain.CollectionNode{{Name: "List", Method: "GET"}},
			}},
		}},
	}

	workspaces, err := h.transfer.Workspaces(context.Background())
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}

	if len(workspaces) != 2 || workspaces[0].ID != "other" || workspaces[1].ID != "personal" {
		t.Fatalf("workspaces = %+v, want both in the order the list holds them", workspaces)
	}
	if workspaces[0].Active || !workspaces[1].Active {
		t.Errorf("active flags = %v and %v, want only the open workspace marked",
			workspaces[0].Active, workspaces[1].Active)
	}
	if len(workspaces[0].Collections) != 1 {
		t.Fatalf("other workspace holds %d collections, want the one", len(workspaces[0].Collections))
	}
	taken := workspaces[0].Collections[0]
	if taken.ID != "payments" || taken.Name != "Payments" {
		t.Errorf("collection = %+v, want the one of that workspace", taken)
	}
	if taken.Requests != 3 || taken.Folders != 1 {
		t.Errorf("counts = %d requests in %d folders, want 3 in 1", taken.Requests, taken.Folders)
	}
}

// A copy out of another workspace takes the collection whole — folders, requests, scripts and
// variables — because the walk that reads it there is the one an export writes down.
func TestImportFromWorkspaceCopiesTheCollectionsWhole(t *testing.T) {
	h := newHarness()
	h.collections.full = domain.Collection{
		ID:        "payments",
		Name:      "Payments",
		Variables: []domain.Variable{{Name: "version", Value: "v2"}},
		Items: []domain.CollectionNode{{
			Name: "Charge", Method: "POST", URL: "https://api.example.com/charge",
			Scripts: &domain.Scripts{Post: "pm.test('ok', () => {});"},
		}},
		Children: []domain.Collection{{
			ID:    "refunds",
			Name:  "Refunds",
			Items: []domain.CollectionNode{{Name: "List", Method: "GET"}},
		}},
	}
	h.collections.importedTree = []domain.Collection{{ID: "stored", Name: "Payments"}}
	ctx := context.Background()

	summary, err := h.transfer.ImportFromWorkspace(ctx, "other", []string{"payments"}, "parent")
	if err != nil {
		t.Fatalf("ImportFromWorkspace: %v", err)
	}

	if len(h.collections.imported) != 1 {
		t.Fatalf("imported = %d collections, want the one asked for", len(h.collections.imported))
	}
	copied := h.collections.imported[0]
	if copied.Name != "Payments" || h.collections.parentID != "parent" {
		t.Errorf("imported %q at %q, want the copy where the person asked for it",
			copied.Name, h.collections.parentID)
	}
	if len(copied.Variables) != 1 || copied.Variables[0].Name != "version" {
		t.Errorf("variables = %+v, want the level's own carried whole", copied.Variables)
	}
	if len(copied.Items) != 1 || copied.Items[0].Scripts == nil ||
		copied.Items[0].Scripts.Post != "pm.test('ok', () => {});" {
		t.Errorf("items = %+v, want the request with its script", copied.Items)
	}
	if summary.Collections != 1 || summary.Requests != 2 || summary.Name != "Payments" {
		t.Errorf("summary = %+v, want the counts of what was copied", summary)
	}
	if len(summary.Tree) != 1 || summary.Tree[0].ID != "stored" {
		t.Errorf("tree = %+v, want the tree the fake answered", summary.Tree)
	}

	_, err = h.transfer.ImportFromWorkspace(ctx, "other", nil, "")
	if domain.CodeOf(err) != domain.CodeNothingToImport {
		t.Errorf("no ids = %v, want %v", err, domain.CodeNothingToImport)
	}
}

// A secret's value is the one thing an export may read out of the database, and only when the
// person asked for it: without the ask the file names the variable and carries no value.
func TestExportAsksForSecretValuesOnlyWhenAsked(t *testing.T) {
	h := newHarness()
	h.collections.full = domain.Collection{ID: "ledger", Name: "Ledger"}
	h.environments.state = domain.EnvState{Environments: []domain.Environment{{
		ID:   "staging",
		Name: "Staging",
		Vars: []domain.Variable{
			{ID: "v1", Name: "baseUrl", Value: "https://st.example.com", Kind: domain.VariableText},
			{ID: "v2", Name: "apiKey", Kind: domain.VariableSecret, HasValue: true},
		},
	}}}
	h.environments.reveal = "revealed-key"
	ctx := context.Background()

	export, err := h.transfer.export(ctx, "ledger", FormatSchemo, false)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if export.Name != "Ledger" || h.collections.fullID != "ledger" {
		t.Errorf("export = %q for %q, want the collection Full was asked for",
			export.Name, h.collections.fullID)
	}
	if len(h.environments.revealed) != 0 {
		t.Errorf("revealed %v, want no value read without being asked", h.environments.revealed)
	}
	if got := exportedVariable(t, export.Data, "apiKey"); !got.found || !got.secret ||
		got.value != "" {
		t.Errorf("apiKey = %+v, want it named as a secret with no value", got)
	}
	if got := exportedVariable(t, export.Data, "baseUrl"); got.value != "https://st.example.com" {
		t.Errorf("baseUrl = %+v, want a text value carried as it is", got)
	}

	export, err = h.transfer.export(ctx, "ledger", FormatSchemo, true)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !slices.Equal(h.environments.revealed, []string{"v2"}) {
		t.Errorf("revealed %v, want the secret the export asked for", h.environments.revealed)
	}
	if got := exportedVariable(t, export.Data, "apiKey"); got.value != "revealed-key" {
		t.Errorf("apiKey = %+v, want the value the person asked to carry", got)
	}
}

// A command is not a file: cURL is what Curl writes, so asking Export for it is the window saying
// the wrong thing rather than a format that failed.
func TestExportRefusesACommandFile(t *testing.T) {
	h := newHarness()
	h.collections.full = domain.Collection{ID: "ledger", Name: "Ledger"}

	_, err := h.transfer.export(context.Background(), "ledger", FormatCurl, false)
	if domain.CodeOf(err) != domain.CodeUnknownFormat {
		t.Errorf("Export as curl = %v, want %v", err, domain.CodeUnknownFormat)
	}
}

// The paste back into this app reads `# name` above each command, so writing one names the request
// and joins the commands with a blank line — and the secrets are as the caller asked.
func TestCurlJoinsACommandPerRequestUnderItsName(t *testing.T) {
	h := newHarness()
	h.collections.requests = []domain.ResolvedRequest{
		{Node: domain.CollectionNode{Name: "List users", URL: "https://api.example.com/users"}},
		{Node: domain.CollectionNode{Name: "Create user", URL: "https://api.example.com/users/42"}},
	}
	ctx := context.Background()

	text, err := h.transfer.Curl(ctx, "ledger", true)
	if err != nil {
		t.Fatalf("Curl: %v", err)
	}
	want := "# List users\ncurl https://api.example.com/users\n\n" +
		"# Create user\ncurl https://api.example.com/users/42"
	if text != want {
		t.Errorf("curl = %q, want %q", text, want)
	}
	if !slices.Equal(h.commands.asked, []bool{true, true}) {
		t.Errorf("secrets asked = %v, want each command written with them", h.commands.asked)
	}

	if _, err := h.transfer.Curl(ctx, "ledger", false); err != nil {
		t.Fatalf("Curl: %v", err)
	}
	if !slices.Equal(h.commands.asked, []bool{true, true, false, false}) {
		t.Errorf("secrets asked = %v, want them withheld where the caller asked", h.commands.asked)
	}

	h.collections.requests = nil
	_, err = h.transfer.Curl(ctx, "ledger", false)
	if domain.CodeOf(err) != domain.CodeNodeHasNoRequests {
		t.Errorf("empty level = %v, want %v", err, domain.CodeNodeHasNoRequests)
	}
}

// exportedVar is one variable of a Schemo file's environment list, read back out of the bytes so a
// test can tell a value that was left out from a variable that was.
type exportedVar struct {
	value  string
	found  bool
	secret bool
}

func exportedVariable(t *testing.T, data []byte, name string) exportedVar {
	t.Helper()

	var file struct {
		Environments []struct {
			Variables []struct {
				Name   string `json:"name"`
				Value  string `json:"value"`
				Secret bool   `json:"secret"`
			} `json:"variables"`
		} `json:"environments"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("unmarshalling the export: %v", err)
	}
	for _, environment := range file.Environments {
		for _, variable := range environment.Variables {
			if variable.Name == name {
				return exportedVar{value: variable.Value, found: true, secret: variable.Secret}
			}
		}
	}
	return exportedVar{}
}
