package environment

import (
	"context"
	"fmt"
	"strings"

	"schemo/internal/domain"
	"schemo/internal/dotenv"
)

// Variable reads one variable by name in the scope a script names — the active environment, or the
// globals. It is the read behind `pm.environment.get` and `pm.globals.get`.
//
// Unlike the snapshot, it answers with the value, secrets included: a script that signs a request
// needs the token, and this value goes into the sandbox and nowhere else — not into the window, not
// into a log of ours. A script that prints it prints it itself, which is its own business.
func (u *UseCase) Variable(
	ctx context.Context,
	scope domain.VarScope,
	name string,
) (string, bool, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return "", false, err
	}
	state, err := u.store.EnvState(ctx, workspace)
	if err != nil {
		return "", false, err
	}

	switch scope {
	case domain.ScopeEnvironment:
		env, ok := findEnvironment(state, state.ActiveID)
		if !ok {
			// No environment is selected, and that is an answer rather than a mistake: a script
			// asking for a name finds nothing.
			return "", false, nil
		}
		return byName(env.Vars, name)
	case domain.ScopeGlobals:
		return byName(state.Globals, name)
	default:
		return "", false, fmt.Errorf("scope %q: %w", scope, domain.ErrNotAllowed)
	}
}

// SetVariable writes a variable by name, adding it when nobody has that name yet. Adding is the
// point: a script that records a token for the next request of a run must not have to know whether
// that variable exists, and the sheet is where names are looked at, not here.
func (u *UseCase) SetVariable(
	ctx context.Context,
	scope domain.VarScope,
	name string,
	value string,
) error {
	return u.write(ctx, scope, name, value, false)
}

// Capture writes a response value into a variable and returns the sheet's state. A secret stays
// secret when overwritten.
func (u *UseCase) Capture(
	ctx context.Context,
	scope domain.VarScope,
	name string,
	value string,
	secret bool,
) (domain.EnvState, error) {
	if err := u.write(ctx, scope, name, value, secret); err != nil {
		return domain.EnvState{}, err
	}
	return u.Snapshot(ctx)
}

// CaptureVariable writes a captured value, secret or not.
func (u *UseCase) CaptureVariable(
	ctx context.Context,
	scope domain.VarScope,
	name string,
	value string,
	secret bool,
) error {
	return u.write(ctx, scope, name, value, secret)
}

func (u *UseCase) write(
	ctx context.Context,
	scope domain.VarScope,
	name string,
	value string,
	secret bool,
) error {
	name = strings.TrimSpace(name)
	// A name no `{{token}}` can carry is a row nobody can use: the script would be writing into a
	// place the request it is running around cannot read.
	if name == "" || !validName(name) {
		return fmt.Errorf("variable name %q: %w", name, domain.ErrNotAllowed)
	}

	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}

	state, err := u.store.EnvState(ctx, workspace)
	if err != nil {
		return err
	}

	where := domain.EnvScope{}
	switch scope {
	case domain.ScopeEnvironment:
		if _, ok := findEnvironment(state, state.ActiveID); !ok {
			return domain.Refuse(domain.CodeNoEnvironment, domain.ErrNotAllowed, nil)
		}
		where.Environment = state.ActiveID
	case domain.ScopeGlobals:
	default:
		return fmt.Errorf("scope %q: %w", scope, domain.ErrNotAllowed)
	}

	if existing, ok := existing(scopeVariables(state, where), name); ok {
		existing.Value = value
		if secret {
			existing.Kind = domain.VariableSecret
		}
		return u.store.SaveVariable(ctx, workspace, where, existing)
	}

	position, err := nextVariablePosition(state, where)
	if err != nil {
		return err
	}
	kind := domain.VariableText
	if secret {
		kind = domain.VariableSecret
	}
	return u.store.SaveVariable(ctx, workspace, where, domain.Variable{
		ID:       u.ids(),
		Name:     name,
		Value:    value,
		Kind:     kind,
		Enabled:  true,
		Position: position,
	})
}

// byName is the answer a script's `get` gets, and it is the answer a `{{token}}` would get: a row
// that is switched off is off for both. A script reading a value the request itself would not carry
// is the switch meaning two different things.
func byName(variables []domain.Variable, name string) (string, bool, error) {
	for _, v := range variables {
		if v.Enabled && v.Name == name {
			return v.Value, true, nil
		}
	}
	return "", false, nil
}

func existing(variables []domain.Variable, name string) (domain.Variable, bool) {
	for _, v := range variables {
		if v.Name == name {
			return v, true
		}
	}
	return domain.Variable{}, false
}

// A secret leaves this side masked — see hideSecretValues.

// writable refuses a write into an environment the window has closed. `readonly` is the design's
// "Prod": the point of it is that nothing typed in the window lands inside by accident, and since
// the flag is a flag on a row rather than a rule the store knows, this is the only side that can
// hold that promise for every caller. The window's own controls check it too — a caller that
// forgot one would otherwise write into a closed environment in silence, which is what a button
// outside this window did.
//
// The globals scope is never closed: nothing about a name that applies everywhere is a value
// somebody marked as too dangerous to touch.
func writable(state domain.EnvState, scope domain.EnvScope) error {
	env, ok := findEnvironment(state, scope.Environment)
	if ok && env.Readonly {
		return domain.Refuse(domain.CodeEnvironmentReadOnly, domain.ErrNotAllowed, nil)
	}
	return nil
}

// AddVariable appends a variable to a scope.
func (u *UseCase) AddVariable(
	ctx context.Context,
	scope domain.EnvScope,
	draft VariableDraft,
) (domain.EnvState, error) {
	workspace, state, err := u.current(ctx)
	if err != nil {
		return domain.EnvState{}, err
	}
	if err := writable(state, scope); err != nil {
		return domain.EnvState{}, err
	}
	position, err := nextVariablePosition(state, scope)
	if err != nil {
		return domain.EnvState{}, err
	}

	name := strings.TrimSpace(draft.Name)
	if name != "" && !validName(name) {
		return domain.EnvState{}, fmt.Errorf("variable name: %w", domain.ErrNotAllowed)
	}
	if draft.Kind != domain.VariableSecret {
		draft.Kind = domain.VariableText
	}

	v := domain.Variable{
		ID:       u.ids(),
		Name:     name,
		Value:    draft.Value,
		Kind:     draft.Kind,
		Enabled:  true,
		Position: position,
	}
	if err := u.store.SaveVariable(ctx, workspace, scope, v); err != nil {
		return domain.EnvState{}, err
	}
	return u.snapshot(ctx, workspace)
}

func (u *UseCase) UpdateVariable(
	ctx context.Context,
	scope domain.EnvScope,
	patch VariablePatch,
) (domain.EnvState, error) {
	workspace, state, err := u.current(ctx)
	if err != nil {
		return domain.EnvState{}, err
	}
	if err := writable(state, scope); err != nil {
		return domain.EnvState{}, err
	}
	existing, ok := findVariable(state, scope, patch.ID)
	if !ok {
		return domain.EnvState{}, fmt.Errorf("variable %s: %w", patch.ID, domain.ErrNotFound)
	}

	name := strings.TrimSpace(patch.Name)
	if name != "" && !validName(name) {
		return domain.EnvState{}, fmt.Errorf("variable name: %w", domain.ErrNotAllowed)
	}

	value := existing.Value
	if patch.SetValue {
		value = patch.Value
	}
	kind := patch.Kind
	if kind != domain.VariableSecret {
		kind = domain.VariableText
	}

	updated := domain.Variable{
		ID:       patch.ID,
		Name:     name,
		Value:    value,
		Kind:     kind,
		Enabled:  patch.Enabled,
		Position: existing.Position,
	}
	if err := u.store.SaveVariable(ctx, workspace, scope, updated); err != nil {
		return domain.EnvState{}, err
	}
	return u.snapshot(ctx, workspace)
}

func (u *UseCase) RemoveVariable(
	ctx context.Context,
	scope domain.EnvScope,
	id string,
) (domain.EnvState, error) {
	workspace, state, err := u.current(ctx)
	if err != nil {
		return domain.EnvState{}, err
	}
	if err := writable(state, scope); err != nil {
		return domain.EnvState{}, err
	}
	if _, ok := findVariable(state, scope, id); !ok {
		return domain.EnvState{}, fmt.Errorf("variable %s: %w", id, domain.ErrNotFound)
	}
	if err := u.store.DeleteVariable(ctx, workspace, id); err != nil {
		return domain.EnvState{}, err
	}
	return u.snapshot(ctx, workspace)
}

// ImportEntries merges a parsed .env file into a scope: a name that is already there is replaced,
// one that is not is appended. It is the same rule the import dialog offers, without the question.
func (u *UseCase) ImportEntries(
	ctx context.Context,
	scope domain.EnvScope,
	entries []dotenv.Entry,
) (domain.EnvState, error) {
	workspace, state, err := u.current(ctx)
	if err != nil {
		return domain.EnvState{}, err
	}
	if err := writable(state, scope); err != nil {
		return domain.EnvState{}, err
	}
	position, err := nextVariablePosition(state, scope)
	if err != nil {
		return domain.EnvState{}, err
	}

	existing := map[string]domain.Variable{}
	for _, v := range scopeVariables(state, scope) {
		existing[v.Name] = v
	}

	for _, entry := range entries {
		kind := domain.VariableText
		if entry.Secret {
			kind = domain.VariableSecret
		}

		v, seen := existing[entry.Name]
		if seen {
			v.Value = entry.Value
			v.Kind = kind
			v.Enabled = true
		} else {
			v = domain.Variable{
				ID:       u.ids(),
				Name:     entry.Name,
				Value:    entry.Value,
				Kind:     kind,
				Enabled:  true,
				Position: position,
			}
			position++
		}
		if err := u.store.SaveVariable(ctx, workspace, scope, v); err != nil {
			return domain.EnvState{}, err
		}
	}
	return u.snapshot(ctx, workspace)
}

// Reveal is the deliberate "show me" behind the sheet's eye button, and the only way a secret's
// value leaves the database.
func (u *UseCase) Reveal(ctx context.Context, id string) (string, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return "", err
	}
	return u.store.VariableValue(ctx, workspace, id)
}

// scopeVariables lists what a scope holds right now; an empty scope is the globals.
func scopeVariables(state domain.EnvState, scope domain.EnvScope) []domain.Variable {
	if scope.Environment == "" {
		return state.Globals
	}
	for _, env := range state.Environments {
		if env.ID == scope.Environment {
			return env.Vars
		}
	}
	return nil
}

func nextVariablePosition(state domain.EnvState, scope domain.EnvScope) (int, error) {
	if scope.Environment != "" {
		if _, ok := findEnvironment(state, scope.Environment); !ok {
			return 0, fmt.Errorf("environment %s: %w", scope.Environment, domain.ErrNotFound)
		}
	}
	return nextPosition(scopeVariables(state, scope),
		func(v domain.Variable) int { return v.Position }), nil
}

// nextPosition is one past the highest place anything in a list stands in. Positions are ordered
// rather than indexed, and a new row goes after the last one rather than into the first gap — which
// is what makes the row the user just added the last one they see.
func nextPosition[T any](items []T, position func(T) int) int {
	out := 0
	for _, item := range items {
		if at := position(item); at >= out {
			out = at + 1
		}
	}
	return out
}

// hideSecretValues copies the state without the secrets' values. The variables themselves stay: the
// sheet lists them, shows their kind and needs their ids to reveal one.
//
// The parameter is named list rather than vars because `vars` is the package that checks a name,
// and a parameter of that name would shadow it inside the one function that might want it.
func hideSecretValues(state domain.EnvState) domain.EnvState {
	hide := func(list []domain.Variable) []domain.Variable {
		out := make([]domain.Variable, len(list))
		for i, v := range list {
			if v.Kind == domain.VariableSecret {
				v.HasValue = v.Value != ""
				v.Value = ""
			}
			out[i] = v
		}
		return out
	}

	for i := range state.Environments {
		state.Environments[i].Vars = hide(state.Environments[i].Vars)
	}
	state.Globals = hide(state.Globals)
	return state
}

// ClearGlobals empties them. The globals cannot be deleted the way an environment can — there is
// one globals scope and no second one to fall back to — so the only destructive thing left is to
// take out what is in them.
//
// One statement per variable rather than a scope-wide delete: the ids come from a state read in
// this same call, so there is nothing to look up, and a clear that stops halfway leaves fewer
// globals than were there rather than a wrong set of them — running it again finishes the job.
func (u *UseCase) ClearGlobals(ctx context.Context) (domain.EnvState, error) {
	workspace, state, err := u.current(ctx)
	if err != nil {
		return domain.EnvState{}, err
	}
	for _, v := range state.Globals {
		if err := u.store.DeleteVariable(ctx, workspace, v.ID); err != nil {
			return domain.EnvState{}, err
		}
	}
	return u.snapshot(ctx, workspace)
}
