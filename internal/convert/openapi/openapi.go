// Package openapi reads an OpenAPI 3.x or Swagger 2.0 document as a collection: paths become
// requests, tags become folders and the servers become the variable every address is written
// against. JSON and YAML decode into one tree and are read once. It is a pure package, so a
// document is read in a test.
package openapi

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"schemo/internal/domain"
)

// methods are the operations a path can hold. Swagger 2.0 has no `trace`, and `default` is not a
// method at all.
var methods = []string{"get", "post", "put", "patch", "delete", "head", "options"}

// loneHole is an address that is one placeholder and nothing else: `<<baseUrl>>`, `{{baseUrl}}`,
// `{baseUrl}` — the shapes a document leaves for whoever reads it to fill in.
var loneHole = regexp.MustCompile(`^(?:<<[^<>]+>>|\{\{[^{}]+\}\}|\{[^{}]+\})$`)

// Import reads a document as one collection. A file that is neither version is refused rather than
// guessed at.
func Import(data []byte) (domain.Document, error) {
	tree, err := decode(data)
	if err != nil {
		return domain.Document{}, err
	}
	var doc document
	if err := remarshal(tree, &doc); err != nil {
		return domain.Document{}, domain.Refuse(domain.CodeNotOpenAPI, domain.ErrNotAllowed, nil)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.") && doc.Swagger != "2.0" {
		return domain.Document{}, domain.Refuse(domain.CodeNotOpenAPI, domain.ErrNotAllowed, nil)
	}

	var out domain.Document
	out.LeftOut(domain.CodeSkippedExternalRef, resolve(tree, tree))
	// The references are spliced in, so the walk below reads one document with no pointers left.
	if err := remarshal(tree, &doc); err != nil {
		return domain.Document{}, domain.Refuse(domain.CodeNotOpenAPI, domain.ErrNotAllowed, nil)
	}

	read := &reader{
		doc:       doc,
		doc2:      doc.Swagger == "2.0",
		out:       &out,
		variables: map[string]domain.Variable{},
	}
	collection := read.collection()
	if len(collection.Items) == 0 && len(collection.Children) == 0 {
		return domain.Document{}, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	out.Collections = append(out.Collections, collection)
	return out, nil
}

// decode turns the bytes into one tree. A YAML document is normalized through JSON so that the two
// notations are the same values — one set of struct tags, one read.
func decode(data []byte) (map[string]any, error) {
	var tree map[string]any
	if json.Unmarshal(data, &tree) == nil {
		return tree, nil
	}

	var fromYAML any
	if err := yaml.Unmarshal(data, &fromYAML); err != nil {
		return nil, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
	}
	normalized, err := json.Marshal(fromYAML)
	if err != nil {
		return nil, domain.Refuse(domain.CodeNotOpenAPI, domain.ErrNotAllowed, nil)
	}
	if err := json.Unmarshal(normalized, &tree); err != nil {
		return nil, domain.Refuse(domain.CodeNotOpenAPI, domain.ErrNotAllowed, nil)
	}
	return tree, nil
}

func remarshal(tree map[string]any, into any) error {
	data, err := json.Marshal(tree)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

type document struct {
	OpenAPI    string        `json:"openapi,omitempty"`
	Swagger    string        `json:"swagger,omitempty"`
	Info       info          `json:"info"`
	Servers    []server      `json:"servers,omitempty"`
	Tags       []tag         `json:"tags,omitempty"`
	Paths      pathMap       `json:"paths"`
	Security   []requirement `json:"security,omitempty"`
	Components *components   `json:"components,omitempty"`

	// FolderTags is Hoppscotch's marker that this document's tags are folder paths: `a/b` reads as a
	// folder `b` inside a folder `a`.
	FolderTags string `json:"x-hoppscotch-folder-tags,omitempty"`

	// Swagger 2.0 keeps the same things in other places.
	Host        string                    `json:"host,omitempty"`
	BasePath    string                    `json:"basePath,omitempty"`
	Schemes     []string                  `json:"schemes,omitempty"`
	SwaggerDefs map[string]map[string]any `json:"securityDefinitions,omitempty"`
}

type info struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type server struct {
	URL       string            `json:"url,omitempty"`
	Variables map[string]srvVar `json:"variables,omitempty"`
}

type srvVar struct {
	Default string `json:"default,omitempty"`
}

type tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type components struct {
	SecuritySchemes map[string]map[string]any `json:"securitySchemes,omitempty"`
}

// requirement is one `security` entry: scheme names with the scopes each is used with. Generators
// also write a bare array of names (`["bearerAuth"]`), which names no declared scheme: it is read
// as empty and the request has no authorization, as in Hoppscotch.
type requirement map[string][]string

func (r *requirement) UnmarshalJSON(data []byte) error {
	var object map[string][]string
	if err := json.Unmarshal(data, &object); err == nil {
		*r = object
		return nil
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return err
	}
	*r = map[string][]string{}
	return nil
}

func (r requirement) first() (string, bool) {
	names := slices.Sorted(maps.Keys(r))
	if len(names) == 0 {
		return "", false
	}
	return names[0], true
}

// pathMap reads `paths` in a fixed order: Go walks a map in no order at all, and a collection whose
// rows moved between two imports of one file would be a collection nobody could compare.
type pathMap map[string]pathItem

type pathItem struct {
	Ref        string
	Parameters []parameter
	Operations map[string]operation
}

// UnmarshalJSON reads a path item's methods and keeps its parameters: the methods are named by the
// document, so they cannot be fields of a struct that would also have to hold `summary` and `$ref`.
func (p *pathItem) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.Operations = map[string]operation{}
	for name, value := range raw {
		switch name {
		case "$ref":
			if err := json.Unmarshal(value, &p.Ref); err != nil {
				return err
			}
		case "parameters":
			if err := json.Unmarshal(value, &p.Parameters); err != nil {
				return err
			}
		case "summary", "description", "servers":
		default:
			var op operation
			if err := json.Unmarshal(value, &op); err != nil {
				return err
			}
			p.Operations[name] = op
		}
	}
	return nil
}

type operation struct {
	OperationID string `json:"operationId,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
	// Not the specification's key, but one some generators write a name under — Hoppscotch reads it.
	Title       string              `json:"title,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Parameters  []parameter         `json:"parameters,omitempty"`
	RequestBody *requestBody        `json:"requestBody,omitempty"`
	Responses   map[string]response `json:"responses,omitempty"`
	Security    []requirement       `json:"security,omitempty"`
	Consumes    []string            `json:"consumes,omitempty"`
}

type parameter struct {
	Ref         string             `json:"$ref,omitempty"`
	Name        string             `json:"name,omitempty"`
	In          string             `json:"in,omitempty"`
	Required    bool               `json:"required,omitempty"`
	Description string             `json:"description,omitempty"`
	Example     any                `json:"example,omitempty"`
	Examples    map[string]example `json:"examples,omitempty"`
	Schema      map[string]any     `json:"schema,omitempty"`
	// Swagger 2.0 writes the type and its default on the parameter itself.
	Type    string `json:"type,omitempty"`
	Default any    `json:"default,omitempty"`
	Enum    []any  `json:"enum,omitempty"`
}

type example struct {
	Value any `json:"value,omitempty"`
}

type requestBody struct {
	Ref      string               `json:"$ref,omitempty"`
	Required bool                 `json:"required,omitempty"`
	Content  map[string]mediaType `json:"content,omitempty"`
}

// response is one declared answer: what the document describes it as, and the body under a media
// type (3.x) or an example per media type beside a schema (2.0).
type response struct {
	Description string               `json:"description,omitempty"`
	Content     map[string]mediaType `json:"content,omitempty"`
	Schema      map[string]any       `json:"schema,omitempty"`
	Examples    map[string]any       `json:"examples,omitempty"`
}

type mediaType struct {
	Schema   map[string]any     `json:"schema,omitempty"`
	Example  any                `json:"example,omitempty"`
	Examples map[string]example `json:"examples,omitempty"`
}
