// Package insomnia reads Insomnia v4 and v5 exports as collections and environments.
package insomnia

import (
	"cmp"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"

	"schemo/internal/convert/dotenv"
	"schemo/internal/domain"
)

// baseEnvironment is the environment Insomnia always applies, beside whichever one is selected.
const baseEnvironment = "Base Environment"

// untitledCollection names a workspace that has no name of its own.
const untitledCollection = "Insomnia"

// Import detects the export version from the document, independent of its filename.
func Import(data []byte) (domain.Document, error) {
	var file export
	if err := json.Unmarshal(data, &file); file.Type == "export" {
		if err != nil {
			return domain.Document{}, domain.Refuse(domain.CodeNotJSON, domain.ErrNotAllowed, nil)
		}
		return importV4(file)
	}
	return importV5(data)
}

func importV4(file export) (domain.Document, error) {
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
		top = []resource{{ID: "", Type: "workspace"}}
	}
	for _, workspace := range top {
		collection := domain.Collection{
			Name:        cmp.Or(strings.TrimSpace(workspace.Name), untitledCollection),
			Description: strings.TrimSpace(workspace.Description),
			Auth:        requestAuth(workspace.Auth, &out),
		}
		read.level(workspace.ID, &collection, &out)
		environments := read.environments(workspace.ID, variablesOf(workspace.Environment))

		if len(collection.Items) > 0 || len(collection.Children) > 0 ||
			(workspace.Scope == "collection" || workspace.Scope == "design") && len(environments) > 0 {
			collection.Environments = environments
			out.Collections = append(out.Collections, collection)
		} else {
			out.Environments = append(out.Environments, standaloneEnvironments(environments)...)
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
	ID             string         `json:"_id" yaml:"-"`
	Type           string         `json:"_type" yaml:"-"`
	Scope          string         `json:"scope" yaml:"-"`
	ParentID       string         `json:"parentId,omitempty" yaml:"-"`
	Name           string         `json:"name,omitempty"`
	Description    string         `json:"description,omitempty"`
	Method         string         `json:"method,omitempty"`
	URL            string         `json:"url,omitempty"`
	Headers        []field        `json:"headers,omitempty"`
	Parameters     []field        `json:"parameters,omitempty"`
	PathParameters []field        `json:"pathParameters,omitempty" yaml:"pathParameters"`
	Body           *body          `json:"body,omitempty"`
	Auth           *auth          `json:"authentication,omitempty" yaml:"authentication"`
	Environment    map[string]any `json:"environment,omitempty"`
	Data           map[string]any `json:"data,omitempty" yaml:"-"`
}

// field is a header, a parameter or a form field. Insomnia writes `disabled` and leaves it out when
// the row is on.
type field struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Type        string `json:"type,omitempty"`
	FileName    string `json:"fileName,omitempty" yaml:"fileName"`
	Description string `json:"description,omitempty"`
}

type body struct {
	MimeType string  `json:"mimeType,omitempty" yaml:"mimeType"`
	FileName string  `json:"fileName,omitempty" yaml:"fileName"`
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
	AddTo    string `json:"addTo,omitempty" yaml:"addTo"`

	GrantType        string `json:"grantType,omitempty" yaml:"grantType"`
	AuthorizationURL string `json:"authorizationUrl,omitempty" yaml:"authorizationUrl"`
	AccessTokenURL   string `json:"accessTokenUrl,omitempty" yaml:"accessTokenUrl"`
	ClientID         string `json:"clientId,omitempty" yaml:"clientId"`
	ClientSecret     string `json:"clientSecret,omitempty" yaml:"clientSecret"`
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
// inside the current one, both in export order.
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
			}
			nested.Variables = variablesOf(one.Environment)
			position++
			r.level(one.ID, &nested, out)
			into.Children = append(into.Children, nested)
		case "request":
			node := requestNode(one, out)
			node.Position = position
			position++
			into.Items = append(into.Items, node)
		}
	}
}

// The base's id is the parent of v4 sub-environments. Older files also place named siblings directly
// under the workspace.
func (r *reader) environments(parent string, inline []domain.Variable) []domain.EnvironmentDraft {
	base := domain.EnvironmentDraft{Name: baseEnvironment, Base: true, Variables: inline}
	var named []domain.EnvironmentDraft
	for _, one := range r.of(parent, "environment") {
		name := strings.TrimSpace(one.Name)
		children := r.of(one.ID, "environment")
		if one.ID != "" && (len(children) > 0 || one.ID == "__BASE_ENVIRONMENT_ID__") ||
			strings.EqualFold(name, baseEnvironment) {
			base.Name = cmp.Or(name, baseEnvironment)
			variables := variablesOf(one.Data)
			base.Variables = renumber(append(variables, missing(inline, variables)...))
			for _, child := range children {
				if childName := strings.TrimSpace(child.Name); childName != "" {
					named = append(named, domain.EnvironmentDraft{Name: childName, Variables: variablesOf(child.Data)})
				}
			}
			continue
		}
		if name != "" {
			named = append(named, domain.EnvironmentDraft{Name: name, Variables: variablesOf(one.Data)})
		}
	}
	return withBase(base, named)
}

func withBase(base domain.EnvironmentDraft, named []domain.EnvironmentDraft) []domain.EnvironmentDraft {
	if len(base.Variables) == 0 && len(named) == 0 {
		return nil
	}
	confidential := map[string]bool{}
	for _, variable := range base.Variables {
		if variable.Kind == domain.VariableSecret {
			confidential[variable.Name] = true
		}
	}
	for _, env := range named {
		for i := range env.Variables {
			if confidential[env.Variables[i].Name] {
				env.Variables[i].Kind = domain.VariableSecret
			}
		}
	}
	return append([]domain.EnvironmentDraft{base}, named...)
}

// A standalone environment file has no collection base after import, so each variant keeps the
// defaults it inherited.
func standaloneEnvironments(environments []domain.EnvironmentDraft) []domain.EnvironmentDraft {
	if len(environments) == 0 {
		return nil
	}
	base := environments[0].Variables
	var out []domain.EnvironmentDraft
	for _, env := range environments {
		if env.Base && len(base) == 0 {
			continue
		}
		if !env.Base {
			env.Variables = renumber(append(slices.Clone(env.Variables), missing(base, env.Variables)...))
		}
		env.Base = false
		out = append(out, env)
	}
	return out
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

// variablesOf reads an environment's data as variables. A value that is not a scalar is written as
// its own JSON, since requests substitute text and flattening nested objects would invent names.
func variablesOf(data map[string]any) []domain.Variable {
	out := []domain.Variable{}
	for _, key := range slices.Sorted(maps.Keys(data)) {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		kind := domain.VariableText
		if dotenv.SecretName(name) {
			kind = domain.VariableSecret
		}
		out = append(out, domain.Variable{
			Name:     name,
			Value:    domain.ValueText(data[key]),
			Kind:     kind,
			Enabled:  true,
			Position: len(out) + 1,
		})
	}
	return out
}

func requestNode(from resource, out *domain.Document) domain.CollectionNode {
	node := domain.CollectionNode{
		Name:        strings.TrimSpace(from.Name),
		Method:      strings.ToUpper(strings.TrimSpace(from.Method)),
		Description: strings.TrimSpace(from.Description),
		Auth:        requestAuth(from.Auth, out),
	}
	if node.Name == "" {
		node.Name = domain.UntitledRequest
	}
	if node.Method == "" {
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

	applyBody(&node, from.Body, out)
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

// applyBody reads what a request sends. A mime type this app has no kind for is written as the text it
// carried, because the text is what the request would send.
func applyBody(node *domain.CollectionNode, from *body, out *domain.Document) {
	if from == nil {
		return
	}
	if from.FileName != "" {
		out.LeftOut(domain.CodeSkippedBinaryFile, 1)
		return
	}
	mime := domain.MediaTypeOf(from.MimeType)
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

// requestAuth maps an export's scheme; an unknown one is counted and becomes `none`, not inherited credentials.
func requestAuth(from *auth, out *domain.Document) *domain.Auth {
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
		if strings.EqualFold(from.In, domain.PlaceQuery) || strings.EqualFold(from.AddTo, domain.PlaceQuery) {
			place = domain.PlaceQuery
		}
		return &domain.Auth{Type: domain.AuthAPIKey, Fields: map[string]string{
			"key": templated(from.Key), "value": templated(from.Value), "place": place,
		}}
	case "oauth2":
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

// template is Insomnia's way of naming a value (`{{ _.base_url }}`), written as this app writes it.
// Its other forms call functions and travel as they stand.
var template = regexp.MustCompile(`\{\{\s*_\.\s*([A-Za-z0-9_.-]+)\s*\}\}`)

func templated(text string) string {
	return template.ReplaceAllString(text, "{{$1}}")
}

// mimeOf is the media type without its parameters: `application/json; charset=utf-8` is json.
