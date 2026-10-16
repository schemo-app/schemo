package publication

// Building the document a page is made of, out of a collection as this device holds it.
//
// The walk is an allow-list: every field of the document is written by a line below, and nothing is
// copied because it happened to be there. That is what makes a secret's value unreachable rather
// than merely removed — there is no field for it to arrive in, and the ports this package is
// handed have no way to read one.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// PageID is the name a page gives something. The ids of a tree are what a synchronization stakes a
// claim with, so no id of one appears in a document: what a page shows is derived from the id under
// the published collection's own, which nobody outside holds, and the derivation is one-way. Twelve
// hex digits are short enough for an address and long enough not to collide by accident.
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

// Build reads a collection and writes the document a page is made of, counting what it carried and
// noting what it did not. It refuses what a page cannot hold at all — a tree too deep, a name too
// long, an address that is not one; a text that is merely long is cut and noted, because losing the
// tail of a description is not a reason to lose a collection.
func (u *UseCase) Build(
	ctx context.Context,
	workspace, collectionID, environmentID, locale string,
) (snapshotv1.Snapshot, Report, error) {
	return u.build(ctx, workspace, collectionID, environmentID, locale, nil)
}

func (u *UseCase) build(
	ctx context.Context,
	workspace, collectionID, environmentID, locale string,
	options *domain.PublicationOptions,
) (snapshotv1.Snapshot, Report, error) {
	tree, err := u.collections.FullOf(ctx, workspace, collectionID)
	if err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}
	state, err := u.environments.Snapshot(ctx)
	if err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}
	environment := active(state, environmentID)

	// What is hidden is decided over every name the app holds, and only the collection's own
	// variables are offered to the page: a global or an environment variable is the reader's own
	// working setup rather than a fact about the collection being documented.
	hidden := hiddenNames(variablesOf(tree, nil), state.Globals, environment.Vars)

	walk := &page{collection: collectionID, hidden: hidden, base: baseOf(environment)}
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
	addresses, err := listed(state, environment, hidden)
	if err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}

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
		Environment:  environmentOf(environment, hidden),
		Environments: addresses,
		Prose:        descriptionProse(description),
	}

	// What has moved at the page since it last went up travels with the document, so the digest covers
	// it: a page whose changelog grew is a page that moved, and a device can say so while offline.
	held, published, err := u.store.Publication(ctx, collectionID)
	if err != nil {
		return snapshotv1.Snapshot{}, Report{}, err
	}
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
		snapshot.Changes = snapshotChanges(appended(held.Changes, items, u.now(), held.Revision+1))
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

// groups flattens a tree to the one level a page draws: every level with requests of its own
// becomes a heading under its own name, without the names of the levels it sits in — a page is
// read as one list of requests, and the folder a level was written in is no part of its name.
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
	method, err := methodOf(node.Method)
	if err != nil {
		return snapshotv1.Endpoint{}, err
	}
	path, err := p.path(node.URL)
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
	return snapshotv1.Endpoint{
		ID:          PageID(p.collection, node.ID),
		Name:        shown,
		Method:      method,
		Path:        path,
		Description: p.cut(name, node.Description, maxTextRunes),
		Parameters:  parameters,
		Body:        body,
		Responses:   responses,
	}, nil
}

// path is the address as a row draws it. The base address is taken off, because the page shows it
// once in a card of its own rather than on every line, and a `{{name}}` a hidden variable answers
// becomes the mask — a token names a variable, and the name of a secret is part of the secret.
func (p *page) path(address string) (string, error) {
	raw := strings.TrimSpace(address)
	for _, char := range raw {
		if char < 0x20 || char == 0x7f {
			return "", domain.Refuse(domain.CodeURLControlChar, domain.ErrNotAllowed, nil)
		}
	}
	if scheme := schemeOf(raw); scheme != "" && scheme != "http" && scheme != "https" {
		return "", domain.Refuse(domain.CodeProtocolUnsupported, domain.ErrNotAllowed,
			domain.Args{"scheme": scheme})
	}

	path := strings.TrimPrefix(raw, "{{"+domain.BaseURLVariable+"}}")
	if p.base != "" {
		path = strings.TrimPrefix(path, p.base)
	}
	if utf8.RuneCountInString(path) > maxTextRunes {
		return "", domain.Refuse(domain.CodeURLTooLong, domain.ErrNotAllowed,
			domain.Args{"max": strconv.Itoa(maxTextRunes)})
	}
	return maskText(path, p.hidden), nil
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
			Example:     clipExample(name, example, &p.report),
		})
	}
	return out, nil
}

// paramRow is one row with the page's word for where it is read.
type paramRow struct {
	row   domain.Row
	where string
}

// body is the one example of a request body a page draws. A binary body is left out whole: what is
// kept for it is the path of a file on this machine, and a path is about the person who published
// rather than about the request.
func (p *page) body(owner string, node domain.CollectionNode) (*snapshotv1.Body, error) {
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
			Example: clipExample(owner, maskText(text, p.hidden), &p.report),
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
		Example: clipExample(owner, strings.Join(pairs, "&"), &p.report),
	}, nil
}

// responses are the example answers saved beside a request. An example with no status to show is
// left out and noted: the page groups answers by the status they carried, and one without a status
// has no tab to be put under.
func (p *page) responses(owner string, node domain.CollectionNode) ([]snapshotv1.Response, error) {
	if err := tooMany("responses", len(node.Responses), maxResponses); err != nil {
		return nil, err
	}

	out := make([]snapshotv1.Response, 0, len(node.Responses))
	for _, example := range node.Responses {
		if example.Status < 100 || example.Status > 599 {
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
			Body:   clipExample(owner, maskText(example.Body, p.hidden), &p.report),
		})
	}
	return out, nil
}

// variables is the collection's own variables as a page may carry them. A hidden one is left out
// entirely rather than blanked, so that what a page lists has no gaps in it, and a variable the
// author has switched off is left out because it is not one the collection states.
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

	names := make([]string, 0, len(left))
	for name := range left {
		names = append(names, name)
	}
	sort.Strings(names)
	hidden := make([]Hidden, 0, len(names))
	for _, name := range names {
		hidden = append(hidden, Hidden{Name: name, Reason: left[name]})
	}
	return out, hidden, nil
}

// cut holds a text to what a page takes, noting whose it was when it has to be shortened.
func (p *page) cut(owner, text string, most int) string {
	clipped, was := clip(text, most)
	if was {
		p.note(domain.CodeTextTooLong, owner, domain.Args{"max": strconv.Itoa(most)})
	}
	return maskText(clipped, p.hidden)
}

// note records one piece of a collection the page could not carry as it is.
func (p *page) note(code domain.Code, name string, args domain.Args) {
	p.report.Problems = append(p.report.Problems, Problem{Code: code, Name: name, Args: args})
}

// clipExample is cut for a value shown as an example, which is a body rather than prose and is
// measured by its own ceiling.
func clipExample(owner, value string, report *Report) string {
	clipped, was := clip(value, maxExampleRunes)
	if was {
		report.Problems = append(report.Problems, Problem{
			Code: domain.CodeTextTooLong,
			Name: owner,
			Args: domain.Args{"max": strconv.Itoa(maxExampleRunes)},
		})
	}
	return clipped
}

// fieldsOf reads the fields of a body off its example: this app keeps a request body as text, so
// the example is the whole of what there is to say about it, and what a page can list is what it
// shows.
func fieldsOf(example string) ([]snapshotv1.Field, error) {
	var shape map[string]any
	if err := json.Unmarshal([]byte(example), &shape); err != nil {
		return nil, nil
	}
	if err := tooMany("fields", len(shape), maxFields); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(shape))
	for name := range shape {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]snapshotv1.Field, 0, len(names))
	for _, name := range names {
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

// variablesOf gathers the variables of every level of a collection: what a level answers for
// `{{tokens}}` is part of what the collection means, and a page that dropped a folder's would be a
// page about a collection nobody has.
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
	if chosen != "" {
		for _, environment := range state.Environments {
			if environment.ID == chosen {
				return environment
			}
		}
	}
	for _, environment := range state.Environments {
		if environment.ID == state.ActiveID {
			return environment
		}
	}
	return domain.Environment{}
}

// environmentNamed is the environment a page was built against, found by the name the server kept:
// an id would be this device's own and a renamed environment would lose its page. Empty when the
// name is unknown here, which leaves the app's own environment standing.
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

// baseOf is the address an environment stands for. It is the one variable every environment has; an
// environment written without it is one whose addresses a page cannot show.
func baseOf(environment domain.Environment) string {
	for _, variable := range environment.Vars {
		if variable.Name == domain.BaseURLVariable {
			return variable.Value
		}
	}
	return ""
}

// listed is the addresses a page may be called from: every environment that stands somewhere, with
// the one this page was built against first, because that is the address a reader is handed. An
// environment with no address of its own is left out rather than named with nothing beside it.
func listed(
	state domain.EnvState,
	chosen domain.Environment,
	hidden map[string]domain.HiddenReason,
) ([]snapshotv1.Environment, error) {
	out := make([]snapshotv1.Environment, 0, len(state.Environments))
	for _, environment := range state.Environments {
		stands := strings.TrimSpace(baseOf(environment)) != ""
		if stands && environment.ID == chosen.ID {
			out = append(out, environmentOf(environment, hidden))
		}
	}
	for _, environment := range state.Environments {
		stands := strings.TrimSpace(baseOf(environment)) != ""
		if stands && environment.ID != chosen.ID {
			out = append(out, environmentOf(environment, hidden))
		}
	}
	if err := tooMany("environments", len(out), maxEnvironments); err != nil {
		return nil, err
	}
	return out, nil
}

// environmentOf is one environment as a page may name it: what it is called and where it stands,
// and nothing else of it, because nothing else of it is the reader's business.
func environmentOf(
	environment domain.Environment,
	hidden map[string]domain.HiddenReason,
) snapshotv1.Environment {
	return snapshotv1.Environment{
		Name:    maskText(environment.Name, hidden),
		BaseURL: maskText(baseOf(environment), hidden),
	}
}
