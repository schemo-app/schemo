package environment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"schemo/internal/domain"
	"schemo/internal/dotenv"
	"schemo/internal/platform"
)

// fakeStore is the in-memory double for the feature's port. It keeps the same rules the SQL does —
// variables belong to a scope, the active environment is a setting — so the tests below are about
// the use case, not about SQLite.
type fakeStore struct {
	envs   []domain.Environment
	vars   map[string]fakeVar
	active string
	// awaiting is whether the space is one of the account's that has not been pulled yet.
	awaiting bool
	// readOnly is a space this person may only read: every write of content is refused.
	readOnly bool
	// asked is the workspaces EnvState was read for: the store keeps one state, and which space a
	// read names is what the per-space calls are about.
	asked []string
}

type fakeVar struct {
	scope domain.EnvScope
	v     domain.Variable
}

func newFakeStore() *fakeStore {
	return &fakeStore{vars: map[string]fakeVar{}}
}

// fakeScope answers with the workspace the test is working in. The store below keeps one flat
// state and ignores the id: what is being tested here is the use case, and the split between
// workspaces is the SQL's own test.
type fakeScope struct{ id string }

func (f fakeScope) ActiveWorkspace(context.Context) (string, error) {
	if f.id == "" {
		return domain.WorkspacePersonalID, nil
	}
	return f.id, nil
}

func (f *fakeStore) EnvState(_ context.Context, workspace string) (domain.EnvState, error) {
	f.asked = append(f.asked, workspace)
	state := domain.EnvState{Environments: []domain.Environment{}, Globals: []domain.Variable{},
		ActiveID: f.active}
	for _, env := range f.envs {
		copied := env
		copied.Vars = []domain.Variable{}
		for _, entry := range sortedVars(f.vars) {
			if entry.scope.Environment == env.ID {
				copied.Vars = append(copied.Vars, entry.v)
			}
		}
		state.Environments = append(state.Environments, copied)
	}
	for _, entry := range sortedVars(f.vars) {
		if entry.scope.Environment == "" {
			state.Globals = append(state.Globals, entry.v)
		}
	}
	return state, nil
}

// sortedVars keeps the map deterministic so positions and order are comparable.
func sortedVars(vars map[string]fakeVar) []fakeVar {
	out := make([]fakeVar, 0, len(vars))
	for _, v := range vars {
		out = append(out, v)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].v.Position > out[j].v.Position; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

func (f *fakeStore) SaveEnvironment(_ context.Context, _ string, env domain.Environment) error {
	if f.readOnly {
		return domain.Refuse(domain.CodeWorkspaceReadOnly, domain.ErrNotAllowed, nil)
	}
	for i := range f.envs {
		if f.envs[i].ID == env.ID {
			f.envs[i] = env
			return nil
		}
	}
	f.envs = append(f.envs, env)
	return nil
}

func (f *fakeStore) DeleteEnvironment(_ context.Context, _ string, id string) error {
	kept := f.envs[:0]
	for _, env := range f.envs {
		if env.ID != id {
			kept = append(kept, env)
		}
	}
	f.envs = kept
	for key, entry := range f.vars {
		if entry.scope.Environment == id {
			delete(f.vars, key)
		}
	}
	return nil
}

func (f *fakeStore) SaveVariable(
	_ context.Context,
	_ string,
	scope domain.EnvScope,
	v domain.Variable,
) error {
	f.vars[v.ID] = fakeVar{scope: scope, v: v}
	return nil
}

func (f *fakeStore) DeleteVariable(_ context.Context, _ string, id string) error {
	delete(f.vars, id)
	return nil
}

func (f *fakeStore) VariableValue(_ context.Context, _ string, id string) (string, error) {
	entry, ok := f.vars[id]
	if !ok {
		return "", domain.ErrNotFound
	}
	return entry.v.Value, nil
}

func (f *fakeStore) SetActiveEnvironment(_ context.Context, _ string, id string) error {
	f.active = id
	return nil
}

func (f *fakeStore) AwaitsFirstPull(context.Context, string) (bool, error) {
	return f.awaiting, nil
}

func newUseCase(t *testing.T) (*UseCase, *fakeStore) {
	t.Helper()
	store := newFakeStore()
	ids := platform.NewIDGen()
	return NewUseCase(store, fakeScope{}, ids), store
}

func seed(t *testing.T, u *UseCase, name string) (domain.EnvState, domain.Environment) {
	t.Helper()
	state, err := u.Create(context.Background(), Draft{Name: name})
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	return state.EnvState, state.Environments[len(state.Environments)-1]
}

func TestCreateValidatesAndActivates(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()

	for _, bad := range []string{"", "   ", strings.Repeat("x", maxNameLength+1)} {
		if _, err := u.Create(ctx, Draft{Name: bad}); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("Create(%q) = %v, want domain.ErrNotAllowed", bad, err)
		}
	}

	state, env := seed(t, u, "Local")
	if env.Name != "Local" {
		t.Errorf("created environment = %+v, want the name it was given", env)
	}
	if state.ActiveID != env.ID {
		t.Errorf("activeId = %q, want the new environment", state.ActiveID)
	}

	// Each one lands after the last, which is the order the list draws them in.
	state, second := seed(t, u, "Prod")
	if second.Position <= env.Position {
		t.Errorf("positions = %d then %d, want the newer one after", env.Position, second.Position)
	}
	if state.ActiveID != second.ID {
		t.Errorf("activeId = %q, want the newest environment", state.ActiveID)
	}
}

func TestUpdateAndDelete(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	_, env := seed(t, u, "Local")

	name := "Prod · EU"
	readonly := true
	state, err := u.Update(ctx, env.ID,
		Patch{Name: &name, Readonly: &readonly, Color: ptr("red")})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := state.Environments[0]
	if got.Name != name || !got.Readonly || got.Color != "red" {
		t.Errorf("after Update: %+v", got)
	}

	if _, err := u.Update(ctx, "nope", Patch{Name: &name}); !errors.Is(err,
		domain.ErrNotFound) {
		t.Errorf("Update of a missing environment = %v, want ErrNotFound", err)
	}

	if _, err := u.Delete(ctx, env.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	state, err = u.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Environments) != 0 || state.ActiveID != "" {
		t.Errorf("after Delete: %+v, want no environments and no active one", state)
	}
}

func TestVariablesAndVarValueKeepsSecrets(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	_, env := seed(t, u, "Local")

	state, err := u.AddVariable(ctx, domain.EnvScope{Environment: env.ID},
		VariableDraft{Kind: domain.VariableSecret})
	if err != nil {
		t.Fatalf("AddVariable: %v", err)
	}
	v := state.Environments[0].Vars[0]

	state, err = u.UpdateVariable(ctx, domain.EnvScope{Environment: env.ID}, VariablePatch{
		ID: v.ID, Name: "token", Kind: domain.VariableSecret, Enabled: true, Value: "s3cret",
		SetValue: true,
	})
	if err != nil {
		t.Fatalf("UpdateVariable: %v", err)
	}
	v = state.Environments[0].Vars[0]
	if v.Name != "token" || v.Value != "" || !v.HasValue {
		t.Errorf("a secret came back as %+v, want a name, no value and hasValue", v)
	}

	// Renaming a secret without sending its value back keeps the stored one.
	state, err = u.UpdateVariable(ctx, domain.EnvScope{Environment: env.ID}, VariablePatch{
		ID: v.ID, Name: "api_token", Kind: domain.VariableSecret, Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpdateVariable (rename): %v", err)
	}
	if value, err := u.Reveal(ctx, v.ID); err != nil || value != "s3cret" {
		t.Errorf("after a rename: Reveal = %q, %v; want the value kept", value, err)
	}
	if state.Environments[0].Vars[0].Name != "api_token" {
		t.Errorf("the rename did not take: %+v", state.Environments[0].Vars[0])
	}

	// A name the grammar would never resolve is refused here, not silently stored.
	if _, err := u.UpdateVariable(ctx, domain.EnvScope{Environment: env.ID}, VariablePatch{
		ID: v.ID, Name: "not a name", Kind: domain.VariableText, Enabled: true, SetValue: true,
	}); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("a bad name = %v, want ErrNotAllowed", err)
	}

	if _, err := u.RemoveVariable(ctx, domain.EnvScope{Environment: env.ID}, v.ID); err != nil {
		t.Fatalf("RemoveVariable: %v", err)
	}
	state, _ = u.Snapshot(ctx)
	if len(state.Environments[0].Vars) != 0 {
		t.Errorf("the variable is still there: %+v", state.Environments[0].Vars)
	}
}

func TestResolutionOrderAndMasking(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	_, env := seed(t, u, "Local")

	if _, err := u.AddVariable(ctx, domain.EnvScope{},
		VariableDraft{Kind: domain.VariableText}); err != nil {
		t.Fatalf("AddVariable (globals): %v", err)
	}
	if _, err := u.AddVariable(ctx, domain.EnvScope{Environment: env.ID},
		VariableDraft{Kind: domain.VariableText}); err != nil {
		t.Fatalf("AddVariable: %v", err)
	}
	if _, err := u.AddVariable(ctx, domain.EnvScope{Environment: env.ID},
		VariableDraft{Kind: domain.VariableSecret}); err != nil {
		t.Fatalf("AddVariable: %v", err)
	}

	state, _ := u.Snapshot(ctx)
	global := state.Globals[0]
	over := state.Environments[0].Vars[0]
	secret := state.Environments[0].Vars[1]

	fill := func(scope domain.EnvScope, v domain.Variable, name, value string,
		kind domain.VariableKind) {
		t.Helper()
		if _, err := u.UpdateVariable(ctx, scope, VariablePatch{
			ID: v.ID, Name: name, Kind: kind, Enabled: true, Value: value, SetValue: true,
		}); err != nil {
			t.Fatalf("UpdateVariable(%s): %v", name, err)
		}
	}
	fill(domain.EnvScope{}, global, "token", "global-value", domain.VariableText)
	fill(domain.EnvScope{Environment: env.ID}, over, "token", "env-value", domain.VariableText)
	fill(domain.EnvScope{Environment: env.ID}, secret, "secret", "s3cret", domain.VariableSecret)

	// The environment wins over the globals: the order the design names is
	// Request → Environment → Globals.
	resolved, err := u.SubstituteTexts(ctx, nil, []string{"{{token}}"}, false, "")
	if err != nil {
		t.Fatalf("SubstituteTexts: %v", err)
	}
	if resolved[0] != "env-value" {
		t.Errorf("token resolved to %q, want the environment's value", resolved[0])
	}

	// Substitution: the request gets the values, everything that outlives it gets the mask — and a
	// secret is the difference between the two.
	sent, err := u.SubstituteTexts(ctx, nil, []string{"{{token}}/{{secret}}"}, false, "")
	if err != nil {
		t.Fatalf("SubstituteTexts: %v", err)
	}
	if sent[0] != "env-value/s3cret" {
		t.Errorf("for sending = %q, want the real values", sent[0])
	}
	masked, err := u.SubstituteTexts(ctx, nil, []string{"{{token}}/{{secret}}"}, true, "")
	if err != nil {
		t.Fatalf("SubstituteTexts (masked): %v", err)
	}
	if masked[0] != "env-value/"+domain.SecretMask {
		t.Errorf("masked = %q, want the secret replaced by the mask", masked[0])
	}

	// Several texts at once, and a name that repeats across them is one thing missing.
	missing, err := u.Missing(ctx, nil, []string{"{{token}}/{{nope}}", "{{nope}}/{{other}}"}, "")
	if err != nil {
		t.Fatalf("Missing: %v", err)
	}
	if len(missing) != 2 || missing[0] != "nope" || missing[1] != "other" {
		t.Errorf("Missing = %v, want nope once and then other", missing)
	}

	// A disabled variable stops resolving, which is what the enabled flag is for.
	if _, err := u.UpdateVariable(ctx, domain.EnvScope{Environment: env.ID}, VariablePatch{
		ID: over.ID, Name: "token", Kind: domain.VariableText, Enabled: false, Value: "env-value",
		SetValue: true,
	}); err != nil {
		t.Fatalf("UpdateVariable (disable): %v", err)
	}
	again, err := u.SubstituteTexts(ctx, nil, []string{"{{token}}"}, false, "")
	if err != nil {
		t.Fatalf("SubstituteTexts: %v", err)
	}
	if again[0] != "global-value" {
		t.Errorf("with the override disabled, token = %q, want the global", again[0])
	}
}

// A request a pre-request script rewrote reaches the record already filled in: its `{{tokens}}` are
// gone, and the value that replaced one is all that is left of the secret. Masking such a text
// cannot be a walk over tokens — it has none — so a value that is still a value is hidden wherever
// it stands. This is the text the record, the export and the window are made of.
func TestAMaskedTextHidesASecretThatHasNoTokenLeft(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	_, env := seed(t, u, "Local")

	if _, err := u.AddVariable(ctx, domain.EnvScope{Environment: env.ID},
		VariableDraft{Kind: domain.VariableSecret}); err != nil {
		t.Fatalf("AddVariable: %v", err)
	}
	state, err := u.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	secret := state.Environments[0].Vars[0]
	const value = "s3cret-token-value"
	if _, err := u.UpdateVariable(ctx, domain.EnvScope{Environment: env.ID}, VariablePatch{
		ID: secret.ID, Name: "token", Kind: domain.VariableSecret, Enabled: true,
		Value: value, SetValue: true,
	}); err != nil {
		t.Fatalf("UpdateVariable: %v", err)
	}

	texts := []string{"https://api.example.com/a?t=" + value, "Bearer " + value}
	masked, err := u.SubstituteTexts(ctx, nil, texts, true, "")
	if err != nil {
		t.Fatalf("SubstituteTexts (masked): %v", err)
	}
	for i, want := range []string{
		"https://api.example.com/a?t=" + domain.SecretMask,
		"Bearer " + domain.SecretMask,
	} {
		if masked[i] != want {
			t.Errorf("masked[%d] = %q, want %q", i, masked[i], want)
		}
	}

	// The send path is the other half, and it is untouched: the request goes out with the value.
	sent, err := u.SubstituteTexts(ctx, nil, texts, false, "")
	if err != nil {
		t.Fatalf("SubstituteTexts: %v", err)
	}
	if sent[0] != texts[0] || sent[1] != texts[1] {
		t.Errorf("for sending = %q, want the values left as they are", sent)
	}
}

// A value too short to be a credential is not searched for. The search replaces plain text written
// for other reasons, and hiding a character or two would leave a record describing a request nobody
// wrote — the worse of the two answers.
func TestAShortSecretValueIsNotSearchedFor(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	_, env := seed(t, u, "Local")

	if _, err := u.AddVariable(ctx, domain.EnvScope{Environment: env.ID},
		VariableDraft{Kind: domain.VariableSecret}); err != nil {
		t.Fatalf("AddVariable: %v", err)
	}
	state, err := u.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	secret := state.Environments[0].Vars[0]
	if _, err := u.UpdateVariable(ctx, domain.EnvScope{Environment: env.ID}, VariablePatch{
		ID: secret.ID, Name: "token", Kind: domain.VariableSecret, Enabled: true,
		Value: "ab", SetValue: true,
	}); err != nil {
		t.Fatalf("UpdateVariable: %v", err)
	}

	masked, err := u.SubstituteTexts(ctx, nil, []string{"https://api.example.com/ab/cab"}, true, "")
	if err != nil {
		t.Fatalf("SubstituteTexts (masked): %v", err)
	}
	if masked[0] != "https://api.example.com/ab/cab" {
		t.Errorf("masked = %q, want the text left alone", masked[0])
	}
}

// A request can pin itself to an environment apart from the window's, and when it does, that pin
// is what "Environment" means for it in the resolution order — not whatever the window is on.
func TestAPinnedEnvironmentOverridesTheWindows(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()

	_, local := seed(t, u, "Local")
	_, prod := seed(t, u, "Prod") // Create activates what it makes, so the window is on Prod now.

	setHost := func(envID, value string) {
		t.Helper()
		if _, err := u.AddVariable(ctx, domain.EnvScope{Environment: envID},
			VariableDraft{Kind: domain.VariableText}); err != nil {
			t.Fatalf("AddVariable: %v", err)
		}
		state, err := u.Snapshot(ctx)
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		var added domain.Variable
		for _, e := range state.Environments {
			if e.ID == envID {
				added = e.Vars[len(e.Vars)-1]
			}
		}
		if _, err := u.UpdateVariable(ctx, domain.EnvScope{Environment: envID}, VariablePatch{
			ID: added.ID, Name: "host", Kind: domain.VariableText, Enabled: true, Value: value,
			SetValue: true,
		}); err != nil {
			t.Fatalf("UpdateVariable: %v", err)
		}
	}
	setHost(local.ID, "local-host")
	setHost(prod.ID, "prod-host")

	// The window is on Prod, but the request is pinned to Local.
	pinned, err := u.SubstituteTexts(ctx, nil, []string{"{{host}}"}, false, local.ID)
	if err != nil {
		t.Fatalf("SubstituteTexts: %v", err)
	}
	if pinned[0] != "local-host" {
		t.Errorf("pinned = %q, want Local's value despite the window being on Prod", pinned[0])
	}

	// Nothing pinned still means the window's own.
	unpinned, err := u.SubstituteTexts(ctx, nil, []string{"{{host}}"}, false, "")
	if err != nil {
		t.Fatalf("SubstituteTexts: %v", err)
	}
	if unpinned[0] != "prod-host" {
		t.Errorf("unpinned = %q, want the window's own environment", unpinned[0])
	}

	// A pin that answers to nothing — its environment deleted — reads as no environment at all,
	// the way a stale ActiveID always has, rather than quietly falling back to the window's.
	stale, err := u.Missing(ctx, nil, []string{"{{host}}"}, "not-a-real-id")
	if err != nil {
		t.Fatalf("Missing: %v", err)
	}
	if len(stale) != 1 || stale[0] != "host" {
		t.Errorf("Missing(stale pin) = %v, want host missing", stale)
	}
}

// The window's own levels stand over a collection's: a variable a person wrote in an environment
// is the answer for every request, and a name a collection brought along in an import must not take
// its place. The collection still answers the names no other level holds, and a row switched off
// answers nothing — the same rule the environment's own rows follow.
func TestEnvironmentVariablesStandOverTheCollections(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	_, env := seed(t, u, "Local")

	if _, err := u.AddVariable(ctx, domain.EnvScope{Environment: env.ID},
		VariableDraft{Kind: domain.VariableText}); err != nil {
		t.Fatalf("AddVariable: %v", err)
	}
	if _, err := u.AddVariable(ctx, domain.EnvScope{Environment: env.ID},
		VariableDraft{Kind: domain.VariableText}); err != nil {
		t.Fatalf("AddVariable: %v", err)
	}

	snapshot, err := u.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	envURL, envPage := snapshot.Environments[0].Vars[0], snapshot.Environments[0].Vars[1]
	set := func(v domain.Variable, name, value string) {
		t.Helper()
		if _, err := u.UpdateVariable(ctx, domain.EnvScope{Environment: env.ID}, VariablePatch{
			ID: v.ID, Name: name, Kind: domain.VariableText, Enabled: true, Value: value,
			SetValue: true,
		}); err != nil {
			t.Fatalf("UpdateVariable(%s): %v", name, err)
		}
	}
	set(envURL, "baseUrl", "https://env")
	set(envPage, "page", "3")

	above := []domain.Variable{
		{Name: "baseUrl", Value: "https://collection", Kind: domain.VariableText, Enabled: true},
		{Name: "tenant", Value: "example", Kind: domain.VariableText, Enabled: true},
	}

	resolved, err := u.SubstituteTexts(ctx, above,
		[]string{"{{baseUrl}}/{{page}}/{{tenant}}"}, false, "")
	if err != nil {
		t.Fatalf("SubstituteTexts: %v", err)
	}
	if resolved[0] != "https://env/3/example" {
		t.Errorf("resolved = %q, want the environment's base over the collection's, and the "+
			"collection's own tenant", resolved[0])
	}

	// The collection's own row switched off is a level that answered nothing: a name no other level
	// holds resolves to nothing at all.
	above[1].Enabled = false
	off, err := u.SubstituteTexts(ctx, above, []string{"{{tenant}}"}, false, "")
	if err != nil {
		t.Fatalf("SubstituteTexts(disabled): %v", err)
	}
	if off[0] != "" {
		t.Errorf("resolved = %q, want the switched-off collection's tenant to answer nothing", off[0])
	}

	// And what a request in no collection asks is unchanged: no levels above it, no answers.
	none, err := u.SubstituteTexts(ctx, nil, []string{"{{baseUrl}}"}, false, "")
	if err != nil {
		t.Fatalf("SubstituteTexts(none): %v", err)
	}
	if none[0] != "https://env" {
		t.Errorf("resolved = %q, want the environment's base", none[0])
	}
}

func TestImportEntriesMergesByName(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	_, env := seed(t, u, "Local")
	scope := domain.EnvScope{Environment: env.ID}

	if _, err := u.ImportEntries(ctx, scope, []dotenv.Entry{
		{Name: "base_url", Value: "https://one.example.com"},
		{Name: "api_token", Value: "t1", Secret: true},
	}); err != nil {
		t.Fatalf("ImportEntries: %v", err)
	}
	state, err := u.ImportEntries(ctx, scope, []dotenv.Entry{
		{Name: "base_url", Value: "https://two.example.com"},
		{Name: "page_size", Value: "10"},
	})
	if err != nil {
		t.Fatalf("ImportEntries (second): %v", err)
	}

	vars := state.Environments[0].Vars
	if len(vars) != 3 {
		t.Fatalf("vars = %d, want three after a merge", len(vars))
	}
	byName := map[string]domain.Variable{}
	for _, v := range vars {
		byName[v.Name] = v
	}
	if byName["base_url"].Value != "https://two.example.com" {
		t.Errorf("base_url = %q, want the imported value to replace the old one",
			byName["base_url"].Value)
	}
	if byName["api_token"].Kind != domain.VariableSecret {
		t.Errorf("the secret lost its kind: %+v", byName["api_token"])
	}
	if byName["page_size"].Value != "10" {
		t.Errorf("the new variable was not added: %+v", byName["page_size"])
	}
}

func TestEnsureDefaultsSeedsOnlyAnEmptyState(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()

	state, err := u.EnsureDefaults(ctx)
	if err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	if len(state.Environments) != 1 || state.Environments[0].Name != "Local · dev" {
		t.Fatalf("state = %+v, want the seed environment", state.Environments)
	}
	if vars := state.Environments[0].Vars; len(vars) != 1 || vars[0].Name != "baseUrl" {
		t.Errorf("vars = %+v, want a baseUrl to start from", vars)
	}
	if state.ActiveID != state.Environments[0].ID {
		t.Errorf("activeId = %q, want the seeded environment", state.ActiveID)
	}

	// A second call leaves the user's own setup alone.
	if _, err := u.Create(ctx, Draft{Name: "Prod"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	state, err = u.EnsureDefaults(ctx)
	if err != nil {
		t.Fatalf("EnsureDefaults (again): %v", err)
	}
	if len(state.Environments) != 2 {
		t.Errorf("environments = %d, want the two that exist already", len(state.Environments))
	}
}

// A space of the account that this device has not read yet looks empty and is not: its environments
// are on the server, and one made here would be a second beside them.
func TestEnsureDefaultsLeavesASpaceThatHasNotBeenPulled(t *testing.T) {
	u, store := newUseCase(t)
	store.awaiting = true

	state, err := u.EnsureDefaults(context.Background())
	if err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	if len(state.Environments) != 0 {
		t.Errorf("environments = %+v, want none made before the first pull", state.Environments)
	}
}

// A space this person may only read is left as it is found: the app does not make an environment
// there, and entering the space is not made to fail over it.
func TestEnsureDefaultsLeavesASpaceThatCannotBeWrittenIn(t *testing.T) {
	u, store := newUseCase(t)
	store.readOnly = true

	state, err := u.EnsureDefaults(context.Background())
	if err != nil {
		t.Fatalf("EnsureDefaults: %v", err)
	}
	if len(state.Environments) != 0 {
		t.Errorf("environments = %+v, want none made in a space nobody can write to",
			state.Environments)
	}
}

func ptr[T any](v T) *T { return &v }

// Renaming answers exactly as creating does: a name that is empty and one that is too long are two
// different sentences, and the window words them from the code that travels with the refusal. A
// bare sentinel would leave it with the generic "not allowed" for both.
func TestRenameRefusesTheSameNamesCreateDoes(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()

	_, env := seed(t, u, "Local")
	blank := "   "
	long := strings.Repeat("x", maxNameLength+1)

	for _, tc := range []struct {
		name  string
		patch Patch
		want  domain.Code
	}{
		{"blank", Patch{Name: &blank}, domain.CodeNameEmpty},
		{"too long", Patch{Name: &long}, domain.CodeNameTooLong},
	} {
		_, err := u.Update(ctx, env.ID, tc.patch)
		if !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("Update with a %s name = %v, want domain.ErrNotAllowed", tc.name, err)
			continue
		}
		if got := domain.CodeOf(err); got != tc.want {
			t.Errorf("Update with a %s name = %q, want %q", tc.name, got, tc.want)
		}
	}

	// And the created case still says what it always said.
	if _, err := u.Create(ctx, Draft{}); domain.CodeOf(err) != domain.CodeNameEmpty {
		t.Errorf("Create(%q) = %q, want %q", "", domain.CodeOf(err), domain.CodeNameEmpty)
	}
}

// A draft carries the colour the environment is drawn with. The create form asks for the name and
// the colour together, and patching what was just made would be a second step the form never shows.
func TestCreateTakesItsColour(t *testing.T) {
	u, _ := newUseCase(t)

	state, err := u.Create(context.Background(), Draft{Name: "Stage", Color: "purple"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := state.Environments[0].Color; got != "purple" {
		t.Errorf("color = %q, want the colour the draft named", got)
	}
	if state.ActiveID != state.Environments[0].ID {
		t.Errorf("activeId = %q, want the environment just made", state.ActiveID)
	}
}

// A copy is the keys a new environment needs and not the values another one holds: the names, the
// kinds and the order come over, and a secret's value does not.
func TestCreateCopiesTheVariablesOfItsBase(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	base := baseWithVars(t, u)

	state, err := u.Create(ctx, Draft{Name: "Stage", StartFrom: base.ID})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	made := state.Environments[len(state.Environments)-1]
	if made.ID == base.ID {
		t.Fatal("the new environment is the base itself")
	}
	if made.ID != state.ID {
		t.Errorf("created id = %q, want the environment it names %q", state.ID, made.ID)
	}
	if len(made.Vars) != 3 {
		t.Fatalf("vars = %d, want the base's three", len(made.Vars))
	}

	names := make([]string, 0, len(made.Vars))
	for i, v := range made.Vars {
		names = append(names, v.Name)
		if v.ID == base.Vars[i].ID {
			t.Errorf("%s kept the base's id, want a variable of this environment's own", v.Name)
		}
		if v.Position != i+1 {
			t.Errorf("%s is at %d, want the base's order kept", v.Name, v.Position)
		}
	}
	if got := strings.Join(names, ", "); got != "baseUrl, token, pageSize" {
		t.Errorf("names = %s, want the base's own list in its own order", got)
	}
	if made.Vars[0].Value != "https://api.example.com" {
		t.Errorf("baseUrl = %q, want an ordinary value copied as it was", made.Vars[0].Value)
	}
}

// The one value a copy does not carry. What a base is for is the list of keys an environment needs;
// a token that two environments silently share is a token nobody remembers changing in both.
func TestACopiedSecretArrivesEmpty(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	base := baseWithVars(t, u)

	state, err := u.Create(ctx, Draft{Name: "Stage", StartFrom: base.ID})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	copied := state.Environments[len(state.Environments)-1].Vars[1]
	if copied.Name != "token" || copied.Kind != domain.VariableSecret {
		t.Fatalf("copied = %+v, want the base's secret, still a secret", copied)
	}
	if copied.Value != "" || copied.HasValue {
		t.Errorf("copied secret = %q (hasValue %v), want it empty", copied.Value, copied.HasValue)
	}

	// And the base still holds its own: the copy took nothing from it.
	revealed, err := u.Reveal(ctx, base.Vars[1].ID)
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if revealed != "s3cret" {
		t.Errorf("the base's secret = %q, want it untouched", revealed)
	}
}

// A base nobody can find is refused before anything is written: a draft that names an environment
// and gets an empty one made anyway is a draft whose form lied about what it did.
func TestCreateRefusesABaseThatIsNotThere(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	seed(t, u, "Local")

	if _, err := u.Create(ctx, Draft{Name: "Stage", StartFrom: "not-found"}); !errors.Is(
		err, domain.ErrNotFound) {
		t.Errorf("Create with an unknown base = %v, want domain.ErrNotFound", err)
	}
	state, err := u.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Environments) != 1 {
		t.Errorf("environments = %d, want nothing made from a base that is not there",
			len(state.Environments))
	}
}

// An environment made from nothing is empty and not a copy of the globals: the two scopes are told
// apart by an empty id, and a branch that read the wrong one would fill every new environment with
// what is only meant to apply everywhere.
func TestCreateWithNoBaseHasNoVariables(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()
	if _, err := u.AddVariable(ctx, domain.EnvScope{}, VariableDraft{
		Name: "locale", Kind: domain.VariableText, Value: "en-US",
	}); err != nil {
		t.Fatalf("AddVariable: %v", err)
	}

	state, err := u.Create(ctx, Draft{Name: "Stage"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := state.Environments[0].Vars; len(got) != 0 {
		t.Errorf("vars = %+v, want none in an environment made from nothing", got)
	}
}

// baseWithVars is the setup the copy tests share: an environment holding the three keys a copy is
// about — an ordinary value, a secret with one, and a second ordinary value.
func baseWithVars(t *testing.T, u *UseCase) domain.Environment {
	t.Helper()
	ctx := context.Background()
	_, base := seed(t, u, "Local")

	for _, draft := range []VariableDraft{
		{Name: "baseUrl", Kind: domain.VariableText, Value: "https://api.example.com"},
		{Name: "token", Kind: domain.VariableSecret, Value: "s3cret"},
		{Name: "pageSize", Kind: domain.VariableText, Value: "25"},
	} {
		if _, err := u.AddVariable(ctx, domain.EnvScope{Environment: base.ID}, draft); err != nil {
			t.Fatalf("AddVariable(%s): %v", draft.Name, err)
		}
	}

	state, err := u.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return state.Environments[0]
}

// Masking a text for a named space reads that space's values rather than the ones the window is
// on: the MCP server answers about spaces that are not on screen, and a mask made of the wrong
// space's secrets would be no mask at all.
func TestMaskingReadsTheSpaceItWasAskedAbout(t *testing.T) {
	u, store := newUseCase(t)
	ctx := context.Background()
	_, env := seed(t, u, "Local")
	if _, err := u.AddVariable(ctx, domain.EnvScope{Environment: env.ID}, VariableDraft{
		Name: "token", Kind: domain.VariableSecret, Value: "s3cret-value",
	}); err != nil {
		t.Fatalf("AddVariable: %v", err)
	}

	out, err := u.MaskTextsIn(ctx, "space-b", nil, []string{"Bearer s3cret-value"}, env.ID)
	if err != nil {
		t.Fatalf("MaskTextsIn: %v", err)
	}
	if want := "Bearer " + domain.SecretMask; len(out) != 1 || out[0] != want {
		t.Errorf("masked = %q, want the secret left as the app's mask", out)
	}
	if len(store.asked) == 0 || store.asked[len(store.asked)-1] != "space-b" {
		t.Errorf("the store was asked for %q, want the space the caller named", store.asked)
	}
}

// A name another environment of the space wears is refused when creating and when renaming, and an
// environment keeping its own name is not a collision with itself.
func TestAnEnvironmentTakesNoNameAnotherOneWears(t *testing.T) {
	u, _ := newUseCase(t)
	ctx := context.Background()

	seed(t, u, "Local")
	_, prod := seed(t, u, "Prod")

	_, err := u.Create(ctx, Draft{Name: "Local"})
	if !errors.Is(err, domain.ErrConflict) || domain.CodeOf(err) != domain.CodeNameTaken {
		t.Errorf("Create(%q) = %v, want the refusal %q", "Local", err, domain.CodeNameTaken)
	}
	taken := "Local"
	_, err = u.Update(ctx, prod.ID, Patch{Name: &taken})
	if domain.CodeOf(err) != domain.CodeNameTaken {
		t.Errorf("renaming to %q = %v, want the refusal %q", taken, err, domain.CodeNameTaken)
	}
	own := "Prod"
	if _, err := u.Update(ctx, prod.ID, Patch{Name: &own}); err != nil {
		t.Errorf("renaming to its own name = %v, want it accepted", err)
	}
}
