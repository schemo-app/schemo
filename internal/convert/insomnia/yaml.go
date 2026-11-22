package insomnia

import (
	"bytes"
	"cmp"
	"io"
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
	Name             string         `yaml:"name"`
	Color            string         `yaml:"color"`
	Data             map[string]any `yaml:"data"`
	ConfidentialKeys []string       `yaml:"confidentialKeys"`
	IsPrivate        bool           `yaml:"isPrivate"`
	Meta             struct {
		IsPrivate bool `yaml:"isPrivate"`
	} `yaml:"meta"`
	SubEnvironments []yamlEnvironment `yaml:"subEnvironments"`
}

// private is where v5 files put the flag: on the environment or in its meta.
func (e yamlEnvironment) private() bool { return e.IsPrivate || e.Meta.IsPrivate }

// environmentFile is v5's own type for environments that belong to no collection.
const environmentFile = "environment.insomnia.rest/5.0"

// decodeV5 reads a v5 document and the type it declares.
func decodeV5(data []byte) (yamlExport, string, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil && err != io.EOF {
		return yamlExport{}, "", domain.Refuse(domain.CodeFileUnreadable, domain.ErrNotAllowed, nil)
	}
	var header struct {
		Type string `yaml:"type"`
	}
	if err := root.Decode(&header); err != nil {
		return yamlExport{}, "", domain.Refuse(domain.CodeNotInsomnia, domain.ErrNotAllowed, nil)
	}
	switch header.Type {
	case "collection.insomnia.rest/5.0", "spec.insomnia.rest/5.0", environmentFile:
	default:
		return yamlExport{}, "", domain.Refuse(domain.CodeNotInsomnia, domain.ErrNotAllowed, nil)
	}
	var file yamlExport
	if err := root.Decode(&file); err != nil || decoder.Decode(&root) != io.EOF {
		return yamlExport{}, "", domain.Refuse(domain.CodeFileUnreadable, domain.ErrNotAllowed, nil)
	}
	return file, header.Type, nil
}

func importV5(data []byte) (domain.Document, error) {
	file, kind, err := decodeV5(data)
	if err != nil {
		return domain.Document{}, err
	}

	var out domain.Document
	base := file.Environments
	drafts := []domain.EnvironmentDraft{}
	for _, sub := range base.SubEnvironments {
		if name := strings.TrimSpace(sub.Name); name != "" {
			drafts = append(drafts, subDraft(name, sub.Color,
				secrets(variablesOf(sub.Data, &out), sub.ConfidentialKeys, sub.private())))
		}
	}

	if kind == environmentFile {
		// No collection to hold blocks: the base always applies, the subs apply to every collection.
		out.Globals = secrets(variablesOf(base.Data, &out), base.ConfidentialKeys, false)
		for _, draft := range drafts {
			out.Environments = append(out.Environments, domain.EnvironmentDraft{
				Name: draft.Name, Color: draft.Color, Variables: draft.Blocks[0].Variables})
		}
	} else {
		collection := domain.Collection{
			Name:        cmp.Or(strings.TrimSpace(file.Name), untitledCollection),
			Description: strings.TrimSpace(file.Meta.Description),
			Auth:        requestAuth(file.Authentication, &out),
		}
		vars := newValues()
		vars.add(plain(secrets(variablesOf(base.Data, &out), base.ConfidentialKeys, false), &out), &out)
		readYAMLLevel(file.Collection, &collection, vars, &out)
		collection.Variables = vars.list
		if len(collection.Items) > 0 || len(collection.Children) > 0 ||
			len(collection.Variables) > 0 || len(drafts) > 0 {
			out.Collections = []domain.Collection{collection}
			out.Environments = blocksFor(0, drafts)
		}
	}
	out.LeftOut(domain.CodeSkippedCookieJar, len(file.CookieJar.Cookies))
	if len(out.Collections) == 0 && len(out.Environments) == 0 && len(out.Globals) == 0 {
		return domain.Document{}, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	return out, nil
}

func readYAMLLevel(items []yamlItem, into *domain.Collection, vars *values, out *domain.Document) {
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
			// Folders hold no variables here; theirs join the collection's.
			vars.add(plain(secrets(variablesOf(item.Environment, out), item.ConfidentialKeys, false), out), out)
			readYAMLLevel(item.Children, &folder, vars, out)
			into.Children = append(into.Children, folder)
		case item.Method != "" || strings.HasPrefix(item.Meta.ID, "req_"):
			node := requestNode(item.resource, out)
			node.Position = int64(index)
			into.Items = append(into.Items, node)
		}
	}
}
