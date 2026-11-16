package insomnia

import (
	"bytes"
	"cmp"
	"io"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"schemo/internal/domain"
)

type yamlExport struct {
	Name           string          `yaml:"name"`
	Meta           metadata        `yaml:"meta"`
	Collection     []yamlItem      `yaml:"collection"`
	Authentication *auth           `yaml:"authentication"`
	Environments   yamlEnvironment `yaml:"environments"`
	CookieJar      struct {
		Cookies []yaml.Node `yaml:"cookies"`
	} `yaml:"cookieJar"`
}

type metadata struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
}

type yamlItem struct {
	resource         `yaml:",inline"`
	Meta             metadata   `yaml:"meta"`
	Children         []yamlItem `yaml:"children"`
	ConfidentialKeys []string   `yaml:"confidentialKeys"`
}

type yamlEnvironment struct {
	Name             string            `yaml:"name"`
	Color            string            `yaml:"color"`
	Data             map[string]any    `yaml:"data"`
	ConfidentialKeys []string          `yaml:"confidentialKeys"`
	SubEnvironments  []yamlEnvironment `yaml:"subEnvironments"`
}

func importV5(data []byte) (domain.Document, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil && err != io.EOF {
		return domain.Document{}, domain.Refuse(domain.CodeFileUnreadable, domain.ErrNotAllowed, nil)
	}
	var header struct {
		Type string `yaml:"type"`
	}
	if err := root.Decode(&header); err != nil {
		return domain.Document{}, domain.Refuse(domain.CodeNotInsomnia, domain.ErrNotAllowed, nil)
	}
	switch header.Type {
	case "collection.insomnia.rest/5.0", "spec.insomnia.rest/5.0", "environment.insomnia.rest/5.0":
	default:
		return domain.Document{}, domain.Refuse(domain.CodeNotInsomnia, domain.ErrNotAllowed, nil)
	}
	var file yamlExport
	if err := root.Decode(&file); err != nil || decoder.Decode(&root) != io.EOF {
		return domain.Document{}, domain.Refuse(domain.CodeFileUnreadable, domain.ErrNotAllowed, nil)
	}

	var out domain.Document
	base := yamlVariables(file.Environments.Data, file.Environments.ConfidentialKeys)
	if len(base) > 0 {
		out.Environments = append(out.Environments, domain.EnvironmentDraft{
			Name:      cmp.Or(strings.TrimSpace(file.Environments.Name), strings.TrimSpace(file.Name), baseEnvironment),
			Color:     file.Environments.Color,
			Variables: base,
		})
	}
	for _, environment := range file.Environments.SubEnvironments {
		name := strings.TrimSpace(environment.Name)
		if name == "" {
			continue
		}
		own := yamlVariables(environment.Data, environment.ConfidentialKeys)
		out.Environments = append(out.Environments, domain.EnvironmentDraft{
			Name:      name,
			Color:     environment.Color,
			Variables: renumber(append(own, missing(base, own)...)),
		})
	}

	if len(file.Collection) > 0 {
		collection := domain.Collection{
			Name:        cmp.Or(strings.TrimSpace(file.Name), "Insomnia"),
			Description: strings.TrimSpace(file.Meta.Description),
			Auth:        requestAuth(file.Authentication, &out),
		}
		// The base supplies defaults for its own collection even when no environment is selected.
		for _, variable := range base {
			if variable.Kind != domain.VariableSecret {
				collection.Variables = append(collection.Variables, variable)
			}
		}
		collection.Variables = renumber(collection.Variables)
		readYAMLLevel(file.Collection, &collection, &out)
		if len(collection.Items) > 0 || len(collection.Children) > 0 {
			out.Collections = []domain.Collection{collection}
		}
	}
	out.LeftOut(domain.CodeSkippedCookieJar, len(file.CookieJar.Cookies))
	if len(out.Collections) == 0 && len(out.Environments) == 0 {
		return domain.Document{}, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	return out, nil
}

func readYAMLLevel(items []yamlItem, into *domain.Collection, out *domain.Document) {
	for index, item := range items {
		item.Description = cmp.Or(item.Meta.Description, item.Description)
		switch {
		case strings.HasPrefix(item.Meta.ID, "fld_") || (item.Children != nil && item.Method == "" && item.URL == ""):
			folder := domain.Collection{
				Name:        cmp.Or(strings.TrimSpace(item.Name), "Untitled Folder"),
				Description: strings.TrimSpace(item.Description),
				Position:    int64(index),
				Auth:        requestAuth(item.Auth, out),
			}
			for _, variable := range yamlVariables(item.Environment, item.ConfidentialKeys) {
				if variable.Kind == domain.VariableSecret {
					out.LeftOut(domain.CodeSkippedSecretValue, 1)
					continue
				}
				folder.Variables = append(folder.Variables, variable)
			}
			folder.Variables = renumber(folder.Variables)
			readYAMLLevel(item.Children, &folder, out)
			into.Children = append(into.Children, folder)
		case item.Method != "" || strings.HasPrefix(item.Meta.ID, "req_"):
			node := requestNode(item.resource, out)
			node.Position = int64(index)
			into.Items = append(into.Items, node)
		}
	}
}

func yamlVariables(data map[string]any, confidential []string) []domain.Variable {
	variables := variablesOf(data)
	for i := range variables {
		if slices.Contains(confidential, variables[i].Name) {
			variables[i].Kind = domain.VariableSecret
		}
	}
	return variables
}
