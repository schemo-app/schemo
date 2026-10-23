package command

import (
	"encoding/json"
	"net/url"
	"regexp"
	"schemo/internal/domain"
	"slices"
	"strings"
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

// A repeated name keeps the position of its first occurrence and the value of its last, which is
// what would have been sent. Cookie is the exception: a request carries one, so repeats leave as
// one joined header, appended last.
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

// bodyShape is the kind of the command's data and the content type it goes with when the command
// wrote none. Plain `-F` fields are a multipart form (the draft composes that body from rows);
// anything else is read off the text: JSON if it parses, XML if it opens with `<`, a form for `k=v`
// pairs, otherwise text.
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

// isFormPairs reports whether the body reads as form pairs: it holds an `=` and every `&`-separated
// part has no space. A bare name is allowed (`q=x&note` is what curl posts).
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
// chip shows what was asked for and stays editable. See writtenAuth for credentials written as an
// `Authorization` header.
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

// hasLeftover reports positionals other than the chosen URL, so a second URL or a stray file name
// is an error rather than a silently dropped argument.
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

// curlArgs is what the flags of a command said, before any of it is put together into a request.
type curlArgs struct {
	method       optString
	explicitURL  optString
	user         optString
	bearer       optString
	nonBasicAuth bool
	digest       bool
	get          bool
	entries      []headerEntry
	data         []string
	forms        []headerEntry
	positionals  []string
}

func fromCurl(tokens []string) (pendingRequest, Reason) {
	args, reason := readCurlFlags(tokens)
	if reason != "" {
		return pendingRequest{}, reason
	}
	return args.request()
}

// readCurlFlags walks the tokens once, taking each flag this app has an answer for and stepping
// over the ones it has none for, so that an operand is never taken for a flag's argument.
func readCurlFlags(tokens []string) (curlArgs, Reason) {
	args := curlArgs{entries: []headerEntry{}}

	for cursor := 0; cursor < len(tokens); cursor++ {
		raw := tokens[cursor]
		if raw == "--" {
			// Everything after `--` is an operand, even if it starts with a dash.
			args.positionals = append(args.positionals, tokens[cursor+1:]...)
			break
		}

		if cluster := splitCluster(raw); len(cluster) > 1 {
			// Joined into a new slice: the caller's tokens are not rewritten under it.
			tokens = slices.Concat(tokens[:cursor], cluster, tokens[cursor+1:])
			raw = tokens[cursor]
		}

		flag, inline := splitFlag(raw)
		if inline == nil && !strings.HasPrefix(raw, "-") {
			args.positionals = append(args.positionals, raw)
			continue
		}

		switch flag {
		case "-X", "--request":
			if v := take(tokens, &cursor, inline); v.set {
				args.method = optString{value: strings.ToUpper(v.value), set: true}
			}
		case "-H", "--header":
			// An empty argument is not a header; anything else is one even without a colon.
			if v := take(tokens, &cursor, inline); v.set && v.value != "" {
				args.entries = append(args.entries, parseHeaderArg(v.value))
			}
		case "-d", "--data", "--data-raw", "--data-binary", "--data-ascii":
			if v := take(tokens, &cursor, inline); v.set {
				args.data = append(args.data, v.value)
			}
		case "--data-urlencode":
			if v := take(tokens, &cursor, inline); v.set {
				args.data = append(args.data, urlencodeData(v.value))
			}
		case "--json":
			if v := take(tokens, &cursor, inline); v.set {
				args.data = append(args.data, v.value)
				args.entries = append(args.entries,
					headerEntry{name: "Content-Type", value: domain.MediaJSON})
			}
		case "--url":
			if v := take(tokens, &cursor, inline); v.set {
				args.explicitURL = v
			}
		case "-I", "--head":
			args.method = optString{value: "HEAD", set: true}
		case "-T", "--upload-file":
			// curl uploads a file with PUT. The path itself goes: this app sends a body, and a file
			// on the pasting machine's disk is what a command line cannot carry over.
			if v := take(tokens, &cursor, inline); v.set && !args.method.set {
				args.method = optString{value: "PUT", set: true}
			}
		case "-G", "--get":
			args.get = true
		case "-u", "--user":
			if v := take(tokens, &cursor, inline); v.set {
				args.user = v
			}
		case "-A", "--user-agent":
			if v := take(tokens, &cursor, inline); v.set {
				args.entries = append(args.entries, headerEntry{name: "User-Agent", value: v.value})
			}
		case "-b", "--cookie":
			// A value without `=` names a cookie *file*, not a cookie.
			if v := take(tokens, &cursor, inline); v.set && v.value != "" && strings.Contains(v.value, "=") {
				args.entries = append(args.entries, headerEntry{name: "Cookie", value: v.value})
			}
		case "-F", "--form", "--form-string":
			if v := take(tokens, &cursor, inline); v.set {
				name, value, _ := strings.Cut(v.value, "=")
				// `-F 'file=@path'` is an upload this app has no way to reproduce.
				if strings.HasPrefix(value, "@") || strings.HasPrefix(value, "<") {
					return curlArgs{}, ReasonUnsupportedMultipart
				}
				args.forms = append(args.forms, headerEntry{name: name, value: value})
			}
		case "--digest":
			args.digest = true
		case "--oauth2-bearer":
			if v := take(tokens, &cursor, inline); v.set {
				args.bearer = v
			}
		case "--ntlm", "--negotiate", "--anyauth", "--proxy-anyauth":
			// Schemes this app has no answer for. Inventing a Basic credential for them would be
			// sending one the command never asked for, so `-u` is left alone.
			args.nonBasicAuth = true
		default:
			if curlFlags[flag] {
				take(tokens, &cursor, inline)
			}
		}
	}
	return args, ""
}

// request puts what the flags said together: where it goes, what it carries, who it says it is and
// how it is sent.
func (a curlArgs) request() (pendingRequest, Reason) {
	url := pickURL(a.positionals)
	if a.explicitURL.set {
		url = a.explicitURL.value
	}
	if url == "" {
		return pendingRequest{}, ReasonNoURL
	}
	if hasLeftover(a.positionals, url) {
		return pendingRequest{}, ReasonLeftover
	}

	entries, auth := writtenAuth(a.entries, a.auth())

	body := strings.Join(a.data, "&")
	formRows := []domain.FormRow{}
	if body == "" && len(a.forms) > 0 {
		// A plain `-F name=value` is a multipart field, not a form-encoded body: the draft composes
		// the multipart body from the rows, so the request goes out as the command meant it.
		for _, f := range a.forms {
			formRows = append(formRows, domain.FormRow{Name: f.name, Value: f.value, Enabled: true})
		}
	}
	if a.get {
		// `-G` moves the data into the query string instead of sending it, and with it goes any
		// body the request would have carried.
		url = appendQuery(url, body)
		body = ""
	}

	kind, contentType := bodyShape(body, formRows)
	result := pendingRequest{
		url:     url,
		entries: withContentType(entries, contentType),
		body:    body,
		form:    formRows,
		kind:    kind,
		auth:    auth,
	}
	switch {
	case a.method.set:
		result.method = a.method.value
	case (body != "" || len(formRows) > 0) && !a.get:
		result.method = "POST"
	default:
		result.method = "GET"
	}
	return result, ""
}

// auth is the scheme the flags name, or none. A bearer token wins over a login also given (curl
// takes both and the server reads the bearer); a written header wins over both, which `writtenAuth`
// handles.
func (a curlArgs) auth() *domain.Auth {
	switch {
	case a.bearer.set:
		return bearerScheme(a.bearer.value)
	case a.user.set && !a.nonBasicAuth:
		login, password := splitCredential(a.user.value)
		if a.digest {
			return digestScheme(login, password)
		}
		return basicScheme(login, password)
	}
	return nil
}

// urlencodeData is one `--data-urlencode` argument as its part of the body. The name keeps its
// spelling and what follows the first `=` is percent-encoded (a space as `%20`, not `+`). The
// `@file` forms need a file this app cannot read, so they travel as written.
func urlencodeData(value string) string {
	name, content, found := strings.Cut(value, "=")
	if found && strings.Contains(name, "@") {
		return value
	}
	if !found {
		if strings.HasPrefix(value, "@") {
			return value
		}
		return escaped(value)
	}
	if name == "" {
		// `=content` is the content alone, encoded, with no name in front of it.
		return escaped(content)
	}
	return name + "=" + escaped(content)
}

// escaped is curl's own percent-encoding: the unreserved characters keep their spelling and a
// space is `%20` — QueryEscape's set with its `+` turned back into one.
func escaped(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// The row goes with the credential: left beside the scheme it would win, since a written header is
// what goes out, and editing the chip would do nothing. A scheme that cannot be rebuilt from a
// header (Digest, Negotiate, NTLM) stays the header it is.
func writtenAuth(entries []headerEntry, auth *domain.Auth) ([]headerEntry, *domain.Auth) {
	for i, e := range entries {
		if !strings.EqualFold(e.name, "Authorization") {
			continue
		}
		scheme, ok := authFromHeader(e.value)
		if !ok {
			return entries, auth
		}
		rest := make([]headerEntry, 0, len(entries)-1)
		rest = append(rest, entries[:i]...)
		return append(rest, entries[i+1:]...), scheme
	}
	return entries, auth
}

func authFromHeader(value string) (*domain.Auth, bool) {
	value = strings.TrimSpace(value)
	name, rest, found := strings.Cut(value, " ")
	if !found {
		return nil, false
	}
	switch {
	case strings.EqualFold(name, "basic"):
		login, password, ok := domain.BasicCredentials(value)
		if !ok {
			return nil, false
		}
		return basicScheme(login, password), true
	case strings.EqualFold(name, "bearer"):
		if token := strings.TrimSpace(rest); token != "" {
			return bearerScheme(token), true
		}
	}
	return nil, false
}
