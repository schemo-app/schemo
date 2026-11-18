package workspace

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"schemo/internal/domain"
)

// Resolver captures the variables for a request and produces its wire and masked text together.
//
//wails:ignore
func (s *Environments) Resolver(
	ctx context.Context, workspace string, above []domain.Variable, envID string,
) (func(string) (string, string), error) {
	state, err := s.store.EnvState(ctx, workspace)
	if err != nil {
		return nil, err
	}
	live, _ := variableLookup(state, true, above, envID)
	masked, secrets := variableLookup(state, false, above, envID)
	return func(text string) (string, string) {
		return interpolate(text, live, false), redact(interpolate(text, masked, true), secrets)
	}, nil
}

//wails:ignore
func (s *Environments) SubstituteTexts(
	ctx context.Context,
	above []domain.Variable,
	texts []string,
	mask bool,
	envID string,
) ([]string, error) {
	resolver, secrets, err := s.resolver(ctx, !mask, above, envID)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(texts))
	for i, text := range texts {
		out[i] = interpolate(text, resolver, mask)
		if mask {
			out[i] = redact(out[i], secrets)
		}
	}
	return out, nil
}

// ReferenceTexts resolves a request for an export without secrets: a secret becomes a shell
// variable (`$SCHEMO_TOKEN`) and a pasted secret value is masked.
//
//wails:ignore
func (s *Environments) ReferenceTexts(
	ctx context.Context,
	above []domain.Variable,
	texts []string,
	envID string,
) ([]string, error) {
	resolve, secrets, err := s.resolver(ctx, false, above, envID)
	if err != nil {
		return nil, err
	}
	referenced := func(name string) (domain.Resolution, bool) {
		r, ok := resolve(name)
		if ok && r.Kind == domain.VariableSecret {
			r.Value = shellVariable(name)
		}
		return r, ok
	}
	out := make([]string, len(texts))
	for i, text := range texts {
		out[i] = redact(interpolate(text, referenced, false), secrets)
	}
	return out, nil
}

// shellVariable is the name a secret goes out under: the variable's own, upper-cased, with a
// prefix that says which app it belongs to and everything a shell cannot read replaced.
func shellVariable(name string) string {
	runes := []rune(strings.ToUpper(name))
	for i, r := range runes {
		switch {
		case 'A' <= r && r <= 'Z', '0' <= r && r <= '9', r == '_':
		default:
			runes[i] = '_'
		}
	}
	return "$SCHEMO_" + string(runes)
}

//wails:ignore
func (s *Environments) Missing(
	ctx context.Context,
	above []domain.Variable,
	texts []string,
	envID string,
) ([]string, error) {
	resolver, _, err := s.resolver(ctx, false, above, envID)
	if err != nil {
		return nil, err
	}

	out := []string{}
	seen := map[string]bool{}
	for _, text := range texts {
		for _, name := range missing(text, resolver) {
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

// InspectTexts exposes the origin of variables shown by a draft, with secret values withheld.
//
//wails:ignore
func (s *Environments) InspectTexts(
	ctx context.Context, above []domain.Variable, texts []string, envID string,
) ([]string, map[string]domain.Resolution, error) {
	resolve, secrets, err := s.resolver(ctx, false, above, envID)
	if err != nil {
		return nil, nil, err
	}
	unresolved := []string{}
	resolved := map[string]domain.Resolution{}
	seen := map[string]bool{}
	for _, text := range texts {
		for _, token := range tokens(text) {
			if seen[token.name] {
				continue
			}
			seen[token.name] = true
			value, ok := resolve(token.name)
			if !ok {
				unresolved = append(unresolved, token.name)
				continue
			}
			value.Value = redact(value.Value, secrets)
			resolved[token.name] = value
		}
	}
	return unresolved, resolved, nil
}

//wails:ignore
func (s *Environments) MaskTexts(
	ctx context.Context,
	above []domain.Variable,
	texts []string,
	envID string,
) ([]string, error) {
	_, secrets, err := s.resolver(ctx, false, above, envID)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(texts))
	for i, text := range texts {
		out[i] = redact(text, secrets)
	}
	return out, nil
}

// MaskTextsIn is MaskTexts with a named workspace's secrets.
//
//wails:ignore
func (s *Environments) MaskTextsIn(
	ctx context.Context,
	workspace string,
	above []domain.Variable,
	texts []string,
	envID string,
) ([]string, error) {
	_, secrets, err := s.resolverIn(ctx, workspace, false, above, envID)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(texts))
	for i, text := range texts {
		out[i] = redact(text, secrets)
	}
	return out, nil
}

func (s *Environments) resolver(
	ctx context.Context,
	revealSecrets bool,
	above []domain.Variable,
	envID string,
) (lookup, []string, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, nil, err
	}
	return s.resolverIn(ctx, workspace, revealSecrets, above, envID)
}

func (s *Environments) resolverIn(
	ctx context.Context,
	workspace string,
	revealSecrets bool,
	above []domain.Variable,
	envID string,
) (lookup, []string, error) {
	state, err := s.store.EnvState(ctx, workspace)
	if err != nil {
		return nil, nil, err
	}
	resolve, secrets := variableLookup(state, revealSecrets, above, envID)
	return resolve, secrets, nil
}

func variableLookup(state domain.EnvState, revealSecrets bool, above []domain.Variable, envID string) (lookup, []string) {
	active := map[string]domain.Variable{}
	env, _ := state.Selected(envID)
	for _, v := range env.Vars {
		if v.Enabled && v.Name != "" {
			active[v.Name] = v
		}
	}

	globals := map[string]domain.Variable{}
	for _, v := range state.Globals {
		if v.Enabled && v.Name != "" {
			globals[v.Name] = v
		}
	}

	collections := map[string]domain.Variable{}
	for _, v := range baseVariables(state, env) {
		if v.Enabled && v.Name != "" {
			collections[v.Name] = v
		}
	}
	folders := map[string]domain.Variable{}
	runtime := map[string]domain.Variable{}
	for _, v := range above {
		if !v.Enabled || v.Name == "" {
			continue
		}
		switch v.Source {
		case domain.ScopeFolder:
			folders[v.Name] = v
		case domain.ScopeRun:
			runtime[v.Name] = v
		default:
			collections[v.Name] = v
		}
	}

	secrets := []string{}
	if !revealSecrets {
		secrets = secretValues(state.CollectionEnvironments, runtime, folders, collections, active, globals)
	}

	return func(name string) (domain.Resolution, bool) {
		if v, ok := runtime[name]; ok {
			return resolution(v, "runtime", revealSecrets), true
		}
		if v, ok := folders[name]; ok {
			return resolution(v, "folder", revealSecrets), true
		}
		if v, ok := active[name]; ok {
			value := resolution(v, "env", revealSecrets)
			value.EnvironmentID = env.ID
			return value, true
		}
		if v, ok := collections[name]; ok {
			return resolution(v, "collection", revealSecrets), true
		}
		if v, ok := globals[name]; ok {
			return resolution(v, "global", revealSecrets), true
		}
		return domain.Resolution{}, false
	}, secrets
}

func baseVariables(state domain.EnvState, env domain.Environment) []domain.Variable {
	if env.CollectionID == "" || env.Base {
		return nil
	}
	for _, base := range state.CollectionEnvironments {
		if base.CollectionID == env.CollectionID && base.Base && !base.Folder {
			return base.Vars
		}
	}
	return nil
}

func secretValues(scoped []domain.Environment, levels ...map[string]domain.Variable) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(v domain.Variable) {
		if v.Kind == domain.VariableSecret && v.Value != "" && !seen[v.Value] {
			seen[v.Value] = true
			out = append(out, v.Value)
		}
	}
	for _, level := range levels {
		for _, v := range level {
			add(v)
		}
	}
	for _, env := range scoped {
		for _, v := range env.Vars {
			add(v)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		if c := cmp.Compare(len(b), len(a)); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	return out
}

func resolution(v domain.Variable, source string, reveal bool) domain.Resolution {
	out := domain.Resolution{
		Source:   source,
		Kind:     v.Kind,
		HasValue: v.Value != "",
	}
	if reveal || v.Kind != domain.VariableSecret {
		out.Value = v.Value
	}
	return out
}
