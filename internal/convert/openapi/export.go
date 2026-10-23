package openapi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"schemo/internal/domain"
)

// schemeNames are the names the three schemes this app can write are declared under. The reader
// answers to any name — what a scheme is, the requirement's definition says — so these are labels.
var schemeNames = map[domain.AuthType]string{
	domain.AuthBearer: "bearerAuth",
	domain.AuthBasic:  "basicAuth",
	domain.AuthAPIKey: "apiKey",
	domain.AuthOAuth2: "oauth2",
}

// Export writes a collection as an OpenAPI 3.1 document in YAML: paths and schemas, no scripts.
func Export(collection domain.Collection) ([]byte, error) {
	w := &writer{
		paths:   map[string]map[string]any{},
		schemes: map[string]map[string]any{},
	}
	w.collectionAuth(collection.Auth)
	w.walk(collection, "")

	// The format requires a title and a version, and this side knows no version better than the one
	// a document written by hand starts at.
	info := map[string]any{"title": collection.Name, "version": "1.0.0"}
	if strings.TrimSpace(collection.Description) != "" {
		info["description"] = collection.Description
	}
	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    info,
		"paths":   w.paths,
	}
	if address := serverAddress(collection.Variables); address != "" {
		doc["servers"] = []any{map[string]any{"url": address}}
	}
	if len(w.tags) > 0 {
		doc["tags"] = w.tags
		// The marker says the tag names are folder paths: a reader that knows it, and this app in
		// turn, finds the nesting back rather than one flat folder called `a/b`.
		doc["x-hoppscotch-folder-tags"] = "slash"
	}
	if len(w.security) > 0 {
		doc["security"] = w.security
	}
	if len(w.schemes) > 0 {
		doc["components"] = map[string]any{"securitySchemes": w.schemes}
	}

	data, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("collection %q could not be written: %w", collection.Name, err)
	}
	return data, nil
}

// serverAddress is the address every request is written against: the value of the collection's own
// `baseUrl` variable, which is the variable the reader declares out of a document in turn.
func serverAddress(variables []domain.Variable) string {
	for _, variable := range variables {
		if variable.Name == domain.BaseURLVariable && variable.Value != "" {
			return variable.Value
		}
	}
	return ""
}

// writer gathers the document as it is built: the paths the requests became, the schemes declared
// along the way, and the tags the folders named.
type writer struct {
	paths        map[string]map[string]any
	schemes      map[string]map[string]any
	security     []any
	tags         []any
	declaredType domain.AuthType
	declaredName string
}

// collectionAuth writes the scheme the whole document asks for. Digest, AWS and JWT have no shape
// here, and such a scheme is left out rather than written as something it is not.
func (w *writer) collectionAuth(auth *domain.Auth) {
	if auth == nil {
		return
	}
	definition := schemeDefinition(*auth)
	if definition == nil {
		return
	}
	w.declaredType = auth.Type
	w.declaredName = schemeNames[auth.Type]
	w.schemes[w.declaredName] = definition
	w.security = []any{map[string]any{w.declaredName: []any{}}}
}

// walk writes one level: a folder hands its own path down as the tag of every request inside it,
// and a request at the top of a collection belongs to no tag at all.
func (w *writer) walk(collection domain.Collection, tag string) {
	for _, entry := range collection.Level() {
		if entry.Collection != nil {
			w.folder(*entry.Collection, tag)
			continue
		}
		w.operation(*entry.Node, tag)
	}
}

// folder walks a folder under its whole path as the tag, so the document says the tags nest and
// readers find the folders as they were. Every folder is declared, empty ones included.
func (w *writer) folder(folder domain.Collection, parent string) {
	path := folder.Name
	if parent != "" {
		path = parent + "/" + folder.Name
	}
	declaration := map[string]any{"name": path}
	if strings.TrimSpace(folder.Description) != "" {
		declaration["description"] = folder.Description
	}
	w.tags = append(w.tags, declaration)
	w.walk(folder, path)
}

// operation writes one request as the operation of its path: the name as the summary, the address
// as the path, and what it carries and asks for beside them.
func (w *writer) operation(node domain.CollectionNode, tag string) {
	path, parameters := pathOf(node)
	item := w.paths[path]
	if item == nil {
		item = map[string]any{}
		w.paths[path] = item
	}

	parameters = append(parameters, rowParameters(node)...)
	op := map[string]any{"summary": node.Name}
	if strings.TrimSpace(node.Description) != "" {
		op["description"] = node.Description
	}
	if tag != "" {
		op["tags"] = []any{tag}
	}
	if len(parameters) > 0 {
		op["parameters"] = parameters
	}
	if body := bodyOf(node); body != nil {
		op["requestBody"] = body
	}
	if security := w.nodeSecurity(node.Auth); security != nil {
		op["security"] = security
	}
	// The format demands an answer for every operation: the saved examples where the request has
	// them, and the plain success where it has none.
	op["responses"] = writtenResponses(node)
	// A path and a method name one operation, and a second request on the same pair is the later
	// one: the map decides that, rather than an error nobody could act on.
	item[methodKey(node.Method)] = op
}

// writtenResponses is a request's saved answers as the document declares them: one per example,
// described by its name and keyed by status, with the body written as the text the example holds so
// a round trip does not reflow it. A request with no examples gets the plain success.
func writtenResponses(node domain.CollectionNode) map[string]any {
	if len(node.Responses) == 0 {
		return map[string]any{"200": map[string]any{"description": "Successful response"}}
	}
	out := map[string]any{}
	for _, one := range node.Responses {
		status := one.Status
		if status == 0 {
			status = 200
		}
		description := strings.TrimSpace(one.Name)
		if description == "" {
			description = "Example"
		}
		entry := map[string]any{"description": description}
		if one.Body != "" {
			mime := domain.MediaJSON
			for _, header := range one.Headers {
				if strings.EqualFold(header.Name, "Content-Type") &&
					strings.TrimSpace(header.Value) != "" {
					mime = header.Value
				}
			}
			entry["content"] = map[string]any{mime: map[string]any{"example": one.Body}}
		}
		// One answer per status in this format, so a second example of the same code is the later
		// one — the map decides that, rather than a key nobody could write.
		out[strconv.Itoa(status)] = entry
	}
	return out
}

// pathOf is an address as the document writes it: the server variable comes off, a `:name` becomes
// the template `{name}` with a parameter from the request's row, and the query string is left to
// the rows. A `{{name}}` variable stays in the path.
func pathOf(node domain.CollectionNode) (string, []any) {
	path := strings.TrimPrefix(node.URL, "{{"+domain.BaseURLVariable+"}}")
	// A query string is a URL's rows, not part of a path, and the rows are written as parameters.
	if at := strings.IndexByte(path, '?'); at >= 0 {
		path = path[:at]
	}
	parameters := []any{}
	declared := map[string]bool{}
	path = domain.ReplacePathParams(path, func(name string) string {
		if !declared[name] {
			declared[name] = true
			parameters = append(parameters, pathParameter(name, node.PathParams))
		}
		return "{" + name + "}"
	})
	return path, parameters
}

// pathParameter declares one name of a path, with the value and the description the request's row
// of that name holds.
func pathParameter(name string, rows []domain.Row) map[string]any {
	parameter := map[string]any{
		"name":     name,
		"in":       "path",
		"required": true,
		"schema":   map[string]any{"type": "string"},
	}
	for _, row := range rows {
		if row.Name != name {
			continue
		}
		if row.Value != "" {
			parameter["example"] = row.Value
		}
		if row.Description != "" {
			parameter["description"] = row.Description
		}
	}
	return parameter
}

// headerElsewhere lists the headers a document states elsewhere: content type with the body, accept
// with the response, authorization in the security section. Rows with these names are left out, so
// a pasted token stays out of the file.
var headerElsewhere = map[string]bool{"content-type": true, "accept": true, "authorization": true}

// rowParameters writes the enabled rows as the operation's parameters: a query or a header the
// request would carry, with the value it would carry as the example.
func rowParameters(node domain.CollectionNode) []any {
	out := []any{}
	for _, row := range node.Params {
		if !row.Enabled || strings.TrimSpace(row.Name) == "" {
			continue
		}
		// Every query parameter is written optional: a document describes what the API accepts, and
		// marking the rows this app filled in as required would be the document lying about the API.
		parameter := map[string]any{
			"name":     row.Name,
			"in":       "query",
			"required": false,
			"schema":   map[string]any{"type": "string"},
		}
		if row.Value != "" {
			parameter["example"] = row.Value
		}
		if row.Description != "" {
			parameter["description"] = row.Description
		}
		out = append(out, parameter)
	}
	for _, row := range node.Headers {
		if !row.Enabled || strings.TrimSpace(row.Name) == "" ||
			headerElsewhere[strings.ToLower(strings.TrimSpace(row.Name))] {
			continue
		}
		parameter := map[string]any{
			"name":   row.Name,
			"in":     "header",
			"schema": map[string]any{"type": "string"},
		}
		if row.Value != "" {
			parameter["example"] = row.Value
		}
		if row.Description != "" {
			parameter["description"] = row.Description
		}
		out = append(out, parameter)
	}
	return out
}

// bodyOf is the body a request would send as the document describes it: one media type holding the
// example the request carries. A body that is not there is not described at all.
func bodyOf(node domain.CollectionNode) map[string]any {
	switch domain.KindOf(node.BodyKind) {
	case domain.BodyJSON:
		if node.Body == "" {
			return nil
		}
		return mediaOf(domain.MediaJSON, jsonExample(node.Body))
	case domain.BodyXML:
		if node.Body == "" {
			return nil
		}
		return mediaOf(domain.MediaXML, node.Body)
	case domain.BodyForm:
		fields := formExample(node.Form)
		if len(fields) == 0 {
			return nil
		}
		return mediaOf(domain.MediaMultipart, fields)
	case domain.BodyBinary:
		// A file's bytes are not the collection's to carry, so a binary body has nothing to say.
		return nil
	default:
		if node.Body == "" {
			return nil
		}
		return mediaOf(domain.MediaText, node.Body)
	}
}

func mediaOf(mime string, example any) map[string]any {
	return map[string]any{
		"content": map[string]any{mime: map[string]any{"example": example}},
	}
}

// jsonExample is a JSON body as the value the document shows. A body that does not parse — and one
// that parses to nothing, like `null` — is written as the text it was.
func jsonExample(body string) any {
	var value any
	if err := json.Unmarshal([]byte(body), &value); err != nil || value == nil {
		return body
	}
	return value
}

// formExample is a form body's enabled rows as one object: a name to its value, and a file row to
// an empty string, since a document carries no file.
func formExample(form []domain.FormRow) map[string]any {
	fields := map[string]any{}
	for _, row := range form {
		if !row.Enabled || strings.TrimSpace(row.Name) == "" {
			continue
		}
		value := row.Value
		if row.File {
			value = ""
		}
		fields[row.Name] = value
	}
	return fields
}

// nodeSecurity is what one operation asks for: nothing where the request inherits, an empty list
// where it asks for nothing, and a scheme where the collection declares none of the request's kind.
func (w *writer) nodeSecurity(auth *domain.Auth) []any {
	if auth == nil {
		return nil
	}
	if auth.Type == domain.AuthNone {
		return []any{}
	}
	if auth.Type == w.declaredType && w.declaredName != "" {
		return []any{map[string]any{w.declaredName: []any{}}}
	}
	name := w.nodeSchemeName(*auth)
	if name == "" {
		return nil
	}
	return []any{map[string]any{name: []any{}}}
}

// nodeSchemeName names and declares the scheme a request asks for when the collection declares none
// of its kind: the base name if free or already holding the same scheme (then shared), otherwise
// the smallest free `_2`, `_3` … suffix.
func (w *writer) nodeSchemeName(auth domain.Auth) string {
	definition := schemeDefinition(auth)
	if definition == nil {
		// A kind of authorization a document has no shape for is written as nothing: the request
		// then inherits, rather than being written as a scheme it is not.
		return ""
	}
	base := schemeNames[auth.Type]
	name := base
	for number := 2; ; number++ {
		existing, taken := w.schemes[name]
		if !taken || reflect.DeepEqual(existing, definition) {
			w.schemes[name] = definition
			return name
		}
		name = base + "_" + strconv.Itoa(number)
	}
}

// schemeDefinition is one of the three schemes a document here declares, written the way the
// reader answers it back: the same type, scheme name or key, so that the trip is whole.
func schemeDefinition(auth domain.Auth) map[string]any {
	switch auth.Type {
	case domain.AuthBearer:
		return map[string]any{"type": "http", "scheme": "bearer"}
	case domain.AuthBasic:
		return map[string]any{"type": "http", "scheme": "basic"}
	case domain.AuthAPIKey:
		place := auth.Answer("place")
		if place == "" {
			place = domain.PlaceHeader
		}
		// A key field nobody filled in leaves the document a name it cannot leave out: the format
		// demands one, and an empty one would be invalid.
		name := auth.Answer("key")
		if name == "" {
			name = "api_key"
		}
		return map[string]any{"type": "apiKey", "name": name, "in": place}
	case domain.AuthOAuth2:
		// One flow, the one the grant names, with the scopes the scheme holds read back as the map
		// the format asks for.
		scopes := map[string]any{}
		for _, scope := range strings.Fields(auth.Answer("scope")) {
			scopes[scope] = ""
		}
		authUrl, tokenUrl := auth.Answer("authUrl"), auth.Answer("tokenUrl")
		var flow string
		var body map[string]any
		switch grant := auth.Answer("grant"); grant {
		case domain.GrantImplicit:
			flow, body = "implicit", map[string]any{"authorizationUrl": authUrl, "scopes": scopes}
		case domain.GrantPassword:
			flow, body = "password", map[string]any{"tokenUrl": tokenUrl, "scopes": scopes}
		case domain.GrantClientCredentials:
			flow, body = "clientCredentials", map[string]any{"tokenUrl": tokenUrl, "scopes": scopes}
		default:
			// authorization_code, which is also what a scheme with no grant at all is run as.
			flow = "authorizationCode"
			body = map[string]any{"authorizationUrl": authUrl, "tokenUrl": tokenUrl, "scopes": scopes}
		}
		return map[string]any{"type": "oauth2", "flows": map[string]any{flow: body}}
	}
	return nil
}

// methodKey is one method as a path's own key. A request with no method is written as GET, which is
// the method the tree itself gives one.
func methodKey(method string) string {
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {
		return "get"
	}
	return method
}
