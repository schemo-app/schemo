package openapi

import (
	"strings"

	"schemo/pkg/jsonpointer"
)

// resolve splices local `$ref`s and returns how many it could not follow; other files are not read.
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

// pointer follows a local JSON pointer (RFC 6901 §6) and returns a spliced copy, which keeps cycles out.
func pointer(root map[string]any, ref string, depth int) (map[string]any, bool) {
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
