package sqlite

import (
	"context"
	"errors"
	"testing"

	"schemo/internal/domain"
)

// Two spaces are two data sets. Everything below is written into the personal one, and the second
// one has to come back empty of all of it — this is the test the whole column exists for, and it
// stops passing the moment a query forgets its scope.
func TestTwoWorkspacesDoNotSeeEachOther(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTeam(t, store)

	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-1", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.SaveEnvironment(ctx, ws,
		domain.Environment{ID: "env-1", Name: "Local", Position: 1}); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-1"}, domain.Variable{
		ID: "v1", Name: "base_url", Value: "https://api.example.com", Kind: domain.VariableText,
		Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}
	if err := store.SaveCollection(ctx, ws,
		domain.Collection{ID: "col-1", Name: "Пользователи", Position: 1}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	draft := domain.NewDraft()
	draft.ID = domain.DraftCommandLine
	draft.URL = "https://api.example.com/users"
	if err := store.SaveDraft(ctx, ws, draft); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	records, err := store.Records(ctx, team, "", 10)
	if err != nil {
		t.Fatalf("Records(%s): %v", team, err)
	}
	if len(records) != 0 {
		t.Errorf("the second space sees %d record(s), want none", len(records))
	}

	state, err := store.EnvState(ctx, team)
	if err != nil {
		t.Fatalf("EnvState(%s): %v", team, err)
	}
	if len(state.Environments) != 0 || len(state.Globals) != 0 {
		t.Errorf("the second space sees %+v, want no environment and no globals", state)
	}

	collections, err := store.Collections(ctx, team)
	if err != nil {
		t.Fatalf("Collections(%s): %v", team, err)
	}
	if len(collections) != 0 {
		t.Errorf("the second space sees %d collection(s), want none", len(collections))
	}

	if _, err := store.Draft(ctx, team, domain.DraftCommandLine); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the second space's command line = %v, want ErrNotFound", err)
	}

	records, err = store.Records(ctx, ws, "", 10)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 1 || records[0].ID != "rec-1" {
		t.Errorf("records = %+v, want the one that was saved", records)
	}
	if n := countRows(t, store, "environments", ws); n != 1 {
		t.Errorf("the personal space holds %d environment(s), want its one", n)
	}
	if n := countRows(t, store, "variables", ws); n != 1 {
		t.Errorf("the personal space holds %d variable(s), want its one", n)
	}
	collections, err = store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(collections) != 1 || collections[0].ID != "col-1" {
		t.Errorf("collections = %+v, want the one that was saved", collections)
	}
	if _, err := store.Draft(ctx, ws, domain.DraftCommandLine); err != nil {
		t.Errorf("the personal command line = %v, want the draft that was saved", err)
	}
}

// The command line's draft id is the same fixed word in every space, so it is the one row the store
// cannot find by id alone: the query behind Scripts reads three tables — collections, nodes and
// drafts — and only the last of them needs the workspace named. This is what says it names it, and
// what says the draft written in one space is not the draft written in the other.
func TestTheCommandLineDraftIsPerWorkspace(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTeam(t, store)

	mine := domain.NewDraft()
	mine.ID = domain.DraftCommandLine
	mine.URL = "https://personal.example.com"
	if err := store.SaveDraft(ctx, ws, mine); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	theirs := domain.NewDraft()
	theirs.ID = domain.DraftCommandLine
	theirs.URL = "https://team.example.com"
	if err := store.SaveDraft(ctx, team, theirs); err != nil {
		t.Fatalf("SaveDraft(%s): %v", team, err)
	}

	got, err := store.Draft(ctx, ws, domain.DraftCommandLine)
	if err != nil || got.URL != mine.URL {
		t.Errorf("the personal command line = %q, %v; want its own", got.URL, err)
	}
	got, err = store.Draft(ctx, team, domain.DraftCommandLine)
	if err != nil || got.URL != theirs.URL {
		t.Errorf("the command line of %s = %q, %v; want its own", team, got.URL, err)
	}

	if err := store.SaveScripts(ctx, ws, string(domain.DraftCommandLine),
		&domain.Scripts{Pre: "personal"}); err != nil {
		t.Fatalf("SaveScripts: %v", err)
	}
	if err := store.SaveScripts(ctx, team, string(domain.DraftCommandLine),
		&domain.Scripts{Pre: "team"}); err != nil {
		t.Fatalf("SaveScripts(%s): %v", team, err)
	}

	// The id is one word in two rows, so a read that does not say which space it is in answers with
	// whichever row the plan reached first — and the code set on one command line is not the other's.
	scripts, err := store.Scripts(ctx, ws, string(domain.DraftCommandLine))
	if err != nil {
		t.Fatalf("Scripts: %v", err)
	}
	if scripts == nil || scripts.Pre != "personal" {
		t.Errorf("the personal command line runs %+v, want its own", scripts)
	}
	scripts, err = store.Scripts(ctx, team, string(domain.DraftCommandLine))
	if err != nil {
		t.Fatalf("Scripts(%s): %v", team, err)
	}
	if scripts == nil || scripts.Pre != "team" {
		t.Errorf("the command line of %s runs %+v, want its own", team, scripts)
	}
}

// Globals belong to a space, and the unique index says so: one name may be taken once in each. An
// index without workspace_id would make the second save a conflict rather than a second variable.
func TestGlobalsCanShareANameAcrossWorkspaces(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTeam(t, store)

	for _, space := range []string{ws, team} {
		if err := store.SaveVariable(ctx, space, domain.EnvScope{}, domain.Variable{
			ID: "host-" + space, Name: "host", Value: "https://" + space + ".example.com",
			Kind: domain.VariableText, Enabled: true,
		}); err != nil {
			t.Fatalf("SaveVariable in %s: %v", space, err)
		}
	}

	// The name is not enough to find a global: each space resolves its own value for it.
	for _, space := range []string{ws, team} {
		state, err := store.EnvState(ctx, space)
		if err != nil {
			t.Fatalf("EnvState(%s): %v", space, err)
		}
		if len(state.Globals) != 1 || state.Globals[0].Name != "host" {
			t.Fatalf("the globals of %s = %+v, want the one host", space, state.Globals)
		}
		if want := "https://" + space + ".example.com"; state.Globals[0].Value != want {
			t.Errorf("host in %s = %q, want its own %q", space, state.Globals[0].Value, want)
		}
	}
}

// Deleting a space takes everything in it. The cascade reaches the tables that carry the column
// directly and, through the rows that hang off them, the ones that do not — and the space beside it
// keeps every row of its own.
func TestDeleteWorkspaceTakesItsData(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTeam(t, store)

	if err := store.SaveRecord(ctx, team, sampleRecord("rec-team", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord(%s): %v", team, err)
	}
	if err := store.SaveEnvironment(ctx, team,
		domain.Environment{ID: "env-team", Name: "Local", Position: 1}); err != nil {
		t.Fatalf("SaveEnvironment(%s): %v", team, err)
	}
	if err := store.SaveVariable(ctx, team, domain.EnvScope{Environment: "env-team"}, domain.Variable{
		ID: "v-team", Name: "token", Kind: domain.VariableSecret, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable(%s): %v", team, err)
	}
	if err := store.SaveCollection(ctx, team,
		domain.Collection{ID: "col-team", Name: "Заказы", Position: 1}); err != nil {
		t.Fatalf("SaveCollection(%s): %v", team, err)
	}
	if err := store.SaveScriptRun(ctx, team, domain.ScriptRun{
		ID: "run-team", RecordID: "rec-team", Scope: domain.ScriptPre, OK: true,
	}); err != nil {
		t.Fatalf("SaveScriptRun(%s): %v", team, err)
	}
	theirs := domain.NewDraft()
	theirs.ID = domain.DraftCommandLine
	theirs.URL = "https://team.example.com"
	if err := store.SaveDraft(ctx, team, theirs); err != nil {
		t.Fatalf("SaveDraft(%s): %v", team, err)
	}

	// The personal space holds the same shapes, so the delete has something it must not touch.
	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-mine", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.SaveEnvironment(ctx, ws,
		domain.Environment{ID: "env-mine", Name: "Local", Position: 1}); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-mine"}, domain.Variable{
		ID: "v-mine", Name: "token", Kind: domain.VariableSecret, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}
	if err := store.SaveCollection(ctx, ws,
		domain.Collection{ID: "col-mine", Name: "Пользователи", Position: 1}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	if err := store.SaveScriptRun(ctx, ws, domain.ScriptRun{
		ID: "run-mine", RecordID: "rec-mine", Scope: domain.ScriptPre, OK: true,
	}); err != nil {
		t.Fatalf("SaveScriptRun: %v", err)
	}
	mine := domain.NewDraft()
	mine.ID = domain.DraftCommandLine
	mine.URL = "https://personal.example.com"
	if err := store.SaveDraft(ctx, ws, mine); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	if err := store.DeleteWorkspace(ctx, team); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	// One row of each in the personal space, and none at all in the one that was deleted. The reports
	// of scripts are counted too: they hang off a record, but they carry the workspace themselves.
	for _, table := range []string{"records", "environments", "variables", "collections",
		"script_runs", "drafts"} {
		if n := countRows(t, store, table, team); n != 0 {
			t.Errorf("%s still holds %d row(s) of the deleted space, want none", table, n)
		}
		if n := countRows(t, store, table, ws); n != 1 {
			t.Errorf("%s holds %d row(s) of the personal space, want its one", table, n)
		}
	}

	if _, err := store.Workspace(ctx, team); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the deleted workspace = %v, want ErrNotFound", err)
	}
}

// The pointer to the space on screen is a preference of the installation, and every bad answer to
// it — nothing stored, or a name whose space has since gone — falls back to the oldest row that is
// left. A window with no workspace to draw is not a state the app has.
func TestActiveWorkspaceFallsBackToTheDefault(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTeam(t, store)

	active, err := store.ActiveWorkspace(ctx)
	if err != nil || active != domain.WorkspacePersonalID {
		t.Errorf("with nothing stored = %q, %v; want the one the schema wrote", active, err)
	}

	// Write the pointer by hand rather than through SetActiveWorkspace: a space that was deleted while
	// it was on screen leaves exactly this behind.
	if err := store.SaveSetting(ctx, domain.SettingActiveWorkspace, "no-such-workspace"); err != nil {
		t.Fatalf("SaveSetting: %v", err)
	}
	active, err = store.ActiveWorkspace(ctx)
	if err != nil || active != domain.WorkspacePersonalID {
		t.Errorf("pointing at a space that is gone = %q, %v; want the oldest left", active, err)
	}

	if err := store.SetActiveWorkspace(ctx, team); err != nil {
		t.Fatalf("SetActiveWorkspace: %v", err)
	}
	active, err = store.ActiveWorkspace(ctx)
	if err != nil || active != team {
		t.Errorf("after the switch = %q, %v; want %q", active, err, team)
	}
}

// "Reopen the last workspace" is a switch the app started with, so an absent row means on: a
// database written before the preference existed opens where it was left.
func TestReopenLastDefaultsToOn(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	reopen, err := store.ReopenLast(ctx)
	if err != nil || !reopen {
		t.Errorf("with nothing stored = %v, %v; want on", reopen, err)
	}

	if err := store.SaveSetting(ctx, domain.SettingReopenWorkspace, "false"); err != nil {
		t.Fatalf("SaveSetting: %v", err)
	}
	reopen, err = store.ReopenLast(ctx)
	if err != nil || reopen {
		t.Errorf("with it stored off = %v, %v; want off", reopen, err)
	}

	// The pointer is not what this reads: the preference and the space last left in are two rows.
	if err := store.SetActiveWorkspace(ctx, domain.WorkspacePersonalID); err != nil {
		t.Fatalf("SetActiveWorkspace: %v", err)
	}
	if reopen, err = store.ReopenLast(ctx); err != nil || reopen {
		t.Errorf("after a switch = %v, %v; want the stored off", reopen, err)
	}
}

// Which environment a space is working in is a column of its own. Two spaces are two answers, and a
// setting that kept one would resolve the variables of a space the request never happened in.
func TestActiveEnvironmentIsPerWorkspace(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTeam(t, store)

	for _, space := range []string{ws, team} {
		if err := store.SaveEnvironment(ctx, space, domain.Environment{
			ID: "env-" + space, Name: "Local", Position: 1,
		}); err != nil {
			t.Fatalf("SaveEnvironment(%s): %v", space, err)
		}
	}

	if err := store.SetActiveEnvironment(ctx, ws, "env-"+ws); err != nil {
		t.Fatalf("SetActiveEnvironment: %v", err)
	}

	mine, err := store.EnvState(ctx, ws)
	if err != nil {
		t.Fatalf("EnvState: %v", err)
	}
	if mine.ActiveID != "env-"+ws {
		t.Errorf("the personal space works in %q, want its own environment", mine.ActiveID)
	}

	theirs, err := store.EnvState(ctx, team)
	if err != nil {
		t.Fatalf("EnvState(%s): %v", team, err)
	}
	if theirs.ActiveID != "" {
		t.Errorf("the second space works in %q, want none — nothing was set there", theirs.ActiveID)
	}
}

// The oldest row is where a stale pointer lands and where the window goes when the space it was
// showing is deleted. It is the row the schema writes on a fresh installation until that one is
// deleted itself — the rule is about being the oldest, not about being the default.
func TestFirstWorkspaceIsTheOldest(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTeam(t, store)

	first, err := store.FirstWorkspace(ctx)
	if err != nil || first != domain.WorkspacePersonalID {
		t.Errorf("FirstWorkspace = %q, %v; want the row the schema wrote", first, err)
	}

	if err := store.DeleteWorkspace(ctx, domain.WorkspacePersonalID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	first, err = store.FirstWorkspace(ctx)
	if err != nil || first != team {
		t.Errorf("after the born-with row went = %q, %v; want %q", first, err, team)
	}
}

// A pointer left naming a space that is gone answers with the oldest row that is still there, which
// is what a delete of the space on screen leaves behind.
func TestActiveWorkspaceFallsBackToTheOldestLeft(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTeam(t, store)

	if err := store.SetActiveWorkspace(ctx, domain.WorkspacePersonalID); err != nil {
		t.Fatalf("SetActiveWorkspace: %v", err)
	}
	if err := store.DeleteWorkspace(ctx, domain.WorkspacePersonalID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	active, err := store.ActiveWorkspace(ctx)
	if err != nil || active != team {
		t.Errorf("after the space on screen went = %q, %v; want %q", active, err, team)
	}
}

// What each space holds is counted per space: the numbers below differ, so a query that forgot
// workspace_id would answer the same for both and fail here. A folder is a collection that sits
// inside another, and it counts like the one holding it.
func TestWorkspaceCountsCountWhatEachSpaceHolds(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store) // the born-with space: two collections, one of them nested
	seedTeam(t, store)

	for _, env := range []domain.Environment{
		{ID: "env-1", Name: "Local · dev", Position: 1},
	} {
		if err := store.SaveEnvironment(ctx, domain.WorkspacePersonalID, env); err != nil {
			t.Fatalf("SaveEnvironment: %v", err)
		}
	}
	for _, env := range []domain.Environment{
		{ID: "env-2", Name: "Stage · qa", Position: 1},
		{ID: "env-3", Name: "Prod", Position: 2, Readonly: true},
	} {
		if err := store.SaveEnvironment(ctx, team, env); err != nil {
			t.Fatalf("SaveEnvironment: %v", err)
		}
	}

	if err := store.SaveRun(ctx, domain.CollectionRun{
		ID: "run-1", CollectionID: "col-1", StartedAt: 1, Results: []domain.CollectionRunResult{},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := store.SaveCollection(ctx, team, domain.Collection{
		ID: "col-2", Name: "Чужая", Position: 0,
	}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	counts, err := store.Counts(ctx)
	if err != nil {
		t.Fatalf("Counts: %v", err)
	}
	want := map[string]domain.WorkspaceCounts{
		domain.WorkspacePersonalID: {Collections: 2, Environments: 1, Runs: 1},
		team:                       {Collections: 1, Environments: 2, Runs: 0},
	}
	for id, expected := range want {
		if counts[id] != expected {
			t.Errorf("counts of %s = %+v, want %+v", id, counts[id], expected)
		}
	}
}
