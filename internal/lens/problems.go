package lens

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"schemo/internal/domain"
)

// Diagnose reads an error response for what it says is wrong with the request. No diagnosis unless
// the error is in a known shape and names a part of the request.
func (d *Document) Diagnose(status int) (domain.Diagnosis, bool) {
	if status < 400 || d.kind != domain.DocJSON || d.root.kind != domain.NodeObject {
		return domain.Diagnosis{}, false
	}
	readers := []func(*node, int) *domain.Diagnosis{
		readJSONAPI, readProblem, readSpring, readNested, readWrapped, readFieldMap,
	}
	for _, read := range readers {
		if found := read(d.root, status); found != nil && hasField(found.Problems) {
			return *found, true
		}
	}
	return domain.Diagnosis{}, false
}

func (n *node) member(key string) *node {
	if n == nil || n.kind != domain.NodeObject {
		return nil
	}
	for _, kid := range n.kids {
		if kid.name == key {
			return kid
		}
	}
	return nil
}

// words is a scalar's text, or empty for anything else.
func (n *node) words() string {
	if n == nil {
		return ""
	}
	switch n.kind {
	case domain.NodeString, domain.NodeNumber, domain.NodeBoolean:
		return strings.TrimSpace(n.scalar)
	}
	return ""
}

// first is the first of the members that has words.
func (n *node) first(keys ...string) string {
	for _, key := range keys {
		if w := n.member(key).words(); w != "" {
			return w
		}
	}
	return ""
}

func (n *node) at() string {
	if n == nil {
		return ""
	}
	return jsonPath(n.steps())
}

func pointer(segments []string) string {
	var b strings.Builder
	replace := strings.NewReplacer("~", "~0", "/", "~1")
	for _, s := range segments {
		b.WriteString("/" + replace.Replace(s))
	}
	return b.String()
}

// splitField reads `a.b`, `a[b]` and `a.0.b`.
func splitField(field string) []string {
	field = strings.TrimPrefix(strings.TrimSpace(field), "$")
	field = strings.NewReplacer("[", ".", "]", "", `"`, "", "'", "").Replace(field)
	var out []string
	for _, part := range strings.Split(field, ".") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// bodyField converts a server's field notation to a body JSON Pointer.
func bodyField(field string) (where, name string) {
	segments := splitField(field)
	if len(segments) == 0 {
		return "", ""
	}
	return "body", pointer(segments)
}

// fromPointer takes a JSON Pointer as is and anything else as a dotted field.
func fromPointer(text string) (where, name string) {
	if strings.HasPrefix(text, "/") {
		return "body", text
	}
	return bodyField(text)
}

func readJSONAPI(root *node, _ int) *domain.Diagnosis {
	errs := root.member("errors")
	if errs == nil || errs.kind != domain.NodeArray || len(errs.kids) == 0 {
		return nil
	}
	for _, e := range errs.kids {
		if e.kind != domain.NodeObject ||
			(e.member("title") == nil && e.member("detail") == nil && e.member("source") == nil) {
			return nil
		}
	}
	out := &domain.Diagnosis{Format: "jsonapi"}
	for _, e := range errs.kids {
		problem := domain.Problem{Message: e.first("detail", "title"), Path: e.at()}
		source := e.member("source")
		switch {
		case source.member("pointer") != nil:
			problem.Where, problem.Field = fromPointer(source.member("pointer").words())
		case source.member("parameter") != nil:
			problem.Where, problem.Field = "query", source.member("parameter").words()
		case source.member("header") != nil:
			problem.Where, problem.Field = "header", source.member("header").words()
		}
		out.Problems = append(out.Problems, problem)
	}
	return out
}

// readProblem reads RFC 9457 and its common validation extensions.
func readProblem(root *node, _ int) *domain.Diagnosis {
	if root.first("title", "detail") == "" ||
		(root.member("type") == nil && root.member("status") == nil && root.member("instance") == nil) {
		return nil
	}
	out := &domain.Diagnosis{Format: "problem"}
	for _, key := range []string{"errors", "invalid-params", "invalid_params", "invalidParams"} {
		out.Problems = append(out.Problems, problemsOf(root.member(key))...)
	}
	return out
}

func readSpring(root *node, _ int) *domain.Diagnosis {
	if root.member("status").words() == "" || root.member("error").words() == "" ||
		root.member("path") == nil {
		return nil
	}
	return &domain.Diagnosis{Format: "spring", Problems: problemsOf(root.member("errors"))}
}

// readNested reads `{"error": {"message": ..., "param": ...}}`, as payment APIs write it.
func readNested(root *node, _ int) *domain.Diagnosis {
	inner := root.member("error")
	if inner == nil || inner.kind != domain.NodeObject ||
		inner.first("message", "detail", "reason") == "" {
		return nil
	}
	out := &domain.Diagnosis{Format: "error"}
	if param := inner.first("param", "field", "target"); param != "" {
		where, field := bodyField(param)
		out.Problems = append(out.Problems, domain.Problem{
			Message: inner.first("message", "detail", "reason"), Where: where, Field: field,
			Path: inner.member("param").at(),
		})
	}
	return out
}

// readWrapped reads `errors` beside a message: Laravel, Rails, Node validators.
func readWrapped(root *node, _ int) *domain.Diagnosis {
	errs := root.member("errors")
	if errs == nil || (errs.kind != domain.NodeObject && errs.kind != domain.NodeArray) {
		return nil
	}
	return &domain.Diagnosis{Format: "validation", Problems: problemsOf(errs)}
}

// readFieldMap reads a body of only fields and complaints (Django REST) and FastAPI's `detail`
// list; the first kind counts only for 400 and 422.
func readFieldMap(root *node, status int) *domain.Diagnosis {
	if detail := root.member("detail"); detail != nil && detail.kind == domain.NodeArray {
		if problems := problemsOf(detail); len(problems) > 0 {
			return &domain.Diagnosis{Format: "validation", Problems: problems}
		}
	}
	if status != 400 && status != 422 {
		return nil
	}
	var problems []domain.Problem
	for _, kid := range root.kids {
		if kid.name == "detail" || kid.name == "non_field_errors" {
			for _, message := range messagesOf(kid) {
				problems = append(problems, domain.Problem{Message: message, Path: kid.at()})
			}
			continue
		}
		// A field's complaints are a list; a bare string is another kind of body.
		if kid.kind == domain.NodeString {
			return nil
		}
		problems = append(problems, complaints(kid, keyPath(kid.name))...)
	}
	if len(problems) < len(root.kids) {
		return nil
	}
	return &domain.Diagnosis{Format: "validation", Problems: problems}
}

func hasField(problems []domain.Problem) bool {
	for _, p := range problems {
		if p.Field != "" {
			return true
		}
	}
	return false
}

// problemsOf reads a list or map of complaints.
func problemsOf(n *node) []domain.Problem {
	if n == nil {
		return nil
	}
	switch n.kind {
	case domain.NodeArray:
		var out []domain.Problem
		for _, item := range n.kids {
			switch item.kind {
			case domain.NodeObject:
				if p, ok := problemOf(item); ok {
					out = append(out, p)
				}
			case domain.NodeString:
				out = append(out, domain.Problem{Message: item.words(), Path: item.at()})
			}
		}
		return out
	case domain.NodeObject:
		var out []domain.Problem
		for _, kid := range n.kids {
			out = append(out, complaints(kid, keyPath(kid.name))...)
		}
		return out
	}
	return nil
}

// keyPath splits an errors-map key such as `items.0.name` or `$.address.street` into steps.
func keyPath(key string) []string {
	if steps := splitField(key); len(steps) > 0 {
		return steps
	}
	return []string{key}
}

// problemOf reads one entry of a problem list.
func problemOf(item *node) (domain.Problem, bool) {
	message := item.first("message", "msg", "defaultMessage", "reason", "detail", "error", "title")
	if message == "" {
		return domain.Problem{}, false
	}
	problem := domain.Problem{Message: message, Path: item.at()}
	if loc := item.member("loc"); loc != nil && loc.kind == domain.NodeArray {
		problem.Where, problem.Field = located(loc)
		return problem, true
	}
	field := item.first("field", "name", "param", "property", "pointer", "path", "parameter")
	if field != "" {
		problem.Where, problem.Field = fromPointer(field)
	}
	return problem, true
}

// located reads FastAPI's `loc`: the part of the request, then the member.
func located(loc *node) (where, field string) {
	var parts []string
	for _, kid := range loc.kids {
		parts = append(parts, kid.words())
	}
	if len(parts) == 0 {
		return "", ""
	}
	switch parts[0] {
	case "body":
		if len(parts) == 1 {
			return "", ""
		}
		return "body", pointer(parts[1:])
	case "query", "header", "path", "cookie":
		if len(parts) > 1 {
			place := parts[0]
			if place == "path" || place == "cookie" {
				place = ""
			}
			return place, parts[1]
		}
	}
	return "body", pointer(parts)
}

// complaints reads what a field of an errors map holds: messages or nested fields.
func complaints(n *node, at []string) []domain.Problem {
	switch n.kind {
	case domain.NodeString:
		if n.words() == "" {
			return nil
		}
		return []domain.Problem{{Message: n.words(), Where: "body", Field: pointer(at), Path: n.at()}}
	case domain.NodeArray:
		var out []domain.Problem
		for i, item := range n.kids {
			if item.kind == domain.NodeString {
				out = append(out, complaints(item, at)...)
				continue
			}
			out = append(out, complaints(item, append(append([]string{}, at...), strconv.Itoa(i)))...)
		}
		return out
	case domain.NodeObject:
		var out []domain.Problem
		for _, kid := range n.kids {
			out = append(out, complaints(kid, append(append([]string{}, at...), kid.name))...)
		}
		return out
	}
	return nil
}

func messagesOf(n *node) []string {
	switch n.kind {
	case domain.NodeString:
		return []string{n.words()}
	case domain.NodeArray:
		var out []string
		for _, kid := range n.kids {
			if w := kid.words(); w != "" {
				out = append(out, w)
			}
		}
		return out
	}
	return nil
}

var tokenRe = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// Locate finds the body member a pointer names. `{{tokens}}` are read as same-length numbers so
// offsets hold.
func Locate(contentType, text, ptr string) (domain.Span, bool) {
	masked := tokenRe.ReplaceAllStringFunc(text, func(token string) string {
		return strings.Repeat("0", len([]rune(token)))
	})
	doc, err := Open(contentType, masked)
	if err != nil || doc.kind != domain.DocJSON {
		return domain.Span{}, false
	}
	found := followPointer(doc.root, splitPointer(ptr))
	if found == nil {
		return domain.Span{}, false
	}
	return domain.Span{
		Head: doc.units(found.head()), Start: doc.units(found.start), End: doc.units(found.end),
	}, true
}

func splitPointer(ptr string) []string {
	var out []string
	unescape := strings.NewReplacer("~1", "/", "~0", "~")
	for _, part := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		if part != "" {
			out = append(out, unescape.Replace(part))
		}
	}
	return out
}

// followPointer walks a pointer; a key matches ignoring case, `_` and `-` (`Email` finds `email`).
func followPointer(root *node, segments []string) *node {
	at := root
	for _, segment := range segments {
		var next *node
		switch at.kind {
		case domain.NodeArray:
			if i, err := strconv.Atoi(segment); err == nil && i >= 0 && i < len(at.kids) {
				next = at.kids[i]
			}
		case domain.NodeObject:
			next = at.member(segment)
			for _, kid := range at.kids {
				if next == nil && folded(kid.name) == folded(segment) {
					next = kid
				}
			}
		}
		if next == nil {
			return nil
		}
		at = next
	}
	if at == root && len(segments) > 0 {
		return nil
	}
	return at
}

func folded(key string) string {
	var b strings.Builder
	for _, r := range key {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
