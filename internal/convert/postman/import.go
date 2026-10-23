package postman

import (
	"encoding/json"
	"strings"

	"schemo/internal/domain"
)

// Import reads a Postman file as the document it holds, a collection or one environment, told apart
// by what the file says it is. A file that is neither is refused rather than guessed at.
func Import(data []byte) (domain.Document, error) {
	var probe struct {
		Scope string `json:"_postman_variable_scope"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return domain.Document{}, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
	}

	var doc domain.Document
	if probe.Scope == "environment" {
		draft, err := readEnvironment(data)
		if err != nil {
			return domain.Document{}, err
		}
		doc.Environments = append(doc.Environments, draft)
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
		Name:      file.Info.Name,
		Variables: importedVariables(file.Variable, doc),
		Scripts:   scriptOf(file.Event),
	}
	if file.Info.Description != nil {
		imported.Description = strings.TrimSpace(file.Info.Description.Value)
	}
	if file.Description != nil && imported.Description == "" {
		imported.Description = strings.TrimSpace(file.Description.Value)
	}
	if file.Auth != nil {
		imported.Auth = authOf(*file.Auth)
	}
	readLevel(file.Item, &imported, doc)
	return imported, nil
}

// importedVariables reads what a level answers for `{{tokens}}` in the order the file wrote them:
// the editor draws that order and the nearest answer is the last, so it is not sorted. A row with
// no name is an empty row, not a variable.
//
// A value the file marks secret arrives as its name with nothing in it: a collection variable
// cannot be secret here (it leaves the app when exported), and dropping the name would lose one the
// requests still ask for. The count of those left behind goes into the summary.
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

func readLevel(items []item, into *domain.Collection, doc *domain.Document) {
	into.Items = []domain.CollectionNode{}
	into.Children = []domain.Collection{}

	for i, entry := range items {
		position := int64(i)
		if entry.Request != nil {
			into.Items = append(into.Items, requestNode(entry, position, *entry.Request))
			continue
		}

		nested := domain.Collection{
			Name:      entry.Name,
			Position:  position,
			Variables: importedVariables(entry.Variable, doc),
			Scripts:   scriptOf(entry.Event),
		}
		if entry.Description != nil {
			nested.Description = strings.TrimSpace(entry.Description.Value)
		}
		if entry.Auth != nil {
			nested.Auth = authOf(*entry.Auth)
		}
		readLevel(entry.Item, &nested, doc)
		into.Children = append(into.Children, nested)
	}
}

// pathRows are the rows that fill the `:name` segments of an address: one per name it holds, with
// the value and description Postman kept. A name Postman declares but the address does not hold is
// not a segment.
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

	// The saved answers travel as read-only examples beside the request the file put them with.
	// Postman pads a body with NUL bytes where the response had them, and a NUL is not text this
	// app can carry.
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

// A scheme this app does not have is left out, so a level with no auth of its own inherits.
// `noauth` is not unknown: it says this request sends nothing, and left as nil it would inherit and
// send the collection's credentials.
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
		// Postman writes no grant. Reading the scheme as client credentials would ask for the wrong
		// flow; Postman's own dialog runs authorization code, which is also how Hoppscotch imports
		// it.
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

// readEnvironment reads an environment export, the file Postman writes from the environments tab:
// not a collection, no requests. The globals scope is refused rather than read as an environment.
func readEnvironment(data []byte) (domain.EnvironmentDraft, error) {
	var file struct {
		Name   string     `json:"name"`
		Scope  string     `json:"_postman_variable_scope"`
		Values []variable `json:"values"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return domain.EnvironmentDraft{}, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
	}
	if file.Scope != "environment" || (file.Name == "" && len(file.Values) == 0) {
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
