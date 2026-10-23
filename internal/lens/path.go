package lens

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"schemo/internal/domain"
)

// step is one move to a child: by key in an object, by position in an array.
type step struct {
	key     string
	index   int
	isIndex bool
}

// steps is the way from the root to the node.
func (n *node) steps() []step {
	var out []step
	for at := n; at.parent != nil; at = at.parent {
		if at.parent.kind == domain.NodeArray {
			out = append(out, step{index: at.index(), isIndex: true})
		} else {
			out = append(out, step{key: at.name})
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// identifier reports whether a key needs no quoting after a dot.
func identifier(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r)) {
			continue
		}
		return false
	}
	return true
}

// quoted is the text as a JSON string, with `<`, `>` and `&` unescaped.
func quoted(key string) string {
	var out strings.Builder
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(key)
	return strings.TrimSuffix(out.String(), "\n")
}

// jsonPath writes the steps as JSONPath.
func jsonPath(steps []step) string {
	var b strings.Builder
	b.WriteString("$")
	for _, s := range steps {
		switch {
		case s.isIndex:
			fmt.Fprintf(&b, "[%d]", s.index)
		case identifier(s.key):
			b.WriteString("." + s.key)
		default:
			b.WriteString("['" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s.key) + "']")
		}
	}
	return b.String()
}

// accessor writes the steps as `.key`, `["key"]` and `[n]`, the chain most languages share.
func accessor(root string, steps []step, python bool) string {
	var b strings.Builder
	b.WriteString(root)
	for _, s := range steps {
		switch {
		case s.isIndex:
			fmt.Fprintf(&b, "[%d]", s.index)
		case identifier(s.key) && !python:
			b.WriteString("." + s.key)
		default:
			b.WriteString("[" + quoted(s.key) + "]")
		}
	}
	return b.String()
}

// jq writes the steps as a jq filter.
func jq(steps []step) string {
	if len(steps) == 0 {
		return "."
	}
	var b strings.Builder
	for _, s := range steps {
		switch {
		case s.isIndex:
			fmt.Fprintf(&b, "[%d]", s.index)
		case identifier(s.key):
			b.WriteString("." + s.key)
		default:
			b.WriteString(".[" + quoted(s.key) + "]")
		}
	}
	return b.String()
}

// jsonForms is the address in the notations scripts use.
func jsonForms(steps []step) []domain.PathForm {
	return []domain.PathForm{
		{Syntax: "JSONPath", Text: jsonPath(steps)},
		{Syntax: "jq", Text: jq(steps)},
		{Syntax: "JavaScript", Text: accessor("body", steps, false)},
		{Syntax: "Python", Text: accessor("body", steps, true)},
		{Syntax: "Postman", Text: accessor("pm.response.json()", steps, false)},
	}
}

// parseJSONPath reads `.name`, `['name']` and `[n]`. A missing leading `$` is assumed, as the
// JSON:API tree writes paths.
func parseJSONPath(path string) ([]step, error) {
	rest := strings.TrimSpace(path)
	rest = strings.TrimPrefix(rest, "$")
	var out []step
	bad := func() ([]step, error) { return nil, fmt.Errorf("%w: %q", domain.ErrNotFound, path) }

	for first := true; rest != ""; first = false {
		switch {
		case rest[0] == '.':
			rest = rest[1:]
			end := strings.IndexAny(rest, ".[")
			if end < 0 {
				end = len(rest)
			}
			if end == 0 {
				return bad()
			}
			out = append(out, step{key: rest[:end]})
			rest = rest[end:]
		case rest[0] == '[':
			shut, key, isIndex, ok := bracket(rest)
			if !ok {
				return bad()
			}
			if isIndex {
				n, err := strconv.Atoi(key)
				if err != nil || n < 0 {
					return bad()
				}
				out = append(out, step{index: n, isIndex: true})
			} else {
				out = append(out, step{key: key})
			}
			rest = rest[shut+1:]
		case first:
			end := strings.IndexAny(rest, ".[")
			if end < 0 {
				end = len(rest)
			}
			out = append(out, step{key: rest[:end]})
			rest = rest[end:]
		default:
			return bad()
		}
	}
	return out, nil
}

// bracket reads `[...]` at the start of text: where it closes, its content, and whether that is a
// position.
func bracket(text string) (shut int, key string, isIndex, ok bool) {
	if len(text) < 3 {
		return 0, "", false, false
	}
	if quote := text[1]; quote == '\'' || quote == '"' {
		var b strings.Builder
		for i := 2; i < len(text); i++ {
			switch {
			case text[i] == '\\' && i+1 < len(text):
				i++
				b.WriteByte(text[i])
			case text[i] == quote:
				if i+1 < len(text) && text[i+1] == ']' {
					return i + 1, b.String(), false, true
				}
				return 0, "", false, false
			default:
				b.WriteByte(text[i])
			}
		}
		return 0, "", false, false
	}
	end := strings.IndexByte(text, ']')
	if end < 0 {
		return 0, "", false, false
	}
	return end, text[1:end], true, true
}

// follow walks the steps down from a node; ErrNotFound when they lead nowhere.
func follow(from *node, steps []step) (*node, error) {
	at := from
	for _, s := range steps {
		var next *node
		switch {
		case s.isIndex && at.kind == domain.NodeArray:
			if s.index < len(at.kids) {
				next = at.kids[s.index]
			}
		case !s.isIndex && at.kind == domain.NodeObject:
			for _, kid := range at.kids {
				if kid.name == s.key {
					next = kid
					break
				}
			}
		}
		if next == nil {
			return nil, domain.ErrNotFound
		}
		at = next
	}
	return at, nil
}
