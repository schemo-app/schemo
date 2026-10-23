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
)

func partsOf(draft domain.Draft, inherits *domain.Auth, cookie string) parts {
	raw := collectParts(draft, cookie)
	raw.auth = authToApply(draft.Auth, inherits)
	return raw
}

func (s *Drafts) prepare(
	ctx context.Context,
	draft domain.Draft,
	inherits *domain.Auth,
	above []domain.Variable,
	cookie string,
) (Prepared, error) {
	raw := partsOf(draft, inherits, cookie)
	texts := raw.texts()

	live, err := s.vars.SubstituteTexts(ctx, above, texts, false, draft.EnvironmentID)
	if err != nil {
		return Prepared{}, err
	}
	hidden, err := s.vars.SubstituteTexts(ctx, above, texts, true, draft.EnvironmentID)
	if err != nil {
		return Prepared{}, err
	}

	return s.assemble(ctx, draft, raw.putBack(live), raw.putBack(hidden))
}

// maskedRequest renders a request that has already been filled in (a script handed it back and the
// written-down copy is made from it). Nothing is resolved: its braces are its own text by now, and
// reading them again would eat a value that happens to carry one. Only the secrets a value carries
// are hidden; the sent side is the text as it came.
func (s *Drafts) maskedRequest(ctx context.Context, draft domain.Draft) (Prepared, error) {
	raw := partsOf(draft, nil, "")
	texts := raw.texts()

	hidden, err := s.vars.MaskTexts(ctx, nil, texts, draft.EnvironmentID)
	if err != nil {
		return Prepared{}, err
	}

	return s.assemble(ctx, draft, raw.putBack(texts), raw.putBack(hidden))
}

// assemble is the request built from its two renderings: what goes out, and the copy that is
// written down.
func (s *Drafts) assemble(
	ctx context.Context,
	draft domain.Draft,
	sent, stored parts,
) (Prepared, error) {
	kind := domain.KindOf(draft.BodyKind)

	// The body is rendered twice from one encoding. The boundary is minted by the first rendering
	// and handed to the second, because a differing boundary would make the written-down copy
	// describe a request that was never sent.
	body, boundary, err := s.renderBody(kind, sent.body, draft.BodyFile, sent.form, "", false)
	if err != nil {
		return Prepared{}, err
	}
	masked, _, err := s.renderBody(kind, stored.body, draft.BodyFile, stored.form, boundary, true)
	if err != nil {
		return Prepared{}, err
	}

	// The authorization is worked out from each rendering of the fields, so the written-down copy
	// carries masked credentials rather than the ones that went out. A scheme that holds a
	// conversation to answer (a token to fetch, a challenge to meet) must recognise the second call
	// as the same one and not hold it twice: same scheme, same answers, same request.
	sentAuth, err := s.auth.Materialize(ctx, sent.auth,
		authRequest(draft.Method, sent.url, sent.headers, body))
	if err != nil {
		return Prepared{}, err
	}
	storedAuth, err := s.auth.Materialize(ctx, stored.auth,
		authRequest(draft.Method, stored.url, stored.headers, masked))
	if err != nil {
		return Prepared{}, err
	}

	// A credential that cannot be put on the request yet goes to the engine instead, unless the
	// user wrote an Authorization row themselves: that is the more precise answer, and not one a
	// challenge should be answered over.
	digest := sentAuth.Digest
	if digest != nil && hasHeader(sent.headers, "Authorization") {
		digest = nil
	}

	sentURL, sentHeaders := withAuth(sent.url, sent.headers, sentAuth)
	storedURL, storedHeaders := withAuth(stored.url, stored.headers, storedAuth)

	return Prepared{
		Method: draft.Method,
		URL:    sentURL,
		Headers: withContentType(withCookie(sentHeaders, sent.cookie), kind, draft.BodyFile,
			boundary),
		Body:      body,
		BodyKind:  kind,
		Form:      sent.form,
		BodyFile:  draft.BodyFile,
		MaskedURL: storedURL,
		MaskedHeaders: withContentType(withCookie(storedHeaders, stored.cookie), kind, draft.BodyFile,
			boundary),
		MaskedBody: masked,
		Digest:     digest,
	}, nil
}

func authRequest(
	method, rawURL string,
	headers []domain.HeaderPair,
	body string,
) domain.AuthRequest {
	return domain.AuthRequest{Method: method, URL: rawURL, Headers: headers, Body: []byte(body)}
}

// authToApply is the authorization this request goes out with: the one the draft chose or, when it
// chose "Inherit", the one the levels above answered with, which the caller resolved. Nothing above
// a request is "None": a request that inherits from nothing sends no credentials.
//
// It is normalized here, where every reader takes it: a scheme's default answers (Bearer's prefix)
// count even when the window never wrote them down.
func authToApply(chosen domain.Auth, inherits *domain.Auth) domain.Auth {
	if chosen.Type != domain.AuthInherit {
		return chosen.Normalized()
	}
	if inherits == nil {
		return domain.NewAuth(domain.AuthNone)
	}
	return inherits.Normalized()
}

// A row of the same name the user wrote wins: an explicit row is the more precise answer, and two
// Authorization headers are a request no server reads the way the window drew it. The query side
// works on the URL being sent, since a projection into the draft's rows dies on the next keystroke.
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

// withContentType is where the format chosen in the Body popover becomes the header that declares
// it. A row of the same name the user wrote wins, as it does over the Auth chip, and also because a
// written Content-Type is the only way to send a type the kind cannot name (this app's own Accept
// is a vendor one, so that is the expected case).
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

// Raw answers with nothing on purpose: a raw body is text this app does not interpret, so it
// declares nothing about itself and the server reads it as it will. Naming a type would be guessing
// what the user meant. A written header wins, so a text/plain typed once sticks.
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

// Prepared is a request ready to go out: its variables filled in, and beside it the masked copy
// that outlives the send (the record, a preview). Only this side ever sees the first one.
type Prepared struct {
	Method  string
	URL     string
	Headers []domain.HeaderPair
	Body    string
	// The format and what the body was rendered from travel beside the text: a masked copy of a form
	// or of a file cannot be made from the text.
	BodyKind domain.BodyKind
	Form     []domain.FormRow
	BodyFile string

	MaskedURL     string
	MaskedHeaders []domain.HeaderPair
	MaskedBody    string

	// Digest is a credential the request cannot carry: it goes to the engine, the only side that is
	// there when the server says how.
	Digest *domain.DigestCredentials
}

// SendInput is the prepared request as history sends it.
func (p Prepared) SendInput() SendInput {
	return SendInput{
		Method:        p.Method,
		URL:           p.URL,
		Headers:       p.Headers,
		Body:          p.Body,
		BodyKind:      p.BodyKind,
		Form:          p.Form,
		BodyFile:      p.BodyFile,
		MaskedURL:     p.MaskedURL,
		MaskedHeaders: p.MaskedHeaders,
		MaskedBody:    p.MaskedBody,
		Digest:        p.Digest,
	}
}

// Seed is a whole request from outside a draft: a run's request, a followed link, a pasted
// command, a request a script handed back. Headers are pairs because a request can carry the same
// name twice.
type Seed struct {
	Method   string              `json:"method"`
	URL      string              `json:"url"`
	Body     string              `json:"body"`
	BodyKind domain.BodyKind     `json:"bodyKind,omitempty"`
	Form     []domain.FormRow    `json:"form,omitempty"`
	BodyFile string              `json:"bodyFile,omitempty"`
	Headers  []domain.HeaderPair `json:"headers,omitempty"`
	// Auth travels resolved: whoever builds a seed knows where the request came from.
	Auth *domain.Auth `json:"auth,omitempty"`
	// EnvironmentID is the environment this one request resolves its `{{tokens}}` in, when it is
	// not the window's own: a run sends requests saved with a pin.
	EnvironmentID string `json:"environmentId,omitempty"`
}

// Prepared fills in a draft. inherits is what its "Inherit" resolves to and above is what the
// collections around it answer for its tokens; both are nil for a draft in no tree.
//
//wails:ignore
func (s *Drafts) Prepared(
	ctx context.Context,
	d domain.Draft,
	inherits *domain.Auth,
	above []domain.Variable,
) (Prepared, error) {
	// The jar is read once for both renderings: they describe the same request.
	cookie, err := s.jarCookies(ctx, d)
	if err != nil {
		return Prepared{}, err
	}
	return s.prepare(ctx, d, inherits, above, cookie)
}

// Prepare fills in a request that is not a draft (a run's, a followed link) the same way a draft
// is, so there is one answer to what a request looks like when it leaves.
//
//wails:ignore
func (s *Drafts) Prepare(ctx context.Context, seed Seed, above []domain.Variable) (Prepared, error) {
	return s.Prepared(ctx, s.draftOfSeed(seed), nil, above)
}

// Mask re-renders a request a script handed back, for its written-down copy. It has been resolved
// once already: its braces are its own text now, so nothing is resolved again and the jar is not
// asked (the request carries the `Cookie` header it went out with).
//
//wails:ignore
func (s *Drafts) Mask(ctx context.Context, seed Seed) (Prepared, error) {
	return s.maskedRequest(ctx, s.draftOfSeed(seed))
}

// draftOfSeed is a seed seen as a draft.
func (s *Drafts) draftOfSeed(seed Seed) domain.Draft {
	auth := domain.NewAuth(domain.AuthNone)
	if seed.Auth != nil {
		auth = seed.Auth.Normalized()
	}
	return s.withRowIDs(domain.Draft{
		Method:        seed.Method,
		URL:           seed.URL,
		Body:          seed.Body,
		BodyKind:      domain.KindOf(seed.BodyKind),
		Form:          seed.Form,
		BodyFile:      seed.BodyFile,
		Headers:       s.rowsFromHeaders(seed.Headers),
		Auth:          auth,
		EnvironmentID: seed.EnvironmentID,
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
	url     string
	body    string
	form    []domain.FormRow
	headers []domain.HeaderPair
	cookie  string
	auth    domain.Auth
}

// collectParts is what goes out of a draft: enabled, named headers, and the path values written
// into the address before anything is resolved (a value may carry a `{{token}}` of its own). The
// jar's cookie wins over a written `Cookie` row.
func collectParts(d domain.Draft, cookie string) parts {
	out := parts{
		url: domain.FillPath(d.URL, d.PathParams), body: d.Body, form: d.Form, auth: d.Auth,
		cookie: cookie,
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
	out := make([]string, 0, 4+3*len(p.form)+2*len(p.headers)+len(keys))
	out = append(out, p.url, p.body)
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

// jarCookies is the cookie header this request goes out with: what the jar holds for its address.
// The address is resolved first, because its host is usually a `{{token}}`; the levels above are
// not asked, the same answer the window's preview gets.
func (s *Drafts) jarCookies(ctx context.Context, d domain.Draft) (string, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return "", err
	}
	raw := collectParts(d, "").url
	if answers, err := s.vars.SubstituteTexts(ctx, nil, []string{raw}, false,
		d.EnvironmentID); err == nil && len(answers) == 1 {
		raw = answers[0]
	}
	return s.jar.CookieHeader(ctx, workspace, raw)
}

// held is the state of the token the applied authorization fetches, for schemes that fetch one.
func (s *Drafts) held(d domain.Draft, inherits *domain.Auth) *domain.AuthToken {
	auth := authToApply(d.Auth, inherits)
	scheme, ok := domain.SchemeFor(auth.Type)
	if !ok || !scheme.Fetches {
		return nil
	}
	token := s.auth.Held(auth)
	return &token
}

// projected is what the authorization adds to the parameter and header lists, drawn beside the
// rows a person wrote; a written row of the same name drops it, as sending does. Fields stay as
// typed: nothing is resolved for a preview.
func (s *Drafts) projected(d domain.Draft, inherits *domain.Auth) ([]domain.ProjectedRow, error) {
	sent := collectParts(d, "")
	auth := authToApply(d.Auth, inherits)
	out, err := s.auth.Project(auth, authRequest(d.Method, sent.url, nil, d.Body))
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
	above []domain.Variable,
	cookie string,
) ([]domain.ProjectedRow, error) {
	raw := partsOf(d, inherits, cookie)
	hidden, err := s.vars.SubstituteTexts(ctx, above, raw.texts(), true, d.EnvironmentID)
	if err != nil {
		return nil, err
	}
	masked := raw.putBack(hidden)

	rows := []domain.ProjectedRow{}
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
	above []domain.Variable,
	d domain.Draft,
) (int, bool) {
	kind := domain.KindOf(d.BodyKind)
	if kind == domain.BodyBinary || formCarriesFile(raw.form) {
		return 0, false
	}
	live, err := s.vars.SubstituteTexts(ctx, above, raw.texts(), false, d.EnvironmentID)
	if err != nil {
		return 0, false
	}
	sent := raw.putBack(live)
	body, _, err := s.renderBody(kind, sent.body, d.BodyFile, sent.form, "", false)
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
