package postman

import (
	"encoding/json"
	"strings"

	"schemo/internal/domain"
)

// Import reads a Postman file as the document it holds — a collection, an environment or the
// globals — told apart by what the file says it is.
func Import(data []byte) (domain.Document, error) {
	var probe struct {
		Scope string `json:"_postman_variable_scope"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return domain.Document{}, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
	}

	var doc domain.Document
	switch probe.Scope {
	case scopeEnvironment:
		draft, err := readEnvironment(data)
		if err != nil {
			return domain.Document{}, err
		}
		doc.Environments = append(doc.Environments, draft)
		return doc, nil
	case scopeGlobals:
		draft, err := readEnvironment(data)
		if err != nil {
			return domain.Document{}, err
		}
		doc.Globals = draft.Variables
		return doc, nil
	}

	collection, err := readCollection(data, &doc)
	if err != nil {
		return domain.Document{}, err
	}
	doc.Collections = append(doc.Collections, collection)
	return doc, nil
}

// readCollection leaves ids empty: minting them is the tree's business, not this package's.
func readCollection(data []byte, doc *domain.Document) (domain.Collection, error) {
	var file collectionFile
	if err := json.Unmarshal(data, &file); err != nil {
		return domain.Collection{}, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
	}
	if file.Info.Name == "" && len(file.Item) == 0 {
		return domain.Collection{}, domain.Refuse(domain.CodeNotPostman, domain.ErrNotAllowed, nil)
	}

	imported := domain.Collection{
		Name:    file.Info.Name,
		Scripts: scriptOf(file.Event),
	}
	vars := &values{seen: map[string]string{}}
	vars.add(importedVariables(file.Variable, doc), doc)
	if file.Info.Description != nil {
		imported.Description = strings.TrimSpace(file.Info.Description.Value)
	}
	if file.Description != nil && imported.Description == "" {
		imported.Description = strings.TrimSpace(file.Description.Value)
	}
	if file.Auth != nil {
		imported.Auth = authOf(*file.Auth)
	}
	readLevel(file.Item, &imported, vars, doc)
	imported.Variables = domain.OrEmpty(vars.list)
	return imported, nil
}

// importedVariables keeps the file's order (the last answer wins) and skips nameless rows. A secret
// arrives as its name without a value, since collection variables cannot be secret here.
func importedVariables(from []variable, doc *domain.Document) []domain.Variable {
	out := []domain.Variable{}
	for _, v := range from {
		name := strings.TrimSpace(v.Key)
		if name == "" {
			continue
		}
		value := domain.ValueText(v.Value)
		if v.secret() {
			value = ""
			doc.LeftOut(domain.CodeSkippedSecretValue, 1)
		}
		out = append(out, domain.Variable{
			Name:     name,
			Value:    value,
			Kind:     domain.VariableText,
			Enabled:  v.switchedOn(),
			Position: len(out) + 1,
		})
	}
	return out
}

func described(from *text) string {
	if from == nil {
		return ""
	}
	return strings.TrimSpace(from.Value)
}

func scriptOf(events []event) *domain.Scripts {
	var pre []string
	var post []string
	off := domain.Scripts{}
	for _, e := range events {
		code := strings.Join(e.Script.Exec, "\n")
		if strings.TrimSpace(code) == "" {
			continue
		}
		switch e.Listen {
		case "prerequest":
			pre = append(pre, code)
			off.PreOff = off.PreOff || e.Disabled
		case "test":
			post = append(post, code)
			off.PostOff = off.PostOff || e.Disabled
		}
	}
	if len(pre) == 0 && len(post) == 0 {
		return nil
	}
	return &domain.Scripts{
		Pre:     strings.Join(pre, "\n\n"),
		Post:    strings.Join(post, "\n\n"),
		PreOff:  off.PreOff,
		PostOff: off.PostOff,
	}
}

func readLevel(items []item, into *domain.Collection, vars *values, doc *domain.Document) {
	into.Items = []domain.CollectionNode{}
	into.Children = []domain.Collection{}

	for i, entry := range items {
		position := int64(i)
		if entry.Request != nil {
			into.Items = append(into.Items, requestNode(entry, position, *entry.Request))
			continue
		}

		nested := domain.Collection{
			Name:     entry.Name,
			Position: position,
			Scripts:  scriptOf(entry.Event),
		}
		// Folders hold no variables here; theirs join the collection's.
		vars.add(importedVariables(entry.Variable, doc), doc)
		if entry.Description != nil {
			nested.Description = strings.TrimSpace(entry.Description.Value)
		}
		if entry.Auth != nil {
			nested.Auth = authOf(*entry.Auth)
		}
		readLevel(entry.Item, &nested, vars, doc)
		into.Children = append(into.Children, nested)
	}
}

// pathRows fills the address's `:name` segments; declared names missing from the address are skipped.
func pathRows(from url) []domain.Row {
	kept := map[string]field{}
	for _, one := range from.Variable {
		kept[one.Key] = one
	}
	rows := []domain.Row{}
	for _, name := range domain.PathParams(from.Raw) {
		one := kept[name]
		rows = append(rows, domain.Row{
			Name: name, Value: one.Value, Enabled: true, Description: described(one.Description),
		})
	}
	return rows
}

func requestNode(entry item, position int64, from request) domain.CollectionNode {
	node := domain.CollectionNode{
		Name:     entry.Name,
		Position: position,
		Method:   strings.ToUpper(strings.TrimSpace(from.Method)),
		URL:      from.URL.Raw,
		Scripts:  scriptOf(entry.Event),
	}
	if from.Description != nil {
		node.Description = strings.TrimSpace(from.Description.Value)
	}
	if node.Description == "" && entry.Description != nil {
		node.Description = strings.TrimSpace(entry.Description.Value)
	}

	for _, header := range from.Header {
		if strings.TrimSpace(header.Key) == "" {
			continue
		}
		node.Headers = append(node.Headers, domain.Row{
			Name: header.Key, Value: header.Value, Enabled: !header.Disabled,
			Description: described(header.Description),
		})
	}
	// A disabled parameter is not in the address, so it is read from the list Postman keeps beside
	// it — the same list the export writes it back into.
	for _, query := range from.URL.Query {
		if strings.TrimSpace(query.Key) == "" {
			continue
		}
		node.Params = append(node.Params, domain.Row{
			Name: query.Key, Value: query.Value, Enabled: !query.Disabled,
			Description: described(query.Description),
		})
	}
	node.PathParams = pathRows(from.URL)
	applyBody(&node, from.Body)
	if from.Auth != nil {
		if auth := authOf(*from.Auth); auth != nil {
			node.Auth = auth
		}
	}

	// Saved answers become examples; Postman's NUL padding is stripped.
	for _, saved := range entry.Response {
		one := domain.ResponseExample{
			Name:   strings.TrimSpace(saved.Name),
			Status: saved.Code,
			Body:   strings.ReplaceAll(saved.Body, "\x00", ""),
		}
		for _, header := range saved.Header {
			if strings.TrimSpace(header.Key) == "" {
				continue
			}
			one.Headers = append(one.Headers, domain.HeaderPair{
				Name: header.Key, Value: header.Value,
			})
		}
		node.Responses = append(node.Responses, one)
	}
	return node
}

func applyBody(node *domain.CollectionNode, from *body) {
	if from == nil {
		return
	}
	switch from.Mode {
	case "raw":
		node.BodyKind = domain.BodyRaw
		node.Body = from.Raw
	case "urlencoded":
		// The app composes no urlencoded kind, so this arrives as the text it means.
		node.BodyKind = domain.BodyRaw
		parts := make([]string, 0, len(from.URLEncoded))
		for _, row := range from.URLEncoded {
			if row.Disabled {
				continue
			}
			parts = append(parts, row.Key+"="+row.Value)
		}
		node.Body = strings.Join(parts, "&")
	case "formdata":
		node.BodyKind = domain.BodyForm
		for _, row := range from.FormData {
			node.Form = append(node.Form, domain.FormRow{
				Name:    row.Key,
				Value:   row.Value,
				Src:     row.Src,
				File:    row.Src != "",
				Enabled: !row.Disabled,
			})
		}
	case "file":
		node.BodyKind = domain.BodyBinary
		if from.File != nil {
			node.BodyFile = from.File.Src
		}
	}
}

// authOf leaves unknown schemes nil so the request inherits; `noauth` is `none`, or it would inherit
// the collection's credentials.
func authOf(from auth) *domain.Auth {
	if from.Type == "noauth" {
		return &domain.Auth{Type: domain.AuthNone}
	}
	kind, written, ok := schemeOf(from.Type)
	if !ok {
		return nil
	}
	fields := map[string]string{}
	for key, name := range written.Fields {
		fields[key] = fieldValue(from.Extra[written.Name], name)
	}
	if kind == domain.AuthOAuth2 && fields["grant"] == "" {
		fields["grant"] = domain.GrantAuthorizationCode
	}
	return &domain.Auth{Type: kind, Fields: fields}
}

func fieldValue(rows []field, key string) string {
	for _, row := range rows {
		if row.Key == key {
			return row.Value
		}
	}
	return ""
}

// values gathers a collection's variables from its levels; a name keeps its first value.
type values struct {
	list []domain.Variable
	seen map[string]string
}

func (v *values) add(vars []domain.Variable, doc *domain.Document) {
	for _, one := range vars {
		if held, ok := v.seen[one.Name]; ok {
			if held != one.Value {
				doc.LeftOut(domain.CodeSkippedVariableClash, 1)
			}
			continue
		}
		v.seen[one.Name] = one.Value
		one.Position = len(v.list) + 1
		v.list = append(v.list, one)
	}
}
