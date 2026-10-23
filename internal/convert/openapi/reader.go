package openapi

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"schemo/internal/domain"
)

// reader holds what the walk needs beside the document: the variables it declares as it goes, the
// folders it opens, and the order the document declared its tags in.
type reader struct {
	doc       document
	doc2      bool
	out       *domain.Document
	variables map[string]domain.Variable
	root      *folderNode
	topItems  int64
}

func (r *reader) collection() domain.Collection {
	base := r.baseURL()
	r.declare(domain.BaseURLVariable, base, base == "")

	// The operations first, because the folders are built out of the tags they and the document
	// carry: every tag is read once, before anything is placed.
	type operationAt struct {
		node domain.CollectionNode
		tags []string
	}
	operations := []operationAt{}
	for _, path := range slices.Sorted(maps.Keys(r.doc.Paths)) {
		item := r.doc.Paths[path]
		if item.Ref != "" {
			r.out.LeftOut(domain.CodeSkippedExternalRef, 1)
			continue
		}
		for _, method := range methods {
			op, ok := item.Operations[method]
			if !ok {
				continue
			}
			operations = append(operations, operationAt{
				r.request(path, method, op, item.Parameters), op.Tags,
			})
		}
	}

	tags := []string{}
	seen := map[string]bool{}
	for _, one := range operations {
		for _, tag := range one.tags {
			if !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}
	for _, declared := range r.doc.Tags {
		if !seen[declared.Name] {
			seen[declared.Name] = true
			tags = append(tags, declared.Name)
		}
	}
	shouldSplit := r.doc.FolderTags == "slash" || sharedTagPathPrefix(tags)

	r.root = newFolderNode("")
	// The descriptions first: a tag the document describes gets its folder whether or not any
	// operation lands in it, and where two tags fold to one path the first description stands.
	for _, tag := range tags {
		description := ""
		for _, declared := range r.doc.Tags {
			if declared.Name == tag {
				description = strings.TrimSpace(declared.Description)
				break
			}
		}
		if description == "" {
			continue
		}
		segments := tagSegments(tag, shouldSplit)
		if len(segments) == 0 {
			continue
		}
		folder := r.root.at(segments)
		if folder.description == "" {
			folder.description = description
		}
	}
	// Then the requests, each in one folder: the deepest tag it carries, and the top of the
	// collection where it carries none.
	for _, one := range operations {
		segments := longestTagSegments(one.tags, shouldSplit)
		if len(segments) == 0 {
			one.node.Position = r.topItems
			r.topItems++
			r.root.items = append(r.root.items, one.node)
			continue
		}
		folder := r.root.at(segments)
		folder.items = append(folder.items, one.node)
	}

	collection := r.root.collection()
	collection.Name = strings.TrimSpace(r.doc.Info.Title)
	collection.Description = strings.TrimSpace(r.doc.Info.Description)
	if collection.Name == "" {
		collection.Name = "OpenAPI"
	}
	collection.Auth = r.documentAuth()
	for _, name := range slices.Sorted(maps.Keys(r.variables)) {
		collection.Variables = append(collection.Variables, r.variables[name])
	}
	return collection
}

// declare adds a variable the document implies but does not fill in (the base address, a token). It
// is declared switched off and empty, since only a person can give a value the document does not
// carry.
func (r *reader) declare(name, value string, off bool) {
	if _, ok := r.variables[name]; ok {
		return
	}
	r.variables[name] = domain.Variable{
		Name:     name,
		Value:    value,
		Kind:     domain.VariableText,
		Enabled:  !off,
		Position: len(r.variables) + 1,
	}
}

// baseURL is the address every request is written against: the first server of a 3.x document with
// its variables answered, or the scheme, host and base path of a 2.0 one. A server that is itself a
// hole (`<<baseUrl>>`) is no address.
func (r *reader) baseURL() string {
	if !r.doc2 {
		if len(r.doc.Servers) == 0 {
			return ""
		}
		address := r.doc.Servers[0].URL
		for name, variable := range r.doc.Servers[0].Variables {
			address = strings.ReplaceAll(address, "{"+name+"}", variable.Default)
		}
		return addressOrNothing(strings.TrimSuffix(strings.TrimSpace(address), "/"))
	}

	if r.doc.Host == "" {
		return r.doc.BasePath
	}
	scheme := "https"
	if len(r.doc.Schemes) > 0 {
		scheme = r.doc.Schemes[0]
	}
	return addressOrNothing(strings.TrimSuffix(scheme+"://"+r.doc.Host+r.doc.BasePath, "/"))
}

// addressOrNothing is what a document carries as an address, or nothing where it carries a hole
// instead: a value only a person can give, and so one the variable is left empty and off for.
func addressOrNothing(address string) string {
	if loneHole.MatchString(address) {
		return ""
	}
	return address
}

// request is one operation as a request: its address against the declared server, its query and
// header parameters, its body and its authorization.
func (r *reader) request(
	path, method string, op operation, shared []parameter,
) domain.CollectionNode {
	node := domain.CollectionNode{
		Name:   nameOf(op),
		Method: strings.ToUpper(method),
		// What the document describes the operation as, and nothing else: the summary is the name
		// Hoppscotch reads too, and writing it here as well would duplicate the row's own text.
		Description: strings.TrimSpace(op.Description),
	}

	address := "{{" + domain.BaseURLVariable + "}}" + path
	query := []string{}

	// The path item's own parameters apply to every operation in it, and the operation's own come
	// after: a parameter written twice is the operation's.
	for _, param := range append(append([]parameter{}, shared...), op.Parameters...) {
		r.parameter(&node, param, &address, &query)
	}

	r.applyBody(op, &node)
	node.Responses = responsesOf(op)
	if len(query) > 0 {
		address += "?" + strings.Join(query, "&")
	}
	node.URL = address
	node.Auth = r.operationAuth(op)
	return node
}

// responsesOf reads an operation's declared answers as examples: the name is the document's
// description (the code if none), the status is the key when numeric, and the body is its example,
// the first listed example, or a skeleton made from its schema.
func responsesOf(op operation) []domain.ResponseExample {
	out := []domain.ResponseExample{}
	for _, key := range slices.Sorted(maps.Keys(op.Responses)) {
		declared := op.Responses[key]
		status := 200
		if number, err := strconv.Atoi(key); err == nil {
			status = number
		}
		name := strings.TrimSpace(declared.Description)
		if name == "" {
			name = key
		}
		one := domain.ResponseExample{Name: name, Status: status}

		if len(declared.Content) > 0 {
			mime, media := pickMedia(declared.Content)
			one.Headers = append(one.Headers, domain.HeaderPair{Name: "Content-Type", Value: mime})
			one.Body = bodyOfMedia(media)
		} else if len(declared.Examples) > 0 || declared.Schema != nil {
			// Swagger 2.0 keeps an example per media type beside the schema; the body is one, so the
			// first content type the document lists is the one it is read as.
			for _, mime := range slices.Sorted(maps.Keys(declared.Examples)) {
				one.Headers = append(one.Headers, domain.HeaderPair{Name: "Content-Type", Value: mime})
				one.Body = domain.ValueText(declared.Examples[mime])
				break
			}
			if one.Body == "" {
				one.Body = skeleton(declared.Schema)
			}
		}
		out = append(out, one)
	}
	return out
}

// bodyOfMedia is what an answer's media type shows: the example it carries, the first of the
// examples it lists, and otherwise the skeleton of what it describes.
func bodyOfMedia(media mediaType) string {
	if media.Example != nil {
		return domain.ValueText(media.Example)
	}
	for _, name := range slices.Sorted(maps.Keys(media.Examples)) {
		if media.Examples[name].Value != nil {
			return domain.ValueText(media.Examples[name].Value)
		}
	}
	return skeleton(media.Schema)
}

// nameOf is what the row says, in Hoppscotch's order: the summary, the generator's identifier, the
// title, and a plain placeholder if the operation is named nowhere.
func nameOf(op operation) string {
	for _, candidate := range []string{op.Summary, op.OperationID, op.Title} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return domain.UntitledRequest
}

// parameter reads one parameter: a path id becomes a `:name` in the address with a row of the
// request to fill, a query parameter becomes a row and, when it is required, part of the address.
func (r *reader) parameter(
	node *domain.CollectionNode, param parameter, address *string, query *[]string,
) {
	name := strings.TrimSpace(param.Name)
	if name == "" || param.In == "" {
		return
	}
	value := exampleOf(param.Example, param.Examples, param.Schema, param.Default)

	switch param.In {
	case "path":
		token := domain.PathParamName(name)
		var own, shared bool
		*address, own, shared = slot(*address, name, token)
		if shared {
			r.declare(name, "", true)
		}
		if !own {
			return
		}
		row := domain.Row{
			Name: token, Value: value, Enabled: true, Description: strings.TrimSpace(param.Description),
		}
		// A parameter written twice is the operation's, which comes second in the walk.
		at := slices.IndexFunc(node.PathParams, func(one domain.Row) bool { return one.Name == token })
		if at >= 0 {
			node.PathParams[at] = row
		} else {
			node.PathParams = append(node.PathParams, row)
		}
	case "query":
		node.Params = append(node.Params, domain.Row{
			Name: name, Value: value, Enabled: param.Required,
			Description: strings.TrimSpace(param.Description),
		})
		if param.Required {
			*query = append(*query, name+"="+value)
		}
	case "header":
		node.Headers = append(node.Headers, domain.Row{
			Name: name, Value: value, Enabled: param.Required,
			Description: strings.TrimSpace(param.Description),
		})
	case "formData":
		row := domain.FormRow{Name: name, Value: value, Enabled: param.Required}
		if param.Type == "file" {
			row.File = true
			row.Value = ""
		}
		node.Form = append(node.Form, row)
	}
}

// slot writes the `{name}` of a path template into an address. A segment of its own becomes a
// `:token`; one inside other text, such as `v{version}` or `{name}.{ext}`, stays a `{{name}}`
// variable.
func slot(address, name, token string) (out string, own, shared bool) {
	segments := strings.Split(address, "/")
	for i, segment := range segments {
		switch {
		case segment == "{"+name+"}":
			segments[i], own = ":"+token, true
		case strings.Contains(segment, "{"+name+"}"):
			segments[i], shared = strings.ReplaceAll(segment, "{"+name+"}", "{{"+name+"}}"), true
		}
	}
	return strings.Join(segments, "/"), own, shared
}

// exampleOf is the value a request would carry: what the document showed, or a skeleton from the
// schema. A parameter with nothing at all is an empty value.
func exampleOf(
	example any, examples map[string]example, schema map[string]any, fallback any,
) string {
	if example != nil {
		return domain.ValueText(example)
	}
	for _, name := range slices.Sorted(maps.Keys(examples)) {
		if examples[name].Value != nil {
			return domain.ValueText(examples[name].Value)
		}
	}
	if schema != nil {
		if settled := schemaValue(schema); settled != "" {
			return settled
		}
	}
	if fallback != nil {
		return domain.ValueText(fallback)
	}
	return ""
}
