package script

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"schemo/internal/db"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// fakeEngine is the sandbox without goja: it keeps what it was asked to run and answers with a
// report the test describes, so a test says what a script did without writing one.
type fakeEngine struct {
	ran   []domain.ScriptInput
	onRun func(in domain.ScriptInput) domain.ScriptRun
}

func (f *fakeEngine) Run(in domain.ScriptInput) domain.ScriptRun {
	f.ran = append(f.ran, in)
	if f.onRun != nil {
		return f.onRun(in)
	}
	return domain.ScriptRun{Scope: in.Scope, OK: true, Logs: []domain.ScriptLog{},
		Tests: []domain.TestResult{}}
}

// sources is what ran, in the order it ran, which is what a chain is about.
func (f *fakeEngine) sources() []string {
	out := []string{}
	for _, in := range f.ran {
		out = append(out, in.Source)
	}
	return out
}

// fakeScope answers with the workspace the test is working in. The fakes below keep one tree and
// one set of reports and ignore the id: what is being tested here is the service, and the split
// between workspaces is the SQL's own test.
type fakeScope struct{ id string }

func (f fakeScope) ActiveWorkspace(context.Context) (string, error) {
	if f.id == "" {
		return domain.WorkspacePersonalID, nil
	}
	return f.id, nil
}

// ws is the workspace the tests below hand to Before and After — the one the app is born with,
// since none of them is about a second workspace.
const ws = domain.WorkspacePersonalID

// fixture is the tree in a real store: what a level runs and the reports a run keeps are rows, so
// the chain is read the way the app reads it.
type fixture struct {
	t     *testing.T
	store *db.Store
}

// seeded writes the code of one level, the way the editor writes it.
func (f *fixture) seeded(id string, scripts *domain.Scripts) {
	f.t.Helper()
	if err := f.store.SaveScripts(context.Background(), ws, id, scripts); err != nil {
		f.t.Fatalf("SaveScripts(%s): %v", id, err)
	}
}

// runs is every report kept for the record the passes below run around, oldest first.
func (f *fixture) runs() []domain.ScriptRun {
	f.t.Helper()
	runs, err := f.store.ScriptRuns(context.Background(), "rec-1")
	if err != nil {
		f.t.Fatalf("ScriptRuns: %v", err)
	}
	return runs
}

// broken is the database going away under a run.
func (f *fixture) broken() { _ = f.store.Close() }

// fakeVariables is the environments screen without a database. What a script writes is kept here as
// well as in the values, because where it was written is half of what these tests are about.
type fakeVariables struct {
	values  map[domain.VarScope]map[string]string
	refused map[domain.VarScope]string
	writes  []write
	// readOnly is a workspace this person may only read: every write of content is refused by code.
	readOnly bool
}

type write struct {
	scope  domain.VarScope
	name   string
	value  string
	secret bool
}

func newFakeVariables() *fakeVariables {
	return &fakeVariables{values: map[domain.VarScope]map[string]string{},
		refused: map[domain.VarScope]string{}}
}

func (f *fakeVariables) with(scope domain.VarScope, name string, value string) *fakeVariables {
	if f.values[scope] == nil {
		f.values[scope] = map[string]string{}
	}
	f.values[scope][name] = value
	return f
}

func (f *fakeVariables) Variable(
	_ context.Context,
	scope domain.VarScope,
	name string,
) (string, bool, error) {
	value, ok := f.values[scope][name]
	return value, ok, nil
}

func (f *fakeVariables) SetVariable(
	_ context.Context,
	scope domain.VarScope,
	name string,
	value string,
) error {
	if f.readOnly {
		return domain.Refuse(domain.CodeWorkspaceReadOnly, domain.ErrNotAllowed, nil)
	}
	if reason, refused := f.refused[scope]; refused {
		return errors.New(reason)
	}
	f.with(scope, name, value)
	f.writes = append(f.writes, write{scope: scope, name: name, value: value})
	return nil
}

func (f *fakeVariables) Capture(
	ctx context.Context,
	scope domain.VarScope,
	name string,
	value string,
	secret bool,
) error {
	if err := f.SetVariable(ctx, scope, name, value); err != nil {
		return err
	}
	f.writes[len(f.writes)-1].secret = secret
	return nil
}

// newTest seeds one collection with another one inside it and a request inside that, plus a
// request at the top: every shape a chain has to keep straight.
func newScripts(t *testing.T) (*Scripts, *fakeEngine, *fixture, *fixture, *fakeVariables) {
	t.Helper()
	ctx := context.Background()
	store := dbtest.New(t)
	must := func(what string, err error) {
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	must("col-1", store.SaveCollection(ctx, ws, domain.Collection{ID: "col-1", Name: "Users"}))
	must("f-1", store.SaveCollection(ctx, ws, domain.Collection{ID: "f-1", ParentID: "col-1", Name: "Admin"}))
	must("col-2", store.SaveCollection(ctx, ws, domain.Collection{ID: "col-2", Name: "Orders", Position: 1}))
	must("r-2", store.SaveNode(ctx, domain.CollectionNode{ID: "r-2", CollectionID: "col-1", Name: "One",
		Position: 1, Method: "GET"}))
	must("r-1", store.SaveNode(ctx, domain.CollectionNode{ID: "r-1", CollectionID: "f-1", Name: "List",
		Method: "GET"}))
	must("draft", store.SaveDraft(ctx, ws, domain.Draft{ID: domain.DraftID(commandLine), Method: "GET"}))
	must("record", store.SaveRecord(ctx, ws, domain.Record{RecordSummary: domain.RecordSummary{
		ID: "rec-1", Source: domain.SourceManual, Method: "GET", URL: "https://api.example.com/users",
		StartedAt: time.Now().UnixMilli()}}))

	f := &fixture{t: t, store: store}
	f.seeded("col-1", &domain.Scripts{Pre: "console.log('Collection');", Post: "console.log('after collection');"})
	f.seeded("f-1", &domain.Scripts{Pre: "console.log('Nested');"})
	f.seeded("r-1", &domain.Scripts{Pre: "console.log('Request');"})

	engine := &fakeEngine{}
	vars := newFakeVariables()
	scripts := NewScripts(engine, store, vars, fakeScope{}, platform.NewIDGen())
	return scripts, engine, f, f, vars
}

// commandLine is the id of the draft the command line writes into — the one level that is not in
// any tree, and the reason the script package knows a draft at all.
const commandLine = "tab:line"

func pass(nodeID string) domain.ScriptPass {
	return domain.ScriptPass{
		Run:      "run-1",
		RecordID: "rec-1",
		NodeID:   nodeID,
		Request:  &domain.ScriptRequest{Method: "GET", URL: "https://api.example.com/users"},
	}
}

// What runs around a request is everything above it, outermost first; a level that runs nothing
// is not in the chain.
func TestTheChainIsEverythingAboveTheRequest(t *testing.T) {
	scripts, _, _, _, _ := newScripts(t)

	chain, err := scripts.Chain(context.Background(), "r-1")
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("chain = %+v, want the collection, the one inside it and the request", chain)
	}
	if chain[0].NodeID != "col-1" || chain[0].Kind != KindCollection ||
		chain[0].Name != "Users" {
		t.Errorf("chain[0] = %+v, want the collection first", chain[0])
	}
	if chain[1].NodeID != "f-1" || chain[1].Kind != KindCollection {
		t.Errorf("chain[1] = %+v, want the collection inside it next", chain[1])
	}
	if chain[2].NodeID != "r-1" || chain[2].Kind != KindRequest {
		t.Errorf("chain[2] = %+v, want the request last", chain[2])
	}
}

// A collection inside a collection is a level of the chain in its own right, and what is above it
// is still above it: the code the outer collection runs has to run for the nested one's requests
// too.
func TestAChainGoesThroughTheCollectionsInsideOne(t *testing.T) {
	scripts, _, _, _, _ := newScripts(t)
	ctx := context.Background()

	chain, err := scripts.Chain(ctx, "f-1")
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 2 || chain[0].NodeID != "col-1" || chain[1].NodeID != "f-1" {
		t.Errorf("chain of a nested collection = %+v, want the collection around it and itself", chain)
	}
	if chain[1].Kind != KindCollection || chain[1].Name != "Admin" {
		t.Errorf("chain[1] = %+v, want the nested collection as a level of its own", chain[1])
	}

	chain, err = scripts.Chain(ctx, "r-2")
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 1 || chain[0].NodeID != "col-1" {
		t.Errorf("chain of the request at the top = %+v, want the collection only", chain)
	}
}

func TestAChainIsEmptyWhenNothingRuns(t *testing.T) {
	scripts, _, _, _, _ := newScripts(t)
	ctx := context.Background()

	// The request at the root: the collection above it has scripts, the request itself has none.
	chain, err := scripts.Chain(ctx, "r-2")
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 1 || chain[0].NodeID != "col-1" {
		t.Errorf("chain = %+v, want only the collection", chain)
	}

	// A collection is the top of its own tree, so what it runs is the whole chain.
	chain, err = scripts.Chain(ctx, "col-1")
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 1 || chain[0].Kind != KindCollection {
		t.Errorf("chain of a collection = %+v, want itself and nothing above it", chain)
	}
	if chain, err := scripts.Chain(ctx, "col-2"); err != nil || len(chain) != 0 {
		t.Errorf("chain of a collection with no scripts = %+v, %v, want nothing", chain, err)
	}
	// A request the command line composed came from nowhere, so nothing is above it.
	if chain, err := scripts.Chain(ctx, ""); err != nil || len(chain) != 0 {
		t.Errorf("chain of a request from nowhere = %+v, %v, want nothing", chain, err)
	}
	// An id no level has is not an error either: a card open on a node deleted beside it has nothing
	// around it, and a send should not fail over code that is no longer there.
	if chain, err := scripts.Chain(ctx, "not-found"); err != nil || len(chain) != 0 {
		t.Errorf("chain of an id nothing knows = %+v, %v, want nothing", chain, err)
	}
}

func TestBeforeRunsEveryPreScriptInOrder(t *testing.T) {
	scripts, engine, _, store, _ := newScripts(t)

	asked := pass("r-1")
	skip, err := scripts.Before(context.Background(), ws, &asked)
	if err != nil {
		t.Fatalf("Before: %v", err)
	}
	if skip {
		t.Error("the run was called off by a script that said nothing of the kind")
	}

	want := []string{"console.log('Collection');", "console.log('Nested');",
		"console.log('Request');"}
	if got := engine.sources(); !equal(got, want) {
		t.Errorf("scripts that ran = %q, want %q", got, want)
	}
	for _, in := range engine.ran {
		if in.Scope != domain.ScriptPre {
			t.Errorf("scope = %q, want every one of them before the request", in.Scope)
		}
	}

	// Nothing is written yet: a report hangs off the record it ran around, and the record does not
	// exist until the request has been answered.
	if len(store.runs()) != 0 {
		t.Errorf("%d report(s) were written before the record they belong to", len(store.runs()))
	}
	if len(asked.Ran) != 3 {
		t.Errorf("passes that ran = %d, want the three carried to the other half", len(asked.Ran))
	}
}

// A script that changes the request changes what the ones after it see: the collection's script
// runs first and the request's own sees what the collection inside it left.
func TestWhatAScriptChangesTheNextOneSees(t *testing.T) {
	scripts, engine, _, _, _ := newScripts(t)
	engine.onRun = func(in domain.ScriptInput) domain.ScriptRun {
		in.Request.Headers = append(in.Request.Headers, domain.HeaderPair{Name: "X-From", Value: in.Source})
		return domain.ScriptRun{Scope: in.Scope, OK: true}
	}

	asked := pass("r-1")
	if _, err := scripts.Before(context.Background(), ws, &asked); err != nil {
		t.Fatalf("Before: %v", err)
	}

	if len(asked.Request.Headers) != 3 {
		t.Fatalf("headers = %+v, want one per level", asked.Request.Headers)
	}
	if asked.Request.Headers[2].Value != "console.log('Request');" {
		t.Errorf("headers = %+v, want what the request's own script added last", asked.Request.Headers)
	}
}

func TestAPreRequestScriptCanCallTheRequestOff(t *testing.T) {
	scripts, engine, _, _, _ := newScripts(t)
	engine.onRun = func(in domain.ScriptInput) domain.ScriptRun {
		return domain.ScriptRun{Scope: in.Scope, OK: true,
			SkipRequest: in.Source == "console.log('Nested');"}
	}

	asked := pass("r-1")
	skip, err := scripts.Before(context.Background(), ws, &asked)
	if err != nil {
		t.Fatalf("Before: %v", err)
	}
	if !skip {
		t.Error("the nested collection's script called the request off and the run did not hear it")
	}
	// Saying so does not stop the chain: the scripts below it still run, and one of them may undo it.
	if len(engine.ran) != 3 {
		t.Errorf("%d scripts ran, want all three", len(engine.ran))
	}
}

// Only a pre-request script can call a request off: one that says so after the answer came back is
// too late, and saying otherwise would be a lie about what happened.
func TestAPostResponseScriptCannotCallTheRequestOff(t *testing.T) {
	scripts, engine, _, _, _ := newScripts(t)
	engine.onRun = func(in domain.ScriptInput) domain.ScriptRun {
		return domain.ScriptRun{Scope: in.Scope, OK: true, SkipRequest: true}
	}

	after := pass("r-1")
	after.Response = &domain.Response{Status: 200}
	scripts.After(context.Background(), ws, after)

	// After answers with nothing at all, so the flag has nowhere to go — what it must not do is
	// reach the caller, and the caller is the thing that is not here.
	if len(engine.ran) == 0 {
		t.Fatal("the post-response scripts did not run")
	}
	for _, in := range engine.ran {
		if in.Response == nil {
			t.Error("a post-response script was handed no answer")
		}
	}
}

func TestAfterRunsThePostScriptsAndKeepsTheReports(t *testing.T) {
	scripts, engine, _, store, _ := newScripts(t)

	after := pass("r-1")
	after.Response = &domain.Response{Status: 200, Body: `{"data":[]}`}
	scripts.After(context.Background(), ws, after)

	// The collection and the one inside it have post scripts; the request's own has none.
	if len(engine.ran) != 1 || engine.ran[0].Source != "console.log('after collection');" {
		t.Fatalf("scripts that ran = %q, want the collection's", engine.sources())
	}

	runs, err := scripts.Runs(context.Background(), "rec-1")
	if err != nil {
		t.Fatalf("Runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("reports = %+v, want one", runs)
	}
	run := runs[0]
	if run.ID == "" || run.RecordID != "rec-1" || run.NodeID != "col-1" ||
		run.Scope != domain.ScriptPost {
		t.Errorf("report = %+v, want it named by what, where and when", run)
	}
	if run.CreatedAt == 0 {
		t.Errorf("report = %+v, want it stamped", run)
	}
	if len(store.runs()) != 1 {
		t.Errorf("the store kept %d reports, want 1", len(store.runs()))
	}
}

// A store that refuses to keep a report is not a reason to fail a request: the answer came back and
// the request is a record, and a report lost after that is the smaller loss. Neither half may panic
// over it, and the first half may not even notice — it writes nothing.
func TestAStoreThatCannotKeepReportsDoesNotFailTheRequest(t *testing.T) {
	scripts, _, _, store, _ := newScripts(t)

	asked := pass("r-1")
	if _, err := scripts.Before(context.Background(), ws, &asked); err != nil {
		t.Fatalf("Before: %v", err)
	}
	store.broken()
	asked.Response = &domain.Response{Status: 200}
	scripts.After(context.Background(), ws, asked)
}

// A level's code is the level's own: what a collection inside another runs is not what the one
// around it runs, and "nothing here" is the answer the editor draws the inherited text over.
func TestALevelAnswersForItsOwnCode(t *testing.T) {
	scripts, _, _, _, _ := newScripts(t)
	ctx := context.Background()

	if scripts, err := scripts.Scripts(ctx, "col-1"); err != nil || scripts == nil || scripts.Pre == "" {
		t.Errorf("collection scripts = %+v, %v, want its own", scripts, err)
	}
	// A level nobody has written into answers nothing rather than somebody else's code: the chain is
	// what runs a request, and it is not what the editor of one level shows.
	if scripts, err := scripts.Scripts(ctx, "r-2"); err != nil || scripts != nil {
		t.Errorf("scripts of a level with no code = %+v, %v, want nothing", scripts, err)
	}

	// Writing is what gives a level code; taking it off puts the level back to "not set here", which
	// is how it inherits again.
	if _, err := scripts.SaveScripts(ctx, "col-2", &domain.Scripts{Post: "console.log('Own');"}); err != nil {
		t.Fatalf("SaveScripts: %v", err)
	}
	if scripts, err := scripts.Scripts(ctx, "col-2"); err != nil || scripts == nil || scripts.Post == "" {
		t.Errorf("collection scripts = %+v, %v, want what was written", scripts, err)
	}
	if _, err := scripts.SaveScripts(ctx, "col-2", nil); err != nil {
		t.Fatalf("SaveScripts(nil): %v", err)
	}
	if scripts, err := scripts.Scripts(ctx, "col-2"); err != nil || scripts != nil {
		t.Errorf("cleared scripts = %+v, %v, want nothing", scripts, err)
	}

	// A level no row has reads as "not set here" too: a tab whose draft was never written.
	if scripts, err := scripts.Scripts(ctx, "not-found"); err != nil || scripts != nil {
		t.Errorf("scripts of a level that does not exist = %+v, %v, want nothing", scripts, err)
	}
}

// The command line's request is in no tree, so what runs around it is the code of its own draft:
// one level, with nobody's name on it.
func TestTheCommandLineIsAChainOfItsOwn(t *testing.T) {
	scripts, engine, tree, store, _ := newScripts(t)
	ctx := context.Background()

	if chain, err := scripts.Chain(ctx, commandLine); err != nil || len(chain) != 0 {
		t.Errorf("chain of a command line with no code = %+v, %v, want nothing", chain, err)
	}

	tree.seeded(commandLine, &domain.Scripts{
		Pre:  "console.log('before');",
		Post: "console.log('after');",
	})
	chain, err := scripts.Chain(ctx, commandLine)
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 1 || chain[0].Kind != KindDraft || chain[0].Name != "" {
		t.Fatalf("chain = %+v, want the draft's own code and nothing else", chain)
	}

	asked := pass(commandLine)
	if _, err := scripts.Before(ctx, ws, &asked); err != nil {
		t.Fatalf("Before: %v", err)
	}
	asked.Response = &domain.Response{Status: 200}
	scripts.After(ctx, ws, asked)

	if got := engine.sources(); !equal(got,
		[]string{"console.log('before');", "console.log('after');"}) {
		t.Errorf("scripts that ran = %q, want the draft's own two", got)
	}
	if len(store.runs()) != 2 {
		t.Errorf("%d report(s) were kept, want one per script", len(store.runs()))
	}
	if store.runs()[0].NodeID != commandLine {
		t.Errorf("report = %+v, want it named by the level it came from", store.runs()[0])
	}
}

// The run's own scope is the run's: the next request of the same run sees what the last one wrote,
// and a run that starts after it does not.
func TestTheRunScopeBelongsToTheRun(t *testing.T) {
	scripts, engine, _, _, _ := newScripts(t)
	ctx := context.Background()

	seen := ""
	engine.onRun = func(in domain.ScriptInput) domain.ScriptRun {
		if in.Source == "console.log('Collection');" {
			_ = in.Variables.Set(domain.ScopeRun, "page", "2")
		}
		if in.Source == "console.log('Request');" {
			if value, ok, _ := in.Variables.Lookup(domain.ScopeRun, "page"); ok {
				seen = value
			}
		}
		return domain.ScriptRun{Scope: in.Scope, OK: true}
	}
	first := pass("r-1")
	if _, err := scripts.Before(ctx, ws, &first); err != nil {
		t.Fatalf("Before: %v", err)
	}
	if seen != "2" {
		t.Errorf("the request's own script saw %q, want what the collection wrote earlier in the run",
			seen)
	}

	other := pass("r-1")
	other.Run = "run-2"
	found := false
	engine.onRun = func(in domain.ScriptInput) domain.ScriptRun {
		if _, ok, _ := in.Variables.Lookup(domain.ScopeRun, "page"); ok {
			found = true
		}
		return domain.ScriptRun{Scope: in.Scope, OK: true}
	}
	if _, err := scripts.Before(ctx, ws, &other); err != nil {
		t.Fatalf("Before: %v", err)
	}
	if found {
		t.Error("the second run saw what the first one wrote, and a run's scope is its own")
	}
}

// Reading `pm.variables` looks through the environment and the globals after the run's own scope,
// which is the order the next request of the run will resolve in.
func TestReadingAVariableLooksThroughTheScopes(t *testing.T) {
	scripts, engine, _, _, vars := newScripts(t)
	vars.with(domain.ScopeEnvironment, "base", "https://api.example.com")
	vars.with(domain.ScopeGlobals, "token", "from-globals")
	vars.with(domain.ScopeGlobals, "base", "https://globals.example.com")

	asked := map[string]string{}
	engine.onRun = func(in domain.ScriptInput) domain.ScriptRun {
		for _, name := range []string{"base", "token", "no"} {
			value, ok, err := in.Variables.Lookup(domain.ScopeRun, name)
			if err != nil {
				t.Errorf("Get(%s): %v", name, err)
			}
			if ok {
				asked[name] = value
			}
		}
		return domain.ScriptRun{Scope: in.Scope, OK: true}
	}

	engine.onRun = func(in domain.ScriptInput) domain.ScriptRun {
		for _, name := range []string{"base", "token", "no"} {
			value, ok, err := in.Variables.Lookup(domain.ScopeRun, name)
			if err != nil {
				t.Errorf("Get(%s): %v", name, err)
			}
			if ok {
				asked[name] = value
			}
		}
		return domain.ScriptRun{Scope: in.Scope, OK: true}
	}

	first := pass("r-1")
	if _, err := scripts.Before(context.Background(), ws, &first); err != nil {
		t.Fatalf("Before: %v", err)
	}
	if asked["base"] != "https://api.example.com" {
		t.Errorf("base = %q, want the environment to win over the globals", asked["base"])
	}
	if asked["token"] != "from-globals" {
		t.Errorf("token = %q, want it found in the globals", asked["token"])
	}
	if _, found := asked["no"]; found {
		t.Error("a name nobody set was answered with something")
	}
}

// The run's own scope is kept here; the environment and the globals are somebody else's, and
// writing to them goes through the port.
func TestWritingAVariableGoesWhereTheScopeSays(t *testing.T) {
	scripts, engine, _, _, vars := newScripts(t)
	engine.onRun = func(in domain.ScriptInput) domain.ScriptRun {
		if in.Source != "console.log('Collection');" {
			return domain.ScriptRun{Scope: in.Scope, OK: true}
		}
		if err := in.Variables.Set(domain.ScopeEnvironment, "token", "in-environment"); err != nil {
			t.Errorf("Set(environment): %v", err)
		}
		if err := in.Variables.Set(domain.ScopeGlobals, "auth", "in-globals"); err != nil {
			t.Errorf("Set(globals): %v", err)
		}
		return domain.ScriptRun{Scope: in.Scope, OK: true}
	}

	asked := pass("r-1")
	if _, err := scripts.Before(context.Background(), ws, &asked); err != nil {
		t.Fatalf("Before: %v", err)
	}
	if len(vars.writes) != 2 {
		t.Fatalf("writes = %+v, want one per scope", vars.writes)
	}
	if vars.writes[0].scope != domain.ScopeEnvironment || vars.writes[0].name != "token" {
		t.Errorf("write = %+v, want the environment one first", vars.writes[0])
	}
	if vars.writes[1].scope != domain.ScopeGlobals || vars.writes[1].value != "in-globals" {
		t.Errorf("write = %+v, want the globals one", vars.writes[1])
	}
	// The run's own scope is this feature's memory and goes nowhere near the port.
	if len(vars.values[domain.ScopeRun]) != 0 {
		t.Errorf("the run scope = %+v, want it kept here and not written through the port",
			vars.values[domain.ScopeRun])
	}
}

// A scope that cannot be written is the script's business to see, so the error travels out of the
// port and into the sandbox rather than being swallowed here.
func TestAScopeThatRefusesAWriteSaysSo(t *testing.T) {
	scripts, engine, _, _, vars := newScripts(t)
	vars.refused[domain.ScopeEnvironment] = "read-only environment"

	var refused error
	engine.onRun = func(in domain.ScriptInput) domain.ScriptRun {
		refused = in.Variables.Set(domain.ScopeEnvironment, "token", "1")
		return domain.ScriptRun{Scope: in.Scope, OK: true}
	}

	asked := pass("r-1")
	if _, err := scripts.Before(context.Background(), ws, &asked); err != nil {
		t.Fatalf("Before: %v", err)
	}
	if refused == nil || !strings.Contains(refused.Error(), "read-only") {
		t.Errorf("error = %v, want the reason the scope gave", refused)
	}
}

// A script that fails is a report, not the end of the chain: the levels below it still run, and a
// collection that counts its requests keeps counting even when a script of one inside it is broken.
func TestAFailedScriptDoesNotStopTheChain(t *testing.T) {
	scripts, engine, _, store, _ := newScripts(t)
	engine.onRun = func(in domain.ScriptInput) domain.ScriptRun {
		if in.Source == "console.log('Nested');" {
			return domain.ScriptRun{Scope: in.Scope, OK: false, Error: "ReferenceError: nope"}
		}
		return domain.ScriptRun{Scope: in.Scope, OK: true}
	}

	asked := pass("r-1")
	if _, err := scripts.Before(context.Background(), ws, &asked); err != nil {
		t.Fatalf("Before: %v", err)
	}
	asked.Response = &domain.Response{Status: 200}
	scripts.After(context.Background(), ws, asked)

	// The three pre-request scripts of the chain and the collection's post-response one: the failure
	// stopped neither the levels below it nor the second half.
	if len(engine.ran) != 4 {
		t.Fatalf("%d scripts ran, want all four", len(engine.ran))
	}
	if len(store.runs()) != 4 {
		t.Fatalf("%d reports were kept, want one per script", len(store.runs()))
	}
	if store.runs()[1].OK || store.runs()[1].Error != "ReferenceError: nope" {
		t.Errorf("report = %+v, want the failure that script came with", store.runs()[1])
	}
}

func equal(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// A half that is switched off keeps its code and does not run. The level above cannot hand down a
// script it has switched off, either: what it says about itself is that this half is not running.
func TestASwitchedOffHalfDoesNotRun(t *testing.T) {
	scripts, engine, tree, _, _ := newScripts(t)
	// The collection's own half goes off, and the nested one's stays on.
	tree.seeded("col-1", &domain.Scripts{
		Pre: "console.log('Collection');", PreOff: true,
		Post: "console.log('after collection');",
	})

	asked := pass("r-1")
	if _, err := scripts.Before(context.Background(), ws, &asked); err != nil {
		t.Fatalf("Before: %v", err)
	}

	want := []string{"console.log('Nested');", "console.log('Request');"}
	if got := engine.sources(); !equal(got, want) {
		t.Errorf("scripts that ran = %q, want %q", got, want)
	}

	// Writing the code back is not needed to bring it back: the flag is the whole difference.
	if scripts, err := scripts.Scripts(context.Background(), "col-1"); err != nil || scripts == nil ||
		scripts.Pre == "" {
		t.Errorf("scripts = %+v, %v, want the code kept where it was written", scripts, err)
	}
}
