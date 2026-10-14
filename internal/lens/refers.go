package lens

import (
	"strings"

	"schemo/internal/domain"
)

// genericNames are wrapper words that say nothing about their content.
var genericNames = wordSet("data attributes relationships included items results result records " +
	"rows response payload list content edges node body meta links")

// irregular lists plurals that stripping a suffix does not undo.
var irregular = map[string]string{
	"people": "person", "children": "child", "men": "man", "women": "woman",
}

// Resource normalises a word to the name of a kind: `authors`, `author_id` and `authorId` are all
// `author`.
func Resource(word string) string {
	name := folded(word)
	for _, suffix := range []string{"uuid", "id"} {
		if len(name) > len(suffix) && strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
			break
		}
	}
	if one, ok := irregular[name]; ok {
		return one
	}
	return folded(singular(name))
}

// bareID reports whether a key is just "id", so the kind must come from context.
func bareID(key string) bool {
	switch folded(key) {
	case "id", "uuid", "guid", "key":
		return true
	}
	return false
}

// Refers says what a scalar may identify, as kinds like `author`, strongest first: a key
// `author_id` names its own; a bare `id` takes the sibling `type` or the enclosing object's name.
func (d *Document) Refers(path string) (value string, names []string) {
	if d.markup() {
		return "", nil
	}
	n, err := d.find(path)
	if err != nil || (n.kind != domain.NodeString && n.kind != domain.NodeNumber) {
		return "", nil
	}
	value = strings.TrimSpace(n.scalar)
	if value == "" || len(value) > 200 || strings.ContainsAny(value, " \n") {
		return "", nil
	}

	add := func(word string) {
		name := Resource(word)
		if name == "" || genericNames[name] || bareID(name) {
			return
		}
		for _, have := range names {
			if have == name {
				return
			}
		}
		names = append(names, name)
	}

	if !bareID(n.name) {
		add(n.name)
		return value, names
	}
	if n.parent != nil {
		for _, key := range []string{"type", "object", "kind", "resource", "entity"} {
			if w := n.parent.member(key).words(); w != "" {
				add(w)
			}
		}
	}
	for at := n.parent; at != nil; at = at.parent {
		if at.name != "" {
			add(at.name)
		}
	}
	return value, names
}
