package lens

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jinzhu/inflection"

	"schemo/internal/domain"
)

// shape is what the values seen at one place have in common: their kinds and, for objects, their
// members and how often each occurred.
type shape struct {
	kinds   map[string]bool // string, integer, number, boolean, null, object, array
	fields  map[string]*field
	order   []string // members in the order first seen
	objects int      // objects observed here
	items   *shape   // what the elements of the arrays observed here have in common

	// formats counts the formats seen among strings; one is claimed only if every string has it.
	formats map[string]int
	strings int
}

type field struct {
	shape *shape
	seen  int
}

func newShape() *shape {
	return &shape{kinds: map[string]bool{}, fields: map[string]*field{}, formats: map[string]int{}}
}

// observe folds a node into the shape.
func (s *shape) observe(n *node) {
	switch n.kind {
	case domain.NodeNull:
		s.kinds["null"] = true
	case domain.NodeBoolean:
		s.kinds["boolean"] = true
	case domain.NodeNumber:
		if strings.ContainsAny(n.scalar, ".eE") {
			s.kinds["number"] = true
		} else {
			s.kinds["integer"] = true
		}
	case domain.NodeString, domain.NodeAttribute, domain.NodeText:
		s.kinds["string"] = true
		s.strings++
		if f := formatOf(n.scalar); f != "" {
			s.formats[f]++
		}
	case domain.NodeObject, domain.NodeElement:
		s.kinds["object"] = true
		s.objects++
		for _, kid := range n.kids {
			f, ok := s.fields[kid.name]
			if !ok {
				f = &field{shape: newShape()}
				s.fields[kid.name] = f
				s.order = append(s.order, kid.name)
			}
			f.seen++
			f.shape.observe(kid)
		}
	case domain.NodeArray:
		s.kinds["array"] = true
		if s.items == nil {
			s.items = newShape()
		}
		for _, kid := range n.kids {
			s.items.observe(kid)
		}
	}
}

var (
	dateOnly  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	emailForm = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]{2,}$`)
)

// formatOf names a string's format: uuid, date, date-time, uri or email.
func formatOf(s string) string {
	switch {
	case uuidShape.MatchString(s):
		return "uuid"
	case dateOnly.MatchString(s):
		return "date"
	case len(s) >= 19 && len(s) <= 35 && isRFC3339(s):
		return "date-time"
	case strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://"):
		return "uri"
	case emailForm.MatchString(s):
		return "email"
	}
	return ""
}

func isRFC3339(s string) bool {
	_, err := time.Parse(time.RFC3339Nano, s)
	return err == nil
}

// format is the format every string had, or empty.
func (s *shape) format() string {
	if s.strings == 0 {
		return ""
	}
	for name, count := range s.formats {
		if count == s.strings {
			return name
		}
	}
	return ""
}

// pascal writes a key as a type name: `content-type` is ContentType.
func pascal(key string) string {
	var b strings.Builder
	upper := true
	for _, r := range key {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if upper {
				r = unicode.ToUpper(r)
			}
			b.WriteRune(r)
			upper = false
		default:
			upper = true
		}
	}
	name := b.String()
	if name == "" {
		return "Value"
	}
	if unicode.IsDigit(rune(name[0])) {
		return "N" + name
	}
	return name
}

func init() {
	inflection.AddUncountable("data")
}

// singular turns a plural key into the name of its elements.
func singular(name string) string {
	if one := inflection.Singular(name); len(one) > 2 {
		return one
	}
	return name
}

var recordKey = regexp.MustCompile(`^(\d+|[0-9a-fA-F]{8}-[0-9a-fA-F-]{27})$`)

// typ is a shape with names: what the emitters write.
type typ struct {
	kinds    []string // sorted; "null" is not in it, nullable says so
	nullable bool
	format   string
	object   *objectType
	array    *typ // the element type, when the value may be an array
	record   *typ // the value type, when the object is a map from ids to values
}

type objectType struct {
	name   string
	fields []fieldType
}

type fieldType struct {
	key      string
	typ      *typ
	optional bool
}

// namer hands out type names that are unique within one document.
type namer struct{ used map[string]int }

func (n *namer) name(want string) string {
	n.used[want]++
	if n.used[want] == 1 {
		return want
	}
	return want + strconv.Itoa(n.used[want])
}

// resolve types a shape; objects are named after their key.
func resolve(s *shape, name string, names *namer) *typ {
	t := &typ{format: s.format()}
	for kind := range s.kinds {
		if kind == "null" {
			t.nullable = true
			continue
		}
		t.kinds = append(t.kinds, kind)
	}
	slices.Sort(t.kinds)
	if s.kinds["integer"] && s.kinds["number"] {
		t.kinds = without(t.kinds, "integer")
	}

	if s.kinds["object"] {
		if len(s.order) >= 3 && allRecordKeys(s.order) {
			merged := newShape()
			for _, key := range s.order {
				mergeInto(merged, s.fields[key].shape)
			}
			t.record = resolve(merged, singular(name), names)
			t.kinds = without(t.kinds, "object")
		} else {
			t.object = &objectType{name: names.name(pascal(name))}
			for _, key := range s.order {
				f := s.fields[key]
				t.object.fields = append(t.object.fields, fieldType{
					key:      key,
					typ:      resolve(f.shape, key, names),
					optional: f.seen < s.objects,
				})
			}
		}
	}
	if s.kinds["array"] && s.items != nil {
		t.array = resolve(s.items, singular(name), names)
	}
	return t
}

func without(list []string, drop string) []string {
	out := list[:0:0]
	for _, one := range list {
		if one != drop {
			out = append(out, one)
		}
	}
	return out
}

func allRecordKeys(keys []string) bool {
	for _, key := range keys {
		if !recordKey.MatchString(key) {
			return false
		}
	}
	return true
}

// mergeInto folds one shape into another, so map values are typed together.
func mergeInto(into, from *shape) {
	for kind := range from.kinds {
		into.kinds[kind] = true
	}
	into.strings += from.strings
	for name, count := range from.formats {
		into.formats[name] += count
	}
	into.objects += from.objects
	for _, key := range from.order {
		f, ok := into.fields[key]
		if !ok {
			f = &field{shape: newShape()}
			into.fields[key] = f
			into.order = append(into.order, key)
		}
		f.seen += from.fields[key].seen
		mergeInto(f.shape, from.fields[key].shape)
	}
	if from.items != nil {
		if into.items == nil {
			into.items = newShape()
		}
		mergeInto(into.items, from.items)
	}
}

// typesOf infers the type of everything below a node.
func typesOf(n *node, rootName string) *typ {
	s := newShape()
	s.observe(n)
	names := &namer{used: map[string]int{}}
	if !s.kinds["object"] || (len(s.order) >= 3 && allRecordKeys(s.order)) {
		// The root is an alias, so no inner type may take its name.
		names.used[pascal(rootName)]++
	}
	return resolve(s, rootName, names)
}

// objects lists the named object types, each once, outer before inner.
func (t *typ) objects() []*objectType {
	var out []*objectType
	var walk func(*typ)
	walk = func(t *typ) {
		if t == nil {
			return
		}
		if t.object != nil {
			out = append(out, t.object)
			for _, f := range t.object.fields {
				walk(f.typ)
			}
		}
		walk(t.array)
		walk(t.record)
	}
	walk(t)
	return out
}
