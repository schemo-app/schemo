// Package insomnia reads Insomnia v4 and v5 exports as collections and environments.
package insomnia

import (
	"cmp"
	"encoding/json"
	"regexp"
	"strings"

	"schemo/internal/domain"
)

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
		base, subs := read.environments(workspace.ID)
		vars := newValues()
		vars.add(variablesOf(base.Data, &out), &out)
		vars.add(variablesOf(workspace.Environment, &out), &out)
		read.level(workspace.ID, &collection, vars, &out)
		collection.Variables = vars.list

		drafts := make([]domain.EnvironmentDraft, 0, len(subs))
		for _, sub := range subs {
			if name := strings.TrimSpace(sub.Name); name != "" {
				drafts = append(drafts, subDraft(name, sub.Color,
					secrets(variablesOf(sub.Data, &out), nil, sub.IsPrivate)))
			}
		}
		// A workspace that held nothing is not written: it would be a row that opens onto nothing.
		if len(collection.Items) == 0 && len(collection.Children) == 0 &&
			len(collection.Variables) == 0 && len(drafts) == 0 {
			continue
		}
		out.Environments = append(out.Environments, blocksFor(len(out.Collections), drafts)...)
		out.Collections = append(out.Collections, collection)
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
	Color          string         `json:"color,omitempty" yaml:"-"`
	IsPrivate      bool           `json:"isPrivate,omitempty" yaml:"-"`
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
func (r *reader) level(parent string, into *domain.Collection, vars *values, out *domain.Document) {
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
			position++
			// Folders hold no variables here; theirs join the collection's.
			vars.add(variablesOf(one.Environment, out), out)
			r.level(one.ID, &nested, vars, out)
			into.Children = append(into.Children, nested)
		case "request":
			node := requestNode(one, out)
			node.Position = position
			position++
			into.Items = append(into.Items, node)
		}
	}
}

// environments splits a workspace's environments: the base hangs off the workspace and the sub
// environments off the base. Rows beside the base are read as sub environments too.
func (r *reader) environments(workspace string) (resource, []resource) {
	under := r.of(workspace, "environment")
	if len(under) == 0 {
		return resource{}, nil
	}
	return under[0], append(r.of(under[0].ID, "environment"), under[1:]...)
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
			Name: header.Name, Value: templated(header.Value, out), Enabled: !header.Disabled,
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
		value := templated(param.Value, out)
		node.Params = append(node.Params, domain.Row{
			Name: name, Value: value, Enabled: !param.Disabled,
			Description: strings.TrimSpace(param.Description),
		})
		if !param.Disabled {
			query = append(query, name+"="+value)
		}
	}
	node.URL, node.PathParams = addressOf(from, out)
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
func addressOf(from resource, out *domain.Document) (string, []domain.Row) {
	kept := map[string]field{}
	for _, param := range from.PathParameters {
		kept[param.Name] = param
	}
	rows := []domain.Row{}
	for _, name := range domain.PathParams(from.URL) {
		param := kept[name]
		rows = append(rows, domain.Row{
			Name: name, Value: templated(param.Value, out), Enabled: true,
			Description: strings.TrimSpace(param.Description),
		})
	}
	return templated(from.URL, out), rows
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
			row := domain.FormRow{Name: param.Name, Value: templated(param.Value, out), Enabled: !param.Disabled}
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
			node.Body = templated(from.Text, out)
			return
		}
		parts := []string{}
		for _, param := range from.Params {
			if param.Disabled {
				continue
			}
			parts = append(parts, param.Name+"="+templated(param.Value, out))
		}
		node.Body = strings.Join(parts, "&")
	case strings.Contains(mime, "json"):
		node.BodyKind = domain.BodyJSON
		node.Body = templated(from.Text, out)
	case strings.Contains(mime, "xml"):
		node.BodyKind = domain.BodyXML
		node.Body = templated(from.Text, out)
	default:
		node.BodyKind = domain.BodyRaw
		node.Body = templated(from.Text, out)
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
			"prefix": prefix, "token": templated(from.Token, out),
		}}
	case "basic":
		return &domain.Auth{Type: domain.AuthBasic, Fields: map[string]string{
			"username": templated(from.Username, out), "password": templated(from.Password, out),
		}}
	case "apikey":
		place := domain.PlaceHeader
		if strings.EqualFold(from.In, domain.PlaceQuery) || strings.EqualFold(from.AddTo, domain.PlaceQuery) {
			place = domain.PlaceQuery
		}
		return &domain.Auth{Type: domain.AuthAPIKey, Fields: map[string]string{
			"key": templated(from.Key, out), "value": templated(from.Value, out), "place": place,
		}}
	case "oauth2":
		return &domain.Auth{Type: domain.AuthOAuth2, Fields: map[string]string{
			"grant":        domain.GrantNamed(from.GrantType),
			"authUrl":      templated(from.AuthorizationURL, out),
			"tokenUrl":     templated(from.AccessTokenURL, out),
			"clientId":     templated(from.ClientID, out),
			"clientSecret": templated(from.ClientSecret, out),
			"scope":        templated(from.Scope, out),
		}}
	default:
		out.LeftOut(domain.CodeSkippedAuthScheme, 1)
		return &domain.Auth{Type: domain.AuthNone}
	}
}

// template is Insomnia's way of naming a value (`{{ _.base_url }}`), written as this app writes it.
var template = regexp.MustCompile(`\{\{\s*_\.\s*([A-Za-z0-9_.-]+)\s*\}\}`)

// templateTag is an Insomnia function call (`{% uuid %}`): it stays as text and is counted.
var templateTag = regexp.MustCompile(`(?s)\{%.*?%\}`)

func templated(text string, out *domain.Document) string {
	out.LeftOut(domain.CodeSkippedTemplateTag, len(templateTag.FindAllStringIndex(text, -1)))
	return template.ReplaceAllString(text, "{{$1}}")
}
