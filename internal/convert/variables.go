package convert

// Variables files for the environments window: one scope in, one scope out.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"schemo/internal/convert/dotenv"
	"schemo/internal/convert/insomnia"
	"schemo/internal/convert/postman"
	"schemo/internal/convert/schemofile"
	"schemo/internal/desktop"
	"schemo/internal/domain"
)

// VariablesImport is what a variables file did to a scope.
type VariablesImport struct {
	File      string `json:"file"`
	Added     int    `json:"added"`
	Kept      int    `json:"kept"`
	Cancelled bool   `json:"cancelled"`
}

// ImportVariablesFile adds the variables of a picked file to the scope; values already there stay.
func (s *Transfer) ImportVariablesFile(ctx context.Context, title string, scope domain.EnvScope) (VariablesImport, error) {
	path, err := s.host.OpenPath(title, false, application.FileFilter{
		DisplayName: "Variables", Pattern: "*.json;*.yaml;*.yml;*.env",
	})
	if err != nil || path == "" {
		return VariablesImport{Cancelled: path == ""}, err
	}
	data, err := s.files.Read(path)
	if err != nil {
		return VariablesImport{}, err
	}
	vars, err := parseVariables(data)
	if err != nil {
		return VariablesImport{}, err
	}
	report, err := s.environments.ImportVariables(ctx, scope, vars)
	if err != nil {
		return VariablesImport{}, err
	}
	return VariablesImport{File: filepath.Base(path), Added: report.Added, Kept: report.Kept}, nil
}

// ExportVariablesFile saves a scope as a Postman environment, or the globals as Postman globals, and
// returns the file's name; empty means cancelled. Secret values are not written.
func (s *Transfer) ExportVariablesFile(ctx context.Context, title string, scope domain.EnvScope) (string, error) {
	state, err := s.environments.State(ctx)
	if err != nil {
		return "", err
	}
	globals := scope.Environment == ""
	name, file, vars := "", "workspace.postman_globals.json", state.Globals
	if !globals {
		environment, ok := state.Find(scope.Environment)
		if !ok {
			return "", domain.Refuse(domain.CodeNotFound, domain.ErrNotFound, nil)
		}
		name, vars = environment.Name, overlay(environment.Vars, environment.Block(scope.Collection))
		file = desktop.SafeFileName(name, "environment", ".postman_environment.json")
	}
	data, err := postman.ExportEnvironment(name, globals, vars)
	if err != nil {
		return "", err
	}
	path, err := s.host.SaveFile(title, file,
		application.FileFilter{DisplayName: "Postman environment", Pattern: "*.json"})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("file %s: %w", path, err)
	}
	return filepath.Base(path), nil
}

// utf8BOM is what some editors put before the first brace, which JSON does not allow.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// parseVariables reads a variables file by its content: Postman environment or globals, Insomnia
// export, Schemo file, or else .env.
func parseVariables(data []byte) ([]domain.Variable, error) {
	text := bytes.TrimSpace(bytes.TrimPrefix(data, utf8BOM))
	if bytes.HasPrefix(text, []byte("{")) {
		var probe struct {
			Scope  string `json:"_postman_variable_scope"`
			Type   string `json:"_type"`
			Schemo string `json:"schemo"`
		}
		if err := json.Unmarshal(text, &probe); err != nil {
			return nil, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
		}
		switch {
		case probe.Scope != "":
			vars, err := postman.Variables(text)
			if err != nil {
				return nil, err
			}
			return firstOfEach(vars)
		case probe.Type == "export":
			return insomnia.Variables(text)
		case probe.Schemo != "":
			return schemoVariables(text)
		}
		return nil, domain.Refuse(domain.CodeFileUnreadable, domain.ErrNotAllowed, nil)
	}
	vars, err := insomnia.Variables(text)
	switch domain.CodeOf(err) {
	case domain.CodeNotInsomnia, domain.CodeFileUnreadable:
		return dotenvVariables(text)
	}
	return vars, err
}

// schemoVariables is every environment value a Schemo file carries; a name keeps its first value.
func schemoVariables(data []byte) ([]domain.Variable, error) {
	document, err := schemofile.Read(data)
	if err != nil {
		return nil, err
	}
	var vars []domain.Variable
	for _, draft := range document.Environments {
		vars = append(vars, draft.Variables...)
		for _, block := range draft.Blocks {
			vars = append(vars, block.Variables...)
		}
	}
	return firstOfEach(vars)
}

func dotenvVariables(data []byte) ([]domain.Variable, error) {
	var vars []domain.Variable
	for _, entry := range dotenv.Parse(string(data)) {
		kind := domain.VariableText
		if entry.Secret {
			kind = domain.VariableSecret
		}
		vars = append(vars, domain.Variable{Name: entry.Name, Value: entry.Value, Kind: kind, Enabled: true})
	}
	return firstOfEach(vars)
}

// firstOfEach drops later rows of a name and refuses a file that held none.
func firstOfEach(vars []domain.Variable) ([]domain.Variable, error) {
	out := []domain.Variable{}
	seen := map[string]bool{}
	for _, variable := range vars {
		if seen[variable.Name] {
			continue
		}
		seen[variable.Name] = true
		variable.Position = len(out) + 1
		out = append(out, variable)
	}
	if len(out) == 0 {
		return nil, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	return out, nil
}
