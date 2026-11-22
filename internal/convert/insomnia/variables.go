package insomnia

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"schemo/internal/domain"
)

// values gathers one collection's variables from several levels; a name keeps its first value.
type values struct {
	list []domain.Variable
	seen map[string]string
}

func newValues() *values { return &values{list: []domain.Variable{}, seen: map[string]string{}} }

// add counts a later level that disagrees with a value already taken.
func (v *values) add(vars []domain.Variable, out *domain.Document) {
	for _, one := range vars {
		if held, ok := v.seen[one.Name]; ok {
			if held != one.Value {
				out.LeftOut(domain.CodeSkippedVariableClash, 1)
			}
			continue
		}
		v.seen[one.Name] = one.Value
		one.Position = len(v.list) + 1
		v.list = append(v.list, one)
	}
}

// variablesOf reads environment data: nested objects are flattened with dots, scalars become text.
func variablesOf(data map[string]any, out *domain.Document) []domain.Variable {
	vars := []domain.Variable{}
	flatten("", data, &vars, out)
	return vars
}

func flatten(prefix string, data map[string]any, into *[]domain.Variable, out *domain.Document) {
	for _, key := range slices.Sorted(maps.Keys(data)) {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		if prefix != "" {
			name = prefix + "." + name
		}
		if nested, ok := data[key].(map[string]any); ok {
			flatten(name, nested, into, out)
			continue
		}
		*into = append(*into, domain.Variable{
			Name:     name,
			Value:    templated(domain.ValueText(data[key]), out),
			Kind:     domain.VariableText,
			Enabled:  true,
			Position: len(*into) + 1,
		})
	}
}

// secrets marks the confidential names, or every name of a private environment.
func secrets(vars []domain.Variable, confidential []string, private bool) []domain.Variable {
	for index := range vars {
		if private || slices.Contains(confidential, vars[index].Name) {
			vars[index].Kind = domain.VariableSecret
		}
	}
	return vars
}

// plain keeps a secret's name without its value: collection variables are shared and hold none.
func plain(vars []domain.Variable, out *domain.Document) []domain.Variable {
	for index := range vars {
		if vars[index].Kind == domain.VariableSecret {
			vars[index].Kind = domain.VariableText
			vars[index].Value = ""
			out.LeftOut(domain.CodeSkippedSecretValue, 1)
		}
	}
	return vars
}

func subDraft(name, color string, vars []domain.Variable) domain.EnvironmentDraft {
	return domain.EnvironmentDraft{Name: name, Color: color, Blocks: []domain.BlockDraft{{Variables: vars}}}
}

// blocksFor points the drafts' blocks at the collection the document will hold at that index.
func blocksFor(collection int, drafts []domain.EnvironmentDraft) []domain.EnvironmentDraft {
	for index := range drafts {
		for block := range drafts[index].Blocks {
			drafts[index].Blocks[block].Collection = collection
		}
	}
	return drafts
}

// Variables is every environment value an export holds, base first; a name keeps its first value.
func Variables(data []byte) ([]domain.Variable, error) {
	var scratch domain.Document
	vars := newValues()
	var file export
	if err := json.Unmarshal(data, &file); file.Type == "export" {
		if err != nil {
			return nil, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
		}
		for _, one := range file.Resources {
			if one.Type == "environment" {
				vars.add(secrets(variablesOf(one.Data, &scratch), nil, one.IsPrivate), &scratch)
			}
		}
	} else {
		v5, _, err := decodeV5(data)
		if err != nil {
			return nil, err
		}
		base := v5.Environments
		vars.add(secrets(variablesOf(base.Data, &scratch), base.ConfidentialKeys, false), &scratch)
		for _, sub := range base.SubEnvironments {
			vars.add(secrets(variablesOf(sub.Data, &scratch), sub.ConfidentialKeys, sub.private()), &scratch)
		}
	}
	if len(vars.list) == 0 {
		return nil, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	return vars.list, nil
}
