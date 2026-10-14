package lens

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"schemo/internal/domain"
)

// previewRunes is how much of a scalar a view carries.
const previewRunes = 500

// maxChildren caps how many of a container's members the inspector lists at once.
const maxChildren = 300

// Open reads text as a document. The media type only orders the notations tried; text that reads
// as none is ErrUnreadable.
func Open(contentType, text string) (*Document, error) {
	text = strings.TrimPrefix(text, "\uFEFF")
	for _, kind := range candidates(contentType, text) {
		var root *node
		var err error
		switch kind {
		case domain.DocJSON:
			root, err = parseJSON(text)
		case domain.DocXML:
			root, err = parseXML(text)
		case domain.DocHTML:
			root, err = parseHTML(text)
		case domain.DocCSV:
			root, err = parseCSV(text)
		}
		if err == nil && root != nil {
			return &Document{kind: kind, text: text, root: root, ascii: isASCII(text)}, nil
		}
	}
	return nil, domain.ErrUnreadable
}

// csvTypes are the media types that make a body a table; text is never guessed to be one.
var csvTypes = map[string]bool{
	"text/csv": true, "application/csv": true, "text/tab-separated-values": true,
	"text/comma-separated-values": true,
}

// candidates lists the notations to try, the media type's first.
func candidates(contentType, text string) []domain.DocKind {
	media := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	head := strings.TrimSpace(text)
	var out []domain.DocKind
	add := func(kind domain.DocKind) {
		for _, have := range out {
			if have == kind {
				return
			}
		}
		out = append(out, kind)
	}
	switch {
	case strings.HasSuffix(media, "json") || strings.Contains(media, "+json"):
		add(domain.DocJSON)
	case media == "text/html" || media == "application/xhtml+xml":
		add(domain.DocHTML)
	case strings.HasSuffix(media, "xml") || strings.Contains(media, "+xml"):
		add(domain.DocXML)
	case csvTypes[media]:
		add(domain.DocCSV)
	}
	lower := strings.ToLower(head)
	switch {
	case strings.HasPrefix(head, "{") || strings.HasPrefix(head, "["):
		add(domain.DocJSON)
	case strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html"):
		add(domain.DocHTML)
	case strings.HasPrefix(head, "<"):
		add(domain.DocXML)
	}
	return out
}

// At is the deepest node whose text holds the offset (UTF-16 units), or the member whose key does.
func (d *Document) At(units int) domain.NodeView {
	offset := units
	if !d.ascii {
		offset = byteAt(d.text, units)
	}
	return d.view(deepest(d.root, offset))
}

// deepest is the innermost node holding the offset.
func deepest(n *node, offset int) *node {
	for {
		var next *node
		for _, kid := range n.kids {
			from := kid.start
			if kid.keyStart >= 0 {
				from = kid.keyStart
			}
			if offset >= from && offset <= kid.end {
				next = kid
				break
			}
		}
		if next == nil {
			return n
		}
		n = next
	}
}

// Node is the node an address names.
func (d *Document) Node(path string) (domain.NodeView, error) {
	n, err := d.find(path)
	if err != nil {
		return domain.NodeView{}, err
	}
	return d.view(n), nil
}

// markup reports whether addresses in the document are XPath.
func (d *Document) markup() bool { return d.kind == domain.DocXML || d.kind == domain.DocHTML }

func (d *Document) find(path string) (*node, error) {
	if d.markup() {
		return findXPath(d.root, path)
	}
	steps, err := parseJSONPath(path)
	if err != nil {
		return nil, err
	}
	return follow(d.root, steps)
}

// Text is what a copy holds: a scalar's value, unquoted, or a container as written.
func (d *Document) Text(path string) (string, error) {
	n, err := d.find(path)
	if err != nil {
		return "", err
	}
	switch n.kind {
	case domain.NodeObject, domain.NodeArray, domain.NodeElement:
		return d.text[n.start:n.end], nil
	}
	return n.scalar, nil
}

// view describes a node.
func (d *Document) view(n *node) domain.NodeView {
	view := domain.NodeView{
		Kind:  n.kind,
		Name:  n.name,
		Bytes: n.end - n.start,
		Head:  d.units(n.head()),
		Start: d.units(n.start),
		End:   d.units(n.end),
	}
	if d.markup() {
		view.Path = xpath(n)
		view.Forms = xmlForms(n)
		if d.kind == domain.DocHTML {
			view.Forms = htmlForms(n)
		}
		if n.parent != nil {
			view.Parent = xpath(n.parent)
		}
	} else {
		steps := n.steps()
		view.Path = jsonPath(steps)
		view.Forms = jsonForms(steps)
		if d.kind == domain.DocCSV {
			view.Forms = d.csvForms(n, steps)
		}
		if n.parent != nil {
			view.Parent = jsonPath(steps[:len(steps)-1])
		}
	}
	switch n.kind {
	case domain.NodeObject, domain.NodeArray, domain.NodeElement:
		view.Count = len(n.kids)
		view.Children = d.childrenOf(n)
		// An element of only text is that text.
		if n.kind == domain.NodeElement && n.scalar != "" {
			view.Preview = cut(n.scalar)
			view.Readings = stringReadings(n.scalar)
		}
	default:
		view.Preview = cut(n.scalar)
		view.Count = utf8.RuneCountInString(n.scalar)
		view.Readings = readingsOf(n)
	}
	view.Crumbs = d.crumbs(n)
	return view
}

// pathOf is a node's address, in the notation the document's kind uses.
func (d *Document) pathOf(n *node) string {
	if d.markup() {
		return xpath(n)
	}
	return jsonPath(n.steps())
}

// labelOf is how a node names itself in a crumb or a children list: its key, or its index in
// brackets when its parent is an array.
func labelOf(n *node) string {
	if n.parent != nil && n.parent.kind == domain.NodeArray {
		return "[" + strconv.Itoa(n.index()) + "]"
	}
	return n.name
}

// crumbs is the way down from the root to a node, the node itself last.
func (d *Document) crumbs(n *node) []domain.Crumb {
	var chain []*node
	for at := n; at != nil; at = at.parent {
		chain = append(chain, at)
	}
	out := make([]domain.Crumb, len(chain))
	for i, at := range chain {
		out[len(chain)-1-i] = domain.Crumb{Name: labelOf(at), Path: d.pathOf(at)}
	}
	return out
}

// childrenOf lists a container's members without opening them, capped at maxChildren.
func (d *Document) childrenOf(n *node) []domain.NodeChild {
	kids := n.kids
	if len(kids) > maxChildren {
		kids = kids[:maxChildren]
	}
	out := make([]domain.NodeChild, len(kids))
	for i, kid := range kids {
		out[i] = domain.NodeChild{
			Kind: kid.kind, Name: labelOf(kid), Path: d.pathOf(kid), Preview: childPreview(kid),
		}
	}
	return out
}

// childPreview is what a children list shows beside a member's name, without opening it.
func childPreview(n *node) string {
	switch n.kind {
	case domain.NodeObject:
		return fmt.Sprintf("{%d}", len(n.kids))
	case domain.NodeArray:
		return fmt.Sprintf("[%d]", len(n.kids))
	case domain.NodeElement:
		if n.scalar != "" {
			return cut(n.scalar)
		}
		return fmt.Sprintf("<%d>", len(n.kids))
	default:
		return cut(n.scalar)
	}
}

// units converts a byte offset to UTF-16 units.
func (d *Document) units(offset int) int {
	if d.ascii {
		return offset
	}
	return unitsUpTo(d.text, offset)
}

// cut shortens a scalar to previewRunes.
func cut(text string) string {
	if utf8.RuneCountInString(text) <= previewRunes {
		return text
	}
	return string([]rune(text)[:previewRunes]) + "…"
}
