package domain

import (
	"net/url"
	"regexp"
	"strings"
)

// A path parameter is a `:name` segment of an address. It is the request's own slot, which is what
// sets it apart from a variable: `{{name}}` is looked up in the environments, while `:name` is
// replaced by the value of the row of that name the request carries.

// pathToken is one `:name` in an address: where it stands, colon included, and what it is called.
type pathToken struct {
	start, end int
	name       string
}

// pathSlot is a `:name` right after a slash. A name starts with a letter or an underscore, so the
// port of `host:8080` is never one; a hyphen continues it and a dot ends it (`:id.json`). The
// window reads the same grammar in `frontend/src/lib/vars.ts`.
var pathSlot = regexp.MustCompile(`/(:([A-Za-z_][A-Za-z0-9_-]*))`)

// pathTokens finds the `:name` segments before the query or the fragment.
func pathTokens(address string) []pathToken {
	if end := strings.IndexAny(address, "?#"); end >= 0 {
		address = address[:end]
	}
	var out []pathToken
	for _, found := range pathSlot.FindAllStringSubmatchIndex(address, -1) {
		out = append(out, pathToken{start: found[2], end: found[3], name: address[found[4]:found[5]]})
	}
	return out
}

func startsName(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || c == '_'
}

func continuesName(c byte) bool { return startsName(c) || '0' <= c && c <= '9' || c == '-' }

// PathParams lists the names of an address's `:name` segments, in the order it writes them and each
// once.
func PathParams(address string) []string {
	var out []string
	seen := map[string]bool{}
	for _, token := range pathTokens(address) {
		if !seen[token.name] {
			seen[token.name] = true
			out = append(out, token.name)
		}
	}
	return out
}

// ReplacePathParams writes each `:name` of an address as what replace answers for the name. The
// rest of the address, the query included, is left as it is.
func ReplacePathParams(address string, replace func(name string) string) string {
	var out strings.Builder
	last := 0
	for _, token := range pathTokens(address) {
		out.WriteString(address[last:token.start])
		out.WriteString(replace(token.name))
		last = token.end
	}
	out.WriteString(address[last:])
	return out.String()
}

// FillPath writes each `:name` of an address as the value of the row of that name. A name with no
// row, or a row with no value, stays as written: an empty slot is for a person to fill, and an
// address pasted from somewhere else with a literal `:name` in it must go out as it came.
func FillPath(address string, rows []Row) string {
	values := map[string]string{}
	for _, row := range rows {
		if row.Value != "" {
			values[row.Name] = row.Value
		}
	}
	if len(values) == 0 {
		return address
	}
	return ReplacePathParams(address, func(name string) string {
		if value, ok := values[name]; ok {
			return escapeSegment(value)
		}
		return ":" + name
	})
}

// PathParamName is a name as a `:name` segment can carry it: what a name does not allow becomes an
// underscore, and one that would start with a digit gets one in front. A document names its path
// parameters as it likes, and an address can hold only these.
func PathParamName(raw string) string {
	var out strings.Builder
	for i := 0; i < len(raw); i++ {
		if continuesName(raw[i]) {
			out.WriteByte(raw[i])
		} else if out.Len() == 0 || !strings.HasSuffix(out.String(), "_") {
			out.WriteByte('_')
		}
	}
	name := out.String()
	if name == "" || !startsName(name[0]) {
		return "_" + name
	}
	return name
}

// escapeSegment makes a value one segment of a path: a slash in it stays inside the segment as
// `%2F`. A `{{token}}` is left as typed, because the resolver looks for it after this.
func escapeSegment(value string) string {
	var out strings.Builder
	for {
		start := strings.Index(value, "{{")
		if start < 0 {
			break
		}
		end := strings.Index(value[start:], "}}")
		if end < 0 {
			break
		}
		out.WriteString(url.PathEscape(value[:start]))
		out.WriteString(value[start : start+end+2])
		value = value[start+end+2:]
	}
	out.WriteString(url.PathEscape(value))
	return out.String()
}
