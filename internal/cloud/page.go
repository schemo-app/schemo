package cloud

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net/url"
	"regexp"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// Building the document a page is made of, out of a collection as this device holds it.
//
// The walk is an allow-list: every field of the document is written by a line below and nothing is
// copied because it happened to be there. That makes a secret's value unreachable rather than
// merely removed: there is no field for it to arrive in, and the ports this package is handed
// cannot read one.

// PageID is the name a page gives something. The ids of a tree are what a synchronization stakes a
// claim with, so no id of one appears in a document: what a page shows is derived one-way from the
// id under the published collection's own, which nobody outside holds. Twelve hex digits are short
// enough for an address and long enough not to collide by accident.
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
// noting what it did not. It refuses what a page cannot hold at all (a tree too deep, a name too
// long, an address that is not one); a text that is merely long is cut and noted, since losing the
// tail of a description is no reason to lose a collection.
//
//wails:ignore
func (s *Publications) Build(
	ctx context.Context,
	workspace, collectionID, environmentID, locale string,
) (snapshotv1.Snapshot, Report, error) {
	return s.build(ctx, workspace, collectionID, environmentID, locale, nil)
}

func (s *Publications) build(
	ctx context.Context,
	workspace, collectionID, environmentID, locale string,
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
	held, published, err := s.store.Publication(ctx, collectionID)
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

// groups flattens a tree to the one level a page draws: every level with requests of its own
// becomes a heading under its own name, without the names of the levels it sits in. A page is read
// as one list of requests, and the folder a level was written in is no part of its name.
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
// once in a card of its own, and a `{{name}}` a hidden variable answers becomes the mask: a token
// names a variable, and the name of a secret is part of the secret.
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
// kept for it is the path of a file on this machine, which is about the person who published rather
// than about the request.
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
// left out and noted: the page groups answers by status, and one without a status has no tab to be
// put under.
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
// entirely rather than blanked, so what a page lists has no gaps, and a variable the author
// switched off is left out because the collection does not state it.
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

// variablesOf gathers the variables of every level of a collection: what a level answers for
// `{{tokens}}` is part of what the collection means, and a page that dropped a folder's would
// document a collection nobody has.
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

// Reading back the document a page is made of, so what one person published another can take into
// their own tree. The reader stands beside the writer on purpose: a second declaration of the shape
// would drift from the first, and what a publish writes is what an import has to read.

// The bounds a document is read under, and they are the server's own: what arrives here was stored
// by a server, and a server is not this app's to trust with however many bytes it chose to send.
const (
	maxPackedBytes   = 4 << 20
	maxUnpackedBytes = 16 << 20
)

// documentPath is where a page keeps the document of the collection it shows.
const documentPath = "/snapshot.json"

// DocumentAddress is the address a page's document is read from: the page's own address, which is
// what an import link carries, and the one path under it that serves the bytes the page was stored
// as. Only the two schemes a page can stand at are accepted: a link opens whatever it names, and a
// scheme this app has no business reading is not a place to fetch a collection from.
func DocumentAddress(page string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(page))
	if err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", domain.Refuse(domain.CodeURLUnreachable, domain.ErrNotAllowed,
			domain.Args{"url": page, "reason": "the address is not an http or https page"})
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	if !strings.HasSuffix(path, documentPath) {
		path += documentPath
	}
	return parsed.Scheme + "://" + parsed.Host + path, nil
}

// AsDocument reads the bytes a page serves as the collection to write into this app's tree: the
// requests with their parameters, bodies and examples, the one level a page draws put back into
// folders, the variables the page may show, and the environments its addresses were written
// against.
func AsDocument(packed []byte) (domain.Document, error) {
	held, err := expand(packed)
	if err != nil {
		return domain.Document{}, err
	}
	snapshot, err := snapshotv1.UnmarshalSnapshot(held)
	if err != nil {
		return domain.Document{}, notAPage()
	}
	// A document of another version is refused rather than read as if it were this one: a format
	// that quietly means less than it says is worse than a message.
	if snapshot.Format != snapshotv1.Format {
		return domain.Document{}, notAPage()
	}
	if snapshot.Version != snapshotv1.Version {
		return domain.Document{}, domain.Refuse(domain.CodeSnapshotVersion, domain.ErrNotAllowed,
			domain.Args{
				"found":     strconv.FormatInt(snapshot.Version, 10),
				"supported": strconv.Itoa(snapshotv1.Version),
			})
	}

	document := domain.Document{
		Collections:  []domain.Collection{readCollection(snapshot)},
		Environments: readEnvironments(snapshot),
	}
	if len(document.Environments) == 0 && len(snapshot.Collection.Groups) == 0 {
		return domain.Document{}, domain.Refuse(domain.CodeNothingToImport, domain.ErrNotAllowed, nil)
	}
	return document, nil
}

// Group IDs distinguish the collection's own requests from equally named child folders.
func readCollection(snapshot snapshotv1.Snapshot) domain.Collection {
	from := snapshot.Collection
	out := domain.Collection{
		Name:        from.Name,
		Description: from.Description,
	}
	for index, variable := range from.Variables {
		out.Variables = append(out.Variables, domain.Variable{
			Name:     variable.Key,
			Value:    variable.Value,
			Kind:     domain.VariableText,
			Enabled:  true,
			Position: index + 1,
		})
	}

	// One number runs over the requests and the folders both, because that is what draws them in
	// order: a page puts the requests of a level before the levels under it, and so does this.
	at := int64(0)
	own := true
	for _, group := range from.Groups {
		if own && group.ID == from.ID {
			own = false
			for _, endpoint := range group.Endpoints {
				at++
				out.Items = append(out.Items, readEndpoint(endpoint, at))
			}
			continue
		}
		at++
		out.Children = append(out.Children, readFolder(group, at))
	}
	return out
}

// readFolder is one heading of a page written back as a folder. A level inside a level was named
// for where it sat on the page, and it comes back as a folder of that name rather than the nesting
// the page had no room to draw.
func readFolder(group snapshotv1.Group, position int64) domain.Collection {
	out := domain.Collection{Name: group.Name, Description: group.Description, Position: position}
	for index, endpoint := range group.Endpoints {
		out.Items = append(out.Items, readEndpoint(endpoint, int64(index+1)))
	}
	return out
}

// readEndpoint is one request as this app holds it. Everything a page draws is read back here and
// nothing else: a page carries no headers, authorization or script, so a request taken from one
// arrives with its address, parameters, body and examples.
func readEndpoint(from snapshotv1.Endpoint, position int64) domain.CollectionNode {
	node := domain.CollectionNode{
		Name:        from.Name,
		Position:    position,
		Method:      from.Method,
		URL:         addressOf(from.Path),
		Description: from.Description,
	}
	for _, parameter := range from.Parameters {
		name := strings.TrimSpace(parameter.Name)
		if name == "" {
			continue
		}
		row := domain.Row{
			Name:        name,
			Value:       parameter.Example,
			Enabled:     true,
			Description: parameter.Description,
		}
		if parameter.In == "path" {
			node.PathParams = append(node.PathParams, row)
			continue
		}
		node.Params = append(node.Params, row)
	}
	readBody(from.Body, &node)
	for _, response := range from.Responses {
		node.Responses = append(node.Responses, domain.ResponseExample{
			Name:   response.Name,
			Status: int(response.Status),
			Body:   response.Body,
		})
	}
	return node
}

// addressOf is a request's address written back against the variable the environments answer: a
// page shows the path alone, because it shows the address once in a card of its own, and a request
// with nothing in front of it could not be sent anywhere.
func addressOf(path string) string {
	held := strings.TrimSpace(path)
	if held == "" || strings.HasPrefix(held, "{{") || strings.Contains(held, "://") {
		return held
	}
	return "{{" + domain.BaseURLVariable + "}}" + held
}

// readBody puts a body back the way it was written. A form is the rows its example was written as,
// with the file rows named again (their values were never on the page); anything else is the text
// the page showed, which is JSON when the page found fields in it.
func readBody(from *snapshotv1.Body, node *domain.CollectionNode) {
	if from == nil {
		return
	}
	if form := formRows(from); form != nil {
		node.BodyKind = domain.BodyForm
		node.Form = form
		return
	}
	node.Body = from.Example
	if len(from.Fields) > 0 {
		node.BodyKind = domain.BodyJSON
	}
}

func formRows(from *snapshotv1.Body) []domain.FormRow {
	files := 0
	for _, field := range from.Fields {
		if field.Type == "file" {
			files++
		}
	}
	if files == 0 {
		return nil
	}

	var rows []domain.FormRow
	for _, pair := range strings.Split(strings.TrimSpace(from.Example), "&") {
		if pair == "" {
			continue
		}
		name, value, _ := strings.Cut(pair, "=")
		rows = append(rows, domain.FormRow{
			Name:    unescaped(name),
			Value:   unescaped(value),
			Enabled: true,
		})
	}
	for _, field := range from.Fields {
		if field.Type == "file" {
			rows = append(rows, domain.FormRow{Name: field.Name, File: true, Enabled: true})
		}
	}
	return rows
}

func unescaped(text string) string {
	held, err := url.QueryUnescape(text)
	if err != nil {
		return text
	}
	return held
}

// readEnvironments is the addresses the page's requests were written against, as environments this
// app can hold: the variable every address here is in terms of, and the name the page gave it. An
// environment the page did not say where it stands for is left out, since it would be an
// environment of nothing.
func readEnvironments(snapshot snapshotv1.Snapshot) []domain.EnvironmentDraft {
	held := snapshot.Environments
	if len(held) == 0 && strings.TrimSpace(snapshot.Environment.BaseURL) != "" {
		held = []snapshotv1.Environment{snapshot.Environment}
	}

	out := make([]domain.EnvironmentDraft, 0, len(held))
	for _, environment := range held {
		base := strings.TrimSpace(environment.BaseURL)
		if base == "" {
			continue
		}
		out = append(out, domain.EnvironmentDraft{
			Name: environment.Name,
			Variables: []domain.Variable{{
				Name:     domain.BaseURLVariable,
				Value:    base,
				Kind:     domain.VariableText,
				Enabled:  true,
				Position: 1,
			}},
		})
	}
	return out
}

// expand is the document as it was written, read under a bound rather than after one: a document
// that expands without end has already been read into memory by the time its length is known.
func expand(packed []byte) ([]byte, error) {
	if len(packed) > maxPackedBytes {
		return nil, tooLarge(maxPackedBytes)
	}
	reader, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, notAPage()
	}
	defer func() { _ = reader.Close() }()

	held, err := io.ReadAll(io.LimitReader(reader, maxUnpackedBytes+1))
	if err != nil {
		return nil, notAPage()
	}
	if len(held) > maxUnpackedBytes {
		return nil, tooLarge(maxUnpackedBytes)
	}
	return held, nil
}

func notAPage() error {
	return domain.Refuse(domain.CodeNotSchemo, domain.ErrNotAllowed, nil)
}

// tooLarge names the bound that was crossed rather than one of them: the two differ by four times,
// and a person told the wrong number would go looking for whatever is holding sixteen megabytes.
func tooLarge(limit int) error {
	return domain.Refuse(domain.CodeFileTooLarge, domain.ErrNotAllowed,
		domain.Args{"path": documentPath, "limit": strconv.Itoa(limit >> 20)})
}

// List is the pages of a workspace as this device knows them, without asking anybody: a device that is
// offline still draws the list it last saw.
func (s *Publications) List(ctx context.Context, workspace string) ([]domain.Publication, error) {
	return s.store.OfWorkspace(ctx, workspace)
}

// Refresh asks the server what it holds and keeps the answer. There is nobody to ask on a device
// with no session, and what such a device holds is still the list a window draws.
func (s *Publications) Refresh(ctx context.Context, workspace string) ([]domain.Publication, error) {
	known, err := s.store.OfWorkspace(ctx, workspace)
	if err != nil {
		return nil, err
	}
	caller, err := s.caller.Caller(ctx)
	if err != nil {
		return known, nil
	}

	answered, err := s.askServerForPages(ctx, caller, workspace, known)
	if err != nil {
		return nil, err
	}
	for _, held := range answered {
		// The answer names the workspace by the account's id, and the row is this device's: kept as it
		// came, it would be a page under a workspace the window never asks about.
		held.WorkspaceID = workspace
		if err := s.store.SavePublication(ctx, held); err != nil {
			return nil, err
		}
	}
	return s.store.OfWorkspace(ctx, workspace)
}

// askServerForPages is the question this device may put about a workspace. The collections it holds
// here are asked about by id, because a collection nobody here published is not one to invent a
// question about; a device that holds none asks the workspace itself, whose pages a second device has
// as well.
func (s *Publications) askServerForPages(
	ctx context.Context,
	caller domain.Caller,
	workspace string,
	known []domain.Publication,
) ([]domain.Publication, error) {
	if len(known) > 0 {
		ids := make([]string, 0, len(known))
		for _, held := range known {
			ids = append(ids, held.CollectionID)
		}
		return s.server.Pages(ctx, caller, ids)
	}
	row, err := s.store.Workspace(ctx, workspace)
	if err != nil {
		return nil, err
	}
	if row.RemoteID == "" {
		return nil, nil
	}
	return s.server.Published(ctx, caller, row.RemoteID)
}

// descriptionProse derives public sections from top-level H2 headings. The collection keeps
// the complete Markdown document; code blocks, quotes and nested headings are not section titles.
func descriptionProse(markdown string) snapshotv1.Prose {
	source := []byte(markdown)
	document := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(source))
	type heading struct {
		start, end int
		key        string
	}
	var headings []heading
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		held, ok := node.(*ast.Heading)
		if !ok || held.Level > 2 || held.Lines().Len() == 0 {
			continue
		}
		first := held.Lines().At(0)
		last := held.Lines().At(held.Lines().Len() - 1)
		start := bytes.LastIndexByte(source[:first.Start], '\n') + 1
		end := lineEnd(source, last.Start)
		if source[start] != '#' && !strings.HasPrefix(strings.TrimSpace(string(source[start:first.Start])), "#") {
			// Setext headings include the underline after their text.
			end = lineEnd(source, end)
		}
		key := ""
		if held.Level == 2 {
			key = proseSection(headingTitle(held, source))
		}
		headings = append(headings, heading{start: start, end: end, key: key})
	}

	var lead strings.Builder
	sections := make(map[string]string)
	cursor := 0
	for index, held := range headings {
		if held.key == "" {
			continue
		}
		end := len(source)
		if index+1 < len(headings) {
			end = headings[index+1].start
		}
		lead.Write(source[cursor:held.start])
		cursor = end
		body := strings.TrimSpace(string(source[held.end:end]))
		sections[held.key] = strings.TrimSpace(sections[held.key] + "\n\n" + body)
	}
	lead.Write(source[cursor:])
	return snapshotv1.Prose{
		Lead: strings.TrimSpace(lead.String()), Auth: sections["auth"],
		Errors: sections["errors"], Limits: sections["limits"],
	}
}

func headingTitle(heading *ast.Heading, source []byte) string {
	var title strings.Builder
	_ = ast.Walk(heading, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := node.(type) {
		case *ast.Text:
			title.Write(node.Value(source))
			if node.SoftLineBreak() || node.HardLineBreak() {
				title.WriteByte(' ')
			}
		case *ast.String:
			title.Write(node.Value)
		}
		return ast.WalkContinue, nil
	})
	return title.String()
}

func proseSection(title string) string {
	switch strings.ToLower(strings.Join(strings.Fields(title), " ")) {
	case "authorization", "authentication", "auth", "авторизация":
		return "auth"
	case "errors", "ошибки":
		return "errors"
	case "limits", "rate limits", "limitations", "лимиты", "ограничения":
		return "limits"
	default:
		return ""
	}
}

func lineEnd(source []byte, start int) int {
	if offset := bytes.IndexByte(source[start:], '\n'); offset >= 0 {
		return start + offset + 1
	}
	return len(source)
}

type Choice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
}

type PublishPreview struct {
	Options      domain.PublicationOptions `json:"options"`
	Collection   string                    `json:"collectionId"`
	Publication  *domain.Publication       `json:"publication,omitempty"`
	Behind       bool                      `json:"behind"`
	Changes      []domain.PageItem         `json:"changes,omitempty"`
	Environments []Choice                  `json:"environments"`
	Report       Report                    `json:"report"`
	// InheritedFrom names the folder whose page contains this collection, and is empty for a
	// collection that holds a page of its own.
	InheritedFrom string `json:"inheritedFrom,omitempty"`
}

// Change is what this device can say about a collection's page without asking the server: the row
// that answers for it (its own, or one of a folder above it, since a page is a whole tree), whether
// that page is behind, and which of this collection's requests are. InheritedFrom names that
// folder.
type Change struct {
	Behind        bool                `json:"behind"`
	Changes       []domain.PageItem   `json:"changes,omitempty"`
	Page          *domain.Publication `json:"page,omitempty"`
	InheritedFrom string              `json:"inheritedFrom,omitempty"`
}

func (s *Publications) Preview(
	ctx context.Context,
	workspace, collectionID, environmentID string,
	options *domain.PublicationOptions,
) (PublishPreview, error) {
	owner, err := s.owningPageOf(ctx, workspace, collectionID)
	if err != nil {
		return PublishPreview{}, err
	}
	state, err := s.environments.State(ctx)
	if err != nil {
		return PublishPreview{}, err
	}
	// Saying nothing about the environment means the one the page went out with: the popover opens
	// on the page it is about, and a person who has not touched the field is asking for no change.
	if environmentID == "" {
		environmentID = environmentNamed(state, owner.Page.EnvironmentName)
	}
	snapshot, report, err := s.build(ctx, workspace, owner.Collection, environmentID, "", options)
	if err != nil {
		return PublishPreview{}, err
	}
	_, digest, err := Encode(snapshot)
	if err != nil {
		return PublishPreview{}, err
	}

	out := PublishPreview{
		Options:      optionsOf(snapshot),
		Collection:   owner.Collection,
		Environments: environmentChoices(state),
		Report:       report,
	}
	if owner.Held {
		out.Publication = &owner.Page
		out.Behind = behind(owner.Page, digest)
		items, err := s.changesInsideFolder(ctx, workspace, collectionID, owner, snapshot)
		if err != nil {
			return PublishPreview{}, err
		}
		out.Changes = items
		out.InheritedFrom = owner.inheritedFrom()
	}
	return out, nil
}

// Changes is the local half of the question, and the reason the popover can open without a network.
// It builds against the environment the page went out with rather than the one this device works in
// now: a page published from Staging is not behind because the app has since moved to Local.
func (s *Publications) Changes(
	ctx context.Context,
	workspace, collectionID string,
) (Change, error) {
	owner, err := s.owningPageOf(ctx, workspace, collectionID)
	if err != nil {
		return Change{}, err
	}
	if !owner.Held {
		return Change{}, nil
	}
	state, err := s.environments.State(ctx)
	if err != nil {
		return Change{}, err
	}
	snapshot, _, err := s.Build(ctx, workspace, owner.Collection,
		environmentNamed(state, owner.Page.EnvironmentName), "")
	if err != nil {
		return Change{}, err
	}
	_, digest, err := Encode(snapshot)
	if err != nil {
		return Change{}, err
	}
	items, err := s.changesInsideFolder(ctx, workspace, collectionID, owner, snapshot)
	if err != nil {
		return Change{}, err
	}
	return Change{
		Behind:        behind(owner.Page, digest),
		Changes:       items,
		Page:          &owner.Page,
		InheritedFrom: owner.inheritedFrom(),
	}, nil
}

// behind is whether a page differs from the document built now. A page that never recorded its hash
// is not behind: there is nothing to differ from.
func behind(page domain.Publication, digest string) bool {
	return page.ContentHash != "" && page.ContentHash != digest
}

// optionsOf is what a document was built with, as the popover draws it.
func optionsOf(snapshot snapshotv1.Snapshot) domain.PublicationOptions {
	return domain.PublicationOptions{
		Docs:     snapshot.Options.Docs,
		Examples: snapshot.Options.Examples,
	}
}

// changesInsideFolder is what moved at a page inside one folder of it: the outline the last publish
// put up, read against the document built now, both held to the folder's own requests. A folder the
// page itself is of has no filter, and its list is the page's whole one.
func (s *Publications) changesInsideFolder(
	ctx context.Context,
	workspace, collectionID string,
	owner owningPage,
	snapshot snapshotv1.Snapshot,
) ([]domain.PageItem, error) {
	inside, err := s.pageIDsInside(ctx, workspace, collectionID, owner.Collection)
	if err != nil {
		return nil, err
	}
	now, err := outlineOf(snapshot)
	if err != nil {
		return nil, err
	}
	items, _ := moved(within(owner.Page.Outline, inside), within(now, inside))
	return items, nil
}

// pageIDsInside is the page's own names for the requests the asked folder holds, every folder under
// it included: what a folder's popover draws, and nothing about the folders beside it. The folder
// the page itself is of returns no set, since every request of the document is already its own.
func (s *Publications) pageIDsInside(
	ctx context.Context,
	workspace, collectionID, pageID string,
) (map[string]bool, error) {
	if collectionID == pageID {
		return nil, nil
	}
	tree, err := s.collections.FullOf(ctx, workspace, collectionID)
	if err != nil {
		return nil, err
	}
	held := map[string]bool{}
	eachItem(tree, func(id string) { held[PageID(pageID, id)] = true })
	return held, nil
}

// environmentChoices is the environments a page's addresses may be read against.
func environmentChoices(state domain.EnvState) []Choice {
	out := make([]Choice, 0, len(state.Environments))
	for _, environment := range state.Environments {
		out = append(out, Choice{
			ID:      environment.ID,
			Name:    environment.Name,
			BaseURL: baseOf(environment),
		})
	}
	return out
}

// Which variable values a page may carry, and why the rest may not.
//
// The rule has no exception and no switch: a value the app already hides is hidden here too, and so
// is a name that reads like a credential's. A person is shown the list of what will not be on the
// page but is not offered a way to put one back.

// secretWords are the words that make a name a warning in itself. They are matched as whole words
// of a spelled-out name, so `api_key` and `apiKey` are both read as one and `monkey` is not read as
// `key`: the point is to catch what a person meant, not every letter in order.
var secretWords = map[string]bool{
	"token": true, "secret": true, "password": true, "passwd": true, "pass": true, "pwd": true,
	"key": true, "apikey": true, "auth": true, "authorization": true, "bearer": true,
	"session": true, "cookie": true, "signature": true, "credential": true, "credentials": true,
	"private": true,
}

// variableToken is a `{{name}}` as a value is written in terms of one. The grammar is the app's —
// a name is letters, digits and underscores — and anything else between braces is left alone.
var variableToken = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// hiddenNames is every name whose value a page may not carry, with the reason for each. The scopes
// are read together and not one by one: a name is hidden wherever it is used, so a global secret
// hides a collection variable written in terms of it, and so does one of another level.
func hiddenNames(scopes ...[]domain.Variable) map[string]domain.HiddenReason {
	var every []domain.Variable
	for _, scope := range scopes {
		every = append(every, scope...)
	}

	hidden := map[string]domain.HiddenReason{}
	value := map[string]string{}
	for _, variable := range every {
		value[variable.Name] = variable.Value
		if _, held := hidden[variable.Name]; held {
			continue
		}
		switch {
		case variable.Kind == domain.VariableSecret:
			hidden[variable.Name] = domain.HiddenSecret
		case looksLikeSecret(variable.Name):
			hidden[variable.Name] = domain.HiddenLooksSecret
		}
	}

	// A variable written in terms of a hidden one carries that value wherever it is used, so the
	// hiding closes over the references until nothing more is reachable from what is already
	// hidden.
	for changed := true; changed; {
		changed = false
		for _, variable := range every {
			if _, held := hidden[variable.Name]; held {
				continue
			}
			for _, name := range referenced(value[variable.Name]) {
				if _, held := hidden[name]; held {
					hidden[variable.Name] = domain.HiddenLinked
					changed = true
					break
				}
			}
		}
	}
	return hidden
}

// looksLikeSecret is whether a name reads like one a credential is kept in.
func looksLikeSecret(name string) bool {
	return slices.ContainsFunc(wordsOf(name), func(word string) bool { return secretWords[word] })
}

// referenced is the names a value is written in terms of.
func referenced(value string) []string {
	matches := variableToken.FindAllStringSubmatch(value, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match[1])
	}
	return out
}

// maskText replaces every `{{name}}` a hidden variable answers with the mask this app already uses
// for a value that must not leave it. The name goes with the value: a token names a variable, so a
// secret's name is part of the secret, and a page read by a stranger is no place for it.
func maskText(text string, hidden map[string]domain.HiddenReason) string {
	if len(hidden) == 0 || !strings.Contains(text, "{{") {
		return text
	}
	return variableToken.ReplaceAllStringFunc(text, func(token string) string {
		name := variableToken.FindStringSubmatch(token)[1]
		if _, held := hidden[name]; held {
			return domain.SecretMask
		}
		return token
	})
}

// wordsOf splits a name into the words it is made of, on the separators people write names with and
// on the case changes they write them across: `apiKey`, `API-KEY` and `api_key` are the same two
// words.
func wordsOf(name string) []string {
	var out []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			out = append(out, strings.ToLower(string(word)))
			word = nil
		}
	}

	runes := []rune(name)
	for index, char := range runes {
		switch {
		case char == '_' || char == '-' || char == '.' || char == ' ':
			flush()
		case unicode.IsUpper(char) && index > 0 &&
			(unicode.IsLower(runes[index-1]) || unicode.IsDigit(runes[index-1])):
			flush()
			word = append(word, char)
		default:
			word = append(word, char)
		}
	}
	flush()
	return out
}
