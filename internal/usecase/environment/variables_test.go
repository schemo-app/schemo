package environment

import (
	"context"
	"errors"
	"testing"

	"schemo/internal/domain"
	"schemo/internal/dotenv"
)

// A script reads a variable by name in one scope, and the snapshot's rules do not apply to it: a
// secret is read as its value, because signing a request with it is the whole point of having one.
func TestVariableReadsByName(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()

	_, env := seed(t, u, "Local · dev")
	if _, err := u.AddVariable(ctx, domain.EnvScope{Environment: env.ID}, VariableDraft{
		Name: "baseUrl", Value: "https://api.example.com",
	}); err != nil {
		t.Fatalf("AddVariable: %v", err)
	}
	if _, err := u.AddVariable(ctx, domain.EnvScope{}, VariableDraft{
		Name: "token", Value: "s3cret", Kind: domain.VariableSecret,
	}); err != nil {
		t.Fatalf("AddVariable: %v", err)
	}

	value, found, err := u.Variable(ctx, domain.ScopeEnvironment, "baseUrl")
	if err != nil || !found || value != "https://api.example.com" {
		t.Errorf("baseUrl = %q, %v, %v, want the environment's value", value, found, err)
	}
	value, found, err = u.Variable(ctx, domain.ScopeGlobals, "token")
	if err != nil || !found || value != "s3cret" {
		t.Errorf("token = %q, %v, %v, want the secret's value", value, found, err)
	}

	// Each scope answers for itself: the globals have no baseUrl, and the environment has no token.
	if _, found, err := u.Variable(ctx, domain.ScopeGlobals, "baseUrl"); err != nil || found {
		t.Errorf("the globals answered with a variable of the environment: %v, %v", found, err)
	}
	if _, found, err := u.Variable(ctx, domain.ScopeEnvironment, "token"); err != nil || found {
		t.Errorf("the environment answered with a global: %v, %v", found, err)
	}
	if _, found, err := u.Variable(ctx, domain.ScopeEnvironment, "нет-такой"); err != nil || found {
		t.Errorf("an unknown name = %v, %v, want nothing", found, err)
	}
}

// A script writes by name, and a name nobody has is added: a script recording a token for the next
// request of a run cannot be expected to have created the variable first.
func TestSetVariableAddsAndReplaces(t *testing.T) {
	u, store := newUseCase(t)
	ctx := context.Background()

	seed(t, u, "Local · dev")
	if err := u.SetVariable(ctx, domain.ScopeEnvironment, "page", "2"); err != nil {
		t.Fatalf("SetVariable: %v", err)
	}
	if err := u.SetVariable(ctx, domain.ScopeGlobals, "auth", "из-скрипта"); err != nil {
		t.Fatalf("SetVariable: %v", err)
	}

	value, found, err := u.Variable(ctx, domain.ScopeEnvironment, "page")
	if err != nil || !found || value != "2" {
		t.Errorf("page = %q, %v, %v, want what the script wrote", value, found, err)
	}
	if value, _, _ := u.Variable(ctx, domain.ScopeGlobals, "auth"); value != "из-скрипта" {
		t.Errorf("auth = %q, want what the script wrote", value)
	}

	// Writing the same name again replaces it in place: two rows with one name in one scope is a
	// scope that answers differently depending on the order it is read in.
	before, err := store.EnvState(ctx, domain.WorkspacePersonalID)
	if err != nil {
		t.Fatalf("EnvState: %v", err)
	}
	was := placeOf(before.Environments[0].Vars, "page")

	if err := u.SetVariable(ctx, domain.ScopeEnvironment, "page", "3"); err != nil {
		t.Fatalf("SetVariable: %v", err)
	}
	after, err := store.EnvState(ctx, domain.WorkspacePersonalID)
	if err != nil {
		t.Fatalf("EnvState: %v", err)
	}
	if count := countOf(after.Environments[0].Vars, "page"); count != 1 {
		t.Errorf("page appears %d time(s), want once", count)
	}
	if place := placeOf(after.Environments[0].Vars, "page"); place != was {
		t.Errorf("page moved from %d to %d, want it where it was", was, place)
	}
	if value, _, _ := u.Variable(ctx, domain.ScopeEnvironment, "page"); value != "3" {
		t.Errorf("page = %q, want the second value", value)
	}
}

func countOf(variables []domain.Variable, name string) int {
	count := 0
	for _, v := range variables {
		if v.Name == name {
			count++
		}
	}
	return count
}

func placeOf(variables []domain.Variable, name string) int {
	for _, v := range variables {
		if v.Name == name {
			return v.Position
		}
	}
	return -1
}

// What a script cannot do, it says so about: there is no environment selected to write into, there
// is no scope called that, and a name no `{{token}}` can carry is a name nobody can read back.
func TestVariableRefusesWhatItCannotDo(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()

	// No environment exists at all, so none is active.
	if _, found, err := u.Variable(ctx, domain.ScopeEnvironment, "baseUrl"); err != nil || found {
		t.Errorf("a name in a scope that is not selected = %v, %v, want nothing", found, err)
	}
	if err := u.SetVariable(ctx, domain.ScopeEnvironment, "token", "1"); err == nil {
		t.Error("writing to an environment nobody selected was allowed")
	}

	for _, scope := range []domain.VarScope{domain.ScopeRun, "какая-то"} {
		if _, _, err := u.Variable(ctx, scope, "token"); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("reading %q = %v, want ErrNotAllowed", scope, err)
		}
		if err := u.SetVariable(ctx, scope, "token", "1"); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("writing to %q = %v, want ErrNotAllowed", scope, err)
		}
	}

	for _, name := range []string{"", "  ", "a b", "{{x}}"} {
		if err := u.SetVariable(ctx, domain.ScopeGlobals, name, "1"); !errors.Is(err,
			domain.ErrNotAllowed) {
			t.Errorf("writing %q = %v, want ErrNotAllowed", name, err)
		}
	}
}

// The globals are one scope and cannot be deleted, so the destructive thing on offer is emptying
// them — and emptying them must not reach into the environments beside them, which hold variables
// of their own that happen to live in the same table.
func TestClearGlobalsEmptiesOnlyTheGlobals(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	_, env := seed(t, u, "Local")

	for _, one := range []struct {
		scope domain.EnvScope
		name  string
	}{
		{domain.EnvScope{}, "locale"},
		{domain.EnvScope{}, "timeout"},
		{domain.EnvScope{Environment: env.ID}, "baseUrl"},
	} {
		if _, err := u.AddVariable(ctx, one.scope, VariableDraft{
			Name: one.name, Kind: domain.VariableText, Value: "x",
		}); err != nil {
			t.Fatalf("AddVariable(%s): %v", one.name, err)
		}
	}

	state, err := u.ClearGlobals(ctx)
	if err != nil {
		t.Fatalf("ClearGlobals: %v", err)
	}
	if len(state.Globals) != 0 {
		t.Errorf("globals = %+v, want none left", state.Globals)
	}
	if len(state.Environments[0].Vars) != 1 {
		t.Errorf("environment variables = %+v, want its own left alone", state.Environments[0].Vars)
	}
	if state.ActiveID != env.ID {
		t.Errorf("activeId = %q, want the environment still active", state.ActiveID)
	}
}

// Clearing what is already empty is not an error: the button is pressed on a state the window is
// showing, and a state that changed under it is not something to refuse.
func TestClearGlobalsOnAnEmptyScope(t *testing.T) {
	u, _ := newUseCase(t)

	state, err := u.ClearGlobals(context.Background())
	if err != nil {
		t.Fatalf("ClearGlobals: %v", err)
	}
	if len(state.Globals) != 0 {
		t.Errorf("globals = %+v, want none", state.Globals)
	}
}

// Every door a value can come in by, checked at the door: an environment the window has closed is
// one nobody writes into, and the promise is the use case's rather than a button's — the window's
// own controls check the same flag, and the side that can hold it for every caller is this one.
func TestWritesIntoAClosedEnvironmentAreRefused(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	_, env := seed(t, u, "Prod")
	scope := domain.EnvScope{Environment: env.ID}

	state, err := u.AddVariable(ctx, scope, VariableDraft{Name: "token", Kind: domain.VariableText})
	if err != nil {
		t.Fatalf("AddVariable: %v", err)
	}
	v := state.Environments[0].Vars[0]

	closed := true
	if _, err := u.Update(ctx, env.ID, Patch{Readonly: &closed}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	for _, door := range []struct {
		name string
		call func() error
	}{
		{"AddVariable", func() error {
			_, err := u.AddVariable(ctx, scope, VariableDraft{Name: "second"})
			return err
		}},
		{"UpdateVariable", func() error {
			_, err := u.UpdateVariable(ctx, scope, VariablePatch{
				ID: v.ID, Name: "renamed", Kind: domain.VariableText, Enabled: true,
				Value: "x", SetValue: true,
			})
			return err
		}},
		{"RemoveVariable", func() error {
			_, err := u.RemoveVariable(ctx, scope, v.ID)
			return err
		}},
		{"ImportEntries", func() error {
			_, err := u.ImportEntries(ctx, scope,
				[]dotenv.Entry{{Name: "imported", Value: "x"}})
			return err
		}},
	} {
		err := door.call()
		if !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("%s on a closed environment = %v, want domain.ErrNotAllowed", door.name, err)
			continue
		}
		if got := domain.CodeOf(err); got != domain.CodeEnvironmentReadOnly {
			t.Errorf("%s = %q, want %q", door.name, got, domain.CodeEnvironmentReadOnly)
		}
	}

	// And nothing of it landed: the refusal is before the write, not after it.
	after, err := u.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	vars := after.Environments[0].Vars
	if len(vars) != 1 || vars[0].Name != "token" || vars[0].Value != "" {
		t.Errorf("vars = %+v, want the one that was there, untouched", vars)
	}
}

// The line is drawn here on purpose: a script writing a value it has just received is the run doing
// its job and not somebody editing a variable, and refusing it would break every run in an
// environment that was closed to protect it.
func TestAScriptStillWritesToAClosedEnvironment(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	_, env := seed(t, u, "Prod")

	closed := true
	if _, err := u.Update(ctx, env.ID, Patch{Readonly: &closed}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := u.SetVariable(ctx, domain.ScopeEnvironment, "token", "fresh"); err != nil {
		t.Fatalf("SetVariable: %v", err)
	}
	value, ok, err := u.Variable(ctx, domain.ScopeEnvironment, "token")
	if err != nil {
		t.Fatalf("Variable: %v", err)
	}
	if !ok || value != "fresh" {
		t.Errorf("token = %q (%v), want what the script wrote", value, ok)
	}
}

// A value taken from a response becomes a secret when asked, and stays one when a later capture
// does not ask: a token that was hidden must not become visible by being refreshed.
func TestCaptureKeepsASecretSecret(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	seed(t, u, "Local · dev")

	state, err := u.Capture(ctx, domain.ScopeEnvironment, "token", "first", true)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if kind := kindOf(state.Environments[0].Vars, "token"); kind != domain.VariableSecret {
		t.Errorf("token kind = %q, want secret", kind)
	}

	state, err = u.Capture(ctx, domain.ScopeEnvironment, "token", "second", false)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if kind := kindOf(state.Environments[0].Vars, "token"); kind != domain.VariableSecret {
		t.Errorf("token kind after a plain capture = %q, want it still secret", kind)
	}
	if value, _, _ := u.Variable(ctx, domain.ScopeEnvironment, "token"); value != "second" {
		t.Errorf("token = %q, want the second value", value)
	}
	if _, err := u.Capture(ctx, domain.ScopeEnvironment, "not a name", "x", false); err == nil {
		t.Error("a name no token can carry was accepted")
	}
}

func kindOf(variables []domain.Variable, name string) domain.VariableKind {
	for _, v := range variables {
		if v.Name == name {
			return v.Kind
		}
	}
	return ""
}
