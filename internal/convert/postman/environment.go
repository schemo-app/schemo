package postman

import (
	"encoding/json"
	"strings"

	"schemo/internal/domain"
)

// The scopes a variables file says it holds.
const (
	scopeEnvironment = "environment"
	scopeGlobals     = "globals"
)

// Variables reads an environment or globals file as its variables.
func Variables(data []byte) ([]domain.Variable, error) {
	draft, err := readEnvironment(data)
	if err != nil {
		return nil, err
	}
	return draft.Variables, nil
}

// readEnvironment reads an environment or globals export: a name and its values, no requests.
func readEnvironment(data []byte) (domain.EnvironmentDraft, error) {
	var file struct {
		Name   string     `json:"name"`
		Scope  string     `json:"_postman_variable_scope"`
		Values []variable `json:"values"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return domain.EnvironmentDraft{}, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
	}
	known := file.Scope == scopeEnvironment || file.Scope == scopeGlobals
	if !known || (file.Name == "" && len(file.Values) == 0) {
		return domain.EnvironmentDraft{}, domain.Refuse(domain.CodeNotPostman, domain.ErrNotAllowed, nil)
	}

	draft := domain.EnvironmentDraft{Name: strings.TrimSpace(file.Name)}
	for _, v := range file.Values {
		name := strings.TrimSpace(v.Key)
		if name == "" {
			continue
		}
		kind := domain.VariableText
		if v.secret() {
			kind = domain.VariableSecret
		}
		draft.Variables = append(draft.Variables, domain.Variable{
			Name:     name,
			Value:    domain.ValueText(v.Value),
			Kind:     kind,
			Enabled:  v.switchedOn(),
			Position: len(draft.Variables) + 1,
		})
	}
	if draft.Name == "" {
		draft.Name = "Postman"
	}
	return draft, nil
}

// environmentFile is what Postman writes from its environments tab and its globals.
type environmentFile struct {
	Name   string         `json:"name"`
	Values []writtenValue `json:"values"`
	Scope  string         `json:"_postman_variable_scope"`
}

type writtenValue struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}

// ExportEnvironment writes variables as a Postman environment, or as the globals when globals is
// set. Secret values are never written: the type says a value belongs there.
func ExportEnvironment(name string, globals bool, vars []domain.Variable) ([]byte, error) {
	file := environmentFile{Name: name, Values: []writtenValue{}, Scope: scopeEnvironment}
	if globals {
		file.Name, file.Scope = "Globals", scopeGlobals
	}
	for _, variable := range vars {
		key := strings.TrimSpace(variable.Name)
		if key == "" {
			continue
		}
		one := writtenValue{Key: key, Value: variable.Value, Type: "default", Enabled: variable.Enabled}
		if variable.Kind == domain.VariableSecret {
			one.Value, one.Type = "", "secret"
		}
		file.Values = append(file.Values, one)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
