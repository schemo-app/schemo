package insomnia

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"schemo/internal/domain"
)

// workspaceID is the id every export gives its one workspace. Ids here are counted in walk order
// rather than made up, so the same collection always writes the same bytes.
const workspaceID = "wrk_1"

// exportFile is the document: the v4 markers Insomnia's own files carry, and one list holding
// everything — the workspace, the tree under it, and the environments beside it.
type exportFile struct {
	Type      string `json:"_type"`
	Format    int    `json:"__export_format"`
	Source    string `json:"__export_source"`
	Resources []any  `json:"resources"`
}

// writtenRow is what every resource carries: the id this export gave it, its kind, and the row it
// hangs off — null for the workspace, which hangs off nothing.
type writtenRow struct {
	ID       string  `json:"_id"`
	Type     string  `json:"_type"`
	ParentID *string `json:"parentId"`
}

type writtenWorkspace struct {
	writtenRow
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope"`
}

// writtenGroup is a folder, which the reader takes back as a collection inside one. The environment
// beside it is Insomnia's own place for a level's values, and this export leaves it empty.
type writtenGroup struct {
	writtenRow
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Environment map[string]any `json:"environment"`
}

type writtenRequest struct {
	writtenRow
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	Headers     []writtenField `json:"headers"`
	Parameters  []writtenField `json:"parameters"`
	// PathParameters hold the values of the `:name` segments of the url, which Insomnia writes
	// beside it and not in it.
	PathParameters []writtenField `json:"pathParameters,omitempty"`
	Auth           *writtenAuth   `json:"authentication,omitempty"`
	Body           *writtenBody   `json:"body,omitempty"`
}

// writtenField is a header, a parameter or a form row. Its value is written even when empty — a
// file row has no text to send and says so — and `disabled` only when the row is parked.
type writtenField struct {
	Name        string `json:"name"`
	Value       string `json:"value"`
	Disabled    bool   `json:"disabled,omitempty"`
	Type        string `json:"type,omitempty"`
	FileName    string `json:"fileName,omitempty"`
	Description string `json:"description,omitempty"`
}

type writtenBody struct {
	MimeType string         `json:"mimeType"`
	Text     string         `json:"text,omitempty"`
	Params   []writtenField `json:"params,omitempty"`
}

// writtenAuth is an authorization as the format holds one: the scheme's name, and under it the
// fields of that scheme alone.
type writtenAuth struct {
	Type     string `json:"type"`
	Token    string `json:"token,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Key      string `json:"key,omitempty"`
	Value    string `json:"value,omitempty"`
	In       string `json:"in,omitempty"`
}

// writtenEnvironment is one draft as the file keeps it. Insomnia orders `data` itself and says
// whether the values are private; this app has neither to hand it, so both are written empty.
type writtenEnvironment struct {
	writtenRow
	Name              string            `json:"name"`
	Data              map[string]string `json:"data"`
	DataPropertyOrder any               `json:"dataPropertyOrder"`
	Private           bool              `json:"isPrivate"`
	SortKey           int               `json:"metaSortKey"`
}

// Export writes a collection and the environments beside it as an Insomnia v4 export. Scripts are
// not carried: Insomnia runs its own, and a translation is not this app's to guess at.
func Export(
	collection domain.Collection,
	environments []domain.EnvironmentDraft,
	secrets bool,
) ([]byte, error) {
	file := exportFile{
		Type:      "export",
		Format:    4,
		Source:    "schemo",
		Resources: []any{},
	}
	file.Resources = append(file.Resources, writtenWorkspace{
		writtenRow:  writtenRow{ID: workspaceID, Type: "workspace"},
		Name:        collection.Name,
		Description: collection.Description,
		Scope:       "collection",
	})

	tree := &writer{}
	tree.walk(&collection, workspaceID)
	file.Resources = append(file.Resources, tree.rows...)
	file.Resources = append(file.Resources, exportedEnvironments(environments, secrets)...)

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("the collection %q could not be written: %w", collection.Name, err)
	}
	return append(data, '\n'), nil
}

// writer numbers the folders and the requests as it walks, each kind in the order the walk reaches
// it.
type writer struct {
	folders  int
	requests int
	rows     []any
}

// walk writes one level depth-first, in the order the tree draws it: a folder's own row comes
// before everything inside it, so no row ever arrives before the row it hangs off.
func (w *writer) walk(collection *domain.Collection, parent string) {
	for _, entry := range collection.Level() {
		if entry.Collection != nil {
			w.folders++
			id := fmt.Sprintf("fld_%d", w.folders)
			w.rows = append(w.rows, writtenGroup{
				writtenRow:  writtenRow{ID: id, Type: "request_group", ParentID: parentID(parent)},
				Name:        entry.Collection.Name,
				Description: entry.Collection.Description,
				Environment: map[string]any{},
			})
			w.walk(entry.Collection, id)
			continue
		}
		w.requests++
		id := fmt.Sprintf("req_%d", w.requests)
		w.rows = append(w.rows, exportedRequest(*entry.Node, id, parent))
	}
}

// exportedRequest writes one request. The query leaves the address — it travels as the parameter
// rows, which is where Insomnia reads it from — and the fragment stays where the address put it.
func exportedRequest(node domain.CollectionNode, id, parent string) writtenRequest {
	address, query := splitQuery(node.URL)
	return writtenRequest{
		writtenRow:     writtenRow{ID: id, Type: "request", ParentID: parentID(parent)},
		Name:           node.Name,
		Description:    node.Description,
		Method:         strings.ToUpper(node.Method),
		URL:            referenced(address),
		Headers:        exportedRows(node.Headers),
		Parameters:     exportedParameters(node.Params, queryOf(query)),
		Auth:           exportedAuth(node.Auth),
		PathParameters: exportedRows(node.PathParams),
		Body:           exportedBody(node),
	}
}

// exportedRows writes header rows the way the file keeps them: a name is what makes a row one, and
// `disabled` is written only for a row that is parked.
func exportedRows(rows []domain.Row) []writtenField {
	out := []writtenField{}
	for _, row := range rows {
		if strings.TrimSpace(row.Name) == "" {
			continue
		}
		out = append(out, writtenField{
			Name: row.Name, Value: referenced(row.Value), Disabled: !row.Enabled,
			Description: row.Description,
		})
	}
	return out
}

// exportedParameters writes the parameter rows, and then whatever the address carried that no row
// does: the address is this app's own source of query, and the rows usually match it.
func exportedParameters(rows []domain.Row, query []queryPair) []writtenField {
	out := exportedRows(rows)
	accounted := map[string]bool{}
	for _, row := range rows {
		accounted[strings.TrimSpace(row.Name)] = true
	}
	for _, pair := range query {
		if pair.name == "" || accounted[pair.name] {
			continue
		}
		out = append(out, writtenField{Name: pair.name, Value: referenced(pair.value)})
	}
	return out
}

// exportedAuth writes the schemes both tools hold. A scheme with no shape here is left out rather
// than written as another: sending a credential the person never chose is worse than sending none.
func exportedAuth(from *domain.Auth) *writtenAuth {
	if from == nil {
		return nil
	}
	switch from.Type {
	case domain.AuthNone:
		return &writtenAuth{Type: "none"}
	case domain.AuthBearer:
		out := &writtenAuth{Type: "bearer", Token: referenced(from.Answer("token"))}
		// The default is the reader's own default, so writing it would say nothing.
		if prefix := from.Answer("prefix"); prefix != "" && prefix != "Bearer" {
			out.Prefix = prefix
		}
		return out
	case domain.AuthBasic:
		return &writtenAuth{
			Type:     "basic",
			Username: referenced(from.Answer("username")),
			Password: referenced(from.Answer("password")),
		}
	case domain.AuthAPIKey:
		return &writtenAuth{
			Type:  "apikey",
			Key:   referenced(from.Answer("key")),
			Value: referenced(from.Answer("value")),
			In:    from.Answer("place"),
		}
	default:
		return nil
	}
}

// exportedBody writes what a request sends. A binary body has no shape here — the file's own names
// a path, and this app cannot say which one — so it is left out rather than guessed at.
func exportedBody(node domain.CollectionNode) *writtenBody {
	if node.BodyKind == domain.BodyBinary {
		return nil
	}
	if node.BodyKind == "" && node.Body == "" {
		return nil
	}
	switch domain.KindOf(node.BodyKind) {
	case domain.BodyForm:
		params := []writtenField{}
		for _, row := range node.Form {
			one := writtenField{Name: row.Name, Value: referenced(row.Value), Disabled: !row.Enabled}
			if row.File {
				one.Type = "file"
				one.FileName = row.Src
				one.Value = ""
			}
			params = append(params, one)
		}
		return &writtenBody{MimeType: domain.MediaMultipart, Params: params}
	case domain.BodyJSON:
		return &writtenBody{MimeType: domain.MediaJSON, Text: referenced(node.Body)}
	case domain.BodyXML:
		return &writtenBody{MimeType: domain.MediaXML, Text: referenced(node.Body)}
	default:
		return &writtenBody{MimeType: domain.MediaText, Text: referenced(node.Body)}
	}
}

// exportedEnvironments writes the drafts in the order they were given, each hanging off the one
// workspace an export holds.
func exportedEnvironments(drafts []domain.EnvironmentDraft, secrets bool) []any {
	out := []any{}
	for index, draft := range drafts {
		data := map[string]string{}
		for _, variable := range draft.Variables {
			name := strings.TrimSpace(variable.Name)
			if name == "" {
				continue
			}
			data[name] = exportedValue(variable, secrets)
		}
		out = append(out, writtenEnvironment{
			writtenRow: writtenRow{
				ID:       fmt.Sprintf("env_%d", index+1),
				Type:     "environment",
				ParentID: parentID(workspaceID),
			},
			Name:    draft.Name,
			Data:    data,
			SortKey: index,
		})
	}
	return out
}

// exportedValue is what the file may hold of a variable: a secret travels as nothing until the
// caller says the export is for somebody who may see it.
func exportedValue(variable domain.Variable, secrets bool) string {
	if variable.Kind == domain.VariableSecret && !secrets {
		return ""
	}
	return variable.Value
}

// splitQuery takes the query out of an address and leaves everything else where it was, the
// fragment included: a fragment is part of the address, not of the query.
func splitQuery(address string) (string, string) {
	rest, fragment, hasFragment := strings.Cut(address, "#")
	query := ""
	if at := strings.Index(rest, "?"); at >= 0 {
		rest, query = rest[:at], rest[at+1:]
	}
	if hasFragment {
		rest += "#" + fragment
	}
	return rest, query
}

// queryPair is one `name=value` of an address's query. Nothing is decoded: this app keeps these as
// the text the address wrote, and decoding would be an invention about what the text means.
type queryPair struct{ name, value string }

// queryOf reads the pairs of a query. A part with no `=` is a name on its own, which is how a flag
// is written, and it travels as a row with nothing beside it.
func queryOf(query string) []queryPair {
	out := []queryPair{}
	for _, part := range strings.Split(query, "&") {
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		out = append(out, queryPair{name: name, value: value})
	}
	return out
}

// parentID is an id the way the format writes a parent: null where there is none, which is the
// workspace alone.
func parentID(id string) *string { return &id }

// appTemplate is this app's `{{name}}`, which Insomnia names `{{ _.name }}`. The class is the
// reader's own, so the two forms convert each other exactly.
var appTemplate = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)

// referenced writes the `{{name}}` of a text as Insomnia's own form. A text that already holds that
// form is left alone: converting it twice would be inventing a variable.
func referenced(text string) string {
	return appTemplate.ReplaceAllStringFunc(text, func(match string) string {
		name := appTemplate.FindStringSubmatch(match)[1]
		if strings.HasPrefix(name, "_.") {
			return match
		}
		return "{{ _." + name + " }}"
	})
}
