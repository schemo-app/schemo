package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"schemo/internal/convert/dotenv"
	"schemo/internal/db"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

func newEnvironments(t *testing.T) (*Environments, *db.Store) {
	t.Helper()
	store := dbtest.New(t)
	return NewEnvironments(store, domain.WorkspaceScope(domain.WorkspacePersonalID), platform.NewIDGen()), store
}

func mustCreate(t *testing.T, s *Environments, draft EnvironmentInput) string {
	t.Helper()
	id, err := s.Create(t.Context(), draft)
	if err != nil {
		t.Fatalf("Create(%q): %v", draft.Name, err)
	}
	return id
}

func mustState(t *testing.T, s *Environments) domain.EnvState {
	t.Helper()
	state, err := s.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func envOf(t *testing.T, s *Environments, id string) domain.Environment {
	t.Helper()
	env, ok := findEnvironment(mustState(t, s), id)
	if !ok {
		t.Fatalf("environment %s is gone", id)
	}
	return env
}

// fill saves an environment with the given variables, as the window would after editing it.
func fill(t *testing.T, s *Environments, id string, vars ...domain.Variable) domain.Environment {
	t.Helper()
	env := envOf(t, s, id)
	env.Vars = vars
	if err := s.Save(t.Context(), env); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return envOf(t, s, id)
}

func text(id, name, value string) domain.Variable {
	return domain.Variable{ID: id, Name: name, Value: value, Kind: domain.VariableText, Enabled: true}
}

func secret(id, name, value string) domain.Variable {
	return domain.Variable{ID: id, Name: name, Value: value, Kind: domain.VariableSecret, Enabled: true}
}

func code(err error) domain.Code {
	if failure := domain.AsFailure(err); failure != nil {
		return failure.Code
	}
	return ""
}

func TestCreateValidatesNamesAndActivatesTheNewOne(t *testing.T) {
	s, _ := newEnvironments(t)
	id := mustCreate(t, s, EnvironmentInput{Name: "  Local  ", Color: "#ff9500"})
	state := mustState(t, s)
	if state.ActiveID != id || state.Environments[0].Name != "Local" || state.Environments[0].Color != "#ff9500" {
		t.Errorf("state = %+v", state)
	}
	for name, want := range map[string]domain.Code{
		"   ": domain.CodeNameEmpty,
		strings.Repeat("x", maxEnvironmentName+1): domain.CodeNameTooLong,
		"Local": domain.CodeNameTaken,
	} {
		if _, err := s.Create(t.Context(), EnvironmentInput{Name: name}); code(err) != want {
			t.Errorf("Create(%q) = %v, want %s", name, err, want)
		}
	}
	if _, err := s.Create(t.Context(), EnvironmentInput{Name: "Copy", StartFrom: "nope"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a missing base = %v", err)
	}
}

func TestCreateCopiesTheBaseWithoutSecretValues(t *testing.T) {
	s, _ := newEnvironments(t)
	dev := mustCreate(t, s, EnvironmentInput{Name: "Dev"})
	fill(t, s, dev, text("a", "baseUrl", "http://dev"), secret("b", "token", "s3cret"))

	stage := envOf(t, s, mustCreate(t, s, EnvironmentInput{Name: "Stage", StartFrom: dev}))
	if len(stage.Vars) != 2 || stage.Vars[0].Name != "baseUrl" || stage.Vars[0].Value != "http://dev" {
		t.Fatalf("copied vars = %+v", stage.Vars)
	}
	if v := stage.Vars[1]; v.Kind != domain.VariableSecret || v.HasValue || v.ID == "b" {
		t.Errorf("copied secret = %+v, want an empty secret of its own", v)
	}
}

func TestSaveKeepsWithheldSecretsAndTakesNewOnes(t *testing.T) {
	s, _ := newEnvironments(t)
	id := mustCreate(t, s, EnvironmentInput{Name: "Local"})
	env := fill(t, s, id, secret("tok", "token", "s3cret"), text("url", "url", "http://x"))
	if v := env.Vars[0]; v.Value != "" || !v.HasValue {
		t.Fatalf("State showed a secret: %+v", v)
	}

	// The window sends back what it shows: no value, hasValue set.
	env.Name = "Renamed"
	env.Vars[1].Value = "http://y"
	if err := s.Save(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	if value, _ := s.Reveal(t.Context(), "tok"); value != "s3cret" {
		t.Errorf("an untouched secret became %q", value)
	}
	if got := envOf(t, s, id); got.Name != "Renamed" || got.Vars[1].Value != "http://y" {
		t.Errorf("saved = %+v", got)
	}

	fill(t, s, id, secret("tok", "token", "fresh"))
	if value, _ := s.Reveal(t.Context(), "tok"); value != "fresh" {
		t.Errorf("a typed secret was not taken: %q", value)
	}
	if got := envOf(t, s, id); len(got.Vars) != 1 {
		t.Errorf("a variable left out of the list was kept: %+v", got.Vars)
	}
	cleared := secret("tok", "token", "")
	fill(t, s, id, cleared)
	if value, _ := s.Reveal(t.Context(), "tok"); value != "" {
		t.Errorf("a secret sent empty without hasValue kept %q", value)
	}
}

func TestSaveRefusesNamesATokenCannotCarry(t *testing.T) {
	s, _ := newEnvironments(t)
	id := mustCreate(t, s, EnvironmentInput{Name: "Local"})
	mustCreate(t, s, EnvironmentInput{Name: "Prod"})
	env := envOf(t, s, id)

	for name, vars := range map[string][]domain.Variable{
		"bad name": {text("a", "has space", "")},
		"twice":    {text("a", "x", ""), text("b", "x", "")},
		"no id":    {text("", "x", "")},
		"same id":  {text("a", "x", ""), text("a", "y", "")},
	} {
		env.Vars = vars
		if err := s.Save(t.Context(), env); err == nil {
			t.Errorf("%s: saved", name)
		}
	}
	env.Vars = []domain.Variable{text("a", "", ""), text("b", "", "")}
	if err := s.Save(t.Context(), env); err != nil {
		t.Errorf("two rows still being named = %v", err)
	}
	env.Name = "Prod"
	if err := s.Save(t.Context(), env); code(err) != domain.CodeNameTaken {
		t.Errorf("a taken name = %v", err)
	}
}

func TestAClosedEnvironmentTakesNoVariableChanges(t *testing.T) {
	s, _ := newEnvironments(t)
	id := mustCreate(t, s, EnvironmentInput{Name: "Prod"})
	env := fill(t, s, id, text("a", "url", "http://prod"))
	env.Readonly = true
	if err := s.Save(t.Context(), env); err != nil {
		t.Fatal(err)
	}

	env.Name = "Production"
	if err := s.Save(t.Context(), env); err != nil {
		t.Errorf("a rename of a closed environment = %v", err)
	}
	env.Vars[0].Value = "http://oops"
	if err := s.Save(t.Context(), env); code(err) != domain.CodeEnvironmentReadOnly {
		t.Errorf("an edit of a closed environment = %v", err)
	}
	if err := s.ImportEntries(t.Context(), domain.EnvScope{Environment: id},
		[]dotenv.Entry{{Name: "x", Value: "1"}}); code(err) != domain.CodeEnvironmentReadOnly {
		t.Errorf("an import into a closed environment = %v", err)
	}
	if _, err := s.SetInScope(t.Context(), domain.EnvScope{Environment: id}, "x", "1", false); code(err) != domain.CodeEnvironmentReadOnly {
		t.Errorf("an agent write into a closed environment = %v", err)
	}
	// Opening it is part of the same save.
	env.Readonly = false
	if err := s.Save(t.Context(), env); err != nil {
		t.Errorf("opening and editing at once = %v", err)
	}
	// A script still writes: closing is about what a person types by accident.
	if err := s.Activate(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	env = envOf(t, s, id)
	env.Readonly = true
	if err := s.Save(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVariable(t.Context(), domain.ScopeEnvironment, "token", "from-script"); err != nil {
		t.Errorf("a script write into a closed environment = %v", err)
	}
}

func TestGlobalsAreSavedAndCleared(t *testing.T) {
	s, _ := newEnvironments(t)
	if err := s.SaveGlobals(t.Context(), []domain.Variable{text("g", "host", "h")}); err != nil {
		t.Fatal(err)
	}
	if got := mustState(t, s).Globals; len(got) != 1 || got[0].Value != "h" {
		t.Fatalf("globals = %+v", got)
	}
	if err := s.SaveGlobals(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if got := mustState(t, s).Globals; len(got) != 0 {
		t.Errorf("globals after clearing = %+v", got)
	}
}

func TestDeletingTheActiveEnvironmentSelectsNone(t *testing.T) {
	s, _ := newEnvironments(t)
	id := mustCreate(t, s, EnvironmentInput{Name: "Local"})
	fill(t, s, id, text("a", "x", "1"))
	if err := s.Delete(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if state := mustState(t, s); state.ActiveID != "" || len(state.Environments) != 0 {
		t.Errorf("state = %+v", state)
	}
	if err := s.Activate(t.Context(), "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("activating a missing environment = %v", err)
	}
}

func TestResolutionOrderAndMasking(t *testing.T) {
	s, _ := newEnvironments(t)
	ctx := t.Context()
	id := mustCreate(t, s, EnvironmentInput{Name: "Local"})
	if err := s.SaveGlobals(ctx, []domain.Variable{text("g", "token", "global-value")}); err != nil {
		t.Fatal(err)
	}
	fill(t, s, id, text("a", "token", "env-value"), secret("b", "secret", "s3cret-value"))

	sent, _ := s.SubstituteTexts(ctx, nil, []string{"{{token}}/{{secret}}"}, false, "")
	masked, _ := s.SubstituteTexts(ctx, nil, []string{"{{token}}/{{secret}}"}, true, "")
	if sent[0] != "env-value/s3cret-value" || masked[0] != "env-value/"+domain.SecretMask {
		t.Errorf("sent %q, masked %q", sent[0], masked[0])
	}
	// A value with no token left (a script filled it in) is still hidden; a short one is not
	// searched for, since hiding a character or two would rewrite unrelated text.
	hidden, _ := s.SubstituteTexts(ctx, nil, []string{"Bearer s3cret-value"}, true, "")
	if hidden[0] != "Bearer "+domain.SecretMask {
		t.Errorf("hidden = %q", hidden[0])
	}
	missing, _ := s.Missing(ctx, nil, []string{"{{token}}/{{nope}}", "{{nope}}/{{other}}"}, "")
	if strings.Join(missing, ",") != "nope,other" {
		t.Errorf("missing = %v", missing)
	}

	// A disabled row stops resolving; the globals answer instead.
	fill(t, s, id, domain.Variable{ID: "a", Name: "token", Value: "env-value", Kind: domain.VariableText})
	if again, _ := s.SubstituteTexts(ctx, nil, []string{"{{token}}"}, false, ""); again[0] != "global-value" {
		t.Errorf("with the override off = %q", again[0])
	}
}

func TestAShortSecretValueIsNotSearchedFor(t *testing.T) {
	s, _ := newEnvironments(t)
	id := mustCreate(t, s, EnvironmentInput{Name: "Local"})
	fill(t, s, id, secret("a", "token", "ab"))
	masked, _ := s.SubstituteTexts(t.Context(), nil, []string{"https://api.example.com/ab/cab"}, true, "")
	if masked[0] != "https://api.example.com/ab/cab" {
		t.Errorf("masked = %q", masked[0])
	}
}

func TestAPinnedEnvironmentAndCollectionLevels(t *testing.T) {
	s, _ := newEnvironments(t)
	ctx := t.Context()
	local := mustCreate(t, s, EnvironmentInput{Name: "Local"})
	prod := mustCreate(t, s, EnvironmentInput{Name: "Prod"}) // active now
	fill(t, s, local, text("l", "host", "local-host"))
	fill(t, s, prod, text("p", "host", "prod-host"))

	pinned, _ := s.SubstituteTexts(ctx, nil, []string{"{{host}}"}, false, local)
	unpinned, _ := s.SubstituteTexts(ctx, nil, []string{"{{host}}"}, false, "")
	stale, _ := s.Missing(ctx, nil, []string{"{{host}}"}, "not-a-real-id")
	if pinned[0] != "local-host" || unpinned[0] != "prod-host" || len(stale) != 1 {
		t.Errorf("pinned %q, unpinned %q, stale pin missing %v", pinned[0], unpinned[0], stale)
	}

	// The window's levels stand over a collection's; the collection answers the rest.
	above := []domain.Variable{
		{Name: "host", Value: "collection-host", Enabled: true},
		{Name: "tenant", Value: "example", Enabled: true},
	}
	got, _ := s.SubstituteTexts(ctx, above, []string{"{{host}}/{{tenant}}"}, false, "")
	if got[0] != "prod-host/example" {
		t.Errorf("with collection levels = %q", got[0])
	}
}

func TestImportEntriesMergesByName(t *testing.T) {
	s, _ := newEnvironments(t)
	id := mustCreate(t, s, EnvironmentInput{Name: "Local"})
	scope := domain.EnvScope{Environment: id}
	for _, entries := range [][]dotenv.Entry{
		{{Name: "base_url", Value: "https://one"}, {Name: "api_token", Value: "t1", Secret: true}},
		{{Name: "base_url", Value: "https://two"}, {Name: "page_size", Value: "10"}, {Name: " "}},
	} {
		if err := s.ImportEntries(t.Context(), scope, entries); err != nil {
			t.Fatal(err)
		}
	}
	vars := envOf(t, s, id).Vars
	if len(vars) != 3 || vars[0].Value != "https://two" || vars[1].Kind != domain.VariableSecret ||
		vars[2].Name != "page_size" {
		t.Errorf("vars = %+v", vars)
	}
}

func TestEnsureDefaultsSeedsOnlyAnEmptyWorkspace(t *testing.T) {
	s, _ := newEnvironments(t)
	if err := s.EnsureDefaults(t.Context()); err != nil {
		t.Fatal(err)
	}
	state := mustState(t, s)
	if len(state.Environments) != 1 || state.ActiveID != state.Environments[0].ID ||
		state.Environments[0].Vars[0].Name != domain.BaseURLVariable {
		t.Fatalf("seeded = %+v", state)
	}
	mustCreate(t, s, EnvironmentInput{Name: "Prod"})
	if err := s.EnsureDefaults(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := len(mustState(t, s).Environments); n != 2 {
		t.Errorf("environments after a second call = %d", n)
	}
}

func TestImportNumbersATakenNameAndActivatesNothing(t *testing.T) {
	s, _ := newEnvironments(t)
	local := mustCreate(t, s, EnvironmentInput{Name: "Staging"})
	id, err := s.Import(t.Context(), domain.EnvironmentDraft{Name: " Staging ", Variables: []domain.Variable{
		{Name: "url", Value: "one", Enabled: true},
		{Name: "url", Value: "two", Enabled: true},
		{Name: "key", Value: "sk-1", Kind: domain.VariableSecret, Enabled: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	env := envOf(t, s, id)
	if mustState(t, s).ActiveID != local || env.Name != "Staging 2" || len(env.Vars) != 2 || env.Vars[0].Value != "two" {
		t.Errorf("imported = %+v", env)
	}
	if value, _ := s.Reveal(t.Context(), env.Vars[1].ID); value != "sk-1" {
		t.Errorf("imported secret = %q", value)
	}
}

func TestScriptsReadAndWriteByName(t *testing.T) {
	s, _ := newEnvironments(t)
	ctx := t.Context()
	if err := s.SetVariable(ctx, domain.ScopeEnvironment, "token", "x"); code(err) != domain.CodeNoEnvironment {
		t.Errorf("a write with no environment = %v", err)
	}
	mustCreate(t, s, EnvironmentInput{Name: "Local"})
	if err := s.SetVariable(ctx, domain.ScopeEnvironment, "token", "one"); err != nil {
		t.Fatal(err)
	}
	if err := s.Capture(ctx, domain.ScopeEnvironment, "token", "two", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVariable(ctx, domain.ScopeEnvironment, "token", "three"); err != nil {
		t.Fatal(err)
	}
	value, ok, err := s.Variable(ctx, domain.ScopeEnvironment, "token")
	if err != nil || !ok || value != "three" {
		t.Errorf("Variable = %q %v %v", value, ok, err)
	}
	vars := mustState(t, s).Environments[0].Vars
	if len(vars) != 1 || vars[0].Kind != domain.VariableSecret || vars[0].Value != "" {
		t.Errorf("a captured secret stopped being one: %+v", vars)
	}
	if err := s.SetVariable(ctx, domain.ScopeGlobals, "bad name", "x"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("a name no token can carry = %v", err)
	}
}

func TestChangesAreAnnouncedOnlyWhenSomethingChanged(t *testing.T) {
	s, store := newEnvironments(t)
	var heard []domain.Changed
	store.SetNotify(func(c domain.Changed) { heard = append(heard, c) })
	id := mustCreate(t, s, EnvironmentInput{Name: "Local"})
	env := fill(t, s, id, text("a", "x", "1"))
	heard = nil

	if err := s.Save(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 0 {
		t.Errorf("a save that changed nothing announced %v", heard)
	}
	env.Vars[0].Value = "2"
	if err := s.Save(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	want := domain.Changed{Kind: string(domain.EntityVariable), Workspace: domain.WorkspacePersonalID}
	if len(heard) != 1 || heard[0] != want {
		t.Errorf("heard = %v, want %v", heard, want)
	}
}
