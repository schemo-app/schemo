package openapi

import (
	"maps"
	"slices"
	"strings"

	"schemo/pkg/jsonpointer"
)

// These bound schemas that lead back into each other. A body skeleton reads six levels, so nothing
// it would show is lost.
const (
	maxHops    = 8
	maxSplices = 4000
)

// resolveDocument resolves what the reader reads. Reusable definitions are only reached through a
// reference, so they are not expanded where they are declared; security schemes are read in place.
func resolveDocument(tree map[string]any) int {
	source := clone(tree).(map[string]any)
	skipped := 0
	for _, key := range slices.Sorted(maps.Keys(tree)) {
		switch key {
		case "components":
			if components, ok := tree[key].(map[string]any); ok {
				skipped += resolve(components["securitySchemes"], source)
			}
		case "definitions":
		default:
			skipped += resolve(tree[key], source)
		}
	}
	return skipped
}

// resolve splices local `$ref`s into node, reading targets from root, which it never changes, and
// returns how many it could not follow; other files are not read.
func resolve(node any, root map[string]any) int {
	r := resolver{root: root, open: map[string]bool{}}
	return r.walk(node)
}

type resolver struct {
	root    map[string]any
	open    map[string]bool // references being expanded
	splices int
}

// walk goes in key order, so a document reads the same way every time the bounds apply.
func (r *resolver) walk(node any) int {
	skipped := 0
	switch typed := node.(type) {
	case map[string]any:
		if ref, isRef := typed["$ref"].(string); isRef {
			return r.splice(typed, ref)
		}
		for _, key := range slices.Sorted(maps.Keys(typed)) {
			skipped += r.walk(typed[key])
		}
	case []any:
		for _, value := range typed {
			skipped += r.walk(value)
		}
	}
	return skipped
}

// splice puts a reference's target where the reference stands. One that leads back to itself is
// left empty.
func (r *resolver) splice(into map[string]any, ref string) int {
	if len(r.open) == 0 {
		r.splices = 0
	}
	if r.open[ref] || len(r.open) >= maxHops || r.splices >= maxSplices {
		delete(into, "$ref")
		return r.walk(into)
	}
	target, ok := pointer(r.root, ref)
	if !ok {
		return 1
	}
	r.splices++
	delete(into, "$ref")
	for key, value := range target {
		into[key] = clone(value)
	}
	r.open[ref] = true
	defer delete(r.open, ref)
	return r.walk(into)
}

// pointer follows a local JSON pointer (RFC 6901 §6) to the object it names.
func pointer(root map[string]any, ref string) (map[string]any, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	tokens, err := jsonpointer.ParseFragment(ref)
	if err != nil {
		return nil, false
	}
	var node any = root
	for _, token := range tokens {
		switch typed := node.(type) {
		case map[string]any:
			node = typed[token]
		case []any:
			index, ok := jsonpointer.Index(token, len(typed))
			if !ok {
				return nil, false
			}
			node = typed[index]
		default:
			return nil, false
		}
	}
	target, ok := node.(map[string]any)
	return target, ok
}

// clone copies a JSON value.
func clone(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, one := range typed {
			out[key] = clone(one)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, one := range typed {
			out[index] = clone(one)
		}
		return out
	}
	return value
}
