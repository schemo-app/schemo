package publication

// Reading back the document a page is made of, so that what one person published another can take
// into their own tree. The reader stands beside the writer on purpose: a second declaration of the
// shape would drift from the first, and what a publish writes is what an import has to read.

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/url"
	"strconv"
	"strings"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"

	"schemo/internal/domain"
)

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
// as. Only the two schemes a page can stand at — a link opens whatever it names, and a scheme this
// app has no business reading is not something to go and fetch a collection from.
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
// for where it sat on the page, and it comes back as a folder of that name rather than as the
// nesting the page had no room to draw.
func readFolder(group snapshotv1.Group, position int64) domain.Collection {
	out := domain.Collection{Name: group.Name, Description: group.Description, Position: position}
	for index, endpoint := range group.Endpoints {
		out.Items = append(out.Items, readEndpoint(endpoint, int64(index+1)))
	}
	return out
}

// readEndpoint is one request as this app holds it. Everything a page draws is read back here and
// nothing else: a page carries no headers, no authorization and no script, so a request taken from
// one arrives with its address, its parameters, its body and its examples.
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
// with nothing in front of it would be one this app could not send anywhere.
func addressOf(path string) string {
	held := strings.TrimSpace(path)
	if held == "" || strings.HasPrefix(held, "{{") || strings.Contains(held, "://") {
		return held
	}
	return "{{" + domain.BaseURLVariable + "}}" + held
}

// readBody puts a body back the way it was written. A form is the rows its example was written as,
// with the rows that are files named again — their values were never on the page — and anything
// else is the text the page showed, which is JSON when the page found fields in it.
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
// environment the page did not say where it stands for is left out — it would be an environment of
// nothing, and the requests it answered were answered by the ones that stand.
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
