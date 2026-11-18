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

// writtenGroup is a folder together with its own variable overrides.
type writtenGroup struct {
	writtenRow
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Environment map[string]string `json:"environment"`
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

	tree := &writer{secrets: secrets}
	tree.walk(&collection, workspaceID)
	file.Resources = append(file.Resources, tree.rows...)
	file.Resources = append(file.Resources, exportedEnvironments(withCollectionDefaults(collection, environments), secrets)...)

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("the collection %q could not be written: %w", collection.Name, err)
	}
	return append(data, '\n'), nil
}

func withCollectionDefaults(collection domain.Collection, environments []domain.EnvironmentDraft) []domain.EnvironmentDraft {
	drafts := slices.DeleteFunc(slices.Clone(environments), func(env domain.EnvironmentDraft) bool { return env.Folder })
	defaults := ownVariables(collection)
	if len(defaults) == 0 {
		return drafts
	}
	for i := range drafts {
		if drafts[i].Base {
			drafts[i].Variables = append(slices.Clone(drafts[i].Variables), missing(defaults, drafts[i].Variables)...)
			return drafts
		}
	}
	return append([]domain.EnvironmentDraft{{Name: baseEnvironment, Base: true, Variables: defaults}}, drafts...)
}

func ownVariables(collection domain.Collection) []domain.Variable {
	variables := slices.Clone(collection.Variables)
	for _, env := range collection.Environments {
		if env.Folder {
			variables = append(variables, env.Variables...)
		}
	}
	return variables
}

// writer numbers the folders and the requests as it walks, each kind in the order the walk reaches
// it.
type writer struct {
	folders  int
	requests int
	rows     []any
	secrets  bool
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
				Environment: exportedVariables(ownVariables(*entry.Collection), w.secrets),
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

// exportedEnvironments writes the drafts in the order they were given, each hanging off the one
// workspace an export holds.
func exportedEnvironments(drafts []domain.EnvironmentDraft, secrets bool) []any {
	out := []any{}
	parent := workspaceID
	for _, draft := range drafts {
		if draft.Base {
			parent = "__BASE_ENVIRONMENT_ID__"
			break
		}
	}
	for index, draft := range drafts {
		id := fmt.Sprintf("env_%d", index+1)
		owner := parent
		if draft.Base {
			id, owner = parent, workspaceID
		}
		out = append(out, writtenEnvironment{
			writtenRow: writtenRow{
				ID:       id,
				Type:     "environment",
				ParentID: parentID(owner),
			},
			Name:    draft.Name,
			Data:    exportedVariables(draft.Variables, secrets),
			SortKey: index,
		})
	}
	return out
}

func exportedVariables(variables []domain.Variable, secrets bool) map[string]string {
	out := map[string]string{}
	for _, variable := range variables {
		if name := strings.TrimSpace(variable.Name); name != "" && variable.Enabled {
			out[name] = exportedValue(variable, secrets)
		}
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
