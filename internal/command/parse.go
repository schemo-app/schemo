package command

import (
	"encoding/json"
	"regexp"
	"strings"

	"schemo/internal/domain"
)

// headerEntry is a header before folding: the reader collects entries and they are collapsed once,
// at the end of the parse.
type headerEntry struct {
	name  string
	value string
}

// pendingRequest is what the reader builds before the headers are folded and the method defaulted.
type pendingRequest struct {
	method  string
	url     string
	entries []headerEntry
	body    string
	form    []domain.FormRow
	kind    domain.BodyKind
	auth    *domain.Auth
}

func (p pendingRequest) request() Request {
	return Request{
		Method: p.method, URL: p.url, Headers: foldHeaders(p.entries), Body: p.body,
		BodyKind: p.kind, Form: p.form, Auth: p.auth,
	}
}

// A repeated name keeps the position of its first occurrence and takes the value of its last —
// the last is the one that would have been sent. Cookie is the exception: a request carries one
// however many times it was written, so they leave as one joined header, appended last.
func foldHeaders(entries []headerEntry) []domain.HeaderPair {
	var order []string
	byKey := map[string]headerEntry{}
	var cookies []string
	for _, e := range entries {
		key := strings.ToLower(e.name)
		if key == "cookie" {
			// A `Cookie:` header with no value is dropped, not kept as an empty cookie.
			if e.value != "" {
				cookies = append(cookies, e.value)
			}
			continue
		}
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		byKey[key] = e
	}
	out := make([]domain.HeaderPair, 0, len(order)+1)
	for _, key := range order {
		out = append(out, domain.HeaderPair{Name: byKey[key].name, Value: byKey[key].value})
	}
	if len(cookies) > 0 {
		out = append(out, domain.HeaderPair{Name: "Cookie", Value: strings.Join(cookies, "; ")})
	}
	return out
}

var headerSeparatorRe = regexp.MustCompile("[:;]")

// parseHeaderArg splits one header argument. A string with no separator is a name with an empty
// value rather than a mistake — `curl -H x` sends an empty `x`.
func parseHeaderArg(raw string) headerEntry {
	at := headerSeparatorRe.FindStringIndex(raw)
	if at == nil {
		return headerEntry{name: trimSpace(raw)}
	}
	return headerEntry{name: trimSpace(raw[:at[0]]), value: trimSpace(raw[at[0]+1:])}
}

// withContentType declares the type an arrived body left unsaid; a command that wrote one keeps
// its own. It runs before folding, so a Content-Type written with any casing counts.
func withContentType(entries []headerEntry, contentType string) []headerEntry {
	if contentType == "" {
		return entries
	}
	for _, e := range entries {
		if strings.ToLower(e.name) == "content-type" {
			return entries
		}
	}
	out := make([]headerEntry, len(entries), len(entries)+1)
	copy(out, entries)
	return append(out, headerEntry{name: "Content-Type", value: contentType})
}

// bodyShape is what the command's data is: its kind, and the content type it goes with when the
// command wrote none. Plain `-F` fields are a multipart form — the draft composes that body from
// the rows — and anything else is read off the text the way Hoppscotch reads it: a body that parses
// as JSON is JSON, a body opening with `<` is XML (or HTML), `k=v` pairs are a form, and what is
// left is text. The old rule here was always "a form", which mislabelled every JSON paste.
func bodyShape(body string, form []domain.FormRow) (domain.BodyKind, string) {
	if len(form) > 0 {
		return domain.BodyForm, domain.MediaMultipart
	}
	if body == "" {
		return "", ""
	}
	trimmed := trimSpace(body)
	if json.Valid([]byte(trimmed)) {
		return domain.BodyJSON, domain.MediaJSON
	}
	if strings.HasPrefix(trimmed, "<") {
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html") {
			return domain.BodyRaw, domain.MediaHTML
		}
		return domain.BodyXML, domain.MediaXML
	}
	if isFormPairs(trimmed) {
		return domain.BodyRaw, domain.MediaForm
	}
	return domain.BodyRaw, domain.MediaText
}

// isFormPairs reports whether the body reads as form pairs: it holds an `=` and every
// `&`-separated part is a piece of form text with no space in it. A bare name among the pairs is
// part of the form — `q=x&note` is what curl posts — and a sentence with an `=` in it is not.
func isFormPairs(body string) bool {
	if !strings.Contains(body, "=") {
		return false
	}
	for _, pair := range strings.Split(body, "&") {
		if pair == "" || strings.ContainsAny(pair, " \t") {
			return false
		}
	}
	return true
}

// splitCredential is `login:password` cut at the first colon, which is where a Basic credential is
// cut and the only rule there is: a password may hold a colon and a login may not.
func splitCredential(raw string) (string, string) {
	login, password, found := strings.Cut(raw, ":")
	if !found {
		return raw, ""
	}
	return login, password
}

// A credential a command carries becomes a scheme rather than the header it looks like, so the Auth
// chip shows what the command asked for and the answers stay editable. See writtenAuth for a
// credential written as an `Authorization` header.
func basicScheme(user, password string) *domain.Auth {
	auth := domain.NewAuth(domain.AuthBasic).With("username", user).With("password", password)
	return &auth
}

func digestScheme(user, password string) *domain.Auth {
	auth := domain.NewAuth(domain.AuthDigest).With("username", user).With("password", password)
	return &auth
}

func bearerScheme(token string) *domain.Auth {
	auth := domain.NewAuth(domain.AuthBearer).With("token", token)
	return &auth
}

func appendQuery(url, query string) string {
	if query == "" {
		return url
	}
	if strings.Contains(url, "?") {
		return url + "&" + query
	}
	return url + "?" + query
}

// pickURL takes the first positional that reads as a URL, or else the last one, so
// `curl -X POST example.com/x` still finds its URL.
func pickURL(positionals []string) string {
	for _, p := range positionals {
		if looksLikeURL(p) {
			return p
		}
	}
	if len(positionals) == 0 {
		return ""
	}
	return positionals[len(positionals)-1]
}

// hasLeftover reports positionals other than the chosen URL. The chosen one is removed and what is
// left is looked at, so a second URL or a stray file name is an error rather than an argument that
// was silently dropped.
func hasLeftover(positionals []string, url string) bool {
	rest := make([]string, 0, len(positionals))
	removed := false
	for _, p := range positionals {
		if !removed && p == url {
			removed = true
			continue
		}
		rest = append(rest, p)
	}
	return len(rest) > 0
}
