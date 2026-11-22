package workspace

import (
	"errors"
	"strings"
	"testing"

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

func mustCreate(t *testing.T, s *Environments, name string) string {
	t.Helper()
	id, err := s.Create(t.Context(), name, "")
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	return id
}

// mustUse creates an environment and makes it the active one.
func mustUse(t *testing.T, s *Environments, name string) string {
	t.Helper()
	id := mustCreate(t, s, name)
	if err := s.Activate(t.Context(), id); err != nil {
		t.Fatal(err)
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
	env, ok := mustState(t, s).Find(id)
	if !ok {
		t.Fatalf("environment %s is gone", id)
	}
	return env
}

// fill saves an environment with the given variables, as the window would after editing it.
func fill(t *testing.T, s *Environments, id string, vars ...domain.Variable) domain.Environment {
	t.Helper()
	if err := s.SaveVariables(t.Context(), domain.EnvScope{Environment: id}, vars); err != nil {
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

func TestCreateValidatesNamesAndKeepsTheSelection(t *testing.T) {
	s, _ := newEnvironments(t)
	id, err := s.Create(t.Context(), "  Local  ", "#ff9500")
	if err != nil {
		t.Fatal(err)
	}
	state := mustState(t, s)
	if state.ActiveID != "" || state.Environments[0].ID != id || state.Environments[0].Name != "Local" || state.Environments[0].Color != "#ff9500" {
		t.Errorf("state = %+v", state)
	}
	for name, want := range map[string]domain.Code{
		"   ": domain.CodeNameEmpty,
		strings.Repeat("x", maxEnvironmentName+1): domain.CodeNameTooLong,
		"Local":  domain.CodeNameTaken,
		" local": domain.CodeNameTaken,
	} {
		if _, err := s.Create(t.Context(), name, ""); code(err) != want {
			t.Errorf("Create(%q) = %v, want %s", name, err, want)
		}
	}
}

func TestCreateStartsWithNoVariables(t *testing.T) {
	s, _ := newEnvironments(t)
	dev := mustCreate(t, s, "Dev")
	fill(t, s, dev, text("a", "baseUrl", "http://dev"), secret("b", "token", "test-secret"))
	stage := envOf(t, s, mustCreate(t, s, "Stage"))
	if len(stage.Vars) != 0 || len(stage.Collections) != 0 {
		t.Errorf("Create(Stage) = %+v; want empty variables", stage)
	}
}

func TestMetadataUpdatesLeaveVariableScopesUntouched(t *testing.T) {
	s, _ := newEnvironments(t)
	id := mustCreate(t, s, "Local")
	fill(t, s, id, secret("token", "token", "test-secret"))
	saveBlock(t, s, id, "catalog", text("host", "host", "api.test"))
	name, color, readonly := "Development", "blue", true
	if err := s.Update(t.Context(), id, EnvironmentPatch{Name: &name, Color: &color, Readonly: &readonly}); err != nil {
		t.Fatal(err)
	}
	env := envOf(t, s, id)
	if env.Name != name || env.Color != color || !env.Readonly {
		t.Errorf("Update(metadata) = %+v; want changed metadata", env)
	}
	if len(env.Vars) != 1 || !env.Vars[0].HasValue || len(env.Block("catalog")) != 1 {
		t.Fatalf("Update(metadata) changed variable scopes: %+v", env)
	}
	if value, err := s.Reveal(t.Context(), "token"); err != nil || value != "test-secret" {
		t.Errorf("Reveal after metadata update = %q, %v; want stored secret", value, err)
	}
}

func TestSaveKeepsWithheldSecretsAndTakesNewOnes(t *testing.T) {
	s, _ := newEnvironments(t)
	id := mustCreate(t, s, "Local")
	env := fill(t, s, id, secret("tok", "token", "s3cret"), text("url", "url", "http://x"))
	if v := env.Vars[0]; v.Value != "" || !v.HasValue {
		t.Fatalf("State showed a secret: %+v", v)
	}

	// The window sends back what it shows: no value, hasValue set.
	name := "Renamed"
	if err := s.Update(t.Context(), id, EnvironmentPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	env.Vars[1].Value = "http://y"
	if err := s.SaveVariables(t.Context(), domain.EnvScope{Environment: id}, env.Vars); err != nil {
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
	id := mustCreate(t, s, "Local")
	mustCreate(t, s, "Prod")
	env := envOf(t, s, id)

	for name, vars := range map[string][]domain.Variable{
		"bad name": {text("a", "has space", "")},
		"twice":    {text("a", "x", ""), text("b", "x", "")},
		"no id":    {text("", "x", "")},
		"same id":  {text("a", "x", ""), text("a", "y", "")},
	} {
		env.Vars = vars
		if err := s.SaveVariables(t.Context(), domain.EnvScope{Environment: id}, env.Vars); err == nil {
			t.Errorf("%s: saved", name)
		}
	}
	env.Vars = []domain.Variable{text("a", "", ""), text("b", "", "")}
	if err := s.SaveVariables(t.Context(), domain.EnvScope{Environment: id}, env.Vars); err != nil {
		t.Errorf("two rows still being named = %v", err)
	}
	name := "Prod"
	if err := s.Update(t.Context(), id, EnvironmentPatch{Name: &name}); code(err) != domain.CodeNameTaken {
		t.Errorf("a taken name = %v", err)
	}
}

func TestAClosedEnvironmentTakesNoVariableChanges(t *testing.T) {
	s, _ := newEnvironments(t)
	ctx := t.Context()
	id := mustCreate(t, s, "Prod")
	env := fill(t, s, id, text("a", "url", "http://prod"))
	readonly := true
	if err := s.Update(ctx, id, EnvironmentPatch{Readonly: &readonly}); err != nil {
		t.Fatal(err)
	}
	name := "Production"
	if err := s.Update(ctx, id, EnvironmentPatch{Name: &name}); err != nil {
		t.Errorf("Update(readonly name) = %v; want success", err)
	}
	env.Vars[0].Value = "http://oops"
	if err := s.SaveVariables(ctx, domain.EnvScope{Environment: id}, env.Vars); code(err) != domain.CodeEnvironmentReadOnly {
		t.Errorf("SaveVariables(readonly) = %v; want readonly refusal", err)
	}
	if _, err := s.ImportVariables(ctx, domain.EnvScope{Environment: id}, []domain.Variable{text("", "x", "1")}); code(err) != domain.CodeEnvironmentReadOnly {
		t.Errorf("ImportVariables(readonly) = %v; want readonly refusal", err)
	}
	if _, err := s.SetInScope(ctx, domain.EnvScope{Environment: id}, "x", "1", false); code(err) != domain.CodeEnvironmentReadOnly {
		t.Errorf("SetInScope(readonly) = %v; want readonly refusal", err)
	}
	readonly = false
	if err := s.Update(ctx, id, EnvironmentPatch{Readonly: &readonly}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveVariables(ctx, domain.EnvScope{Environment: id}, env.Vars); err != nil {
		t.Fatal(err)
	}
	if err := s.Activate(ctx, id); err != nil {
		t.Fatal(err)
	}
	readonly = true
	if err := s.Update(ctx, id, EnvironmentPatch{Readonly: &readonly}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVariable(ctx, domain.ScopeEnvironment, "token", "from-script"); err != nil {
		t.Errorf("SetVariable(readonly script) = %v; want success", err)
	}
}

func TestGlobalsAreSavedAndCleared(t *testing.T) {
	s, _ := newEnvironments(t)
	if err := s.SaveVariables(t.Context(), domain.EnvScope{}, []domain.Variable{text("g", "host", "h")}); err != nil {
		t.Fatal(err)
	}
	if got := mustState(t, s).Globals; len(got) != 1 || got[0].Value != "h" {
		t.Fatalf("globals = %+v", got)
	}
	if err := s.SaveVariables(t.Context(), domain.EnvScope{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := mustState(t, s).Globals; len(got) != 0 {
		t.Errorf("globals after clearing = %+v", got)
	}
}

func TestDeletingTheActiveEnvironmentSelectsNone(t *testing.T) {
	s, _ := newEnvironments(t)
	id := mustCreate(t, s, "Local")
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
	id := mustUse(t, s, "Local")
	if err := s.SaveVariables(ctx, domain.EnvScope{}, []domain.Variable{text("g", "token", "global-value")}); err != nil {
		t.Fatal(err)
	}
	fill(t, s, id, text("a", "token", "env-value"), secret("b", "secret", "s3cret-value"))

	sent, _ := s.SubstituteTexts(ctx, domain.VarLevels{}, []string{"{{token}}/{{secret}}"}, false)
	masked, _ := s.SubstituteTexts(ctx, domain.VarLevels{}, []string{"{{token}}/{{secret}}"}, true)
	if sent[0] != "env-value/s3cret-value" || masked[0] != "env-value/"+domain.SecretMask {
		t.Errorf("sent %q, masked %q", sent[0], masked[0])
	}
	// A value with no token left (a script filled it in) is still hidden; a short one is not
	// searched for, since hiding a character or two would rewrite unrelated text.
	hidden, _ := s.SubstituteTexts(ctx, domain.VarLevels{}, []string{"Bearer s3cret-value"}, true)
	if hidden[0] != "Bearer "+domain.SecretMask {
		t.Errorf("hidden = %q", hidden[0])
	}
	missing, _ := s.Missing(ctx, domain.VarLevels{}, []string{"{{token}}/{{nope}}", "{{nope}}/{{other}}"})
	if strings.Join(missing, ",") != "nope,other" {
		t.Errorf("missing = %v", missing)
	}

	// A disabled row stops resolving; the globals answer instead.
	fill(t, s, id, domain.Variable{ID: "a", Name: "token", Value: "env-value", Kind: domain.VariableText})
	if again, _ := s.SubstituteTexts(ctx, domain.VarLevels{}, []string{"{{token}}"}, false); again[0] != "global-value" {
		t.Errorf("with the override off = %q", again[0])
	}
}

func TestAShortSecretValueIsNotSearchedFor(t *testing.T) {
	s, _ := newEnvironments(t)
	id := mustCreate(t, s, "Local")
	fill(t, s, id, secret("a", "token", "ab"))
	masked, _ := s.SubstituteTexts(t.Context(), domain.VarLevels{}, []string{"https://api.example.com/ab/cab"}, true)
	if masked[0] != "https://api.example.com/ab/cab" {
		t.Errorf("masked = %q", masked[0])
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
	mustCreate(t, s, "Prod")
	if err := s.EnsureDefaults(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := len(mustState(t, s).Environments); n != 2 {
		t.Errorf("environments after a second call = %d", n)
	}
}

func TestScriptsReadAndWriteByName(t *testing.T) {
	s, _ := newEnvironments(t)
	ctx := t.Context()
	if err := s.SetVariable(ctx, domain.ScopeEnvironment, "token", "x"); code(err) != domain.CodeNoEnvironment {
		t.Errorf("a write with no environment = %v", err)
	}
	mustUse(t, s, "Local")
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
	id := mustCreate(t, s, "Local")
	env := fill(t, s, id, text("a", "x", "1"))
	heard = nil

	if err := s.SaveVariables(t.Context(), domain.EnvScope{Environment: id}, env.Vars); err != nil {
		t.Fatal(err)
	}
	if len(heard) != 0 {
		t.Errorf("a save that changed nothing announced %v", heard)
	}
	env.Vars[0].Value = "2"
	if err := s.SaveVariables(t.Context(), domain.EnvScope{Environment: id}, env.Vars); err != nil {
		t.Fatal(err)
	}
	want := domain.Changed{Kind: string(domain.EntityVariable), Workspace: domain.WorkspacePersonalID}
	if len(heard) != 1 || heard[0] != want {
		t.Errorf("heard = %v, want %v", heard, want)
	}
}
