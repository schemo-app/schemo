package inspect

import (
	"cmp"
	"context"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"schemo/internal/domain"
	"schemo/internal/lens"
)

// Tree supplies the collections whose requests a value is offered to.
type Tree interface {
	Collections(ctx context.Context, workspaceID string) ([]domain.Collection, error)
}

// most is how many requests are offered.
const most = 6

// Suggest finds the collection requests a value looks like it belongs in, by the address each
// takes: `/authors/{{id}}` takes an author's identifier, as `author_id` is one.
func (u *UseCase) Suggest(ctx context.Context, id, path string) ([]domain.Suggestion, error) {
	doc, err := u.document(id)
	if err != nil {
		return nil, err
	}
	value, names := doc.Refers(path)
	if value == "" || len(names) == 0 {
		return nil, nil
	}
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	tree, err := u.tree.Collections(ctx, workspace)
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
