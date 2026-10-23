package openapi

import "testing"

// A $ref is a URI reference (RFC 6901 §6): the characters a URI may not carry are percent-encoded in
// it, and a slash inside a key is ~1.
func TestAReferenceIsReadAsAURIFragmentOfAJSONPointer(t *testing.T) {
	document := func() map[string]any {
		return map[string]any{
			"components": map[string]any{"schemas": map[string]any{
				"My Model": map[string]any{"type": "object"},
				"a/b":      map[string]any{"type": "string"},
				"100%":     map[string]any{"type": "integer"},
			}},
			"list": []any{map[string]any{"type": "array"}},
		}
	}
	for ref, want := range map[string]string{
		"#/components/schemas/My%20Model": "object",
		"#/components/schemas/a~1b":       "string",
		"#/components/schemas/100%25":     "integer",
		"#/list/0":                        "array",
	} {
		root := document()
		holder := map[string]any{"schema": map[string]any{"$ref": ref}}
		if skipped := resolve(holder, root); skipped != 0 {
			t.Errorf("%s: %d references not followed", ref, skipped)
			continue
		}
		if got := holder["schema"].(map[string]any)["type"]; got != want {
			t.Errorf("%s: type = %v, want %s", ref, got, want)
		}
	}
}

func TestAReferenceThatLeadsNowhereIsCounted(t *testing.T) {
	root := map[string]any{"list": []any{map[string]any{"type": "array"}}}
	for _, ref := range []string{"#/list/01", "#/list/1", "#/list/-", "#/missing", "#/bad%zz", "other.json#/a"} {
		holder := map[string]any{"schema": map[string]any{"$ref": ref}}
		if skipped := resolve(holder, root); skipped != 1 {
			t.Errorf("%s: %d references not followed, want 1", ref, skipped)
		}
	}
}
