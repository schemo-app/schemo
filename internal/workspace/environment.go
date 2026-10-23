package workspace

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"schemo/internal/convert/dotenv"
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

// EnvironmentInput is what a new environment is asked for. StartFrom names an environment to copy the
// variables of, empty for a blank one.
type EnvironmentInput struct {
	Name      string `json:"name"`
	Color     string `json:"color,omitempty"`
	StartFrom string `json:"startFrom,omitempty"`
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

// Save writes an environment as the window edited it: its name, colour, access and the whole list
// of its variables in order. A secret sent without a value but with hasValue keeps the stored one,
// since the window never holds it. A closed environment takes no variable changes; opening it is
// part of the same save.
func (s *Environments) Save(ctx context.Context, env domain.Environment) error {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	held, ok := findEnvironment(state, env.ID)
	if !ok {
		return environmentNotFound(env.ID)
	}
	if env.Name, err = domain.CleanName(env.Name, maxEnvironmentName); err != nil {
		return err
	}
	if nameTaken(state.Environments, env.ID, env.Name) {
		return domain.NameTaken()
	}
	vars, err := savedVariables(held.Vars, env.Vars)
	if err != nil {
		return err
	}
	before, _ := savedVariables(held.Vars, held.Vars)
	if held.Readonly && env.Readonly && !slices.Equal(before, vars) {
		return domain.Refuse(domain.CodeEnvironmentReadOnly, domain.ErrNotAllowed, nil)
	}
	env.Position, env.Vars = held.Position, nil
	return s.store.SaveScope(ctx, workspace, &env, domain.EnvScope{Environment: env.ID}, vars)
}

// SaveGlobals writes the whole list of global variables, by the same rules as Save. An empty list
// clears them.
func (s *Environments) SaveGlobals(ctx context.Context, vars []domain.Variable) error {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	saved, err := savedVariables(state.Globals, vars)
	if err != nil {
		return err
	}
	return s.store.SaveScope(ctx, workspace, nil, domain.EnvScope{}, saved)
}

// Create adds an environment at the end of the list and makes it the active one: a new environment
// is being set up, and editing one you are not in is how mistakes happen. A base fills it with the
// same keys, kinds and order; a secret is copied without its value.
func (s *Environments) Create(ctx context.Context, draft EnvironmentInput) (string, error) {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	name, err := domain.CleanName(draft.Name, maxEnvironmentName)
	if err != nil {
		return "", err
	}
	if nameTaken(state.Environments, "", name) {
		return "", domain.NameTaken()
	}
	base := domain.Environment{}
	if draft.StartFrom != "" {
		found, ok := findEnvironment(state, draft.StartFrom)
		if !ok {
			return "", environmentNotFound(draft.StartFrom)
		}
		base = found
	}
	env := domain.Environment{ID: s.ids(), Name: name, Color: draft.Color,
		Position: nextPosition(state.Environments, func(e domain.Environment) int { return e.Position })}
	vars := make([]domain.Variable, len(base.Vars))
	for i, v := range base.Vars {
		vars[i] = domain.Variable{ID: s.ids(), Name: v.Name, Kind: v.Kind, Enabled: v.Enabled, Position: i + 1}
		if v.Kind != domain.VariableSecret {
			vars[i].Value = v.Value
		}
	}
	if err := s.store.SaveScope(ctx, workspace, &env, domain.EnvScope{Environment: env.ID}, vars); err != nil {
		return "", err
	}
	return env.ID, s.store.SetActiveEnvironment(ctx, workspace, env.ID)
}

// Import writes an environment a file described, variables and all. Nothing is activated, and a
// name the workspace already holds is numbered beside it rather than refused: a file arriving under a
// taken name is not the person's mistake.
//
//wails:ignore
func (s *Environments) Import(ctx context.Context, draft domain.EnvironmentDraft) (string, error) {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(draft.Name)
	if name == "" {
		return "", domain.Refuse(domain.CodeNameEmpty, domain.ErrNotAllowed, nil)
	}
	if runes := []rune(name); len(runes) > maxEnvironmentName {
		name = string(runes[:maxEnvironmentName])
	}
	env := domain.Environment{ID: s.ids(), Name: freeName(name, state.Environments), Color: draft.Color,
		Position: nextPosition(state.Environments, func(e domain.Environment) int { return e.Position })}
	vars := importedVariables(s.ids, draft.Variables)
	return env.ID, s.store.SaveScope(ctx, workspace, &env, domain.EnvScope{Environment: env.ID}, vars)
}

// Delete removes an environment and its variables. If it was the active one, the selection falls
// back to none rather than to a sibling the person did not choose.
func (s *Environments) Delete(ctx context.Context, id string) error {
	workspace, state, err := s.load(ctx)
	if err != nil {
		return err
	}
	if _, ok := findEnvironment(state, id); !ok {
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
	if _, ok := findEnvironment(state, id); id != "" && !ok {
		return environmentNotFound(id)
	}
	return s.store.SetActiveEnvironment(ctx, workspace, id)
}

// EnsureDefaults gives an empty workspace the environment the app starts with, so there is
// somewhere to type a base URL. A workspace of the account not pulled yet, or one this person may only
// read, is left as it is: what it holds is the server's.
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

// ParseDotenv reads a .env file into entries for the import dialog.
func (s *Environments) ParseDotenv(text string) []dotenv.Entry {
	return dotenv.Parse(text)
}

// ImportEntries merges parsed .env entries into a scope: a name already there is replaced, a new one
// is appended.
func (s *Environments) ImportEntries(ctx context.Context, scope domain.EnvScope, entries []dotenv.Entry) error {
	workspace, state, err := s.writable(ctx, scope)
	if err != nil {
		return err
	}
	vars := slices.Clone(scopeVariables(state, scope))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Name) == "" {
			continue
		}
		at := slices.IndexFunc(vars, func(v domain.Variable) bool { return v.Name == entry.Name })
		if at < 0 {
			vars = append(vars, domain.Variable{ID: s.ids(), Name: entry.Name})
			at = len(vars) - 1
		}
		vars[at].Value, vars[at].Kind, vars[at].Enabled = entry.Value, kindFor(entry.Secret), true
	}
	return s.saveScope(ctx, workspace, state, scope, vars)
}

// Capture writes a response value into a variable of the active environment or the globals, adding
// it when it is new. A secret stays secret when overwritten.
func (s *Environments) Capture(ctx context.Context, scope domain.VarScope, name, value string, secret bool) error {
	return s.write(ctx, scope, name, value, secret)
}

// Variable reads one enabled variable by name in the scope a script names: the active environment
// or the globals. Unlike State it returns secrets: the value goes into the script sandbox only.
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
		env, ok := findEnvironment(state, state.ActiveID)
		if !ok {
			return "", false, nil
		}
		vars = env.Vars
	case domain.ScopeGlobals:
		vars = state.Globals
	default:
		return "", false, fmt.Errorf("scope %q: %w", scope, domain.ErrNotAllowed)
	}
	for _, v := range vars {
		if v.Enabled && v.Name == name {
			return v.Value, true, nil
		}
	}
	return "", false, nil
}

// SetVariable writes a variable by name for a script, adding it when it is new. Scripts may write
// into a closed environment: closing is about what a person types by accident.
//
//wails:ignore
func (s *Environments) SetVariable(ctx context.Context, scope domain.VarScope, name, value string) error {
	return s.write(ctx, scope, name, value, false)
}

// SetInScope sets a variable by name in a scope for an agent, respecting closed environments, and
// answers with its id.
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
		if _, ok := findEnvironment(state, state.ActiveID); !ok {
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
	vars := slices.Clone(scopeVariables(state, scope))
	at := slices.IndexFunc(vars, func(v domain.Variable) bool { return v.Name == name })
	if at < 0 {
		vars = append(vars, domain.Variable{ID: s.ids(), Name: name, Kind: domain.VariableText, Enabled: true})
		at = len(vars) - 1
	}
	vars[at].Value = value
	if secret {
		vars[at].Kind = domain.VariableSecret
	}
	return vars[at].ID, s.saveScope(ctx, workspace, state, scope, vars)
}

// saveScope writes a scope's full list, numbering positions in order.
func (s *Environments) saveScope(ctx context.Context, workspace string, state domain.EnvState,
	scope domain.EnvScope, vars []domain.Variable) error {
	for i := range vars {
		vars[i].Position = i + 1
	}
	if scope.Environment == "" {
		return s.store.SaveScope(ctx, workspace, nil, scope, vars)
	}
	if _, ok := findEnvironment(state, scope.Environment); !ok {
		return environmentNotFound(scope.Environment)
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
	workspace, state, err := s.load(ctx)
	if err != nil {
		return "", domain.EnvState{}, err
	}
	if env, ok := findEnvironment(state, scope.Environment); ok && env.Readonly {
		return "", domain.EnvState{}, domain.Refuse(domain.CodeEnvironmentReadOnly, domain.ErrNotAllowed, nil)
	}
	if scope.Environment != "" {
		if _, ok := findEnvironment(state, scope.Environment); !ok {
			return "", domain.EnvState{}, environmentNotFound(scope.Environment)
		}
	}
	return workspace, state, nil
}

// savedVariables is the list the window sent, made safe to store: names trimmed and usable in a
// `{{token}}` (or still empty, for a row being named), each name and id once, kinds text or secret,
// positions in order, and withheld secret values taken from what is stored.
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

// importedVariables is what a file's variables become: ids minted here, the file's order, and a
// name listed twice written once with the last value, the way a file of exports reads.
func importedVariables(ids platform.IDGen, from []domain.Variable) []domain.Variable {
	out := []domain.Variable{}
	at := map[string]int{}
	for _, one := range from {
		name := strings.TrimSpace(one.Name)
		if name == "" {
			continue
		}
		if index, seen := at[name]; seen {
			out[index].Value, out[index].Kind, out[index].Enabled = one.Value, kindFor(one.Kind == domain.VariableSecret), one.Enabled
			continue
		}
		at[name] = len(out)
		out = append(out, domain.Variable{ID: ids(), Name: name, Value: one.Value,
			Kind: kindFor(one.Kind == domain.VariableSecret), Enabled: one.Enabled})
	}
	for i := range out {
		out[i].Position = i + 1
	}
	return out
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

// scopeVariables lists what a scope holds; an empty scope is the globals.
func scopeVariables(state domain.EnvState, scope domain.EnvScope) []domain.Variable {
	if scope.Environment == "" {
		return state.Globals
	}
	env, _ := findEnvironment(state, scope.Environment)
	return env.Vars
}

// nextPosition is one past the highest position in a list, so a new row goes last.
func nextPosition[T any](items []T, position func(T) int) int {
	out := 0
	for _, item := range items {
		if at := position(item); at >= out {
			out = at + 1
		}
	}
	return out
}

func findEnvironment(state domain.EnvState, id string) (domain.Environment, bool) {
	for _, env := range state.Environments {
		if env.ID == id {
			return env, true
		}
	}
	return domain.Environment{}, false
}

func nameTaken(all []domain.Environment, except, name string) bool {
	return slices.ContainsFunc(all, func(env domain.Environment) bool {
		return env.ID != except && env.Name == name
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
	for _, one := range state.Environments {
		if one.ID == state.ActiveID {
			return one.Name, nil
		}
	}
	return "", nil
}

// ImportState imports an environment and answers with the whole list.
//
//wails:ignore
func (s *Environments) ImportState(ctx context.Context, draft domain.EnvironmentDraft) (domain.EnvState, error) {
	if _, err := s.Import(ctx, draft); err != nil {
		return domain.EnvState{}, err
	}
	return s.State(ctx)
}
