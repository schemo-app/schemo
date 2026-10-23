package domain

import (
	"errors"
	"mime"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
)

// DraftID names a draft the window is editing: a tab's (see IsTabDraft) or a saved request's,
// keyed by the request's own id.
type DraftID string

type Row struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Value       string `json:"value"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
}

type RowKind string

const (
	RowParams     RowKind = "params"
	RowHeaders    RowKind = "headers"
	RowForm       RowKind = "form"
	RowPathParams RowKind = "pathParams"
)

type Draft struct {
	ID      DraftID `json:"id"`
	Method  string  `json:"method"`
	URL     string  `json:"url"`
	Params  []Row   `json:"params"`
	Headers []Row   `json:"headers"`
	// PathParams holds one row for each `:name` the address carries and is derived from it: the
	// name is the address's, and what the row keeps is the value and the description.
	PathParams    []Row     `json:"pathParams"`
	Auth          Auth      `json:"auth"`
	Body          string    `json:"body"`
	BodyKind      BodyKind  `json:"bodyKind"`
	Form          []FormRow `json:"form"`
	BodyFile      string    `json:"bodyFile,omitempty"`
	EnvironmentID string    `json:"environmentId,omitempty"`
}

func NewDraft() Draft {
	return Draft{
		Method:     "GET",
		Params:     []Row{},
		Headers:    []Row{},
		PathParams: []Row{},
		Auth:       Auth{Type: AuthNone},
		Form:       []FormRow{},
	}
}

// The media types a body is sent as and read back from, named once for every reader and writer.
const (
	MediaJSON      = "application/json"
	MediaXML       = "application/xml"
	MediaForm      = "application/x-www-form-urlencoded"
	MediaMultipart = "multipart/form-data"
	MediaText      = "text/plain"
	MediaHTML      = "text/html"
)

type BodyKind string

const (
	BodyRaw    BodyKind = "raw"
	BodyJSON   BodyKind = "json"
	BodyXML    BodyKind = "xml"
	BodyForm   BodyKind = "form"
	BodyBinary BodyKind = "binary"
)

func KindOf(kind BodyKind) BodyKind {
	if kind == "" {
		return BodyRaw
	}
	return kind
}

type FormRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Value   string `json:"value"`
	Src     string `json:"src,omitempty"`
	File    bool   `json:"file,omitempty"`
	Enabled bool   `json:"enabled"`
}

// MediaTypeOf is the media type of a Content-Type value: type/subtype in lower case, without its
// parameters. A value too damaged for its parameters to be read still names a type before the first
// semicolon.
func MediaTypeOf(contentType string) string {
	media, _, err := mime.ParseMediaType(contentType)
	if err == nil || (media != "" && errors.Is(err, mime.ErrInvalidMediaParameter)) {
		return media
	}
	head, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(head))
}

// IsJSONMedia says whether a media type is JSON: application/json, or any type that says it is
// built on it with the +json suffix of RFC 6839 (application/vnd.api+json, application/problem+json).
func IsJSONMedia(media string) bool {
	return media == MediaJSON || strings.HasSuffix(media, "+json")
}

// IsHTMLMedia says whether a media type is a page: text/html, or its XML spelling.
func IsHTMLMedia(media string) bool {
	return media == MediaHTML || media == "application/xhtml+xml"
}

// IsXMLMedia says whether a media type is XML (RFC 7303): application/xml, text/xml, or a type with
// the +xml suffix.
func IsXMLMedia(media string) bool {
	return media == MediaXML || media == "text/xml" || strings.HasSuffix(media, "+xml")
}

// IsCSVMedia says whether a media type is a table of delimited text.
func IsCSVMedia(media string) bool {
	switch media {
	case "text/csv", "application/csv", "text/comma-separated-values", "text/tab-separated-values":
		return true
	}
	return false
}

// SplitQuery takes the query out of an address (RFC 3986 §3.4) and leaves everything else where it
// was. The fragment stays with the address, and a `?` after a `#` belongs to the fragment.
func SplitQuery(address string) (base, query string) {
	rest, fragment, hasFragment := strings.Cut(address, "#")
	if at := strings.IndexByte(rest, '?'); at >= 0 {
		rest, query = rest[:at], rest[at+1:]
	}
	if hasFragment {
		rest += "#" + fragment
	}
	return rest, query
}

// QueryPair is one `name=value` of a query string.
type QueryPair struct {
	Name  string
	Value string
}

// QueryPairs reads a query string into its pairs, in the order they were written and with the
// repeated names kept, which url.Values cannot do. A part with no `=` is a name on its own, the way
// a flag is written. Nothing is decoded; see Decoded.
func QueryPairs(query string) []QueryPair {
	var pairs []QueryPair
	for _, part := range strings.Split(query, "&") {
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		pairs = append(pairs, QueryPair{Name: name, Value: value})
	}
	return pairs
}

// Decoded is the pair as an HTML form reads it: a plus is a workspace and %XX is a byte. A part that
// does not decode (a lone percent sign) stays as it was written.
func (p QueryPair) Decoded() QueryPair {
	return QueryPair{Name: decodedOr(p.Name), Value: decodedOr(p.Value)}
}

func decodedOr(text string) string {
	if decoded, err := url.QueryUnescape(text); err == nil {
		return decoded
	}
	return text
}

// A path parameter is a `:name` segment of an address, the request's own slot. `{{name}}` is looked
// up in the environments, while `:name` is replaced by the value of the row of that name.

// pathToken is one `:name` in an address: where it stands, colon included, and what it is called.
type pathToken struct {
	start, end int
	name       string
}

// pathSlot is a `:name` right after a slash. A name starts with a letter or underscore, so the port
// of `host:8080` never matches; a hyphen continues it and a dot ends it (`:id.json`). The window
// reads the same grammar in `frontend/src/lib/vars.ts`.
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

// FillPath replaces each `:name` with the value of the row of that name. A name with no row or no
// value stays as written, so an address with a literal `:name` goes out unchanged.
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

// PathParamName makes a name fit a `:name` segment: disallowed characters become underscores and a
// leading digit gets one prefixed.
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

// ResolvedRequest is a saved request as a copy needs it: the whole request, the authorization from
// the nearest level above and the variables of the levels above. The tree carries neither bodies
// nor those answers, so the walk that knows them returns this shape.
type ResolvedRequest struct {
	Node      CollectionNode `json:"node"`
	Auth      *Auth          `json:"auth,omitempty"`
	Variables []Variable     `json:"variables,omitempty"`
}

type HTTPVersion string

const (
	HTTPAuto HTTPVersion = "auto"
	HTTP1    HTTPVersion = "http1"
)

func (v HTTPVersion) Valid() bool {
	return v == HTTPAuto || v == HTTP1
}

type Requests struct {
	HTTPVersion     HTTPVersion `json:"httpVersion"`
	UserAgent       string      `json:"userAgent"`
	Timeout         int         `json:"timeout"`
	MaxRedirects    int         `json:"maxRedirects"`
	FollowRedirects bool        `json:"followRedirects"`
	Cookies         bool        `json:"cookies"`
	AcceptEncoding  bool        `json:"acceptEncoding"`
	ShowAuto        bool        `json:"showAuto"`
}

func DefaultRequests() Requests {
	return Requests{
		Timeout:         30,
		FollowRedirects: true,
		MaxRedirects:    10,
		Cookies:         true,
		HTTPVersion:     HTTPAuto,
		AcceptEncoding:  true,
	}
}

func (r Requests) Valid() error {
	if !r.HTTPVersion.Valid() {
		return Refuse(CodeUnknownHTTPVersion, ErrNotAllowed, Args{"version": string(r.HTTPVersion)})
	}
	return nil
}

type Network struct {
	Proxy        ProxySettings `json:"proxy"`
	Certificates Certificates  `json:"certificates"`
	Requests     Requests      `json:"requests"`
}

type CaptureSource string

const (
	CaptureBody   CaptureSource = "body"
	CaptureHeader CaptureSource = "header"
	CaptureCookie CaptureSource = "cookie"
	CaptureStatus CaptureSource = "status"
)

type Capture struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Source CaptureSource `json:"source"`
	Path   string        `json:"path,omitempty"`
	Scope  VarScope      `json:"scope"`
	Secret bool          `json:"secret,omitempty"`
	Off    bool          `json:"off,omitempty"`
}

var variableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func IsVariableName(name string) bool { return variableName.MatchString(name) }

func (c Capture) Valid() bool {
	if !IsVariableName(c.Name) {
		return false
	}
	switch c.Scope {
	case ScopeRun, ScopeEnvironment, ScopeGlobals:
	default:
		return false
	}
	switch c.Source {
	case CaptureStatus:
		return true
	case CaptureBody, CaptureHeader, CaptureCookie:
		return c.Path != ""
	}
	return false
}

// LoopbackHost says whether a host is this machine by its own spelling: an address in a loopback
// range, or the name localhost. A name that merely starts like an address (127.example.com) is a
// name like any other.
func LoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

// LocalName is LoopbackHost, and any name under .localhost, which RFC 6761 §6.3 reserves for this
// machine. A resolver is only advised to honour that, so whoever trusts the name for more than a
// default also checks the address it was dialed at.
func LocalName(host string) bool {
	if LoopbackHost(host) {
		return true
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return strings.HasSuffix(host, ".localhost")
}
