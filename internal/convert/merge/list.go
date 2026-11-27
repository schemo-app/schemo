package merge

import (
	"encoding/json"
	"strconv"
	"strings"

	"schemo/internal/domain"
)

// list says how one kind of row is told apart, compared and shown; keeps rows stay when dropped.
type list[T any] struct {
	field  Field
	key    func(T) string
	name   func(T) string
	whole  func(T) string
	facets []facet[T]
	keeps  bool
}

type facet[T any] struct {
	attribute Attribute
	value     func(T) string
	show      func(T) string
	take      func(into *T, from T)
}

func (f facet[T]) shown(row T) string {
	if f.show != nil {
		return f.show(row)
	}
	return f.value(row)
}

// form is a whole list as compared.
func (l list[T]) form(rows []T) string {
	numbered, _ := number(rows, l.key)
	var out strings.Builder
	for _, one := range numbered {
		out.WriteString(strconv.Quote(one.key))
		for _, f := range l.facets {
			out.WriteString(strconv.Quote(f.value(one.row)))
		}
	}
	return out.String()
}

func (l list[T]) alike(a, b T) bool {
	for _, f := range l.facets {
		if f.value(a) != f.value(b) {
			return false
		}
	}
	return true
}

type numbered[T any] struct {
	key string
	row T
}

// number tells rows of one name apart by their order, so the second `tag=` meets the second.
func number[T any](rows []T, key func(T) string) ([]numbered[T], map[string]T) {
	seen := map[string]int{}
	out := make([]numbered[T], len(rows))
	index := make(map[string]T, len(rows))
	for at, row := range rows {
		name := key(row)
		one := name + "#" + strconv.Itoa(seen[name])
		seen[name]++
		out[at] = numbered[T]{one, row}
		index[one] = row
	}
	return out, index
}

// mergeList merges rows of one kind: the person's rows keep their order, and new ones follow.
func mergeList[T any](r *run, item *Item, l list[T], base []T, known bool, mine, theirs []T) []T {
	_, past := number(base, l.key)
	if !known {
		past = map[string]T{}
	}
	mineRows, held := number(mine, l.key)
	theirRows, now := number(theirs, l.key)

	var out []T
	for _, one := range mineRows {
		row := one.row
		described, stays := now[one.key]
		before, was := past[one.key]
		switch {
		case stays:
			for _, f := range l.facets {
				var value *string
				if was {
					text := f.value(before)
					value = &text
				}
				change := Change{
					Field: l.field, Name: l.name(one.row), Attribute: f.attribute,
					Mine: shorten(f.shown(one.row)), Theirs: shorten(f.shown(described)),
				}
				if r.weigh(item, change, one.key, value, f.value(one.row), f.value(described)) {
					f.take(&row, described)
				}
			}
		case was && !l.keeps:
			change := Change{
				Field: l.field, Name: l.name(one.row), Kind: ChangeRemoved,
				Conflict: !l.alike(one.row, before), Mine: shorten(l.whole(one.row)),
			}
			if r.offer(item, change, one.key) {
				continue
			}
		}
		out = append(out, row)
	}
	for _, one := range theirRows {
		if _, ok := held[one.key]; ok {
			continue
		}
		change := Change{Field: l.field, Name: l.name(one.row), Kind: ChangeAdded, Theirs: shorten(l.whole(one.row))}
		if before, was := past[one.key]; was {
			// A row the person deleted stays deleted until the description changes it.
			if l.alike(one.row, before) {
				continue
			}
			change.Conflict = true
		}
		if r.offer(item, change, one.key) {
			out = append(out, one.row)
		}
	}
	return out
}

func rowFacets() []facet[domain.Row] {
	return []facet[domain.Row]{
		{
			attribute: AttributeValue, value: func(row domain.Row) string { return row.Value },
			take: func(into *domain.Row, from domain.Row) { into.Value = from.Value },
		},
		{
			attribute: AttributeDescription, value: func(row domain.Row) string { return strings.TrimSpace(row.Description) },
			take: func(into *domain.Row, from domain.Row) { into.Description = from.Description },
		},
		{
			attribute: AttributeEnabled, value: func(row domain.Row) string { return strconv.FormatBool(row.Enabled) },
			take: func(into *domain.Row, from domain.Row) { into.Enabled = from.Enabled },
		},
	}
}

func rowList(field Field, key func(string) string) list[domain.Row] {
	return list[domain.Row]{
		field:  field,
		key:    func(row domain.Row) string { return key(row.Name) },
		name:   func(row domain.Row) string { return row.Name },
		whole:  func(row domain.Row) string { return row.Value },
		facets: rowFacets(),
	}
}

var (
	paramList  = rowList(FieldParam, func(name string) string { return name })
	headerList = rowList(FieldHeader, strings.ToLower) // header names ignore case
	pathList   = rowList(FieldPathParam, func(name string) string { return name })
)

var formList = list[domain.FormRow]{
	field: FieldFormField,
	key:   func(row domain.FormRow) string { return row.Name },
	name:  func(row domain.FormRow) string { return row.Name },
	whole: func(row domain.FormRow) string { return row.Value },
	// A file's path is the person's own, so it is neither compared nor taken.
	facets: []facet[domain.FormRow]{
		{
			attribute: AttributeValue, value: func(row domain.FormRow) string { return row.Value },
			take: func(into *domain.FormRow, from domain.FormRow) { into.Value = from.Value },
		},
		{
			attribute: AttributeEnabled, value: func(row domain.FormRow) string { return strconv.FormatBool(row.Enabled) },
			take: func(into *domain.FormRow, from domain.FormRow) { into.Enabled = from.Enabled },
		},
		{
			attribute: AttributeFile, value: func(row domain.FormRow) string { return strconv.FormatBool(row.File) },
			take: func(into *domain.FormRow, from domain.FormRow) { into.File = from.File },
		},
	},
}

var exampleList = list[domain.ResponseExample]{
	field: FieldExample,
	key:   func(one domain.ResponseExample) string { return one.Name + "\x00" + strconv.Itoa(one.Status) },
	name:  func(one domain.ResponseExample) string { return strconv.Itoa(one.Status) + " " + one.Name },
	whole: func(one domain.ResponseExample) string { return one.Body },
	facets: []facet[domain.ResponseExample]{{
		value: func(one domain.ResponseExample) string {
			headers, _ := json.Marshal(one.Headers)
			return string(headers) + "\n" + canonicalJSON(one.Body)
		},
		show: func(one domain.ResponseExample) string { return one.Body },
		take: func(into *domain.ResponseExample, from domain.ResponseExample) { *into = from },
	}},
}

// variableList keeps what the description drops: the person's own requests may use it.
var variableList = list[domain.Variable]{
	field: FieldVariable,
	key:   func(one domain.Variable) string { return one.Name },
	name:  func(one domain.Variable) string { return one.Name },
	whole: variableShown,
	keeps: true,
	facets: []facet[domain.Variable]{
		{
			attribute: AttributeValue, value: func(one domain.Variable) string { return one.Value }, show: variableShown,
			take: func(into *domain.Variable, from domain.Variable) { into.Value = from.Value },
		},
		{
			attribute: AttributeEnabled, value: func(one domain.Variable) string { return strconv.FormatBool(one.Enabled) },
			take: func(into *domain.Variable, from domain.Variable) { into.Enabled = from.Enabled },
		},
	},
}

func variableShown(one domain.Variable) string {
	if one.Kind == domain.VariableSecret && one.Value != "" {
		return domain.SecretMask
	}
	return one.Value
}

// shownLimit keeps a long body from making the review unreadable.
const shownLimit = 4000

func shorten(text string) string {
	runes := []rune(text)
	if len(runes) <= shownLimit {
		return text
	}
	return string(runes[:shownLimit]) + "…"
}
