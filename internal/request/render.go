package request

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"schemo/internal/convert/command"
	"schemo/internal/domain"
	"schemo/internal/workspace"
)

func partsOf(draft domain.Draft, inherits *domain.Auth, cookie string) parts {
	raw := collectParts(draft, cookie)
	raw.auth = authToApply(draft.Auth, inherits)
	return raw
}

func (s *Drafts) prepare(
	ctx context.Context,
	workspace string,
	draft domain.Draft,
	inherits *domain.Auth,
	above domain.VarLevels,
) (SendInput, error) {
	resolve, err := s.vars.Resolver(ctx, workspace, above)
	if err != nil {
		return SendInput{}, err
	}
	rules := protocols[draft.Protocol]
	cookie := ""
	if !rules.metadata {
		url, _ := resolve(domain.FillPath(draft.URL, draft.PathParams))
		cookie, err = s.jar.CookieHeader(ctx, workspace, rules.httpURL(url))
		if err != nil {
			return SendInput{}, err
		}
	}
	raw := partsOf(draft, inherits, cookie)
	texts := raw.texts()
	live, hidden := make([]string, len(texts)), make([]string, len(texts))
	for i, value := range texts {
		live[i], hidden[i] = resolve(value)
	}
	prepared, err := s.assemble(ctx, draft, raw.putBack(live), raw.putBack(hidden))
	if err == nil && rules.resolveFrame {
		prepared.message = resolve
	}
	return prepared, err
}

// assemble is the request built from its two renderings: what goes out, and the copy that is
// written down.
func (s *Drafts) assemble(
	ctx context.Context,
	draft domain.Draft,
	sent, stored parts,
) (SendInput, error) {
	protocol := draft.Protocol
	if !protocol.Valid() {
		return SendInput{}, fmt.Errorf("unknown request protocol %q", protocol)
	}
	method := protocol.HTTPMethod(draft.Method)
	rules := protocols[protocol]
	kind := requestBodyKind(draft)

	// The first rendering mints the multipart boundary and the second reuses it, so the stored copy
	// matches what was sent.
	body, boundary, err := s.renderRequestBody(draft, sent, kind, "", false)
	if err != nil {
		return SendInput{}, err
	}
	masked, _, err := s.renderRequestBody(draft, stored, kind, boundary, true)
	if err != nil {
		return SendInput{}, err
	}

	// Rendered per copy, so the stored one carries masked credentials. Stateful schemes (token
	// fetch, challenges) must treat the second call as the same request.
	sentAuth, err := s.auth.Materialize(ctx, sent.auth,
		authRequest(method, rules.httpURL(sent.url), sent.headers, body))
	if err != nil {
		return SendInput{}, err
	}
	storedAuth, err := s.auth.Materialize(ctx, stored.auth,
		authRequest(method, rules.httpURL(stored.url), stored.headers, masked))
	if err != nil {
		return SendInput{}, err
	}

	// A credential that needs a challenge goes to the engine, unless the user wrote Authorization.
	digest := sentAuth.Digest
	if digest != nil && hasHeader(sent.headers, "Authorization") {
		digest = nil
	}
	if rules.metadata && (len(sentAuth.Query) != 0 || digest != nil) {
		return SendInput{}, fmt.Errorf("gRPC authorization must use request metadata")
	}

	sentURL, sentHeaders := withAuth(sent.url, sent.headers, sentAuth)
	storedURL, storedHeaders := withAuth(stored.url, stored.headers, storedAuth)

	prepared := SendInput{
		Protocol:      protocol,
		Method:        method,
		URL:           sentURL,
		Headers:       requestHeaders(draft, kind, boundary, withCookie(sentHeaders, sent.cookie)),
		Body:          body,
		BodyKind:      kind,
		MaskedURL:     storedURL,
		MaskedHeaders: requestHeaders(draft, kind, boundary, withCookie(storedHeaders, stored.cookie)),
		MaskedBody:    masked,
		Digest:        digest,
	}
	if rules.metadata {
		prepared.rpc = rpcCall{method: sent.options.RPCMethod, descriptors: sent.options.Descriptors}
	}
	if protocol == "" {
		prepared.Form = sent.form
		prepared.BodyFile = draft.BodyFile
	}
	if rules.replay != nil {
		prepared.RecordOptions = rules.replay(stored.options)
	}
	return prepared, nil
}

func authRequest(
	method, rawURL string,
	headers []domain.HeaderPair,
	body string,
) domain.AuthRequest {
	return domain.AuthRequest{Method: method, URL: rawURL, Headers: headers, Body: []byte(body)}
}

// authToApply is the draft's auth or, for "Inherit", what the levels above resolved to; inheriting
// from nothing sends no credentials. Normalized so scheme defaults apply.
func authToApply(chosen domain.Auth, inherits *domain.Auth) domain.Auth {
	if chosen.Type != domain.AuthInherit {
		return chosen.Normalized()
	}
	if inherits == nil {
		return domain.NewAuth(domain.AuthNone)
	}
	return inherits.Normalized()
}

// withAuth adds the scheme's rows unless the user wrote one of the same name. The query side edits
// the URL being sent, not the draft's rows.
func withAuth(
	rawURL string,
	headers []domain.HeaderPair,
	out domain.AuthOutput,
) (string, []domain.HeaderPair) {
	for _, pair := range out.Headers {
		if hasHeader(headers, pair.Name) {
			continue
		}
		headers = append(headers, pair)
	}
	for _, pair := range out.Query {
		if hasParam(rawURL, pair.Name) {
			continue
		}
		rawURL = appendParam(rawURL, pair)
	}
	return rawURL, headers
}

func hasHeader(headers []domain.HeaderPair, name string) bool {
	return slices.ContainsFunc(headers, func(header domain.HeaderPair) bool {
		return strings.EqualFold(header.Name, name)
	})
}

func hasParam(rawURL, name string) bool {
	return slices.ContainsFunc(paramsFromURL(rawURL), func(row domain.Row) bool {
		return row.Name == name
	})
}

// appendParam puts one more parameter on an address, before the fragment if it has one: a fragment
// is not a parameter, and a query string after it is a query string to nothing.
func appendParam(rawURL string, pair domain.HeaderPair) string {
	fragment := ""
	if hash := strings.IndexByte(rawURL, '#'); hash >= 0 {
		rawURL, fragment = rawURL[:hash], rawURL[hash:]
	}
	separator := "?"
	if strings.ContainsRune(rawURL, '?') {
		separator = "&"
	}
	return rawURL + separator + encodeQuery(pair.Name) + "=" + encodeQuery(pair.Value) + fragment
}

// withCookie is where the jar becomes the header that carries it.
func withCookie(headers []domain.HeaderPair, cookie string) []domain.HeaderPair {
	if cookie == "" {
		return headers
	}
	return append(headers, domain.HeaderPair{Name: "Cookie", Value: cookie})
}

// withContentType adds the body format's header unless the user wrote one, which is also how a type
// the kind cannot name is sent.
func withContentType(
	headers []domain.HeaderPair,
	kind domain.BodyKind,
	file string,
	boundary string,
) []domain.HeaderPair {
	value, ok := contentTypeFor(kind, file, boundary)
	if !ok {
		return headers
	}
	if hasHeader(headers, "Content-Type") {
		return headers
	}
	return append(headers, domain.HeaderPair{Name: "Content-Type", Value: value})
}

// contentTypeFor declares nothing for raw bodies, which the app does not interpret; a written header
// still wins.
func contentTypeFor(kind domain.BodyKind, file string, boundary string) (string, bool) {
	switch kind {
	case domain.BodyJSON:
		return domain.MediaJSON, true
	case domain.BodyXML:
		return domain.MediaXML, true
	case domain.BodyForm:
		// The boundary is the one the body was actually written with, which is why it is handed in
		// rather than minted here: two boundaries would be a header describing nothing.
		return mime.FormatMediaType(domain.MediaMultipart, map[string]string{"boundary": boundary}), true
	case domain.BodyBinary:
		return typeOfFile(file), true
	default:
		return "", false
	}
}

// Seed is a whole request from outside a draft (a run, a link, a command, a script's answer).
// Headers are pairs because names repeat.
type Seed struct {
	Protocol domain.Protocol         `json:"protocol,omitempty"`
	Options  *domain.ProtocolOptions `json:"options,omitempty"`
	Method   string                  `json:"method"`
	URL      string                  `json:"url"`
	Body     string                  `json:"body"`
	BodyKind domain.BodyKind         `json:"bodyKind,omitempty"`
	Form     []domain.FormRow        `json:"form,omitempty"`
	BodyFile string                  `json:"bodyFile,omitempty"`
	Headers  []domain.HeaderPair     `json:"headers,omitempty"`
	// Auth travels resolved: whoever builds a seed knows where the request came from.
	Auth *domain.Auth `json:"auth,omitempty"`
}

// Prepared fills in a draft; inherits and above are nil for a draft in no tree.
//
//wails:ignore
func (s *Drafts) Prepared(
	ctx context.Context,
	d domain.Draft,
	inherits *domain.Auth,
	above domain.VarLevels,
) (SendInput, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return SendInput{}, err
	}
	return s.prepare(ctx, workspace, d, inherits, above)
}

// Prepare fills in a run's or a followed link's request the same way as a draft.
//
//wails:ignore
func (s *Drafts) Prepare(ctx context.Context, seed Seed, above domain.VarLevels) (SendInput, error) {
	return s.Prepared(ctx, s.draftOfSeed(seed), nil, above)
}

// Mask re-renders a script-modified request for storage without resolving again or asking the jar.
//
//wails:ignore
func (s *Drafts) Mask(ctx context.Context, seed Seed) (SendInput, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return SendInput{}, err
	}
	return s.MaskIn(ctx, workspace, seed, domain.VarLevels{})
}

// MaskIn masks an already resolved request within the workspace that sent it.
//
//wails:ignore
func (s *Drafts) MaskIn(ctx context.Context, workspace string, seed Seed, above domain.VarLevels) (SendInput, error) {
	draft := s.draftOfSeed(seed)
	raw := partsOf(draft, nil, "")
	texts := raw.texts()
	hidden, err := s.vars.MaskTextsIn(ctx, workspace, above, texts)
	if err != nil {
		return SendInput{}, err
	}
	return s.assemble(ctx, draft, raw, raw.putBack(hidden))
}

// draftOfSeed is a seed seen as a draft.
func (s *Drafts) draftOfSeed(seed Seed) domain.Draft {
	auth := domain.NewAuth(domain.AuthNone)
	if seed.Auth != nil {
		auth = seed.Auth.Normalized()
	}
	return s.withRowIDs(domain.Draft{
		Protocol: seed.Protocol,
		Options:  seed.Options,
		Method:   seed.Method,
		URL:      seed.URL,
		Body:     seed.Body,
		BodyKind: domain.KindOf(seed.BodyKind),
		Form:     seed.Form,
		BodyFile: seed.BodyFile,
		Headers:  s.rowsFromHeaders(seed.Headers),
		Auth:     auth,
	})
}

// seedOf is what a command came to: the same request, with the environment left to the window.
func seedOf(req command.Request) Seed {
	return Seed{
		Method:   req.Method,
		URL:      req.URL,
		Headers:  req.Headers,
		Body:     req.Body,
		BodyKind: req.BodyKind,
		Form:     req.Form,
		BodyFile: req.BodyFile,
		Auth:     req.Auth,
	}
}

// parts are the texts of a request that variables are resolved in, gathered so all of them are
// resolved in one call and put back in place.
type parts struct {
	options domain.ProtocolOptions
	url     string
	body    string
	form    []domain.FormRow
	headers []domain.HeaderPair
	cookie  string
	auth    domain.Auth
}

// collectParts gathers enabled named headers and writes path values into the address before
// resolution; the jar's cookie beats a written Cookie row.
func collectParts(d domain.Draft, cookie string) parts {
	out := parts{
		url: domain.FillPath(d.URL, d.PathParams), body: d.Body, form: d.Form, auth: d.Auth,
		cookie: cookie, options: optionsOf(d.Options),
	}
	for _, row := range d.Headers {
		if !row.Enabled || strings.TrimSpace(row.Name) == "" {
			continue
		}
		if out.cookie != "" && strings.EqualFold(row.Name, "Cookie") {
			continue
		}
		out.headers = append(out.headers, domain.HeaderPair{Name: row.Name, Value: row.Value})
	}
	return out
}

func (p parts) texts() []string {
	keys := domain.FieldKeys(p.auth.Type)
	out := make([]string, 0, 6+3*len(p.form)+2*len(p.headers)+len(keys))
	out = append(out, p.url, p.body, p.options.Variables, p.options.OperationName, p.options.RPCMethod)
	for _, row := range p.form {
		out = append(out, row.Name, row.Value, row.Src)
	}
	for _, header := range p.headers {
		out = append(out, header.Name, header.Value)
	}
	out = append(out, p.cookie)
	for _, key := range keys {
		out = append(out, p.auth.Answer(key))
	}
	return out
}

func (p parts) putBack(resolved []string) parts {
	at := 0
	next := func() string {
		at++
		return resolved[at-1]
	}
	p.url = next()
	p.body = next()
	p.options.Variables, p.options.OperationName, p.options.RPCMethod = next(), next(), next()

	form := make([]domain.FormRow, len(p.form))
	for i := range p.form {
		form[i] = p.form[i]
		form[i].Name, form[i].Value, form[i].Src = next(), next(), next()
	}
	p.form = form

	headers := make([]domain.HeaderPair, len(p.headers))
	for i := range p.headers {
		headers[i] = domain.HeaderPair{Name: next(), Value: next()}
	}
	p.headers = headers
	p.cookie = next()

	fields := make(map[string]string, len(p.auth.Fields))
	for _, key := range domain.FieldKeys(p.auth.Type) {
		fields[key] = next()
	}
	p.auth = domain.Auth{Type: p.auth.Type, Fields: fields}
	return p
}

// jarCookies resolves the address (its host is usually a token) and reads the jar for it, as the
// preview does.
func (s *Drafts) jarCookies(ctx context.Context, d domain.Draft) (string, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return "", err
	}
	return s.cookiesIn(ctx, workspace, d)
}

func (s *Drafts) cookiesIn(ctx context.Context, workspace string, d domain.Draft) (string, error) {
	rules := protocols[d.Protocol]
	if rules.metadata {
		return "", nil
	}
	raw := collectParts(d, "").url
	if resolve, err := s.vars.Resolver(ctx, workspace, domain.VarLevels{}); err == nil {
		raw, _ = resolve(raw)
	}
	return s.jar.CookieHeader(ctx, workspace, rules.httpURL(raw))
}

// held is the state of the token the applied authorization fetches, for schemes that fetch one.
// Fields that cannot be resolved yet hold no token.
func (s *Drafts) held(ctx context.Context, d domain.Draft, above workspace.Above) *domain.AuthToken {
	scheme, ok := domain.SchemeFor(authToApply(d.Auth, above.Auth).Type)
	if !ok || !scheme.Fetches {
		return nil
	}
	token := domain.AuthToken{}
	if auth, err := s.liveAuth(ctx, d, above); err == nil {
		token = s.auth.Held(auth)
	}
	return &token
}

// projected is the rows the auth adds, shown beside the written ones; a written row of the same name
// drops it. Fields stay unresolved.
func (s *Drafts) projected(d domain.Draft, inherits *domain.Auth) ([]domain.ProjectedRow, error) {
	sent := collectParts(d, "")
	rules := protocols[d.Protocol]
	auth := authToApply(d.Auth, inherits)
	body := d.Body
	if d.Protocol != "" {
		body, _ = rules.body(sent, true)
	}
	out, err := s.auth.Project(auth, authRequest(d.Protocol.HTTPMethod(d.Method), rules.httpURL(sent.url), sent.headers, body))
	if err != nil {
		return nil, err
	}
	rows := []domain.ProjectedRow{}
	for _, pair := range out.Headers {
		if !hasHeader(sent.headers, pair.Name) {
			rows = append(rows, domain.ProjectedRow{
				Target: domain.RowHeaders, Name: pair.Name, Value: pair.Value, From: auth.Type,
			})
		}
	}
	if rules.metadata {
		return rows, nil
	}
	for _, pair := range out.Query {
		if !hasParam(d.URL, pair.Name) {
			rows = append(rows, domain.ProjectedRow{
				Target: domain.RowParams, Name: pair.Name, Value: pair.Value, From: auth.Type,
			})
		}
	}
	return rows, nil
}

// autoRows are the headers the app itself puts on the wire: the Host, its own name, the
// compression it asks for, the jar's cookies and the body's length, with secrets masked.
func (s *Drafts) autoRows(
	ctx context.Context,
	d domain.Draft,
	inherits *domain.Auth,
	above domain.VarLevels,
	cookie string,
) ([]domain.ProjectedRow, error) {
	raw := partsOf(d, inherits, cookie)
	hidden, err := s.vars.SubstituteTexts(ctx, above, raw.texts(), true)
	if err != nil {
		return nil, err
	}
	masked := raw.putBack(hidden)

	rows := []domain.ProjectedRow{}
	rules := protocols[d.Protocol]
	if rules.metadata {
		return rows, nil
	}
	for _, header := range rules.headers {
		if !hasHeader(masked.headers, header.Name) {
			rows = append(rows, domain.ProjectedRow{Target: domain.RowHeaders, Name: header.Name, Value: header.Value})
		}
	}
	add := func(name, value string) {
		rows = append(rows, domain.ProjectedRow{Target: domain.RowHeaders, Name: name, Value: value})
	}
	if parsed, err := url.Parse(s.net.CompleteURL(masked.url)); err == nil && parsed.Host != "" {
		add("Host", parsed.Host)
	}
	agent, compression := s.net.WireDefaults()
	if agent != "" {
		add("User-Agent", agent)
	}
	if compression {
		add("Accept-Encoding", "gzip")
	}
	if cookie != "" {
		add("Cookie", cookie)
	}
	if length, ok := s.sentLength(ctx, raw, above, d); ok {
		add("Content-Length", strconv.Itoa(length))
	}
	return rows, nil
}

// sentLength is the length of the body as it goes out, for bodies whose whole text is known here:
// a binary body and a form carrying files are read only by a send.
func (s *Drafts) sentLength(
	ctx context.Context,
	raw parts,
	above domain.VarLevels,
	d domain.Draft,
) (int, bool) {
	kind := domain.KindOf(d.BodyKind)
	if d.Protocol.Live() || (d.Protocol == "" && (kind == domain.BodyBinary || formCarriesFile(raw.form))) {
		return 0, false
	}
	live, err := s.vars.SubstituteTexts(ctx, above, raw.texts(), false)
	if err != nil {
		return 0, false
	}
	sent := raw.putBack(live)
	body, _, err := s.renderRequestBody(d, sent, kind, "", false)
	if err != nil || body == "" {
		return 0, false
	}
	return len(body), true
}

// renderBody is one body as the text that goes on the wire. `used` is the boundary the form wrote;
// a boundary handed in is reused, so the live and masked copies are one encoding.
func (s *Drafts) renderBody(
	kind domain.BodyKind,
	text string,
	file string,
	rows []domain.FormRow,
	boundary string,
	masked bool,
) (body string, used string, err error) {
	switch kind {
	case domain.BodyForm:
		return s.renderForm(rows, boundary, masked)
	case domain.BodyBinary:
		if masked || file == "" {
			return "", "", nil
		}
		data, err := s.files.Read(file)
		if err != nil {
			return "", "", err
		}
		return string(data), "", nil
	default:
		return text, "", nil
	}
}

// renderForm writes the enabled, named rows as multipart. A masked file part names the file and
// carries nothing: a record is a description, not the upload.
func (s *Drafts) renderForm(
	rows []domain.FormRow,
	boundary string,
	masked bool,
) (string, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if boundary != "" {
		if err := writer.SetBoundary(boundary); err != nil {
			return "", "", fmt.Errorf("form boundary: %w", err)
		}
	}
	used := writer.Boundary()

	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if !row.Enabled || name == "" {
			continue
		}
		if !row.File {
			if err := writer.WriteField(name, row.Value); err != nil {
				return "", "", fmt.Errorf("form field %q: %w", name, err)
			}
			continue
		}
		// CreatePart rather than CreateFormFile: the latter hardcodes application/octet-stream.
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", multipart.FileContentDisposition(name, filepath.Base(row.Src)))
		header.Set("Content-Type", typeOfFile(row.Src))
		part, err := writer.CreatePart(header)
		if err != nil {
			return "", "", fmt.Errorf("form part %q: %w", name, err)
		}
		if masked {
			continue
		}
		data, err := s.files.Read(row.Src)
		if err != nil {
			return "", "", err
		}
		if _, err := part.Write(data); err != nil {
			return "", "", fmt.Errorf("writing %s: %w", row.Src, err)
		}
	}
	if err := writer.Close(); err != nil {
		return "", "", fmt.Errorf("closing the form: %w", err)
	}
	return buf.String(), used, nil
}

// typeOfFile goes by the extension, as every client does; an unknown one still declares bytes.
func typeOfFile(path string) string {
	if guessed := mime.TypeByExtension(filepath.Ext(path)); guessed != "" {
		return guessed
	}
	return "application/octet-stream"
}
