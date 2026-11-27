package openapi

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

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

func TestASchemaThatLeadsBackToItselfIsReadTheSameEveryTime(t *testing.T) {
	spec := []byte(`openapi: 3.0.3
info: {title: Loops, version: "1"}
paths:
  /nodes:
    post:
      requestBody:
        content:
          application/json:
            schema: {$ref: "#/components/schemas/Node"}
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: "#/components/schemas/Page"}
components:
  schemas:
    Node:
      type: object
      properties:
        id: {type: string}
        parent: {$ref: "#/components/schemas/Node"}
        children: {type: array, items: {$ref: "#/components/schemas/Node"}}
        page: {$ref: "#/components/schemas/Page"}
    Page:
      type: object
      properties:
        first: {$ref: "#/components/schemas/Node"}
        next: {$ref: "#/components/schemas/Page"}
`)
	first, err := Import(spec)
	if err != nil {
		t.Fatal(err)
	}
	node := first.Collections[0].Items[0]
	if !strings.Contains(node.Body, `"id": "string"`) || !strings.Contains(node.Body, `"first"`) {
		t.Errorf("body = %s, want the schema's own fields and the page it holds", node.Body)
	}
	for range 20 {
		again, err := Import(spec)
		if err != nil {
			t.Fatal(err)
		}
		if again.Collections[0].Items[0].Body != node.Body ||
			again.Collections[0].Items[0].Responses[0].Body != node.Responses[0].Body {
			t.Fatal("two reads of one document disagree")
		}
	}
}

// Schemas that all point at each other must not make the read grow without end.
func TestDenselyLinkedSchemasStayBounded(t *testing.T) {
	const count = 12
	var spec strings.Builder
	spec.WriteString(`{"openapi":"3.0.3","info":{"title":"Dense","version":"1"},"paths":{"/a":{"post":` +
		`{"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/S0"}}}}}}},` +
		`"components":{"schemas":{`)
	for i := range count {
		if i > 0 {
			spec.WriteString(",")
		}
		fmt.Fprintf(&spec, `"S%d":{"type":"object","properties":{`, i)
		for j := range count {
			if j > 0 {
				spec.WriteString(",")
			}
			fmt.Fprintf(&spec, `"p%d":{"$ref":"#/components/schemas/S%d"}`, j, j)
		}
		spec.WriteString("}}")
	}
	spec.WriteString("}}}")

	started := time.Now()
	if _, err := Import([]byte(spec.String())); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("reading took %v", took)
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
