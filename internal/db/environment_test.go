package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"schemo/internal/domain"
)

func TestEnvStateRoundTrip(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	env := domain.Environment{ID: "env-1", Name: "Local", Color: "green", Position: 1}
	if err := store.SaveEnvironment(ctx, ws, env); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	if err := store.SaveEnvironment(ctx, ws,
		domain.Environment{ID: "env-2", Name: "Prod", Readonly: true, Position: 2}); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}

	if err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-1"}, domain.Variable{
		ID: "v1", Name: "base_url", Value: "https://api.example.com", Kind: domain.VariableText,
		Enabled: true, Position: 1,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-1"}, domain.Variable{
		ID: "v2", Name: "token", Value: "s3cret", Kind: domain.VariableSecret, Enabled: true, Position: 2,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{}, domain.Variable{
		ID: "g1", Name: "page_size", Value: "10", Kind: domain.VariableText, Enabled: true, Position: 1,
	}); err != nil {
		t.Fatalf("SaveVariable (globals): %v", err)
	}
	if err := store.SetActiveEnvironment(ctx, ws, "env-1"); err != nil {
		t.Fatalf("SetActiveEnvironment: %v", err)
	}

	state, err := store.EnvState(ctx, ws)
	if err != nil {
		t.Fatalf("EnvState: %v", err)
	}
	if state.ActiveID != "env-1" {
		t.Errorf("activeId = %q, want env-1", state.ActiveID)
	}
	if len(state.Environments) != 2 {
		t.Fatalf("environments = %d, want 2", len(state.Environments))
	}
	if state.Environments[0].Name != "Local" || state.Environments[1].Name != "Prod" {
		t.Errorf("environments came back in the wrong order: %v", state.Environments)
	}
	if !state.Environments[1].Readonly {
		t.Error("Prod lost its readonly flag")
	}
	local := state.Environments[0]
	if len(local.Vars) != 2 || local.Vars[0].Name != "base_url" || local.Vars[1].Name != "token" {
		t.Errorf("variables = %v, want base_url then token", local.Vars)
	}
	if local.Vars[1].Kind != domain.VariableSecret || !local.Vars[1].HasValue {
		t.Errorf("the secret lost its kind or its value: %+v", local.Vars[1])
	}
	if len(state.Globals) != 1 || state.Globals[0].Name != "page_size" {
		t.Errorf("globals = %v, want one row", state.Globals)
	}
}

func TestVariablesMigrateBetweenScopes(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	// The same name may exist in an environment and in the globals: that is how an override works.
	for _, scope := range []domain.EnvScope{{Environment: "env-1"}, {}} {
		v := domain.Variable{ID: "v-" + scope.Environment, Name: "token", Value: "x",
			Kind: domain.VariableText, Enabled: true}
		if err := store.SaveVariable(ctx, ws, scope, v); err != nil {
			t.Fatalf("SaveVariable in %q: %v", scope.Environment, err)
		}
	}

	// And a second one under the same scope is a conflict rather than a database error.
	err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-1"}, domain.Variable{
		ID: "other", Name: "token", Kind: domain.VariableText, Enabled: true,
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate name = %v, want domain.ErrConflict", err)
	}

	// The same variable, renamed, keeps its row: an update is not a conflict with itself.
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-1"}, domain.Variable{
		ID: "v-env-1", Name: "api_token", Kind: domain.VariableSecret, Value: "s", Enabled: true,
	}); err != nil {
		t.Errorf("renaming a variable failed: %v", err)
	}
	if value, err := store.VariableValue(ctx, ws, "v-env-1"); err != nil || value != "s" {
		t.Errorf("after the rename: value = %q, err = %v; want the row updated in place", value, err)
	}
}

func TestDeleteEnvironmentTakesItsVariables(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveEnvironment(ctx, ws,
		domain.Environment{ID: "env-1", Name: "Local"}); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-1"}, domain.Variable{
		ID: "v1", Name: "base_url", Kind: domain.VariableText, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{}, domain.Variable{
		ID: "g1", Name: "page_size", Kind: domain.VariableText, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable (globals): %v", err)
	}

	if err := store.DeleteEnvironment(ctx, ws, "env-1"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}

	state, err := store.EnvState(ctx, ws)
	if err != nil {
		t.Fatalf("EnvState: %v", err)
	}
	if len(state.Environments) != 0 {
		t.Errorf("environments = %v, want none", state.Environments)
	}
	if len(state.Globals) != 1 {
		t.Errorf("globals = %v, want the global to survive the environment", state.Globals)
	}
	if _, err := store.VariableValue(ctx, ws, "v1"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the environment's variable outlived it: %v", err)
	}
}

func TestVariableValueAndMissing(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveVariable(ctx, ws, domain.EnvScope{}, domain.Variable{
		ID: "v1", Name: "token", Value: "s3cret", Kind: domain.VariableSecret, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}

	value, err := store.VariableValue(ctx, ws, "v1")
	if err != nil || value != "s3cret" {
		t.Errorf("VariableValue = %q, %v; want the stored value", value, err)
	}
	if _, err := store.VariableValue(ctx, ws, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown id = %v, want domain.ErrNotFound", err)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if _, ok, err := store.Setting(ctx, domain.SettingActiveWorkspace); err != nil || ok {
		t.Errorf("Setting on an empty table = ok:%v err:%v, want absent", ok, err)
	}
	if err := store.SaveSetting(ctx, "theme", "dark"); err != nil {
		t.Fatalf("SaveSetting: %v", err)
	}
	if err := store.SaveSetting(ctx, "theme", "light"); err != nil {
		t.Fatalf("SaveSetting (overwrite): %v", err)
	}

	if held, _, err := store.Setting(ctx, "theme"); err != nil || held != "light" {
		t.Errorf("theme = %q (%v), want the overwritten value", held, err)
	}
}

func TestStorePathIsInsideTheDataDir(t *testing.T) {
	dir := tempDataDir(t)
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Both sides are cleaned before they are compared: the store's path is built with filepath.Join
	// and carries the separator of the platform, while the directory is the string t.TempDir()
	// answered with — which is mixed when GOTMPDIR itself has a forward slash in it.
	if filepath.Dir(store.Path()) != filepath.Clean(string(dir)) {
		t.Errorf("database at %q, want it inside %q", store.Path(), dir)
	}
}

// A deleted environment and a deleted variable are gone from everything the window draws, and the
// name a deleted variable had is free again: "delete it and add it back" is how anybody fixes a
// variable they got wrong, and an index that kept the name of a row nobody can see would refuse it.
func TestADeletedEnvironmentAndItsVariables(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveEnvironment(ctx, ws,
		domain.Environment{ID: "env-1", Name: "Local"}); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	for _, v := range []domain.Variable{
		{ID: "v1", Name: "base_url", Value: "https://api.example.com", Kind: domain.VariableText,
			Enabled: true},
		{ID: "v2", Name: "token", Value: "s3cret", Kind: domain.VariableSecret, Enabled: true},
	} {
		if err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-1"}, v); err != nil {
			t.Fatalf("SaveVariable %s: %v", v.Name, err)
		}
	}

	if err := store.DeleteVariable(ctx, ws, "v2"); err != nil {
		t.Fatalf("DeleteVariable: %v", err)
	}
	held, err := store.EnvState(ctx, ws)
	if err != nil {
		t.Fatalf("EnvState: %v", err)
	}
	left := held.Environments[0].Vars
	if len(left) != 1 || left[0].Name != "base_url" {
		t.Errorf("variables = %+v, want the one that is left", left)
	}
	if _, err := store.VariableValue(ctx, ws, "v2"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("the value of a deleted variable = %v, want not found", err)
	}
	// The name came back with the deletion, which is what the partial index is for: the same name in
	// the same scope saves cleanly under a new id.
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-1"}, domain.Variable{
		ID: "v3", Name: "token", Value: "another", Kind: domain.VariableSecret, Enabled: true,
	}); err != nil {
		t.Errorf("reusing the name of a deleted variable: %v", err)
	}

	// The environment takes its variables with it, and both leave the screen.
	if err := store.DeleteEnvironment(ctx, ws, "env-1"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	state, err := store.EnvState(ctx, ws)
	if err != nil {
		t.Fatalf("EnvState: %v", err)
	}
	left = state.Globals
	if len(state.Environments) != 0 || len(left) != 0 {
		t.Errorf("a deleted environment left %d environments and %d variables behind",
			len(state.Environments), len(left))
	}

	// Nothing was removed: the rows are on disk with the marker on them, which is what the other side
	// is told about.
	var variables, environments int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM entities WHERE kind = 'variable' AND deleted_at IS NOT NULL`,
	).Scan(&variables); err != nil {
		t.Fatalf("counting the marked variables: %v", err)
	}
	// All three: the one deleted by hand, and the two the environment took with it.
	if variables != 3 {
		t.Errorf("%d variables are marked, want the three that are gone", variables)
	}
	if err := store.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM entities WHERE kind = 'environment' AND deleted_at IS NOT NULL`,
	).Scan(&environments); err != nil {
		t.Fatalf("counting the marked environments: %v", err)
	}
	if environments != 1 {
		t.Errorf("%d environments are marked, want 1", environments)
	}
}
