// Package environment owns the environments and the variables that resolve against them: what a
// request interpolates, in which scope, and which of the values are secrets.
package environment

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// maxNameLength is the design's limit for an environment's name: long enough for "Prod · EU-West",
// short enough to stay one chip in the titlebar.
const maxNameLength = 40

type UseCase struct {
	store Store
	scope domain.Scope
	ids   platform.IDGen
}

func NewUseCase(store Store, scope domain.Scope, ids platform.IDGen) *UseCase {
	return &UseCase{store: store, scope: scope, ids: ids}
}

// Snapshot is the whole screen. Secret values are withheld: a variable that has one says so, and
// Reveal is what shows it.
//
// The workspace is resolved once, at the door, and carried as a value from there on: an operation
// that read the pointer again halfway through would write half of itself into the workspace the
// user has just switched away from.
func (u *UseCase) Snapshot(ctx context.Context) (domain.EnvState, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.EnvState{}, err
	}
	return u.snapshot(ctx, workspace)
}

func (u *UseCase) snapshot(ctx context.Context, workspace string) (domain.EnvState, error) {
	state, err := u.store.EnvState(ctx, workspace)
	if err != nil {
		return domain.EnvState{}, err
	}
	return hideSecretValues(state), nil
}

// Create adds an environment at the end of the list. It becomes the active one: a new environment
// is being set up, and editing the variables of one you are not in is how mistakes happen.
//
// A draft that names a base is filled from it — the same keys, kinds and order — so that a stage
// environment starts as a copy of dev rather than as a blank page. A secret is copied without its
// value: what a copy is for is the set of keys an environment needs, and a value that two
// environments silently share is a value nobody remembers changing in both.
func (u *UseCase) Create(ctx context.Context, draft Draft) (Created, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return Created{}, err
	}

	name, err := validEnvironmentName(draft.Name)
	if err != nil {
		return Created{}, err
	}

	current, err := u.store.EnvState(ctx, workspace)
	if err != nil {
		return Created{}, err
	}
	if nameTaken(current.Environments, "", name) {
		return Created{}, domain.NameTaken()
	}

	base := domain.Environment{}
	if draft.StartFrom != "" {
		found, ok := findEnvironment(current, draft.StartFrom)
		if !ok {
			return Created{}, fmt.Errorf("environment %s: %w", draft.StartFrom,
				domain.ErrNotFound)
		}
		base = found
	}

	position := nextPosition(current.Environments,
		func(env domain.Environment) int { return env.Position })
	env := domain.Environment{ID: u.ids(), Name: name, Color: draft.Color, Position: position}
	if err := u.store.SaveEnvironment(ctx, workspace, env); err != nil {
		return Created{}, err
	}
	if err := u.store.SetActiveEnvironment(ctx, workspace, env.ID); err != nil {
		return Created{}, err
	}

	// The copies are numbered from one up, in the order the base lists them — which is the base's own
	// order, by position — exactly as a seeded environment numbers its first variable. Asking the
	// store for "the next position" would answer from a state read before this environment existed
	// and hand every copy the same row number.
	//
	// A database error halfway through the copy leaves a half-filled environment on the next read
	// rather than a state that cannot exist. Filling it again is what the user would do about it, and
	// the copy's names cannot collide — the base's are unique within a scope and the new scope is
	// empty.
	scope := domain.EnvScope{Environment: env.ID}
	for i, v := range base.Vars {
		if err := u.store.SaveVariable(ctx, workspace, scope,
			copiedVariable(v, u.ids(), i+1)); err != nil {
			return Created{}, err
		}
	}
	state, err := u.snapshot(ctx, workspace)
	if err != nil {
		return Created{}, err
	}
	return Created{EnvState: state, ID: env.ID}, nil
}

// Import writes an environment a file described, variables and all, in one call — a file is one
// thing. Nothing is activated: importing is not switching to. A name the space already holds is
// numbered beside it rather than refused, because a file arriving under a taken name is not the
// person's mistake.
func (u *UseCase) Import(ctx context.Context, draft domain.EnvironmentDraft) (Created, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return Created{}, err
	}
	name, err := importName(draft.Name)
	if err != nil {
		return Created{}, err
	}

	current, err := u.store.EnvState(ctx, workspace)
	if err != nil {
		return Created{}, err
	}
	env := domain.Environment{
		ID:    u.ids(),
		Name:  freeName(name, current.Environments),
		Color: draft.Color,
		Position: nextPosition(current.Environments,
			func(e domain.Environment) int { return e.Position }),
	}
	if err := u.store.SaveEnvironment(ctx, workspace, env); err != nil {
		return Created{}, err
	}

	scope := domain.EnvScope{Environment: env.ID}
	for _, v := range importedVariables(u.ids, draft.Variables) {
		if err := u.store.SaveVariable(ctx, workspace, scope, v); err != nil {
			return Created{}, err
		}
	}
	state, err := u.snapshot(ctx, workspace)
	if err != nil {
		return Created{}, err
	}
	return Created{EnvState: state, ID: env.ID}, nil
}

// importName keeps a file's name inside the ceiling rather than refusing it: a name longer than a
// person would type is the file's own doing.
func importName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", domain.Refuse(domain.CodeNameEmpty, domain.ErrNotAllowed, nil)
	}
	runes := []rune(name)
	if len(runes) > maxNameLength {
		return string(runes[:maxNameLength]), nil
	}
	return name, nil
}

// freeName is the name the environment is written under: the file's own, numbered beside it when
// the space already holds one — two environments under one name are two things nobody can tell
// apart in a list.
func freeName(name string, taken []domain.Environment) string {
	held := map[string]bool{}
	for _, env := range taken {
		held[strings.ToLower(env.Name)] = true
	}
	if !held[strings.ToLower(name)] {
		return name
	}
	for number := 2; ; number++ {
		candidate := numbered(name, number)
		if !held[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

// numbered is the name with its number beside it, kept inside the ceiling: a name already at the
// ceiling has no room left, and the number is not the part to drop.
func numbered(name string, number int) string {
	suffix := fmt.Sprintf(" %d", number)
	runes := []rune(name)
	if room := maxNameLength - len([]rune(suffix)); room > 0 && len(runes) > room {
		runes = runes[:room]
	}
	return string(runes) + suffix
}

// importedVariables is what a file's variables become: ids are minted here, the order is the
// file's, and a name the file listed twice is written once with the last value — the way a file of
// exports reads — because one scope holds a name once.
func importedVariables(ids platform.IDGen, from []domain.Variable) []domain.Variable {
	out := []domain.Variable{}
	at := map[string]int{}
	for _, one := range from {
		name := strings.TrimSpace(one.Name)
		if name == "" {
			continue
		}
		if index, seen := at[name]; seen {
			out[index].Value = one.Value
			out[index].Kind = kindOrText(one.Kind)
			out[index].Enabled = one.Enabled
			continue
		}
		one.ID = ids()
		one.Name = name
		one.Kind = kindOrText(one.Kind)
		at[name] = len(out)
		out = append(out, one)
	}
	for index := range out {
		out[index].Position = index + 1
	}
	return out
}

func kindOrText(kind domain.VariableKind) domain.VariableKind {
	if kind == domain.VariableSecret {
		return domain.VariableSecret
	}
	return domain.VariableText
}

// copiedVariable is one variable of a base, reborn in a new environment: the same key, the same
// kind and the same place in the list. A secret arrives with an empty value; everything else
// arrives as it was — a baseUrl that differs only in the host is worth copying, a token is not.
func copiedVariable(v domain.Variable, id string, position int) domain.Variable {
	copied := domain.Variable{
		ID:       id,
		Name:     v.Name,
		Kind:     v.Kind,
		Enabled:  v.Enabled,
		Position: position,
	}
	if v.Kind != domain.VariableSecret {
		copied.Value = v.Value
	}
	return copied
}

// Update applies a patch. The readonly flag is what the design calls "Prod": it is the Access
// switch, and it closes the environment to every write the user could make — the check itself lives
// in the writers (see writable), and this is the one call that may still change an environment.
func (u *UseCase) Update(
	ctx context.Context,
	id string,
	patch Patch,
) (domain.EnvState, error) {
	workspace, state, err := u.current(ctx)
	if err != nil {
		return domain.EnvState{}, err
	}
	env, ok := findEnvironment(state, id)
	if !ok {
		return domain.EnvState{}, noEnvironment(id)
	}

	if patch.Name != nil {
		// Refused the way a new one is: an empty name and a name that is too long are two different
		// things to say, and a bare sentinel leaves the window nothing to say either of them with.
		name, err := validEnvironmentName(*patch.Name)
		if err != nil {
			return domain.EnvState{}, err
		}
		if nameTaken(state.Environments, id, name) {
			return domain.EnvState{}, domain.NameTaken()
		}
		env.Name = name
	}
	if patch.Color != nil {
		env.Color = *patch.Color
	}
	if patch.Readonly != nil {
		env.Readonly = *patch.Readonly
	}

	// Vars are written by their own calls; SaveEnvironment only needs the environment itself.
	env.Vars = nil
	if err := u.store.SaveEnvironment(ctx, workspace, env); err != nil {
		return domain.EnvState{}, err
	}
	return u.snapshot(ctx, workspace)
}

// Delete removes an environment and everything in it. If it was the active one, the selection
// falls back to no environment rather than to a sibling: resolving against an environment the user
// did not choose would be worse than resolving against nothing.
func (u *UseCase) Delete(ctx context.Context, id string) (domain.EnvState, error) {
	workspace, state, err := u.current(ctx)
	if err != nil {
		return domain.EnvState{}, err
	}
	if _, ok := findEnvironment(state, id); !ok {
		return domain.EnvState{}, noEnvironment(id)
	}
	if err := u.store.DeleteEnvironment(ctx, workspace, id); err != nil {
		return domain.EnvState{}, err
	}
	if state.ActiveID == id {
		if err := u.store.SetActiveEnvironment(ctx, workspace, ""); err != nil {
			return domain.EnvState{}, err
		}
	}
	return u.snapshot(ctx, workspace)
}

// Activate selects the environment the request preview resolves against.
func (u *UseCase) Activate(ctx context.Context, id string) (domain.EnvState, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.EnvState{}, err
	}
	if id != "" {
		state, err := u.store.EnvState(ctx, workspace)
		if err != nil {
			return domain.EnvState{}, err
		}
		if _, ok := findEnvironment(state, id); !ok {
			return domain.EnvState{}, noEnvironment(id)
		}
	}
	if err := u.store.SetActiveEnvironment(ctx, workspace, id); err != nil {
		return domain.EnvState{}, err
	}
	return u.snapshot(ctx, workspace)
}

// EnsureDefaults gives a workspace the one environment the app has always started with, so a space
// the user has just made has somewhere to type a base URL instead of an empty screen. It only ever
// acts on an empty state: as soon as anything exists, the user's own setup is the answer. A space
// of the account that has not been read yet looks empty and is not, so it is left for the pull.
func (u *UseCase) EnsureDefaults(ctx context.Context) (domain.EnvState, error) {
	workspace, state, err := u.current(ctx)
	if err != nil {
		return domain.EnvState{}, err
	}
	if len(state.Environments) > 0 || len(state.Globals) > 0 {
		return u.snapshot(ctx, workspace)
	}
	awaiting, err := u.store.AwaitsFirstPull(ctx, workspace)
	if err != nil {
		return domain.EnvState{}, err
	}
	if awaiting {
		return u.snapshot(ctx, workspace)
	}

	env := domain.Environment{ID: u.ids(), Name: "Local · dev", Position: 1}
	if err := u.store.SaveEnvironment(ctx, workspace, env); err != nil {
		// A space this person may only read is not given an environment of the app's making: it is
		// shared content, and an empty screen there is the server's emptiness.
		if domain.CodeOf(err) == domain.CodeWorkspaceReadOnly {
			return u.snapshot(ctx, workspace)
		}
		return domain.EnvState{}, err
	}
	baseURL := domain.Variable{
		ID:      u.ids(),
		Name:    domain.BaseURLVariable,
		Value:   "http://localhost:8000",
		Kind:    domain.VariableText,
		Enabled: true,
		// One-based to match the environment above: positions are ordered, not indexed.
		Position: 1,
	}
	if err := u.store.SaveVariable(ctx, workspace, domain.EnvScope{Environment: env.ID},
		baseURL); err != nil {
		return domain.EnvState{}, err
	}
	if err := u.store.SetActiveEnvironment(ctx, workspace, env.ID); err != nil {
		return domain.EnvState{}, err
	}
	return u.snapshot(ctx, workspace)
}

// current is the space the window is in and what it holds. Every call that looks into an
// environment or writes one starts here: the space is resolved once and then travels, so the answer
// is read from the space the call began in.
func (u *UseCase) current(ctx context.Context) (string, domain.EnvState, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return "", domain.EnvState{}, err
	}
	state, err := u.store.EnvState(ctx, workspace)
	return workspace, state, err
}

// noEnvironment is the answer to a call that names an environment this space does not hold.
func noEnvironment(id string) error {
	return fmt.Errorf("environment %s: %w", id, domain.ErrNotFound)
}

// nameTaken reports whether an environment other than `except` already wears the name.
func nameTaken(all []domain.Environment, except string, name string) bool {
	return slices.ContainsFunc(all, func(env domain.Environment) bool {
		return env.ID != except && env.Name == name
	})
}

func findEnvironment(state domain.EnvState, id string) (domain.Environment, bool) {
	for _, env := range state.Environments {
		if env.ID == id {
			return env, true
		}
	}
	return domain.Environment{}, false
}

// validEnvironmentName is a name an environment may carry, trimmed, or the refusal that says why it
// may not. Creating one and renaming one ask the same question, so they answer it the same way.
func validEnvironmentName(raw string) (string, error) {
	return domain.CleanName(raw, maxNameLength)
}
