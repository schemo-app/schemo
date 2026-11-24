package cloud

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/url"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"slices"
	"strconv"
	"strings"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
)

// Building a page's document from a collection. The walk is an allow-list: every field is written
// explicitly, so a secret value has no field to arrive in.

// PageID derives a public id one-way from the published collection's id, so no sync id leaks.
// Twelve hex digits are short for an address and still collision-safe.
func PageID(collectionID, entityID string) string {
	held := hmac.New(sha256.New, []byte(collectionID))
	held.Write([]byte(entityID))
	return hex.EncodeToString(held.Sum(nil))[:12]
}

// Report is what a page carries, counted, and what it does not, named — so that a person is told
// what a stranger will read before anything is published rather than after.
type Report struct {
	Folders   int       `json:"folders"`
	Requests  int       `json:"requests"`
	Examples  int       `json:"examples"`
	Variables int       `json:"variables"`
	Hidden    []Hidden  `json:"hidden,omitempty"`
	Problems  []Problem `json:"problems,omitempty"`
	// Bytes is what the publish carries, compressed: what the page weighs on the wire.
	Bytes int `json:"sizeBytes"`
}

// Hidden is one variable whose value does not go on a page, and the reason it does not.
type Hidden struct {
	Name   string              `json:"name"`
	Reason domain.HiddenReason `json:"reason"`
}

// Problem is one piece of a collection the page could not carry as it is. It is not a refusal: the
// page goes out without that piece, and the window says which request it was about.
type Problem struct {
	Code domain.Code `json:"code"`
	Name string      `json:"name"`
	Args domain.Args `json:"args,omitempty"`
}

// Build writes a collection as a page document. It refuses what a page cannot hold (too deep, a name
// too long, a bad address) and cuts and notes text that is merely long.
//
//wails:ignore
func (s *Publications) Build(
	ctx context.Context,
	workspace, collectionID, environmentID, locale string,
) (snapshotv1.Snapshot, Report, error) {
	var ids []string
	if environmentID != "" {
		ids = []string{environmentID}
	}
	return s.build(ctx, workspace, collectionID, ids, locale, nil)
}

func (s *Publications) build(
	ctx context.Context,
	workspace, collectionID string,
	environmentIDs []string,
	locale string,
	options *domain.PublicationOptions,
) (snapshotv1.Snapshot, Report, error) {
	tree, err := s.collections.FullOf(ctx, workspace, collectionID)
	if err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}
	state, err := s.environments.State(ctx)
	if err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}
	held, published, err := s.store.Publication(ctx, collectionID)
	if err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}
	selected := selectedEnvironments(state, held, environmentIDs)
	if err := tooMany("environments", len(selected), maxEnvironments); err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}
	var environment domain.Environment
	if len(selected) > 0 {
		environment = selected[0]
	}

	// Hiding considers every variable, but only the collection's own are published: globals and
	// environments are the reader's setup, not the collection.
	vars := [][]domain.Variable{variablesOf(tree, nil), state.Globals}
	for _, selected := range selected {
		vars = append(vars, selected.Vars, selected.Block(collectionID))
	}
	hidden := hiddenNames(vars...)

	walk := &page{collection: collectionID, hidden: hidden, base: baseOf(environment, collectionID)}
	groups, err := walk.groups(tree, 0)
	if err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}
	walk.report.Folders = len(groups)
	if err := tooMany("folders", walk.report.Folders, maxFolders); err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}
	if err := tooMany("requests", walk.report.Requests, maxRequests); err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}

	title := strings.TrimSpace(tree.Name)
	if title == "" {
		err := domain.Refuse(domain.CodeNameEmpty, domain.ErrNotAllowed, nil)
		return snapshotv1.Snapshot{}, Report{}, err
	}
	shown := maskText(title, hidden)
	if err := checkName(shown, maxTitleRunes); err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}
	variables, left, err := walk.variables(tree)
	if err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}

	// Mask the complete document before deriving the public sections from it.
	description := walk.cut(title, tree.Description, maxTextRunes)
	addresses := listed(selected, collectionID, hidden)

	snapshot := snapshotv1.Snapshot{
		Format:    snapshotv1.Format,
		Version:   snapshotv1.Version,
		Generator: snapshotv1.Generator{Name: platform.Name, Version: platform.NewBuildInfo().Version},
		Locale:    locale,
		Collection: snapshotv1.Collection{
			ID:          PageID(collectionID, collectionID),
			Name:        shown,
			Description: description,
			Groups:      groups,
			Variables:   variables,
		},
		Environment:  environmentOf(environment, collectionID, hidden),
		Environments: addresses,
		Prose:        descriptionProse(description),
	}

	// What has moved at the page since it last went up travels with the document, so the digest covers
	// it: a page whose changelog grew is a page that moved, and a device can say so while offline.
	chosen := held.PageOptions()
	if options != nil {
		chosen = *options
	}
	applyOptions(&snapshot, chosen)
	if !chosen.Examples {
		walk.report.Examples = 0
	}
	if published {
		items, cut, err := pending(held, snapshot)
		if err != nil {
			return snapshotv1.Snapshot{}, Report{}, err
		}
		if cut {
			walk.note(domain.CodeTooManyItems, shown,
				domain.Args{"what": "changes", "max": strconv.Itoa(maxReleaseItems)})
		}
		snapshot.Changes = snapshotChanges(appended(held.Changes, items, s.now(), held.Revision+1))
	}

	packed, _, err := Encode(snapshot)
	if err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}
	report := walk.report
	report.Variables = len(variables)
	report.Hidden = left
	report.Bytes = len(packed)
	return snapshot, report, nil
}

// page is one walk of one collection, holding what the whole walk has to answer the same way.
type page struct {
	collection string
	base       string
	hidden     map[string]domain.HiddenReason
	report     Report
}

// groups flattens the tree: every level with requests becomes a heading under its own name.
func (p *page) groups(level domain.Collection, depth int) ([]snapshotv1.Group, error) {
	if depth > maxDepth {
		return nil, domain.Refuse(domain.CodeFoldersTooDeep, domain.ErrNotAllowed,
			domain.Args{"max": strconv.Itoa(maxDepth)})
	}
	name := strings.TrimSpace(level.Name)
	if name == "" {
		return nil, domain.Refuse(domain.CodeNameEmpty, domain.ErrNotAllowed, nil)
	}
	shown := maskText(name, p.hidden)
	if err := checkName(shown, maxNameRunes); err != nil {
		return nil, err
	}

	group := snapshotv1.Group{ID: PageID(p.collection, level.ID), Name: shown}
	if depth > 0 {
		group.Description = p.cut(shown, level.Description, maxTextRunes)
	}
	for _, node := range level.Items {
		endpoint, err := p.endpoint(node)
		if err != nil {
			return nil, err
		}
		group.Endpoints = append(group.Endpoints, endpoint)
	}

	var out []snapshotv1.Group
	if len(group.Endpoints) > 0 || group.Description != "" {
		out = append(out, group)
	}
	for _, child := range level.Children {
		nested, err := p.groups(child, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, nested...)
	}
	return out, nil
}

// endpoint is one request as a page shows it. Everything a page draws is written here and nothing
// else: no header, no authorization, no script, no environment binding.
func (p *page) endpoint(node domain.CollectionNode) (snapshotv1.Endpoint, error) {
	name := strings.TrimSpace(node.Name)
	if name == "" {
		name = domain.UntitledRequest
	}
	shown := maskText(name, p.hidden)
	if err := checkName(shown, maxNameRunes); err != nil {
		return snapshotv1.Endpoint{}, err
	}
	if !node.Protocol.Valid() {
		return snapshotv1.Endpoint{}, domain.Refuse(domain.CodeProtocolUnsupported, domain.ErrNotAllowed,
			domain.Args{"scheme": string(node.Protocol)})
	}
	method, err := methodOf(node.Protocol.HTTPMethod(node.Method))
	if err != nil {
		return snapshotv1.Endpoint{}, err
	}
	path, err := p.path(node.URL, node.Protocol)
	if err != nil {
		return snapshotv1.Endpoint{}, err
	}
	parameters, err := p.parameters(node)
	if err != nil {
		return snapshotv1.Endpoint{}, err
	}
	body, err := p.body(name, node)
	if err != nil {
		return snapshotv1.Endpoint{}, err
	}
	responses, err := p.responses(name, node)
	if err != nil {
		return snapshotv1.Endpoint{}, err
	}

	p.report.Requests++
	p.report.Examples += len(responses)
	endpoint := snapshotv1.Endpoint{
		ID:          PageID(p.collection, node.ID),
		Name:        shown,
		Method:      method,
		Path:        path,
		Description: p.cut(name, node.Description, maxTextRunes),
		Parameters:  parameters,
		Body:        body,
		Responses:   responses,
	}
	p.writeEndpointProtocol(&endpoint, node)
	return endpoint, nil
}

// parameters are the rows a request is written in terms of. A row named like a credential keeps its
// name and its description and loses its example: an example of a key is a key.
func (p *page) parameters(node domain.CollectionNode) ([]snapshotv1.Parameter, error) {
	rows := make([]paramRow, 0, len(node.PathParams)+len(node.Params))
	for _, row := range node.PathParams {
		rows = append(rows, paramRow{row: row, where: "path"})
	}
	for _, row := range node.Params {
		rows = append(rows, paramRow{row: row, where: "query"})
	}
	if err := tooMany("parameters", len(rows), maxParams); err != nil {
		return nil, err
	}

	out := make([]snapshotv1.Parameter, 0, len(rows))
	for _, held := range rows {
		name := strings.TrimSpace(held.row.Name)
		if name == "" {
			continue
		}
		if err := checkName(name, maxNameRunes); err != nil {
			return nil, err
		}
		example := maskText(held.row.Value, p.hidden)
		if looksLikeSecret(name) {
			example = ""
		}
		out = append(out, snapshotv1.Parameter{
			Name:        name,
			In:          held.where,
			Type:        "string",
			Required:    held.where == "path",
			Description: p.cut(name, held.row.Description, maxTextRunes),
			Example:     p.clipped(name, example, maxExampleRunes),
		})
	}
	return out, nil
}

// paramRow is one row with the page's word for where it is read.
type paramRow struct {
	row   domain.Row
	where string
}

// body is the request's example body. A binary body is omitted, since only a local file path is kept.
func (p *page) body(owner string, node domain.CollectionNode) (*snapshotv1.Body, error) {
	if node.Protocol.Live() {
		return nil, nil
	}
	switch domain.KindOf(node.BodyKind) {
	case domain.BodyBinary:
		p.note(domain.CodeBodyTypeUnsupported, owner, nil)
		return nil, nil
	case domain.BodyForm:
		return p.formBody(owner, node)
	default:
		text := strings.TrimSpace(node.Body)
		if text == "" {
			return nil, nil
		}
		fields, err := fieldsOf(text)
		if err != nil {
			return nil, err
		}
		return &snapshotv1.Body{
			Fields:  fields,
			Example: p.clipped(owner, maskText(text, p.hidden), maxExampleRunes),
		}, nil
	}
}

// formBody is a form body as the rows it is made of. A row that is a file keeps its name and loses
// its value for the same reason a binary body does.
func (p *page) formBody(owner string, node domain.CollectionNode) (*snapshotv1.Body, error) {
	held := make([]domain.FormRow, 0, len(node.Form))
	for _, row := range node.Form {
		if row.Enabled {
			held = append(held, row)
		}
	}
	if err := tooMany("fields", len(held), maxFields); err != nil {
		return nil, err
	}

	fields := make([]snapshotv1.Field, 0, len(held))
	pairs := make([]string, 0, len(held))
	for _, row := range held {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		if err := checkName(name, maxNameRunes); err != nil {
			return nil, err
		}
		if row.File {
			fields = append(fields, snapshotv1.Field{Name: name, Type: "file"})
			continue
		}
		fields = append(fields, snapshotv1.Field{Name: name, Type: "string"})
		pairs = append(pairs, url.QueryEscape(name)+"="+url.QueryEscape(maskText(row.Value, p.hidden)))
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return &snapshotv1.Body{
		Fields:  fields,
		Example: p.clipped(owner, strings.Join(pairs, "&"), maxExampleRunes),
	}, nil
}

// responses are the saved examples; one without a status is skipped and noted, since pages group by status.
func (p *page) responses(owner string, node domain.CollectionNode) ([]snapshotv1.Response, error) {
	if err := tooMany("responses", len(node.Responses), maxResponses); err != nil {
		return nil, err
	}

	out := make([]snapshotv1.Response, 0, len(node.Responses))
	for _, example := range node.Responses {
		if !node.Protocol.ValidStatus(example.Status) {
			p.note(domain.CodeStatusOutOfRange, owner,
				domain.Args{"status": strconv.Itoa(example.Status)})
			continue
		}
		name := strings.TrimSpace(example.Name)
		shown := maskText(name, p.hidden)
		if err := checkName(shown, maxNameRunes); err != nil {
			return nil, err
		}
		out = append(out, snapshotv1.Response{
			Status: int64(example.Status),
			Name:   shown,
			Body:   p.clipped(owner, maskText(example.Body, p.hidden), maxExampleRunes),
		})
	}
	return out, nil
}

// variables are the collection's own enabled variables; hidden ones are omitted rather than blanked.
func (p *page) variables(tree domain.Collection) ([]snapshotv1.Variable, []Hidden, error) {
	held := variablesOf(tree, nil)
	if err := tooMany("variables", len(held), maxVariables); err != nil {
		return nil, nil, err
	}

	out := make([]snapshotv1.Variable, 0, len(held))
	left := map[string]domain.HiddenReason{}
	for _, variable := range held {
		name := strings.TrimSpace(variable.Name)
		if name == "" || !variable.Enabled {
			continue
		}
		if reason, hidden := p.hidden[name]; hidden {
			left[name] = reason
			continue
		}
		out = append(out, snapshotv1.Variable{
			Key:   name,
			Value: p.cut(name, variable.Value, maxTextRunes),
		})
	}

	hidden := make([]Hidden, 0, len(left))
	for _, name := range slices.Sorted(maps.Keys(left)) {
		hidden = append(hidden, Hidden{Name: name, Reason: left[name]})
	}
	return out, hidden, nil
}

// cut holds prose to what a page takes and masks hidden values in it.
func (p *page) cut(owner, text string, most int) string {
	return maskText(p.clipped(owner, text, most), p.hidden)
}

// clipped shortens text to most runes, noting whose it was when it had to be shortened.
func (p *page) clipped(owner, text string, most int) string {
	clipped, was := clip(text, most)
	if was {
		p.note(domain.CodeTextTooLong, owner, domain.Args{"max": strconv.Itoa(most)})
	}
	return clipped
}

// note records one piece of a collection the page could not carry as it is.
func (p *page) note(code domain.Code, name string, args domain.Args) {
	p.report.Problems = append(p.report.Problems, Problem{Code: code, Name: name, Args: args})
}

// fieldsOf lists a body's fields from its example text, the only form a body is kept in.
func fieldsOf(example string) ([]snapshotv1.Field, error) {
	var shape map[string]any
	if err := json.Unmarshal([]byte(example), &shape); err != nil {
		return nil, nil
	}
	if err := tooMany("fields", len(shape), maxFields); err != nil {
		return nil, err
	}

	out := make([]snapshotv1.Field, 0, len(shape))
	for _, name := range slices.Sorted(maps.Keys(shape)) {
		out = append(out, snapshotv1.Field{Name: name, Type: jsonType(shape[name])})
	}
	return out, nil
}

// jsonType is the word a page writes beside a field, from the shape of the example's value.
func jsonType(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "string"
}

// schemeOf is the protocol of an address written absolutely, and nothing at all for one written in
// terms of a variable or of the base address.
func schemeOf(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return parsed.Scheme
}

// variablesOf gathers the variables of every level of a collection.
func variablesOf(level domain.Collection, into []domain.Variable) []domain.Variable {
	into = append(into, level.Variables...)
	for _, child := range level.Children {
		into = variablesOf(child, into)
	}
	return into
}

// active is the environment a page's base address is read from: the one the person chose, or the
// one the app is working in.
func active(state domain.EnvState, chosen string) domain.Environment {
	if environment, ok := state.Find(chosen); ok && chosen != "" {
		return environment
	}
	environment, _ := state.Find(state.ActiveID)
	return environment
}

// environmentNamed finds the page's environment by name (ids are per device); empty when unknown.
func environmentNamed(state domain.EnvState, name string) string {
	if name == "" {
		return ""
	}
	for _, environment := range state.Environments {
		if environment.Name == name {
			return environment.ID
		}
	}
	return ""
}

// baseOf is the baseUrl an environment gives a collection: its block wins over the shared value.
func baseOf(environment domain.Environment, collection string) string {
	base := ""
	for _, vars := range [][]domain.Variable{environment.Vars, environment.Block(collection)} {
		for _, variable := range vars {
			if variable.Enabled && variable.Name == domain.BaseURLVariable {
				base = variable.Value
			}
		}
	}
	return base
}

// selectedEnvironments restores publication choices by name because IDs are device-local.
func selectedEnvironments(state domain.EnvState, held domain.Publication, ids []string) []domain.Environment {
	if ids == nil {
		for _, name := range held.EnvironmentNames {
			if id := environmentNamed(state, name); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			environment := active(state, environmentNamed(state, held.EnvironmentName))
			if environment.ID != "" {
				ids = []string{environment.ID}
			}
		}
	}
	out := make([]domain.Environment, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if environment, ok := state.Find(id); ok && !seen[id] {
			out = append(out, environment)
			seen[id] = true
		}
	}
	return out
}

func listed(selected []domain.Environment, collection string, hidden map[string]domain.HiddenReason) []snapshotv1.Environment {
	out := make([]snapshotv1.Environment, 0, len(selected))
	for _, environment := range selected {
		out = append(out, environmentOf(environment, collection, hidden))
	}
	return out
}

func environmentNames(snapshot snapshotv1.Snapshot) []string {
	names := make([]string, 0, len(snapshot.Environments))
	for _, environment := range snapshot.Environments {
		names = append(names, environment.Name)
	}
	return names
}

// environmentOf is one environment as a page may name it: what it is called and where it stands,
// and nothing else of it, because nothing else of it is the reader's business.
func environmentOf(
	environment domain.Environment,
	collection string,
	hidden map[string]domain.HiddenReason,
) snapshotv1.Environment {
	return snapshotv1.Environment{
		Name:    maskText(environment.Name, hidden),
		BaseURL: maskText(baseOf(environment, collection), hidden),
	}
}
