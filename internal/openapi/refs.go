package openapi

import (
	"strconv"
	"strings"
)

// resolve splices every local `$ref` into the tree it points at and answers how many references it
// could not follow. A reference into another file is left as it stands: this package reads one
// document and has no files beside it, and inventing what such a reference points at would be
// inventing a request.
func resolve(node any, root map[string]any) int {
	return resolveAt(node, root, 0)
}

func resolveAt(node any, root map[string]any, depth int) int {
	// A document that references itself in a circle would otherwise walk forever.
	if depth > 32 {
		return 0
	}
	switch typed := node.(type) {
	case map[string]any:
		skipped := 0
		if ref, isRef := typed["$ref"].(string); isRef {
			target, ok := pointer(root, ref, depth)
			if !ok {
				return 1
			}
			delete(typed, "$ref")
			for key, value := range target {
				typed[key] = value
			}
		}
		for _, value := range typed {
			skipped += resolveAt(value, root, depth+1)
		}
		return skipped
	case []any:
		skipped := 0
		for _, value := range typed {
			skipped += resolveAt(value, root, depth+1)
		}
		return skipped
	default:
		return 0
	}
}

// pointer follows a local JSON pointer — `#/components/schemas/User` — and answers a copy of what
// it found, already spliced. The copy is what keeps a circle from being written into itself.
func pointer(root map[string]any, ref string, depth int) (map[string]any, bool) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	var node any = root
	for _, step := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		step = strings.ReplaceAll(strings.ReplaceAll(step, "~1", "/"), "~0", "~")
		switch typed := node.(type) {
		case map[string]any:
			node = typed[step]
		case []any:
			index, err := strconv.Atoi(step)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			node = typed[index]
		default:
			return nil, false
		}
	}
	target, ok := node.(map[string]any)
	if !ok {
		return nil, false
	}
	copied := map[string]any{}
	for key, value := range target {
		copied[key] = value
	}
	resolveAt(copied, root, depth+1)
	return copied, true
}
