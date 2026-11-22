package insomnia

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
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
	Auth           *auth          `json:"authentication,omitempty"`
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

// writtenEnvironment is the base or a sub environment. Insomnia orders `data` itself, so the order
// is left to it.
type writtenEnvironment struct {
	writtenRow
	Name              string         `json:"name"`
	Color             *string        `json:"color"`
	Data              map[string]any `json:"data"`
	DataPropertyOrder any            `json:"dataPropertyOrder"`
	Private           bool           `json:"isPrivate"`
	SortKey           int            `json:"metaSortKey"`
}

// baseID is the base environment every sub environment hangs off.
const baseID = "env_base"

// Export writes a collection as an Insomnia v4 export: its variables as the base environment and
// each draft, already merged for this collection, as a sub environment. Scripts are not carried.
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
	file.Resources = append(file.Resources, writtenEnvironment{
		writtenRow: writtenRow{ID: baseID, Type: "environment", ParentID: parentID(workspaceID)},
		Name:       "Base Environment",
		Data:       nested(collection.Variables, secrets),
	})
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
	address, query := domain.SplitQuery(node.URL)
	return writtenRequest{
		writtenRow:     writtenRow{ID: id, Type: "request", ParentID: parentID(parent)},
		Name:           node.Name,
		Description:    node.Description,
		Method:         strings.ToUpper(node.Method),
		URL:            referenced(address),
		Headers:        exportedRows(node.Headers),
		Parameters:     exportedParameters(node.Params, domain.QueryPairs(query)),
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

// exportedParameters writes the rows, then the address's pairs no row covers, undecoded.
func exportedParameters(rows []domain.Row, query []domain.QueryPair) []writtenField {
	out := exportedRows(rows)
	accounted := map[string]bool{}
	for _, row := range rows {
		accounted[strings.TrimSpace(row.Name)] = true
	}
	for _, pair := range query {
		if pair.Name == "" || accounted[pair.Name] {
			continue
		}
		out = append(out, writtenField{Name: pair.Name, Value: referenced(pair.Value)})
	}
	return out
}

// exportedAuth writes the schemes both tools hold. A scheme with no shape here is left out rather
// than written as another: sending a credential the person never chose is worse than sending none.
func exportedAuth(from *domain.Auth) *auth {
	if from == nil {
		return nil
	}
	switch from.Type {
	case domain.AuthNone:
		return &auth{Type: "none"}
	case domain.AuthBearer:
		out := &auth{Type: "bearer", Token: referenced(from.Answer("token"))}
		// The default is the reader's own default, so writing it would say nothing.
		if prefix := from.Answer("prefix"); prefix != "" && prefix != "Bearer" {
			out.Prefix = prefix
		}
		return out
	case domain.AuthBasic:
		return &auth{
			Type:     "basic",
			Username: referenced(from.Answer("username")),
			Password: referenced(from.Answer("password")),
		}
	case domain.AuthAPIKey:
		return &auth{
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

// exportedEnvironments writes the drafts in the order they were given, under the base.
func exportedEnvironments(drafts []domain.EnvironmentDraft, secrets bool) []any {
	out := []any{}
	for index, draft := range drafts {
		var color *string
		if draft.Color != "" {
			color = &draft.Color
		}
		out = append(out, writtenEnvironment{
			writtenRow: writtenRow{
				ID:       fmt.Sprintf("env_%d", index+1),
				Type:     "environment",
				ParentID: parentID(baseID),
			},
			Name:    draft.Name,
			Color:   color,
			Data:    nested(draft.Variables, secrets),
			SortKey: index,
		})
	}
	return out
}

// nested writes `a.b` as `{"a": {"b": ...}}`, which `{{ _.a.b }}` reads; a name that cannot nest
// stays flat. Disabled variables are left out: Insomnia would apply them.
func nested(vars []domain.Variable, secrets bool) map[string]any {
	data := map[string]any{}
	for _, variable := range vars {
		name := strings.TrimSpace(variable.Name)
		if name == "" || !variable.Enabled {
			continue
		}
		value := exportedValue(variable, secrets)
		if _, held := data[name]; !nestInto(data, strings.Split(name, "."), value) && !held {
			data[name] = value
		}
	}
	return data
}

func nestInto(data map[string]any, path []string, value string) bool {
	if slices.Contains(path, "") {
		return false
	}
	for _, part := range path[:len(path)-1] {
		next, held := data[part]
		if !held {
			next = map[string]any{}
			data[part] = next
		}
		level, ok := next.(map[string]any)
		if !ok {
			return false
		}
		data = level
	}
	last := path[len(path)-1]
	if _, held := data[last]; held {
		return false
	}
	data[last] = value
	return true
}

// exportedValue is what the file may hold of a variable: a secret travels as nothing until the
// caller says the export is for somebody who may see it.
func exportedValue(variable domain.Variable, secrets bool) string {
	if variable.Kind == domain.VariableSecret && !secrets {
		return ""
	}
	return variable.Value
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
