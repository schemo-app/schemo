package workspace

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"schemo/internal/db"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// maxEnvironmentName is the longest environment name: long enough for "Prod · EU-West", short enough to
// stay one chip in the titlebar.
const maxEnvironmentName = 40

type Environments struct {
	store *db.Store
	scope domain.Scope
	ids   platform.IDGen
}

func NewEnvironments(store *db.Store, scope domain.Scope, ids platform.IDGen) *Environments {
	return &Environments{store: store, scope: scope, ids: ids}
}

// InWorkspace addresses a named workspace without changing the desktop selection.
//
//wails:ignore
func (s *Environments) InWorkspace(workspace string) *Environments {
	return NewEnvironments(s.store, domain.WorkspaceScope(workspace), s.ids)
}

// EnvironmentPatch changes metadata without reading or resending variables.
type EnvironmentPatch struct {
	Name     *string `json:"name,omitempty"`
	Color    *string `json:"color,omitempty"`
	Readonly *bool   `json:"readonly,omitempty"`
}

// VariablePatch changes an individual value without resending its entire scope.
type VariablePatch struct {
	Name    *string              `json:"name,omitempty"`
	Value   *string              `json:"value,omitempty"`
	Kind    *domain.VariableKind `json:"kind,omitempty"`
	Enabled *bool                `json:"enabled,omitempty"`
}

// State is every environment and the globals, with secret values withheld: a secret says whether
// it has a value, and Reveal shows it.
func (s *Environments) State(ctx context.Context) (domain.EnvState, error) {
	_, state, err := s.load(ctx)
	if err != nil {
		return domain.EnvState{}, err
	}
	return hideSecretValues(state), nil
}

// Update changes only the supplied environment metadata.
func (s *Environments) Update(ctx context.Context, id string, patch EnvironmentPatch) error {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	env, ok := state.Find(id)
	if !ok {
		return environmentNotFound(id)
	}
	if patch.Name != nil {
		env.Name, err = domain.CleanName(*patch.Name, maxEnvironmentName)
		if err != nil {
			return err
		}
		if nameTaken(state.Environments, id, env.Name) {
			return domain.NameTaken()
		}
	}
	if patch.Color != nil {
		env.Color = *patch.Color
	}
	if patch.Readonly != nil {
		env.Readonly = *patch.Readonly
	}
	return s.store.SaveEnvironment(ctx, workspace, env)
}

// SaveVariables replaces one scope's variables, preserving withheld secret values.
func (s *Environments) SaveVariables(ctx context.Context, scope domain.EnvScope, vars []domain.Variable) error {
	workspace, state, err := s.writable(ctx, scope)
	if err != nil {
		return err
	}
	saved, err := savedVariables(state.Variables(scope), vars)
	if err != nil {
		return err
	}
	return s.saveScope(ctx, workspace, scope, saved)
}

// UpdateVariable edits one variable, preserving every other value in its scope.
func (s *Environments) UpdateVariable(ctx context.Context, scope domain.EnvScope, id string, patch VariablePatch) error {
	workspace, state, err := s.writable(ctx, scope)
	if err != nil {
		return err
	}
	vars := state.Variables(scope)
	at := slices.IndexFunc(vars, func(variable domain.Variable) bool { return variable.ID == id })
	if at < 0 {
		return fmt.Errorf("variable %s: %w", id, domain.ErrNotFound)
	}
	variable := vars[at]
	if patch.Name != nil {
		variable.Name = strings.TrimSpace(*patch.Name)
		if variable.Name != "" && !domain.IsVariableName(variable.Name) {
			return fmt.Errorf("variable name %q: %w", variable.Name, domain.ErrNotAllowed)
		}
	}
	if patch.Value != nil {
		variable.Value = *patch.Value
	}
	if patch.Kind != nil {
		variable.Kind = kindFor(*patch.Kind == domain.VariableSecret)
	}
	if patch.Enabled != nil {
		variable.Enabled = *patch.Enabled
	}
	err = s.store.SaveVariable(ctx, workspace, scope, variable)
	if errors.Is(err, domain.ErrConflict) {
		return domain.NameTaken()
	}
	return err
}

// Reorder puts the environments in the order given; ids it does not name keep their place after
// them. The switcher in the titlebar lists them the same way.
func (s *Environments) Reorder(ctx context.Context, ids []string) error {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	at := map[string]int{}
	for i, id := range ids {
		at[id] = i
	}
	ordered := slices.Clone(state.Environments)
	slices.SortStableFunc(ordered, func(a, b domain.Environment) int {
		ia, oka := at[a.ID]
		ib, okb := at[b.ID]
		switch {
		case oka && okb:
			return ia - ib
		case oka:
			return -1
		case okb:
			return 1
		}
		return 0
	})
	changed := []domain.Environment{}
	for i, env := range ordered {
		if env.Position != i+1 {
			env.Position = i + 1
			changed = append(changed, env)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	return s.store.SaveEnvironments(ctx, workspace, changed)
}

// Duplicate copies an environment under a name of the window's wording, writable whatever the
// original is, blocks included. Secret values are copied too: the copy is on the same device.
func (s *Environments) Duplicate(ctx context.Context, id, name string) (string, error) {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	from, ok := state.Find(id)
	if !ok {
		return "", environmentNotFound(id)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", domain.Refuse(domain.CodeNameEmpty, domain.ErrNotAllowed, nil)
	}
	env := domain.Environment{ID: s.ids(), Name: freeName(name, state.Environments), Color: from.Color,
		Position: nextEnvironmentPosition(state.Environments)}
	writes := []db.ScopeWrite{{Scope: domain.EnvScope{Environment: env.ID}, Vars: s.copyVariables(from.Vars)}}
	for collection, block := range from.Collections {
		if len(block) > 0 {
			writes = append(writes, db.ScopeWrite{
				Scope: domain.EnvScope{Environment: env.ID, Collection: collection},
				Vars:  s.copyVariables(block)})
		}
	}
	return env.ID, s.store.SaveScopes(ctx, workspace, db.ScopeChanges{Environment: &env, Scopes: writes})
}

// Merge moves what one environment holds into another and deletes it: the shared values into the
// shared ones, a block into the block of the same collection. A name both hold keeps the target's
// value. The selection follows the values when the source was the active one.
func (s *Environments) Merge(ctx context.Context, from, into string) error {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	source, ok := state.Find(from)
	if !ok {
		return environmentNotFound(from)
	}
	target, ok := state.Find(into)
	if !ok || from == into {
		return environmentNotFound(into)
	}
	if source.Readonly || target.Readonly {
		return domain.Refuse(domain.CodeEnvironmentReadOnly, domain.ErrNotAllowed, nil)
	}
	writes := []db.ScopeWrite{{Scope: domain.EnvScope{Environment: into},
		Vars: s.addMissing(target.Vars, source.Vars, nil)}}
	for collection, block := range source.Collections {
		writes = append(writes, db.ScopeWrite{
			Scope: domain.EnvScope{Environment: into, Collection: collection},
			Vars:  s.addMissing(target.Block(collection), block, nil)})
	}
	if err := s.store.SaveScopes(ctx, workspace, db.ScopeChanges{Scopes: writes, DeleteEnv: from}); err != nil {
		return err
	}
	if state.ActiveID == from {
		return s.store.SetActiveEnvironment(ctx, workspace, into)
	}
	return nil
}

// copyVariables is a list under new ids, values and all.
func (s *Environments) copyVariables(from []domain.Variable) []domain.Variable {
	out := make([]domain.Variable, len(from))
	for i, v := range from {
		v.ID, v.HasValue, v.Position = s.ids(), false, i+1
		out[i] = v
	}
	return out
}

// findByName is the environment worn under a name, case and surrounding spaces aside.
func findByName(all []domain.Environment, name string) (domain.Environment, bool) {
	for _, env := range all {
		if sameName(env.Name, name) {
			return env, true
		}
	}
	return domain.Environment{}, false
}

func sameName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// Create appends an empty environment without changing the active selection.
func (s *Environments) Create(ctx context.Context, name, color string) (string, error) {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	name, err = domain.CleanName(name, maxEnvironmentName)
	if err != nil {
		return "", err
	}
	if nameTaken(state.Environments, "", name) {
		return "", domain.NameTaken()
	}
	env := domain.Environment{ID: s.ids(), Name: name, Color: color,
		Position: nextEnvironmentPosition(state.Environments)}
	return env.ID, s.store.SaveEnvironment(ctx, workspace, env)
}

// Delete removes an environment and its variables. If it was the active one, the selection falls
// back to none rather than to a sibling the person did not choose.
func (s *Environments) Delete(ctx context.Context, id string) error {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	if _, ok := state.Find(id); !ok {
		return environmentNotFound(id)
	}
	if err := s.store.DeleteEnvironment(ctx, workspace, id); err != nil {
		return err
	}
	if state.ActiveID == id {
		return s.store.SetActiveEnvironment(ctx, workspace, "")
	}
	return nil
}

// Activate selects the environment requests resolve against; empty selects none.
func (s *Environments) Activate(ctx context.Context, id string) error {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	if _, ok := state.Find(id); id != "" && !ok {
		return environmentNotFound(id)
	}
	return s.store.SetActiveEnvironment(ctx, workspace, id)
}

// EnsureDefaults gives an empty workspace its starting environment, except unpulled or read-only ones.
func (s *Environments) EnsureDefaults(ctx context.Context) error {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	if len(state.Environments) > 0 || len(state.Globals) > 0 {
		return nil
	}
	if awaiting, err := s.store.AwaitsFirstPull(ctx, workspace); err != nil || awaiting {
		return err
	}
	env := domain.Environment{ID: s.ids(), Name: "Local · dev", Position: 1}
	baseURL := domain.Variable{ID: s.ids(), Name: domain.BaseURLVariable, Value: "http://localhost:8000",
		Kind: domain.VariableText, Enabled: true, Position: 1}
	err = s.store.SaveScope(ctx, workspace, &env, domain.EnvScope{Environment: env.ID}, []domain.Variable{baseURL})
	if domain.CodeOf(err) == domain.CodeWorkspaceReadOnly {
		return nil
	}
	if err != nil {
		return err
	}
	return s.store.SetActiveEnvironment(ctx, workspace, env.ID)
}

// Reveal is the deliberate "show me" behind the eye button, and the only way a secret's value
// reaches the window.
func (s *Environments) Reveal(ctx context.Context, id string) (string, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return "", err
	}
	return s.store.VariableValue(ctx, workspace, id)
}

// Capture writes a response value into a variable of the active environment or the globals, adding
// it when it is new. A secret stays secret when overwritten.
func (s *Environments) Capture(ctx context.Context, scope domain.VarScope, name, value string, secret bool) error {
	return s.write(ctx, scope, name, value, secret)
}

// Variable reads one enabled variable for a script. Unlike State it returns secrets, for the sandbox only.
//
//wails:ignore
func (s *Environments) Variable(ctx context.Context, scope domain.VarScope, name string) (string, bool, error) {
	_, state, err := s.load(ctx)
	if err != nil {
		return "", false, err
	}
	var vars []domain.Variable
	switch scope {
	case domain.ScopeEnvironment:
		env, ok := state.Find(state.ActiveID)
		if !ok {
			return "", false, nil
		}
		vars = env.Vars
	case domain.ScopeGlobals:
		vars = state.Globals
	default:
		return "", false, fmt.Errorf("scope %q: %w", scope, domain.ErrNotAllowed)
	}
	if at := indexOf(vars, name); at >= 0 && vars[at].Enabled {
		return vars[at].Value, true, nil
	}
	return "", false, nil
}

func indexOf(vars []domain.Variable, name string) int {
	return slices.IndexFunc(vars, func(v domain.Variable) bool { return v.Name == name })
}

// SetVariable writes a variable for a script, even into a closed environment.
//
//wails:ignore
func (s *Environments) SetVariable(ctx context.Context, scope domain.VarScope, name, value string) error {
	return s.write(ctx, scope, name, value, false)
}

// SetInScope writes a variable for an agent, respecting closed environments, and returns its id.
//
//wails:ignore
func (s *Environments) SetInScope(ctx context.Context, scope domain.EnvScope, name, value string, secret bool) (string, error) {
	name = strings.TrimSpace(name)
	if !domain.IsVariableName(name) {
		return "", fmt.Errorf("variable name %q: %w", name, domain.ErrNotAllowed)
	}
	workspace, state, err := s.writable(ctx, scope)
	if err != nil {
		return "", err
	}
	return s.setByName(ctx, workspace, state, scope, name, value, secret)
}

func (s *Environments) write(ctx context.Context, scope domain.VarScope, name, value string, secret bool) error {
	name = strings.TrimSpace(name)
	if !domain.IsVariableName(name) {
		return fmt.Errorf("variable name %q: %w", name, domain.ErrNotAllowed)
	}
	workspace, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	where := domain.EnvScope{}
	switch scope {
	case domain.ScopeEnvironment:
		if _, ok := state.Find(state.ActiveID); !ok {
			return domain.Refuse(domain.CodeNoEnvironment, domain.ErrNotAllowed, nil)
		}
		where.Environment = state.ActiveID
	case domain.ScopeGlobals:
	default:
		return fmt.Errorf("scope %q: %w", scope, domain.ErrNotAllowed)
	}
	_, err = s.setByName(ctx, workspace, state, where, name, value, secret)
	return err
}

func (s *Environments) setByName(ctx context.Context, workspace string, state domain.EnvState,
	scope domain.EnvScope, name, value string, secret bool) (string, error) {
	vars := slices.Clone(state.Variables(scope))
	at := indexOf(vars, name)
	if at < 0 {
		vars = append(vars, domain.Variable{ID: s.ids(), Name: name, Kind: domain.VariableText, Enabled: true})
		at = len(vars) - 1
	}
	vars[at].Value = value
	if secret {
		vars[at].Kind = domain.VariableSecret
	}
	return vars[at].ID, s.saveScope(ctx, workspace, scope, vars)
}

// saveScope numbers variables in display order before persisting them.
func (s *Environments) saveScope(ctx context.Context, workspace string, scope domain.EnvScope, vars []domain.Variable) error {
	for index := range vars {
		vars[index].Position = index + 1
	}
	return s.store.SaveScope(ctx, workspace, nil, scope, vars)
}

// load is the workspace a call works in and what it holds. The workspace is resolved once and travels, so
// a call that began in one workspace never writes into the one the window switched to meanwhile.
func (s *Environments) load(ctx context.Context) (string, domain.EnvState, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return "", domain.EnvState{}, err
	}
	state, err := s.store.EnvState(ctx, workspace)
	return workspace, state, err
}

// writable is load for a write into a scope a person may have closed. The globals never close.
func (s *Environments) writable(ctx context.Context, scope domain.EnvScope) (string, domain.EnvState, error) {
	if scope.Environment == "" && scope.Collection != "" {
		return "", domain.EnvState{}, fmt.Errorf("variable scope %+v: %w", scope, domain.ErrNotAllowed)
	}
	workspace, state, err := s.load(ctx)
	if err != nil || scope.Environment == "" {
		return workspace, state, err
	}
	env, ok := state.Find(scope.Environment)
	if !ok {
		return "", domain.EnvState{}, environmentNotFound(scope.Environment)
	}
	if env.Readonly {
		return "", domain.EnvState{}, domain.Refuse(domain.CodeEnvironmentReadOnly, domain.ErrNotAllowed, nil)
	}
	return workspace, state, nil
}

// savedVariables validates the sent list: usable trimmed names unique in the set, unique ids, text
// or secret kinds, ordered positions, and withheld secrets kept from storage.
func savedVariables(held, sent []domain.Variable) ([]domain.Variable, error) {
	stored := map[string]domain.Variable{}
	for _, v := range held {
		stored[v.ID] = v
	}
	out := make([]domain.Variable, 0, len(sent))
	ids, names := map[string]bool{}, map[string]bool{}
	for i, v := range sent {
		v.Name = strings.TrimSpace(v.Name)
		if v.ID == "" || ids[v.ID] {
			return nil, fmt.Errorf("variable id %q: %w", v.ID, domain.ErrNotAllowed)
		}
		if v.Name != "" && !domain.IsVariableName(v.Name) {
			return nil, fmt.Errorf("variable name %q: %w", v.Name, domain.ErrNotAllowed)
		}
		if v.Name != "" && names[v.Name] {
			return nil, domain.NameTaken()
		}
		ids[v.ID], names[v.Name] = true, true
		v.Kind = kindFor(v.Kind == domain.VariableSecret)
		if v.Value == "" && v.HasValue {
			v.Value = stored[v.ID].Value
		}
		v.HasValue, v.Position = false, i+1
		out = append(out, v)
	}
	return out, nil
}

// freeName is the file's name, numbered beside it when the workspace already holds one.
func freeName(name string, taken []domain.Environment) string {
	held := map[string]bool{}
	for _, env := range taken {
		held[strings.ToLower(env.Name)] = true
	}
	if !held[strings.ToLower(name)] {
		return name
	}
	for number := 2; ; number++ {
		suffix := fmt.Sprintf(" %d", number)
		runes := []rune(name)
		if room := maxEnvironmentName - len(suffix); len(runes) > room {
			runes = runes[:room]
		}
		if candidate := string(runes) + suffix; !held[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

// hideSecretValues copies the state without the secrets' values; the variables stay, with whether
// they have one.
func hideSecretValues(state domain.EnvState) domain.EnvState {
	hide := func(list []domain.Variable) []domain.Variable {
		out := make([]domain.Variable, len(list))
		for i, v := range list {
			if v.Kind == domain.VariableSecret {
				v.HasValue, v.Value = v.Value != "", ""
			}
			out[i] = v
		}
		return out
	}
	for i := range state.Environments {
		state.Environments[i].Vars = hide(state.Environments[i].Vars)
		blocks := map[string][]domain.Variable{}
		for collection, block := range state.Environments[i].Collections {
			blocks[collection] = hide(block)
		}
		state.Environments[i].Collections = blocks
	}
	state.Globals = hide(state.Globals)
	return state
}

func kindFor(secret bool) domain.VariableKind {
	if secret {
		return domain.VariableSecret
	}
	return domain.VariableText
}

func nextEnvironmentPosition(environments []domain.Environment) int {
	position := 0
	for _, env := range environments {
		position = max(position, env.Position)
	}
	return position + 1
}

func nameTaken(all []domain.Environment, except, name string) bool {
	return slices.ContainsFunc(all, func(env domain.Environment) bool {
		return env.ID != except && sameName(env.Name, name)
	})
}

func environmentNotFound(id string) error {
	return fmt.Errorf("environment %s: %w", id, domain.ErrNotFound)
}

// ActiveName is the name of the active environment, empty when none is.
//
//wails:ignore
func (s *Environments) ActiveName(ctx context.Context) (string, error) {
	state, err := s.State(ctx)
	if err != nil {
		return "", err
	}
	active, _ := state.Find(state.ActiveID)
	return active.Name, nil
}
