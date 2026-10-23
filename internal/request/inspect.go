package request

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"schemo/internal/domain"
	"schemo/internal/lens"
	"slices"
	"strings"
	"sync"
)

const (
	// kept is how many documents are held at once.
	kept = 8
	// largest is the biggest body read, in bytes.
	largest = 8 << 20
)

type Inspector struct {
	tree  Tree
	scope domain.Scope

	mu    sync.Mutex
	docs  map[string]*lens.Document
	order []string
}

func NewInspector(tree Tree, scope domain.Scope) *Inspector {
	return &Inspector{tree: tree, scope: scope, docs: map[string]*lens.Document{}}
}

// Open reads a body and returns its handle. A body that is not a document (or is too large) gets
// no handle and no error.
func (s *Inspector) Open(text, contentType string) (domain.Opened, error) {
	if len(text) > largest {
		return domain.Opened{}, nil
	}
	sum := sha256.Sum256([]byte(contentType + "\x00" + text))
	id := hex.EncodeToString(sum[:8])

	s.mu.Lock()
	defer s.mu.Unlock()
	if doc, ok := s.docs[id]; ok {
		s.touch(id)
		return domain.Opened{ID: id, Kind: doc.Kind()}, nil
	}
	doc, err := lens.Open(contentType, text)
	if errors.Is(err, domain.ErrUnreadable) {
		return domain.Opened{}, nil
	}
	if err != nil {
		return domain.Opened{}, err
	}
	s.docs[id] = doc
	s.order = append(s.order, id)
	if len(s.order) > kept {
		delete(s.docs, s.order[0])
		s.order = s.order[1:]
	}
	return domain.Opened{ID: id, Kind: doc.Kind()}, nil
}

// touch moves a document to the end of the eviction order.
func (s *Inspector) touch(id string) {
	for i, held := range s.order {
		if held == id {
			s.order = append(append(s.order[:i:i], s.order[i+1:]...), id)
			return
		}
	}
}

// document is the one a handle names; an evicted handle is ErrNotFound.
func (s *Inspector) document(id string) (*lens.Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, ok := s.docs[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	s.touch(id)
	return doc, nil
}

// At is the node at an offset in UTF-16 units.
func (s *Inspector) At(id string, offset int) (domain.NodeView, error) {
	doc, err := s.document(id)
	if err != nil {
		return domain.NodeView{}, err
	}
	return doc.At(offset), nil
}

// Node is the node an address names.
func (s *Inspector) Node(id, path string) (domain.NodeView, error) {
	doc, err := s.document(id)
	if err != nil {
		return domain.NodeView{}, err
	}
	return doc.Node(path)
}

// Text is what a copy of the node holds.
func (s *Inspector) Text(id, path string) (string, error) {
	doc, err := s.document(id)
	if err != nil {
		return "", err
	}
	return doc.Text(path)
}

// Diagnose reads an error response; a body that is not one gives an empty diagnosis.
func (s *Inspector) Diagnose(id string, status int) (domain.Diagnosis, error) {
	doc, err := s.document(id)
	if err != nil {
		return domain.Diagnosis{}, err
	}
	found, _ := doc.Diagnose(status)
	return found, nil
}

// Locate finds the request body member a JSON Pointer names, or nil.
func (s *Inspector) Locate(contentType, text, pointer string) *domain.Span {
	span, ok := lens.Locate(contentType, text, pointer)
	if !ok {
		return nil
	}
	return &span
}

// Body writes a node as a request body, without server-owned members when bare.
func (s *Inspector) Body(id, path string, bare bool) (string, error) {
	doc, err := s.document(id)
	if err != nil {
		return "", err
	}
	return doc.Body(path, bare)
}

// Types writes the type of a node's subtree in a notation.
func (s *Inspector) Types(id, path string, format domain.TypeFormat) (string, error) {
	doc, err := s.document(id)
	if err != nil {
		return "", err
	}
	return doc.Types(path, format)
}

// TypeFormats lists the notations a type is written in, in the order offered.
func (s *Inspector) TypeFormats() []domain.TypeFormat { return domain.TypeFormats }

// Tree supplies the collections whose requests a value is offered to.
type Tree interface {
	Collections(ctx context.Context, workspaceID string) ([]domain.Collection, error)
}

// most is how many requests are offered.
const most = 6

// Suggest finds the collection requests a value looks like it belongs in, by the address each
// takes: `/authors/{{id}}` takes an author's identifier, as `author_id` is one.
func (s *Inspector) Suggest(ctx context.Context, id, path string) ([]domain.Suggestion, error) {
	doc, err := s.document(id)
	if err != nil {
		return nil, err
	}
	value, names := doc.Refers(path)
	if value == "" || len(names) == 0 {
		return nil, nil
	}
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	tree, err := s.tree.Collections(ctx, workspace)
	if err != nil {
		return nil, err
	}

	var found []scored
	var walk func(collections []domain.Collection)
	walk = func(collections []domain.Collection) {
		for _, collection := range collections {
			for _, node := range collection.Items {
				if s, ok := fit(node, names, value); ok {
					found = append(found, s)
				}
			}
			walk(collection.Children)
		}
	}
	walk(tree)

	// Best fit first; among equals reads before writes, then tree order.
	slices.SortStableFunc(found, func(a, b scored) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		if a.read == b.read {
			return 0
		}
		if a.read {
			return -1
		}
		return 1
	})
	out := make([]domain.Suggestion, 0, most)
	for _, s := range found {
		if len(out) == most {
			break
		}
		out = append(out, s.Suggestion)
	}
	return out, nil
}

type scored struct {
	domain.Suggestion
	score int
	read  bool
}

// hole is a place in an address that takes a value.
type hole struct {
	raw, name, before string
	query             bool
}

var holeShapes = []*regexp.Regexp{
	regexp.MustCompile(`^\{\{\s*([\w.-]+)\s*\}\}$`),
	regexp.MustCompile(`^:([\w-]+)$`),
	regexp.MustCompile(`^\{([\w-]+)\}$`),
	regexp.MustCompile(`^<([\w-]+)>$`),
}

func holeName(text string) (string, bool) {
	for _, shape := range holeShapes {
		if m := shape.FindStringSubmatch(text); m != nil {
			return m[1], true
		}
	}
	return "", false
}

// holesOf finds the places in an address that take a value.
func holesOf(address string) []hole {
	path, query, _ := strings.Cut(address, "?")
	var out []hole
	before := ""
	for _, segment := range strings.Split(path, "/") {
		if name, ok := holeName(segment); ok {
			out = append(out, hole{raw: segment, name: name, before: before})
			continue
		}
		if segment != "" && !strings.HasSuffix(segment, ":") {
			before = segment
		}
	}
	for _, pair := range strings.Split(query, "&") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		if name, ok := holeName(value); ok {
			out = append(out, hole{raw: value, name: key, query: true, before: name})
		}
	}
	return out
}

// fit scores how well a request takes the value: the path word before a hole, or the hole's name,
// matching a kind the value identifies. The first kind counts most.
func fit(node domain.CollectionNode, names []string, value string) (scored, bool) {
	best := 0
	var at hole
	for _, h := range holesOf(node.URL) {
		for i, want := range names {
			points := 0
			switch {
			case !h.query && lens.Resource(h.before) == want:
				points = 4
			case lens.Resource(h.name) == want:
				points = 4
			}
			if points == 0 {
				continue
			}
			if i == 0 {
				points++
			}
			if points > best {
				best, at = points, h
			}
		}
	}
	if best == 0 {
		return scored{}, false
	}
	for _, want := range names {
		if strings.Contains(strings.ToLower(node.Name), want) {
			best++
			break
		}
	}

	escape := url.PathEscape
	if at.query {
		escape = url.QueryEscape
	}
	method := strings.ToUpper(node.Method)
	return scored{
		Suggestion: domain.Suggestion{
			NodeID: node.ID, Name: node.Name, Method: method, URL: node.URL,
			Filled: strings.Replace(node.URL, at.raw, escape(value), 1),
		},
		score: best,
		read:  method == "GET" || method == "",
	}, true
}
