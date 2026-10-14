package scripting

import (
	"context"
	"strings"
	"testing"

	"schemo/internal/domain"
)

func answered(body string) *domain.Response {
	return &domain.Response{
		Status:      201,
		ContentType: "application/json",
		Body:        body,
		Headers: []domain.HeaderPair{
			{Name: "X-Request-Id", Value: "abc-1"},
			{Name: "Set-Cookie", Value: "sid=s3cret; Path=/; HttpOnly"},
		},
	}
}

// valueOf is what a variable holds, as the next request would find it.
func valueOf(vars *fakeVariables, scope domain.VarScope, name string) string {
	value, _, _ := vars.Variable(context.Background(), scope, name)
	return value
}

func hidden(c domain.Capture) domain.Capture {
	c.Secret = true
	return c
}

// reportsOf is what the level's own captures reported: the seeded collection above it has code of
// its own, which is another test's business.
func reportsOf(store *fakeStore, level string) []domain.ScriptRun {
	var out []domain.ScriptRun
	for _, run := range store.runs {
		if run.NodeID == level {
			out = append(out, run)
		}
	}
	return out
}

// rule is a capture of the body, or of another source when the path names one of the fixed kinds.
func rule(
	name string, source domain.CaptureSource, path string, scope domain.VarScope,
) domain.Capture {
	return domain.Capture{ID: name, Name: name, Source: source, Path: path, Scope: scope}
}

func capturePass(node string, response *domain.Response) domain.ScriptPass {
	at := pass(node)
	at.Response = response
	return at
}

// A capture keeps what the response said where the next request will read it, and reports what it
// did in the tab that shows what ran around the record.
func TestACaptureKeepsAValueOfTheResponse(t *testing.T) {
	uc, _, tree, store, vars := newTest()
	tree.seeded("r-2", &domain.Scripts{Captures: []domain.Capture{
		rule("userId", domain.CaptureBody, "$.data.id", domain.ScopeEnvironment),
		rule("requestId", domain.CaptureHeader, "x-request-id", domain.ScopeGlobals),
		hidden(rule("session", domain.CaptureCookie, "sid", domain.ScopeEnvironment)),
		rule("code", domain.CaptureStatus, "", domain.ScopeRun),
	}})

	uc.After(context.Background(), ws, capturePass("r-2", answered(`{"data": {"id": 42}}`)))

	if got := valueOf(vars, domain.ScopeEnvironment, "userId"); got != "42" {
		t.Errorf("userId = %q, want the id from the body", got)
	}
	if got := valueOf(vars, domain.ScopeGlobals, "requestId"); got != "abc-1" {
		t.Errorf("requestId = %q, want the header, whatever its case", got)
	}
	secret := false
	for _, w := range vars.writes {
		if w.name == "session" && w.value == "s3cret" && w.secret {
			secret = true
		}
	}
	if !secret {
		t.Errorf("writes = %+v, want the cookie kept as a secret", vars.writes)
	}
	if got, ok := uc.runVariable("run-1", "code"); !ok || got != "201" {
		t.Errorf("code = %q, %v, want the status in the run's own scope", got, ok)
	}

	reports := reportsOf(store, "r-2")
	if len(reports) != 1 || !reports[0].OK || len(reports[0].Tests) != 4 {
		t.Fatalf("reports = %+v, want one passing report of four captures", reports)
	}
	for _, log := range reports[0].Logs {
		if strings.Contains(log.Message, "s3cret") {
			t.Errorf("the report %q says a secret", log.Message)
		}
	}
}

// A capture that finds nothing fails on its own line, and the others still run.
func TestACaptureThatFindsNothingFailsAlone(t *testing.T) {
	uc, _, tree, store, vars := newTest()
	tree.seeded("r-2", &domain.Scripts{Captures: []domain.Capture{
		rule("missing", domain.CaptureBody, "$.nope", domain.ScopeEnvironment),
		rule("code", domain.CaptureStatus, "", domain.ScopeEnvironment),
	}})

	uc.After(context.Background(), ws, capturePass("r-2", answered(`{"data": 1}`)))

	report := reportsOf(store, "r-2")[0]
	if report.OK || report.Tests[0].Passed || report.Tests[0].Error == "" || !report.Tests[1].Passed {
		t.Errorf("report = %+v, want the first to fail with a reason and the second to pass", report)
	}
	if got := valueOf(vars, domain.ScopeEnvironment, "code"); got != "201" {
		t.Errorf("code = %q, want the rule after a failed one to have run", got)
	}
}

// Captures do not run for a request that got no answer, and one that is switched off is not run.
func TestCapturesWaitForAnAnswerAndRespectTheirSwitch(t *testing.T) {
	uc, _, tree, store, _ := newTest()
	off := rule("code", domain.CaptureStatus, "", domain.ScopeEnvironment)
	off.Off = true
	tree.seeded("r-2", &domain.Scripts{Captures: []domain.Capture{off}})

	uc.After(context.Background(), ws, capturePass("r-2", answered(`{}`)))
	failed := &domain.Response{Error: "connection refused"}
	tree.scripts["r-2"].Captures[0].Off = false
	uc.After(context.Background(), ws, capturePass("r-2", failed))

	if reports := reportsOf(store, "r-2"); len(reports) != 0 {
		t.Errorf("reports = %+v, want none: one rule was off and the other had no answer", reports)
	}
}

// The editor writes code and does not mention captures; a level's captures survive that.
func TestSavingCodeKeepsTheCaptures(t *testing.T) {
	uc, _, tree, _, _ := newTest()
	ctx := context.Background()
	kept := rule("code", domain.CaptureStatus, "", domain.ScopeRun)
	tree.seeded("r-2", &domain.Scripts{Captures: []domain.Capture{kept}})

	if err := uc.SaveScripts(ctx, "r-2", &domain.Scripts{Post: "console.log(1)"}); err != nil {
		t.Fatalf("SaveScripts: %v", err)
	}
	held, _ := uc.Scripts(ctx, "r-2")
	if held == nil || held.Post == "" || len(held.Captures) != 1 {
		t.Errorf("scripts = %+v, want the new code and the old capture", held)
	}
}

// Taking the code off a level puts it back to inheriting, and its captures are not code.
func TestTakingTheCodeOffKeepsTheCaptures(t *testing.T) {
	uc, _, tree, _, _ := newTest()
	ctx := context.Background()
	kept := rule("code", domain.CaptureStatus, "", domain.ScopeRun)
	tree.seeded("r-2", &domain.Scripts{Post: "x", Captures: []domain.Capture{kept}})

	if err := uc.SaveScripts(ctx, "r-2", nil); err != nil {
		t.Fatalf("SaveScripts: %v", err)
	}
	held, _ := uc.Scripts(ctx, "r-2")
	if held == nil || held.Post != "" || len(held.Captures) != 1 {
		t.Errorf("scripts = %+v, want the capture alone", held)
	}
}

func TestCapturesAreSavedWithoutTouchingCodeAndRefuseWhatCannotRun(t *testing.T) {
	uc, _, _, _, _ := newTest()
	ctx := context.Background()

	token := hidden(rule("token", domain.CaptureBody, "$.token", domain.ScopeEnvironment))
	token.ID = ""
	saved, err := uc.SaveCaptures(ctx, "r-1", []domain.Capture{token})
	if err != nil {
		t.Fatalf("SaveCaptures: %v", err)
	}
	if saved.Pre == "" || len(saved.Captures) != 1 || saved.Captures[0].ID == "" {
		t.Errorf("saved = %+v, want the request's code kept and the rule given an id", saved)
	}

	for _, bad := range []domain.Capture{
		{Name: "not a name", Source: domain.CaptureStatus, Scope: domain.ScopeRun},
		{Name: "x", Source: domain.CaptureBody, Scope: domain.ScopeRun},
		{Name: "x", Source: "nowhere", Path: "p", Scope: domain.ScopeRun},
		{Name: "x", Source: domain.CaptureStatus, Scope: "elsewhere"},
	} {
		if _, err := uc.SaveCaptures(ctx, "r-1", []domain.Capture{bad}); err == nil {
			t.Errorf("SaveCaptures accepted %+v", bad)
		}
	}
}

// In a space this person may only read, a capture into the environment is not written there: the
// value is kept for the run, so the next request of it still gets the token, and nothing is
// reported as failed.
func TestACaptureIntoASpaceThatCannotBeWrittenIsKeptForTheRun(t *testing.T) {
	uc, _, tree, store, vars := newTest()
	vars.readOnly = true
	plain := rule("code", domain.CaptureStatus, "", domain.ScopeEnvironment)
	secret := hidden(rule("token", domain.CaptureBody, "$.token", domain.ScopeEnvironment))
	tree.seeded("r-2", &domain.Scripts{Captures: []domain.Capture{plain, secret}})

	uc.After(context.Background(), ws, capturePass("r-2", answered(`{"token":"s3cret"}`)))

	if reports := reportsOf(store, "r-2"); len(reports) != 1 || !reports[0].OK {
		t.Errorf("reports = %+v, want the captures reported as made", reports)
	}
	kept := map[string]string{}
	for _, one := range uc.RunVariables("run-1") {
		kept[one.Name] = one.Value
	}
	if kept["code"] != "201" || kept["token"] != "s3cret" {
		t.Errorf("kept for the run = %v, want both values", kept)
	}
	if len(vars.writes) != 0 {
		t.Errorf("writes = %+v, want nothing written to the space", vars.writes)
	}
}

// A script that sets an environment variable in such a space is let off the same way, and any other
// refusal is still its own error.
func TestAScriptSettingAVariableInASpaceThatCannotBeWrittenKeepsItForTheRun(t *testing.T) {
	uc, _, _, _, vars := newTest()
	store := scriptVariables{ctx: context.Background(), owner: uc, run: "run-1"}

	vars.readOnly = true
	if err := store.Set(domain.ScopeEnvironment, "token", "abc"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, ok, _ := store.Lookup(domain.ScopeRun, "token"); !ok || got != "abc" {
		t.Errorf("run variable = %q, %v, want the value kept for the run", got, ok)
	}

	vars.readOnly = false
	vars.refused[domain.ScopeGlobals] = "database is locked"
	if err := store.Set(domain.ScopeGlobals, "other", "x"); err == nil {
		t.Error("another refusal was swallowed as if the space were read-only")
	}
}
