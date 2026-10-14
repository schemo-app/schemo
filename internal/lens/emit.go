package lens

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ettle/strcase"

	"schemo/internal/domain"
)

// Types writes the type of a node's subtree, inferred from the values: a member some elements lack
// is optional, and a null makes it nullable.
func (d *Document) Types(path string, format domain.TypeFormat) (string, error) {
	if d.kind != domain.DocJSON && d.kind != domain.DocCSV {
		return "", domain.ErrNotAllowed
	}
	n, err := d.find(path)
	if err != nil {
		return "", err
	}
	name := n.name
	if name == "" {
		name = "Response"
	}
	t := typesOf(n, name)

	switch format {
	case domain.TypeTypeScript:
		return emitTypeScript(t, pascal(name)), nil
	case domain.TypeGo:
		return emitGo(t, pascal(name)), nil
	case domain.TypePython:
		return emitPython(t, pascal(name)), nil
	case domain.TypeJSONSchema:
		return emitJSONSchema(t, pascal(name))
	}
	return "", domain.ErrNotFound
}

func emitTypeScript(t *typ, rootName string) string {
	var b strings.Builder
	objects := t.objects()
	for i, o := range objects {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "export interface %s {\n", o.name)
		for _, f := range o.fields {
			opt := ""
			if f.optional {
				opt = "?"
			}
			note := ""
			if f.typ.format != "" {
				note = " // " + f.typ.format
			}
			fmt.Fprintf(&b, "  %s%s: %s;%s\n", tsKey(f.key), opt, tsType(f.typ), note)
		}
		b.WriteString("}\n")
	}
	if t.object == nil {
		if len(objects) > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "export type %s = %s;\n", rootName, tsType(t))
	}
	return b.String()
}

func tsKey(key string) string {
	if identifier(key) && !strings.ContainsAny(key, "$") {
		return key
	}
	return quoted(key)
}

func tsType(t *typ) string {
	var parts []string
	for _, kind := range t.kinds {
		switch kind {
		case "string":
			parts = append(parts, "string")
		case "integer", "number":
			parts = append(parts, "number")
		case "boolean":
			parts = append(parts, "boolean")
		case "object":
			parts = append(parts, t.object.name)
		case "array":
			if t.array == nil {
				parts = append(parts, "unknown[]")
				continue
			}
			inner := tsType(t.array)
			if strings.Contains(inner, " | ") {
				inner = "(" + inner + ")"
			}
			parts = append(parts, inner+"[]")
		}
	}
	if t.record != nil {
		parts = append(parts, "Record<string, "+tsType(t.record)+">")
	}
	if t.nullable {
		parts = append(parts, "null")
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, " | ")
}

func emitGo(t *typ, rootName string) string {
	var body strings.Builder
	objects := t.objects()
	for i, o := range objects {
		if i > 0 {
			body.WriteString("\n")
		}
		fmt.Fprintf(&body, "type %s struct {\n", o.name)
		used := map[string]bool{}
		for _, f := range o.fields {
			name := goName(f.key)
			for used[name] {
				name += "_"
			}
			used[name] = true
			tag := f.key
			if f.optional {
				tag += ",omitempty"
			}
			fmt.Fprintf(&body, "\t%s %s `json:%q`\n", name, goFieldType(f), tag)
		}
		body.WriteString("}\n")
	}
	if t.object == nil {
		if len(objects) > 0 {
			body.WriteString("\n")
		}
		fmt.Fprintf(&body, "type %s = %s\n", rootName, goType(t))
	}
	out := body.String()
	if strings.Contains(out, "time.Time") {
		out = "import \"time\"\n\n" + out
	}
	return out
}

// goName writes a key as an exported Go field name, with initialisms in capitals.
func goName(key string) string { return strcase.ToGoPascal(pascal(key)) }

// goFieldType is a member's type, a pointer when it can be absent or null.
func goFieldType(f fieldType) string {
	inner := goType(f.typ)
	if (f.optional || f.typ.nullable) && !strings.HasPrefix(inner, "[]") &&
		!strings.HasPrefix(inner, "map[") && inner != "any" {
		return "*" + inner
	}
	return inner
}

func goType(t *typ) string {
	if len(t.kinds) > 1 || (len(t.kinds) == 0 && t.record == nil) {
		return "any"
	}
	if t.record != nil {
		return "map[string]" + goType(t.record)
	}
	switch t.kinds[0] {
	case "string":
		if t.format == "date-time" {
			return "time.Time"
		}
		return "string"
	case "integer":
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "object":
		return t.object.name
	case "array":
		if t.array == nil {
			return "[]any"
		}
		return "[]" + goType(t.array)
	}
	return "any"
}

func emitPython(t *typ, rootName string) string {
	var b strings.Builder
	objects := t.objects()
	uses := []string{"Any", "TypedDict"}
	if anyOptional(objects) {
		uses = append(uses, "NotRequired")
	}
	sortUnique(&uses)
	fmt.Fprintf(&b, "from typing import %s\n", strings.Join(uses, ", "))

	for i := len(objects) - 1; i >= 0; i-- {
		o := objects[i]
		b.WriteString("\n\n")
		if pythonClassOK(o) {
			fmt.Fprintf(&b, "class %s(TypedDict):\n", o.name)
			if len(o.fields) == 0 {
				b.WriteString("    pass\n")
			}
			for _, f := range o.fields {
				fmt.Fprintf(&b, "    %s: %s\n", f.key, pyField(f))
			}
			continue
		}
		fmt.Fprintf(&b, "%s = TypedDict(%q, {\n", o.name, o.name)
		for _, f := range o.fields {
			fmt.Fprintf(&b, "    %s: %s,\n", quoted(f.key), pyField(f))
		}
		b.WriteString("})\n")
	}
	if t.object == nil {
		fmt.Fprintf(&b, "\n\n%s = %s\n", rootName, pyType(t))
	}
	return b.String()
}

func anyOptional(objects []*objectType) bool {
	for _, o := range objects {
		for _, f := range o.fields {
			if f.optional {
				return true
			}
		}
	}
	return false
}

func sortUnique(list *[]string) {
	slices.Sort(*list)
	*list = slices.Compact(*list)
}

var pythonKeywords = wordSet("and as assert break class continue def del elif else except false " +
	"finally for from global if import in is lambda none nonlocal not or pass raise return true try " +
	"while with yield")

func wordSet(words string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.Fields(words) {
		out[word] = true
	}
	return out
}

// pythonClassOK reports whether every member fits a TypedDict class body.
func pythonClassOK(o *objectType) bool {
	for _, f := range o.fields {
		if !identifier(f.key) || pythonKeywords[strings.ToLower(f.key)] {
			return false
		}
	}
	return true
}

func pyField(f fieldType) string {
	if f.optional {
		return "NotRequired[" + pyType(f.typ) + "]"
	}
	return pyType(f.typ)
}

func pyType(t *typ) string {
	var parts []string
	for _, kind := range t.kinds {
		switch kind {
		case "string":
			parts = append(parts, "str")
		case "integer":
			parts = append(parts, "int")
		case "number":
			parts = append(parts, "float")
		case "boolean":
			parts = append(parts, "bool")
		case "object":
			parts = append(parts, t.object.name)
		case "array":
			if t.array == nil {
				parts = append(parts, "list[Any]")
			} else {
				parts = append(parts, "list["+pyType(t.array)+"]")
			}
		}
	}
	if t.record != nil {
		parts = append(parts, "dict[str, "+pyType(t.record)+"]")
	}
	if t.nullable {
		parts = append(parts, "None")
	}
	if len(parts) == 0 {
		return "Any"
	}
	return strings.Join(parts, " | ")
}

// obj is a JSON object that keeps insertion order.
type obj []entry

type entry struct {
	key   string
	value any
}

func (o obj) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteString("{")
	for i, e := range o {
		if i > 0 {
			b.WriteString(",")
		}
		value, err := json.Marshal(e.value)
		if err != nil {
			return nil, err
		}
		b.WriteString(quoted(e.key) + ":" + string(value))
	}
	b.WriteString("}")
	return []byte(b.String()), nil
}

func emitJSONSchema(t *typ, title string) (string, error) {
	root := schemaOf(t)
	root = append(obj{
		{"$schema", "https://json-schema.org/draft/2020-12/schema"},
		{"title", title},
	}, root...)
	out, err := json.MarshalIndent(root, "", "  ")
	return string(out), err
}

func schemaOf(t *typ) obj {
	types := append([]string(nil), t.kinds...)
	if t.nullable {
		types = append(types, "null")
	}
	var out obj
	switch len(types) {
	case 0:
	case 1:
		out = append(out, entry{"type", types[0]})
	default:
		out = append(out, entry{"type", types})
	}
	if t.format != "" {
		out = append(out, entry{"format", t.format})
	}
	if t.object != nil {
		props := obj{}
		var required []string
		for _, f := range t.object.fields {
			props = append(props, entry{f.key, schemaOf(f.typ)})
			if !f.optional {
				required = append(required, f.key)
			}
		}
		out = append(out, entry{"properties", props})
		if len(required) > 0 {
			out = append(out, entry{"required", required})
		}
	}
	if t.record != nil {
		out = append(out, entry{"additionalProperties", schemaOf(t.record)})
	}
	if t.array != nil {
		out = append(out, entry{"items", schemaOf(t.array)})
	}
	return out
}
