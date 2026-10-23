// Package schemofile reads and writes this app's own collection file, the one format that loses
// nothing: folders, authorization, scripts, variables with their kinds, and environments beside the
// collections. It is a pure package, so both directions are tested against a file.
package schemofile

import (
	"encoding/json"
	"strings"

	"schemo/internal/domain"
)

// Version is what a file written here says it is. A file of a version this build does not know is
// refused rather than read as if it were this one.
const Version = "1.0"

type file struct {
	Schemo       string        `json:"schemo"`
	Collections  []level       `json:"collections,omitempty"`
	Environments []environment `json:"environments,omitempty"`
}

// level is a collection or one of the collections inside it. Ids and timestamps are not written:
// they name rows of this database, and a file that carries them promises a tree it cannot keep.
type level struct {
	Name        string          `json:"name"`
	Position    int64           `json:"position"`
	Description string          `json:"description,omitempty"`
	Auth        *domain.Auth    `json:"auth,omitempty"`
	Variables   []variable      `json:"variables,omitempty"`
	Scripts     *domain.Scripts `json:"scripts,omitempty"`
	Items       []request       `json:"requests,omitempty"`
	Children    []level         `json:"collections,omitempty"`
}

type request struct {
	Name        string                   `json:"name"`
	Method      string                   `json:"method,omitempty"`
	URL         string                   `json:"url,omitempty"`
	Params      []row                    `json:"params,omitempty"`
	Headers     []row                    `json:"headers,omitempty"`
	PathParams  []row                    `json:"pathParams,omitempty"`
	Body        string                   `json:"body,omitempty"`
	BodyKind    domain.BodyKind          `json:"bodyKind,omitempty"`
	Form        []formRow                `json:"form,omitempty"`
	BodyFile    string                   `json:"bodyFile,omitempty"`
	Auth        *domain.Auth             `json:"auth,omitempty"`
	Scripts     *domain.Scripts          `json:"scripts,omitempty"`
	Description string                   `json:"description,omitempty"`
	Responses   []domain.ResponseExample `json:"responses,omitempty"`
	Position    int64                    `json:"position"`
}

type row struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
}

type formRow struct {
	Name    string `json:"name"`
	Value   string `json:"value,omitempty"`
	Src     string `json:"src,omitempty"`
	File    bool   `json:"file,omitempty"`
	Enabled bool   `json:"enabled"`
}

// variable is what a level answers for `{{tokens}}`. A secret keeps its name and kind and loses its
// value unless values were asked for: this is the one file the app writes that can carry one, and
// only on request.
type variable struct {
	Name     string              `json:"name"`
	Value    string              `json:"value,omitempty"`
	Kind     domain.VariableKind `json:"kind,omitempty"`
	Position int                 `json:"position"`
	Enabled  bool                `json:"enabled"`
	Secret   bool                `json:"secret,omitempty"`
}

type environment struct {
	Name      string     `json:"name"`
	Color     string     `json:"color,omitempty"`
	Variables []variable `json:"variables,omitempty"`
}

// Write writes the document as a file. Values of secrets are left out unless secrets is set — the
// name and the kind stay, so the other side knows the variable is there and is to be filled in.
func Write(doc domain.Document, secrets bool) ([]byte, error) {
	out := file{Schemo: Version, Collections: []level{}, Environments: []environment{}}
	for _, collection := range doc.Collections {
		written := writtenLevel(collection, secrets)
		written.Position = collection.Position
		out.Collections = append(out.Collections, written)
	}
	for _, draft := range doc.Environments {
		written := environment{Name: draft.Name, Color: draft.Color}
		written.Variables = writtenVariables(draft.Variables, secrets)
		out.Environments = append(out.Environments, written)
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func writtenLevel(collection domain.Collection, secrets bool) level {
	out := level{
		Name:        collection.Name,
		Description: collection.Description,
		Auth:        collection.Auth,
		Variables:   writtenVariables(collection.Variables, secrets),
		Scripts:     collection.Scripts,
		Items:       []request{},
		Children:    []level{},
	}
	for _, entry := range collection.Level() {
		if entry.Collection != nil {
			nested := writtenLevel(*entry.Collection, secrets)
			nested.Position = entry.Collection.Position
			out.Children = append(out.Children, nested)
			continue
		}
		out.Items = append(out.Items, writtenRequest(*entry.Node))
	}
	return out
}

func writtenRequest(node domain.CollectionNode) request {
	out := request{
		Name:        node.Name,
		Method:      node.Method,
		URL:         node.URL,
		Body:        node.Body,
		BodyKind:    node.BodyKind,
		BodyFile:    node.BodyFile,
		Auth:        node.Auth,
		Scripts:     node.Scripts,
		Description: node.Description,
		Responses:   node.Responses,
		Position:    node.Position,
		Params:      writtenRows(node.Params),
		Headers:     writtenRows(node.Headers),
		PathParams:  writtenRows(node.PathParams),
		Form:        []formRow{},
	}
	for _, one := range node.Form {
		out.Form = append(out.Form, formRow{
			Name: one.Name, Value: one.Value, Src: one.Src, File: one.File, Enabled: one.Enabled,
		})
	}
	return out
}

func writtenRows(rows []domain.Row) []row {
	out := []row{}
	for _, one := range rows {
		out = append(out, row{
			Name: one.Name, Value: one.Value, Enabled: one.Enabled, Description: one.Description,
		})
	}
	return out
}

func readRows(rows []row) []domain.Row {
	var out []domain.Row
	for _, one := range rows {
		out = append(out, domain.Row{
			Name: one.Name, Value: one.Value, Enabled: one.Enabled, Description: one.Description,
		})
	}
	return out
}

func writtenVariables(variables []domain.Variable, secrets bool) []variable {
	out := make([]variable, 0, len(variables))
	for _, v := range variables {
		if strings.TrimSpace(v.Name) == "" {
			continue
		}
		one := variable{
			Name:     v.Name,
			Kind:     v.Kind,
			Enabled:  v.Enabled,
			Position: v.Position,
		}
		hidden := v.Kind == domain.VariableSecret && !secrets
		one.Secret = hidden
		if !hidden {
			one.Value = v.Value
		}
		out = append(out, one)
	}
	return out
}

// Read reads a Schemo file as the document it holds. The version is checked before anything else is
// looked at: a file written by a later build may mean things this one would drop without saying so.
func Read(data []byte) (domain.Document, error) {
	var parsed file
	if err := json.Unmarshal(data, &parsed); err != nil {
		return domain.Document{}, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
	}
	if parsed.Schemo != Version {
		return domain.Document{}, domain.Refuse(domain.CodeNotSchemo, domain.ErrNotAllowed, nil)
	}

	doc := domain.Document{}
	for _, one := range parsed.Collections {
		collection := readLevel(one)
		doc.Collections = append(doc.Collections, collection)
	}
	for _, one := range parsed.Environments {
		draft := domain.EnvironmentDraft{Name: one.Name, Color: one.Color}
		for _, v := range one.Variables {
			kind := v.Kind
			if v.Secret {
				kind = domain.VariableSecret
			}
			draft.Variables = append(draft.Variables, domain.Variable{
				Name:     v.Name,
				Value:    v.Value,
				Kind:     kind,
				Enabled:  v.Enabled,
				Position: v.Position,
			})
		}
		doc.Environments = append(doc.Environments, draft)
	}
	if len(doc.Collections) == 0 && len(doc.Environments) == 0 {
		return domain.Document{}, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	return doc, nil
}

func readLevel(from level) domain.Collection {
	out := domain.Collection{
		Name:        from.Name,
		Position:    from.Position,
		Description: from.Description,
		Auth:        from.Auth,
		Variables:   readVariables(from.Variables),
		Scripts:     from.Scripts,
	}
	for _, one := range from.Items {
		node := domain.CollectionNode{
			Name:        one.Name,
			Position:    one.Position,
			Method:      one.Method,
			URL:         one.URL,
			Body:        one.Body,
			BodyKind:    one.BodyKind,
			BodyFile:    one.BodyFile,
			Auth:        one.Auth,
			Scripts:     one.Scripts,
			Description: one.Description,
			Responses:   one.Responses,
			Params:      readRows(one.Params),
			Headers:     readRows(one.Headers),
			PathParams:  readRows(one.PathParams),
		}
		for _, row := range one.Form {
			node.Form = append(node.Form, domain.FormRow{
				Name: row.Name, Value: row.Value, Src: row.Src, File: row.File, Enabled: row.Enabled,
			})
		}
		out.Items = append(out.Items, node)
	}
	for _, one := range from.Children {
		out.Children = append(out.Children, readLevel(one))
	}
	return out
}

func readVariables(from []variable) []domain.Variable {
	out := make([]domain.Variable, 0, len(from))
	for index, v := range from {
		kind := v.Kind
		if v.Secret {
			kind = domain.VariableSecret
		}
		if kind == "" {
			kind = domain.VariableText
		}
		position := v.Position
		if position == 0 {
			position = index + 1
		}
		out = append(out, domain.Variable{
			Name:     v.Name,
			Value:    v.Value,
			Kind:     kind,
			Enabled:  v.Enabled,
			Position: position,
		})
	}
	return out
}
