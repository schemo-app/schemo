package cloud

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/url"
	"schemo/internal/domain"
	"strconv"
	"strings"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
)

// Reading a published page back as a collection to import.

// Read bounds match the server's: stored bytes are not trusted to be small.
const (
	maxPackedBytes   = 4 << 20
	maxUnpackedBytes = 16 << 20
)

// documentPath is where a page keeps the document of the collection it shows.
const documentPath = "/snapshot.json"

// DocumentAddress is where a page's document is served. Only http and https are accepted, since a
// link opens whatever it names.
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

// AsDocument reads a page's bytes as a collection to import: requests, folders, visible variables
// and the environments its addresses were written against.
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
	for _, group := range snapshot.Collection.Groups {
		for _, endpoint := range group.Endpoints {
			if err := endpoint.ValidateProtocol(); err != nil {
				return domain.Document{}, notAPage()
			}
		}
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

// readFolder turns a page heading back into a folder of that name.
func readFolder(group snapshotv1.Group, position int64) domain.Collection {
	out := domain.Collection{Name: group.Name, Description: group.Description, Position: position}
	for index, endpoint := range group.Endpoints {
		out.Items = append(out.Items, readEndpoint(endpoint, int64(index+1)))
	}
	return out
}

// readEndpoint reads a request back; pages carry no headers, auth or scripts.
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
	readEndpointProtocol(from, &node)
	for _, response := range from.Responses {
		node.Responses = append(node.Responses, domain.ResponseExample{
			Name:   response.Name,
			Status: int(response.Status),
			Body:   response.Body,
		})
	}
	return node
}

// addressOf prefixes the path with the base-address variable the imported environments define.
func addressOf(path string) string {
	held := strings.TrimSpace(path)
	if held == "" || strings.HasPrefix(held, "{{") || strings.Contains(held, "://") {
		return held
	}
	return "{{" + domain.BaseURLVariable + "}}" + held
}

// readBody restores a body: a form from its example rows (file rows named again), anything else as
// text, JSON when the page found fields.
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

// readEnvironments turns the page's base addresses into environments; one with no address is skipped.
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
