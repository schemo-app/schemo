package workspace

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"schemo/internal/domain"
)

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

// ReferenceTexts resolves the texts of a request for an export that leaves secret values out: a
// secret resolves to a shell variable of its own (`$SCHEMO_TOKEN`), so the file names where the
// value comes from instead of carrying it. A value the text still spells out is masked as a log's
// is, since a secret a person pasted rather than named must not leave in a file either.
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

// MaskTextsIn is MaskTexts for one named workspace: the values a mask is made of are that workspace's, and
// a reader looking at a workspace the window is not showing must not be answered with the ones on
// screen.
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
	if envID == "" {
		envID = state.ActiveID
	}

	active := map[string]domain.Variable{}
	for _, env := range state.Environments {
		if env.ID != envID {
			continue
		}
		for _, v := range env.Vars {
			if v.Enabled && v.Name != "" {
				active[v.Name] = v
			}
		}
	}

	globals := map[string]domain.Variable{}
	for _, v := range state.Globals {
		if v.Enabled && v.Name != "" {
			globals[v.Name] = v
		}
	}

	collections := map[string]domain.Variable{}
	for _, v := range above {
		if v.Enabled && v.Name != "" {
			collections[v.Name] = v
		}
	}

	secrets := []string{}
	if !revealSecrets {
		secrets = secretValues(collections, active, globals)
	}

	// The window's own levels stand over a collection's: what a person wrote in an environment is
	// the answer they are looking at, and a name a collection brought along in an import must not
	// quietly take its place. A collection still answers the names no other level holds.
	return func(name string) (domain.Resolution, bool) {
		if v, ok := active[name]; ok {
			return resolution(v, "env", revealSecrets), true
		}
		if v, ok := globals[name]; ok {
			return resolution(v, "global", revealSecrets), true
		}
		if v, ok := collections[name]; ok {
			return resolution(v, "collection", revealSecrets), true
		}
		return domain.Resolution{}, false
	}, secrets, nil
}

func secretValues(levels ...map[string]domain.Variable) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, level := range levels {
		for _, v := range level {
			if v.Kind != domain.VariableSecret || v.Value == "" || seen[v.Value] {
				continue
			}
			seen[v.Value] = true
			out = append(out, v.Value)
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
