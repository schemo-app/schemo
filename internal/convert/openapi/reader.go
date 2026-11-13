package openapi

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"schemo/internal/domain"
)

// baseVariable is the variable every request's address starts with: the document's server is read
// into it, or, where the document leaves the address to a person, it is left for them to fill in.
const baseVariable = "{{" + domain.BaseURLVariable + "}}"

// reader holds what the walk needs beside the document: the variables it declares as it goes, the
// folders it opens, and the order the document declared its tags in.
type reader struct {
	doc       document
	doc2      bool
	out       *domain.Document
	variables map[string]domain.Variable
	root      *folderNode
	topItems  int64
	server    string
}

func (r *reader) collection() domain.Collection {
	base, server := r.baseURL()
	r.server = server
	r.declare(domain.BaseURLVariable, base, base == "")

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

func (r *reader) baseURL() (value, server string) {
	if !r.doc2 {
		if len(r.doc.Servers) == 0 {
			return "", baseVariable
		}
		address := r.doc.Servers[0].URL
		for name, variable := range r.doc.Servers[0].Variables {
			address = strings.ReplaceAll(address, "{"+name+"}", variable.Default)
		}
		return splitServer(address)
	}

	if r.doc.Host == "" {
		return r.doc.BasePath, baseVariable
	}
	scheme := "https"
	if len(r.doc.Schemes) > 0 {
		scheme = r.doc.Schemes[0]
	}
	return splitServer(scheme + "://" + r.doc.Host + r.doc.BasePath)
}

func splitServer(address string) (value, server string) {
	address = strings.TrimSuffix(strings.TrimSpace(address), "/")
	if hole := holeServer.FindStringSubmatch(address); hole != nil {
		return "", baseVariable + hole[1]
	}
	return address, baseVariable
}

func (r *reader) request(
	path, method string, op operation, shared []parameter,
) domain.CollectionNode {
	node := domain.CollectionNode{
		Name:        nameOf(op),
		Method:      strings.ToUpper(method),
		Description: strings.TrimSpace(op.Description),
	}

	address := r.server + path
	query := []string{}

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

func nameOf(op operation) string {
	for _, candidate := range []string{op.Summary, op.OperationID, op.Title} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return domain.UntitledRequest
}

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
