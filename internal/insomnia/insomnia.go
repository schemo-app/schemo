// Package insomnia reads an Insomnia export — the v4 `resources` shape — as collections and
// environments. The v5 document is a different one altogether, and this package refuses it rather
// than reading it as v4: a half-read export is worse than a message. It is a pure package — no
// window, no network — so an export is read in a test.
package insomnia

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"

	"schemo/internal/domain"
	"schemo/internal/dotenv"
)

// baseEnvironment is the environment Insomnia always applies, beside whichever one is selected.
const baseEnvironment = "Base Environment"

// Import reads an export as the document it holds: every workspace becomes a collection with its
// folders and requests inside it, and the environments come alongside.
func Import(data []byte) (domain.Document, error) {
	var file export
	if err := json.Unmarshal(data, &file); err != nil {
		return domain.Document{}, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
	}
	if file.Type != "export" {
		return domain.Document{}, domain.Refuse(domain.CodeNotInsomnia, domain.ErrNotAllowed, nil)
	}
	if len(file.Resources) == 0 {
		return domain.Document{}, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}

	var out domain.Document
	read := &reader{resources: file.Resources}

	// Every workspace is a collection, and everything under it hangs off its id. An export of one
	// workspace — the usual case — writes one.
	top := read.of("", "workspace")
	if len(top) == 0 {
		// An export of a single folder or request with no workspace around it still holds requests,
		// and refusing it would be refusing what it clearly is.
		top = []resource{{ID: "", Type: "workspace", Name: "Insomnia"}}
	}
	for _, workspace := range top {
		collection := domain.Collection{
			Name:        strings.TrimSpace(workspace.Name),
			Description: strings.TrimSpace(workspace.Description),
			Auth:        read.auth(workspace.Auth, &out),
			Variables:   variablesOf(workspace.Environment),
		}
		if collection.Name == "" {
			collection.Name = "Insomnia"
		}
		read.level(workspace.ID, &collection, &out)
		read.environments(workspace.ID, &out)

		// A workspace that held nothing is not written: a collection with no requests would be a
		// row in the tree that opens onto nothing.
		if len(collection.Items) > 0 || len(collection.Children) > 0 {
			out.Collections = append(out.Collections, collection)
		}
	}
	if len(out.Collections) == 0 && len(out.Environments) == 0 {
		return domain.Document{}, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	return out, nil
}

type export struct {
	Type      string     `json:"_type"`
	Resources []resource `json:"resources"`
}

type resource struct {
	ID             string         `json:"_id"`
	Type           string         `json:"_type"`
	ParentID       string         `json:"parentId,omitempty"`
	Name           string         `json:"name,omitempty"`
	Description    string         `json:"description,omitempty"`
	Method         string         `json:"method,omitempty"`
	URL            string         `json:"url,omitempty"`
	Headers        []field        `json:"headers,omitempty"`
	Parameters     []field        `json:"parameters,omitempty"`
	PathParameters []field        `json:"pathParameters,omitempty"`
	Body           *body          `json:"body,omitempty"`
	Auth           *auth          `json:"authentication,omitempty"`
	Environment    map[string]any `json:"environment,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
}

// field is a header, a parameter or a form field. Insomnia writes `disabled` and leaves it out when
// the row is on.
type field struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Type        string `json:"type,omitempty"`
	FileName    string `json:"fileName,omitempty"`
	Description string `json:"description,omitempty"`
}

type body struct {
	MimeType string  `json:"mimeType,omitempty"`
	Text     string  `json:"text,omitempty"`
	Params   []field `json:"params,omitempty"`
}

type auth struct {
	Type     string `json:"type,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Token    string `json:"token,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Key      string `json:"key,omitempty"`
	Value    string `json:"value,omitempty"`
	In       string `json:"in,omitempty"`

	GrantType        string `json:"grantType,omitempty"`
	AuthorizationURL string `json:"authorizationUrl,omitempty"`
	AccessTokenURL   string `json:"accessTokenUrl,omitempty"`
	ClientID         string `json:"clientId,omitempty"`
	ClientSecret     string `json:"clientSecret,omitempty"`
	Scope            string `json:"scope,omitempty"`
}

type reader struct {
	resources []resource
}

// of is the resources of one kind that hang off one id, in the export's own order — the order rows
// were created in, which is the order a person saw.
func (r *reader) of(parent, kind string) []resource {
	out := []resource{}
	for _, one := range r.resources {
		if one.ParentID == parent && one.Type == kind {
			out = append(out, one)
		}
	}
	return out
}

// level writes what hangs off one id: a request becomes a node and a group becomes a collection
// inside one — which is what this app calls the same thing — and both keep their turn in the
// export's order.
func (r *reader) level(parent string, into *domain.Collection, out *domain.Document) {
	position := int64(0)
	for _, one := range r.resources {
		if one.ParentID != parent {
			continue
		}
		switch one.Type {
		case "request_group":
			if one.ID == "" {
				continue
			}
			nested := domain.Collection{
				Name:        strings.TrimSpace(one.Name),
				Description: strings.TrimSpace(one.Description),
				Position:    position,
				Variables:   variablesOf(one.Environment),
			}
			position++
			r.level(one.ID, &nested, out)
			into.Children = append(into.Children, nested)
		case "request":
			node := r.request(one, out)
			node.Position = position
			position++
			into.Items = append(into.Items, node)
		}
	}
}

// environments reads the environments of one workspace. Insomnia's base environment is the one that
// always applies, and this app resolves against the active environment alone — so the base values
// are folded into every named environment as well. An import that moved the values each request
// leans on into an environment nobody selects would arrive broken.
func (r *reader) environments(parent string, out *domain.Document) {
	var base []domain.Variable
	named := []domain.EnvironmentDraft{}
	for _, one := range r.of(parent, "environment") {
		name := strings.TrimSpace(one.Name)
		if name == "" {
			continue
		}
		if strings.EqualFold(name, baseEnvironment) {
			base = variablesOf(one.Data)
			continue
		}
		named = append(named, domain.EnvironmentDraft{Name: name, Variables: variablesOf(one.Data)})
	}

	if len(base) > 0 {
		out.Environments = append(out.Environments,
			domain.EnvironmentDraft{Name: baseEnvironment, Variables: base})
	}
	for _, draft := range named {
		draft.Variables = renumber(append(draft.Variables, missing(base, draft.Variables)...))
		out.Environments = append(out.Environments, draft)
	}
}

// missing is what the base environment holds and the environment does not set itself: in Insomnia
// the selected environment wins over the base, so a name the environment writes is its own.
func missing(base, own []domain.Variable) []domain.Variable {
	seen := map[string]bool{}
	for _, one := range own {
		seen[one.Name] = true
	}
	out := []domain.Variable{}
	for _, one := range base {
		if !seen[one.Name] {
			out = append(out, one)
		}
	}
	return out
}

func renumber(vars []domain.Variable) []domain.Variable {
	for index := range vars {
		vars[index].Position = index + 1
	}
	return vars
}

// variablesOf reads an environment's data as variables. Insomnia keeps anything JSON can hold, and
// a value that is not a scalar is written as its own JSON: a request substitutes text, and a nested
// object flattened into names would be an invention.
func variablesOf(data map[string]any) []domain.Variable {
	out := []domain.Variable{}
	for _, name := range slices.Sorted(maps.Keys(data)) {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		kind := domain.VariableText
		if dotenv.SecretName(name) {
			kind = domain.VariableSecret
		}
		out = append(out, domain.Variable{
			Name:     name,
			Value:    domain.ValueText(data[name]),
			Kind:     kind,
			Enabled:  true,
			Position: len(out) + 1,
		})
	}
	return out
}

func (r *reader) request(from resource, out *domain.Document) domain.CollectionNode {
	node := domain.CollectionNode{
		Name:        strings.TrimSpace(from.Name),
		Method:      strings.ToUpper(strings.TrimSpace(from.Method)),
		Description: strings.TrimSpace(from.Description),
		Auth:        r.auth(from.Auth, out),
	}
	if node.Name == "" {
		// The placeholder Hoppscotch writes where a file names a request nowhere.
		node.Name = domain.UntitledRequest
	}
	if node.Method == "" {
		// The method a file leaves out is the one its own dialog runs.
		node.Method = "GET"
	}

	for _, header := range from.Headers {
		if strings.TrimSpace(header.Name) == "" {
			continue
		}
		node.Headers = append(node.Headers, domain.Row{
			Name: header.Name, Value: templated(header.Value), Enabled: !header.Disabled,
			Description: strings.TrimSpace(header.Description),
		})
	}
	// An enabled parameter is part of the address as well as a row of its own, which is the rule
	// the address field follows everywhere: the text is what is sent.
	query := []string{}
	for _, param := range from.Parameters {
		name := strings.TrimSpace(param.Name)
		if name == "" {
			continue
		}
		value := templated(param.Value)
		node.Params = append(node.Params, domain.Row{
			Name: name, Value: value, Enabled: !param.Disabled,
			Description: strings.TrimSpace(param.Description),
		})
		if !param.Disabled {
			query = append(query, name+"="+value)
		}
	}
	node.URL, node.PathParams = addressOf(from)
	if len(query) > 0 {
		separator := "?"
		if strings.Contains(node.URL, "?") {
			separator = "&"
		}
		node.URL += separator + strings.Join(query, "&")
	}

	r.body(&node, from.Body, out)
	return node
}

// addressOf is the url, whose `:name` path parameters are this app's own notation too, and the rows
// that fill them: what the file kept beside the url is the value, and the name is the url's.
func addressOf(from resource) (string, []domain.Row) {
	kept := map[string]field{}
	for _, param := range from.PathParameters {
		kept[param.Name] = param
	}
	rows := []domain.Row{}
	for _, name := range domain.PathParams(from.URL) {
		param := kept[name]
		rows = append(rows, domain.Row{
			Name: name, Value: templated(param.Value), Enabled: true,
			Description: strings.TrimSpace(param.Description),
		})
	}
	return templated(from.URL), rows
}

// body reads what a request sends. A mime type this app has no kind for is written as the text it
// carried, because the text is what the request would send.
func (r *reader) body(node *domain.CollectionNode, from *body, out *domain.Document) {
	if from == nil {
		return
	}
	mime := mimeOf(from.MimeType)
	switch {
	case strings.HasPrefix(mime, domain.MediaMultipart):
		node.BodyKind = domain.BodyForm
		for _, param := range from.Params {
			row := domain.FormRow{Name: param.Name, Value: templated(param.Value), Enabled: !param.Disabled}
			if param.Type == "file" || param.FileName != "" {
				// The field survives and the file does not: a path in the file is where the file
				// was read from, not something this app can send.
				row.File = true
				row.Value = ""
				out.LeftOut(domain.CodeSkippedBinaryFile, 1)
			}
			node.Form = append(node.Form, row)
		}
	case mime == domain.MediaForm:
		node.BodyKind = domain.BodyRaw
		if strings.TrimSpace(from.Text) != "" {
			node.Body = templated(from.Text)
			return
		}
		parts := []string{}
		for _, param := range from.Params {
			if param.Disabled {
				continue
			}
			parts = append(parts, param.Name+"="+templated(param.Value))
		}
		node.Body = strings.Join(parts, "&")
	case strings.Contains(mime, "json"):
		node.BodyKind = domain.BodyJSON
		node.Body = templated(from.Text)
	case strings.Contains(mime, "xml"):
		node.BodyKind = domain.BodyXML
		node.Body = templated(from.Text)
	default:
		node.BodyKind = domain.BodyRaw
		node.Body = templated(from.Text)
	}
}

// auth maps what an export declares onto this app's authorization. A scheme with no shape here is
// counted and becomes `none` rather than nothing: inheriting the collection's would send
// credentials the file never asked for.
func (r *reader) auth(from *auth, out *domain.Document) *domain.Auth {
	if from == nil || from.Disabled {
		return nil
	}
	switch strings.ToLower(from.Type) {
	case "":
		return nil
	case "none":
		return &domain.Auth{Type: domain.AuthNone}
	case "bearer":
		prefix := from.Prefix
		if prefix == "" {
			prefix = "Bearer"
		}
		return &domain.Auth{Type: domain.AuthBearer, Fields: map[string]string{
			"prefix": prefix, "token": templated(from.Token),
		}}
	case "basic":
		return &domain.Auth{Type: domain.AuthBasic, Fields: map[string]string{
			"username": templated(from.Username), "password": templated(from.Password),
		}}
	case "apikey":
		place := domain.PlaceHeader
		if strings.EqualFold(from.In, domain.PlaceQuery) {
			place = domain.PlaceQuery
		}
		return &domain.Auth{Type: domain.AuthAPIKey, Fields: map[string]string{
			"key": templated(from.Key), "value": templated(from.Value), "place": place,
		}}
	case "oauth2":
		// The grant the file names, and the authorization code flow where it names none — which is
		// what a file leaves out and what Hoppscotch imports every one of them as.
		return &domain.Auth{Type: domain.AuthOAuth2, Fields: map[string]string{
			"grant":        domain.GrantNamed(from.GrantType),
			"authUrl":      templated(from.AuthorizationURL),
			"tokenUrl":     templated(from.AccessTokenURL),
			"clientId":     templated(from.ClientID),
			"clientSecret": templated(from.ClientSecret),
			"scope":        templated(from.Scope),
		}}
	default:
		out.LeftOut(domain.CodeSkippedAuthScheme, 1)
		return &domain.Auth{Type: domain.AuthNone}
	}
}

// template is Insomnia's own way of naming a value — `{{ _.base_url }}` — written as this app
// writes it. Its other forms call functions, and a call is not something this app can answer: the
// text travels as it stands.
var template = regexp.MustCompile(`\{\{\s*_\.\s*([A-Za-z0-9_.-]+)\s*\}\}`)

func templated(text string) string {
	return template.ReplaceAllString(text, "{{$1}}")
}

// mimeOf is the media type without its parameters: `application/json; charset=utf-8` is json.
func mimeOf(value string) string {
	if at := strings.Index(value, ";"); at >= 0 {
		value = value[:at]
	}
	return strings.ToLower(strings.TrimSpace(value))
}
