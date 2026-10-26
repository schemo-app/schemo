package request

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"schemo/internal/db"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/internal/workspace"
)

const personal = domain.WorkspacePersonalID

// runs is a run feature over a real store and tree, with a sender that answers what a test set up.
type runs struct {
	*Runs
	t           *testing.T
	store       *db.Store
	collections *workspace.Collections
	sender      *runSender
	events      *runEvents
}

func newRuns(t *testing.T) *runs {
	t.Helper()
	store := dbtest.New(t)
	ids := platform.NewIDGen()
	collections := workspace.NewCollections(store, store, ids)
	envs := workspace.NewEnvironments(store, store, ids)
	events := &runEvents{finished: make(chan domain.CollectionRun, 4)}
	r := &runs{
		Runs: NewRuns(store, collections, envs, nil, nil, nil, events, ids),
		t:    t, store: store, collections: collections, events: events,
		sender: &runSender{store: store, ids: ids, answers: map[string]runAnswer{}, tests: map[string][]domain.TestResult{}},
	}
	r.send = r.sender.send
	return r
}

func (r *runs) collection(name string) string {
	r.t.Helper()
	made, err := r.collections.CreateCollection(r.t.Context(), name, "", "")
	if err != nil {
		r.t.Fatal(err)
	}
	return made.ID
}

func (r *runs) folder(name, parentID string, at int64) string {
	r.t.Helper()
	id := r.collection(name)
	if err := r.collections.Move(r.t.Context(), id, parentID, at); err != nil {
		r.t.Fatal(err)
	}
	return id
}

// request saves a request that answers 200 unless the test says otherwise.
func (r *runs) request(collectionID string, node domain.CollectionNode) string {
	r.t.Helper()
	made, err := r.collections.CreateNode(r.t.Context(), workspace.RequestDraft{
		CollectionID: collectionID, Name: node.Name, Method: "GET",
	})
	if err != nil {
		r.t.Fatal(err)
	}
	node.ID = made.ID
	if node.Method == "" {
		node.Method = "GET"
	}
	if err := r.collections.SaveNode(r.t.Context(), node); err != nil {
		r.t.Fatal(err)
	}
	r.sender.reply(domain.FillPath(node.URL, node.PathParams), 200)
	return node.ID
}

func (r *runs) run(collectionID, nodeID string) domain.CollectionRun {
	r.t.Helper()
	if _, err := r.Run(r.t.Context(), collectionID, nodeID); err != nil {
		r.t.Fatalf("Run: %v", err)
	}
	return r.events.wait(r.t)
}

// runFixture is a collection holding First, a folder with Second and Third, then Fourth.
type runFixture struct {
	*runs
	collectionID, folderID string
	ids                    map[string]string
}

func newRunFixture(t *testing.T) *runFixture {
	r := newRuns(t)
	f := &runFixture{runs: r, collectionID: r.collection("Collection"), ids: map[string]string{}}
	add := func(parent, name string) {
		f.ids[name] = r.request(parent, domain.CollectionNode{
			Name: name, URL: "https://api.example.com/" + name,
			Headers: []domain.Row{
				{ID: name + "-on", Name: "Accept", Value: "application/json", Enabled: true},
				{ID: name + "-off", Name: "X-Off", Value: "no"},
			},
		})
	}
	add(f.collectionID, "First")
	f.folderID = r.folder("Folder", f.collectionID, 1)
	add(f.folderID, "Second")
	add(f.folderID, "Third")
	add(f.collectionID, "Fourth")
	return f
}

func addr(name string) string { return "https://api.example.com/" + name }

func TestRunWalksTheTreeInDrawnOrder(t *testing.T) {
	f := newRunFixture(t)
	run := f.run(f.collectionID, "")

	want := []string{addr("First"), addr("Second"), addr("Third"), addr("Fourth")}
	if got := f.sender.urls(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sent %v, want %v", got, want)
	}
	if run.Passed != 4 || run.Failed != 0 || len(run.Results) != 4 {
		t.Fatalf("run = %+v, want four passed rows", run)
	}
	for i, result := range run.Results {
		if result.Position != int64(i) || !result.OK || *result.Status != 200 || result.RecordID == "" {
			t.Errorf("row %d = %+v", i, result)
		}
	}
	if run.FinishedAt == 0 {
		t.Error("run was not closed")
	}
}

func TestRunSendsOnlyEnabledHeaders(t *testing.T) {
	f := newRunFixture(t)
	f.run(f.collectionID, f.folderID)

	sent := f.sender.requests()
	if len(sent) != 2 || sent[0].URL != addr("Second") {
		t.Fatalf("sent %v, want the folder's two requests", f.sender.urls())
	}
	if len(sent[0].Headers) != 1 || sent[0].Headers[0].Name != "Accept" {
		t.Errorf("headers = %+v, want only the enabled row", sent[0].Headers)
	}
}

func TestRunOfAFolderOrARequestStaysInside(t *testing.T) {
	f := newRunFixture(t)
	if run := f.run(f.collectionID, f.folderID); len(run.Results) != 2 || run.NodeID != f.folderID {
		t.Errorf("folder run = %+v, want its two requests", run)
	}
	if run := f.run(f.collectionID, f.ids["Third"]); len(run.Results) != 1 ||
		run.Results[0].NodeID != f.ids["Third"] {
		t.Errorf("request run = %+v, want the one request", run)
	}
}

// A folder two levels down carries the auth and variables of every level between it and the root.
func TestRunCarriesEveryLevelAbove(t *testing.T) {
	f := newRunFixture(t)
	ctx := t.Context()
	deep := f.folder("Deep", f.folderID, 1)
	f.request(deep, domain.CollectionNode{Name: "Deep", URL: addr("Deep")})
	if err := f.collections.Update(ctx, f.collectionID, patchAuth(bearerAuth("root"))); err != nil {
		t.Fatal(err)
	}
	if err := f.collections.Update(ctx, f.folderID, patchAuth(bearerAuth("middle"))); err != nil {
		t.Fatal(err)
	}
	if err := f.collections.Update(ctx, f.folderID, patchVariables([]domain.Variable{
		{ID: "v", Name: "page", Value: "2", Enabled: true},
	})); err != nil {
		t.Fatal(err)
	}

	f.run(f.collectionID, "")
	tokens := []string{}
	for _, req := range f.sender.requests() {
		tokens = append(tokens, req.Auth.Answer("token"))
	}
	if want := "[root middle middle middle root]"; fmt.Sprint(tokens) != want {
		t.Errorf("tokens = %v, want %s", tokens, want)
	}

	f.sender.reset()
	f.run(f.collectionID, deep)
	sent := f.sender.requests()
	if len(sent) != 1 || sent[0].Auth.Answer("token") != "middle" ||
		len(sent[0].Variables) != 1 || sent[0].Variables[0].Name != "page" {
		t.Errorf("deep request = %+v, want the middle level's auth and variables", sent)
	}
}

func TestRunCountsFailuresAndSkipsAndGoesOn(t *testing.T) {
	f := newRunFixture(t)
	f.sender.answers[addr("First")] = runAnswer{transport: "server did not respond"}
	f.sender.answers[addr("Second")] = runAnswer{skipped: true}
	f.sender.answers[addr("Third")] = runAnswer{refusal: domain.Refuse(domain.CodeVariableMissing,
		domain.ErrNotAllowed, domain.Args{"names": "token"})}

	run := f.run(f.collectionID, "")
	if len(run.Results) != 4 || run.Passed != 1 || run.Failed != 2 {
		t.Fatalf("run = %d rows, %d passed, %d failed; want 4, 1, 2", len(run.Results), run.Passed, run.Failed)
	}
	failed, skipped, refused := run.Results[0], run.Results[1], run.Results[2]
	if failed.OK || failed.Status != nil || failed.Error != "server did not respond" {
		t.Errorf("failed row = %+v", failed)
	}
	if !skipped.Skipped || skipped.OK || skipped.Error != "" {
		t.Errorf("skipped row = %+v", skipped)
	}
	if refused.Failure == nil || refused.Failure.Code != domain.CodeVariableMissing || refused.RecordID != "" {
		t.Errorf("refused row = %+v, want the refusal and no record", refused)
	}
}

func TestRunRowsCarryAssertionsAndAreWritten(t *testing.T) {
	f := newRunFixture(t)
	f.sender.tests[addr("Second")] = []domain.TestResult{{Name: "a", Passed: true}, {Name: "b"}}

	run := f.run(f.collectionID, "")
	last, err := f.Last(t.Context(), f.collectionID, "")
	if err != nil || last == nil {
		t.Fatalf("Last = %v, %v", last, err)
	}
	for i, row := range last.Results {
		if row.RecordID != run.Results[i].RecordID {
			t.Errorf("row %d read back = %+v, want %+v", i, row, run.Results[i])
		}
	}
	if row := last.Results[1]; row.AssertionsPassed != 1 || row.AssertionsTotal != 2 {
		t.Errorf("row = %+v, want 1 of 2 assertions", row)
	}
	if row := last.Results[0]; row.AssertionsTotal != 0 {
		t.Errorf("row = %+v, want nothing asserted", row)
	}
}

func TestStopFinishesTheRequestInFlight(t *testing.T) {
	f := newRunFixture(t)
	f.sender.onSend = func(runRequest) { f.Stop() }
	if run := f.run(f.collectionID, ""); len(run.Results) != 1 || len(f.sender.urls()) != 1 {
		t.Errorf("run = %+v, sent %v; want one request", run, f.sender.urls())
	}
}

func TestOnlyOneRunAtATime(t *testing.T) {
	f := newRunFixture(t)
	held, release := make(chan struct{}, 4), make(chan struct{})
	f.sender.onSend = func(runRequest) { held <- struct{}{}; <-release }

	if _, err := f.Run(t.Context(), f.collectionID, ""); err != nil {
		t.Fatal(err)
	}
	<-held
	if _, err := f.Run(t.Context(), f.collectionID, ""); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("second run = %v, want ErrNotAllowed", err)
	}
	if !f.Stop() {
		t.Error("Stop found nothing to stop")
	}
	close(release)
	f.events.wait(t)

	f.sender.onSend = nil
	if f.Stop() {
		t.Error("Stop after the run reported a run")
	}
	f.run(f.collectionID, f.folderID)
}

func TestRunRefusesNothingToSend(t *testing.T) {
	r := newRuns(t)
	ctx := t.Context()
	empty := r.collection("Empty")
	if _, err := r.Run(ctx, empty, ""); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("empty run = %v, want ErrNotAllowed", err)
	}
	if last, err := r.Last(ctx, empty, ""); err != nil || last != nil {
		t.Errorf("a refused run was stored: %+v, %v", last, err)
	}
	if _, err := r.Run(ctx, "missing", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing collection = %v, want ErrNotFound", err)
	}
	foreign := r.request(r.collection("Other"), domain.CollectionNode{Name: "Foreign", URL: addr("x")})
	if _, err := r.Run(ctx, empty, foreign); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("request of another collection = %v, want ErrNotFound", err)
	}
	// A refused run does not leave the feature thinking one is going.
	if r.Stop() {
		t.Error("a refused run is still running")
	}
}

func TestLastIsPerNode(t *testing.T) {
	f := newRunFixture(t)
	if last, _ := f.Last(t.Context(), f.collectionID, ""); last != nil {
		t.Fatalf("Last before any run = %+v", last)
	}
	f.run(f.collectionID, f.folderID)
	if last, _ := f.Last(t.Context(), f.collectionID, f.folderID); last == nil || last.Passed != 2 {
		t.Errorf("folder's last run = %+v", last)
	}
	if last, _ := f.Last(t.Context(), f.collectionID, ""); last != nil {
		t.Errorf("the collection reports its folder's run: %+v", last)
	}
}

func TestRunPublishesProgress(t *testing.T) {
	f := newRunFixture(t)
	f.run(f.collectionID, f.folderID)

	progress := f.events.progress()
	if len(progress) != 2 {
		t.Fatalf("progress events = %d, want 2", len(progress))
	}
	for i, event := range progress {
		if event.Done != i+1 || event.Total != 2 || event.NodeID != f.folderID || event.Result.NodeID == "" {
			t.Errorf("progress %d = %+v", i, event)
		}
	}
}

// The run fixes its workspace when it starts; switching workspaces later does not move its records.
func TestRunKeepsItsWorkspaceAndEnvironment(t *testing.T) {
	r := newRuns(t)
	ctx := t.Context()
	envs := workspace.NewEnvironments(r.store, r.store, platform.NewIDGen())
	if _, err := envs.Create(ctx, workspace.EnvironmentInput{Name: "Local"}); err != nil {
		t.Fatal(err)
	}
	collection := r.collection("Collection")
	r.request(collection, domain.CollectionNode{Name: "Pinned", URL: addr("a"), EnvironmentID: "env-prod"})
	r.request(collection, domain.CollectionNode{
		Name: "Upload", Method: "POST", URL: addr("b"),
		BodyKind: domain.BodyForm, Form: []domain.FormRow{{ID: "f", Name: "field", Enabled: true}},
		BodyFile: "/tmp/report.pdf",
	})
	scope := &switchingScope{workspace: personal}
	r.scope = scope
	r.sender.onSend = func(runRequest) { scope.workspace = "elsewhere" }

	run := r.run(collection, "")
	sent := r.sender.requests()
	for _, req := range sent {
		if req.Workspace != personal {
			t.Errorf("request went out in %q, want %q", req.Workspace, personal)
		}
	}
	if sent[0].EnvironmentID != "env-prod" {
		t.Errorf("pin = %q, want env-prod", sent[0].EnvironmentID)
	}
	if sent[1].BodyKind != domain.BodyForm || len(sent[1].Form) != 1 || sent[1].BodyFile != "/tmp/report.pdf" {
		t.Errorf("upload = %+v, want its form and file", sent[1])
	}
	if last, _ := r.Last(ctx, collection, ""); run.Environment != "Local" || last.Environment != "Local" {
		t.Errorf("environment = %q / %q, want Local", run.Environment, last.Environment)
	}
}

// An agent's send resolves the levels above in the named workspace and marks the record with its name.
func TestSendOneRequestForAnAgent(t *testing.T) {
	r := newRuns(t)
	ctx := t.Context()
	if err := r.store.SaveWorkspace(ctx, domain.NewWorkspace("b", "B", domain.WorkspacePersonal, "", 1)); err != nil {
		t.Fatal(err)
	}
	r.collections = r.collections.InWorkspace("b")
	collection := r.collection("Shop")
	if err := r.collections.Update(ctx, collection, patchVariables([]domain.Variable{
		{ID: "v", Name: "version", Value: "v1", Enabled: true},
	})); err != nil {
		t.Fatal(err)
	}
	node := r.request(collection, domain.CollectionNode{
		Name: "Product", URL: "https://api.example/products/:id",
		PathParams: []domain.Row{{Name: "id", Value: "a b", Enabled: true}},
	})

	rec, err := r.Send(ctx, "b", node, "Claude Desktop")
	if err != nil || rec.Status != 200 {
		t.Fatalf("Send = %+v, %v", rec, err)
	}
	sent := r.sender.requests()[0]
	if sent.Workspace != "b" || sent.Agent != "Claude Desktop" || sent.Run != "" ||
		sent.URL != "https://api.example/products/a%20b" || len(sent.Variables) != 1 {
		t.Errorf("sent = %+v", sent)
	}
	if _, err := r.Send(ctx, "b", "missing", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing request = %v, want ErrNotFound", err)
	}
}

func bearerAuth(token string) domain.Auth {
	return domain.NewAuth(domain.AuthBearer).With("token", token)
}

type switchingScope struct{ workspace string }

func (s *switchingScope) ActiveWorkspace(context.Context) (string, error) { return s.workspace, nil }

type runAnswer struct {
	status    int
	transport string
	skipped   bool
	refusal   error
}

// runSender answers by URL and writes a real record, so assertion counts can hang off it.
type runSender struct {
	store   *db.Store
	ids     platform.IDGen
	mu      sync.Mutex
	sent    []runRequest
	answers map[string]runAnswer
	tests   map[string][]domain.TestResult
	onSend  func(runRequest)
}

func (f *runSender) reply(url string, status int) { f.answers[url] = runAnswer{status: status} }

func (f *runSender) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = nil
}

func (f *runSender) send(ctx context.Context, req runRequest) (domain.Record, error) {
	f.mu.Lock()
	f.sent = append(f.sent, req)
	answer, ok := f.answers[req.URL]
	onSend := f.onSend
	f.mu.Unlock()
	if onSend != nil {
		onSend(req)
	}
	switch {
	case !ok:
		return domain.Record{}, fmt.Errorf("no answer for %s", req.URL)
	case answer.skipped:
		return domain.Record{Skipped: true}, nil
	case answer.refusal != nil:
		return domain.Record{}, answer.refusal
	}
	rec := domain.Record{RecordSummary: domain.RecordSummary{
		ID: f.ids(), Method: req.Method, URL: req.URL,
		Status: answer.status, Error: answer.transport, StartedAt: time.Now().UnixMilli(),
		Source: domain.SourceManual,
	}}
	if err := f.store.SaveRecord(ctx, req.Workspace, rec); err != nil {
		return domain.Record{}, err
	}
	if tests := f.tests[req.URL]; tests != nil {
		if err := f.store.SaveScriptRun(ctx, req.Workspace, domain.ScriptRun{
			ID: "script-" + rec.ID, RecordID: rec.ID, Scope: domain.ScriptPost, Tests: tests,
		}); err != nil {
			return domain.Record{}, err
		}
	}
	return rec, nil
}

func (f *runSender) requests() []runRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]runRequest{}, f.sent...)
}

func (f *runSender) urls() []string {
	out := []string{}
	for _, req := range f.requests() {
		out = append(out, req.URL)
	}
	return out
}

type runEvents struct {
	mu       sync.Mutex
	all      []any
	finished chan domain.CollectionRun
}

func (e *runEvents) Publish(topic string, payload any) {
	e.mu.Lock()
	e.all = append(e.all, payload)
	e.mu.Unlock()
	if topic == TopicRunFinished {
		e.finished <- payload.(domain.CollectionRun)
	}
}

func (e *runEvents) wait(t *testing.T) domain.CollectionRun {
	t.Helper()
	select {
	case run := <-e.finished:
		return run
	case <-time.After(5 * time.Second):
		t.Fatal("the run never finished")
		return domain.CollectionRun{}
	}
}

func (e *runEvents) progress() []RunProgress {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []RunProgress{}
	for _, payload := range e.all {
		if p, ok := payload.(RunProgress); ok {
			out = append(out, p)
		}
	}
	return out
}

func patchAuth(auth domain.Auth) workspace.CollectionPatch {
	return workspace.CollectionPatch{Auth: &auth}
}

func patchVariables(variables []domain.Variable) workspace.CollectionPatch {
	return workspace.CollectionPatch{Variables: &variables}
}
