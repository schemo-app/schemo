package request

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"schemo/internal/domain"
	"strings"
	"testing"
)

func rowOf(
	rows []domain.ProjectedRow,
	target domain.RowKind,
	name string,
) (domain.ProjectedRow, bool) {
	for _, row := range rows {
		if row.Target == target && row.Name == name {
			return row, true
		}
	}
	return domain.ProjectedRow{}, false
}

// The window has nowhere else to learn which list a projected row belongs to or which scheme put
// it there.
func TestProjectedRowsTravelWithThePreview(t *testing.T) {
	r := newRig(t)
	r.auth.answer = domain.AuthOutput{
		Headers: []domain.HeaderPair{{Name: "Authorization", Value: "Bearer abc"}},
		Query:   []domain.HeaderPair{{Name: "X-API-Key", Value: "s3cret"}},
	}
	d := r.request(Seed{Method: "GET", URL: "https://api.example.com/a",
		Auth: &domain.Auth{Type: domain.AuthBearer}})

	preview := r.preview(t, d)
	header, ok := rowOf(preview.Projected, domain.RowHeaders, "Authorization")
	if !ok {
		t.Fatalf("projected = %+v, want the header the scheme put there", preview.Projected)
	}
	if header.Value != "Bearer abc" || header.From != domain.AuthBearer {
		t.Errorf("projected header = %+v, want what the scheme answered", header)
	}
	param, ok := rowOf(preview.Projected, domain.RowParams, "X-API-Key")
	if !ok || param.Value != "s3cret" {
		t.Errorf("projected = %+v, want the query parameter beside the header", preview.Projected)
	}
}

// A row a person wrote under that name is better, and the rule is the one sending follows: two
// Authorization headers are no request.
func TestAWrittenRowDropsTheProjection(t *testing.T) {
	r := newRig(t)
	r.auth.answer = domain.AuthOutput{
		Headers: []domain.HeaderPair{{Name: "Authorization", Value: "Bearer abc"}},
		Query:   []domain.HeaderPair{{Name: "X-API-Key", Value: "s3cret"}},
	}
	d := r.request(Seed{
		Method:  "GET",
		URL:     "https://api.example.com/a?X-API-Key=hand-written",
		Headers: []domain.HeaderPair{{Name: "authorization", Value: "Token hand-written"}},
		Auth:    &domain.Auth{Type: domain.AuthBearer},
	})

	projected := r.preview(t, d).Projected
	if _, ok := rowOf(projected, domain.RowHeaders, "Authorization"); ok {
		t.Error("the projection survived a row the user wrote themselves")
	}
	if _, ok := rowOf(projected, domain.RowParams, "X-API-Key"); ok {
		t.Error("the projection survived a parameter already in the address")
	}
}

// A row that is switched off does not go out, so it is not the better answer to anything.
func TestADisabledRowDoesNotDropTheProjection(t *testing.T) {
	r := newRig(t)
	r.auth.answer = domain.AuthOutput{
		Headers: []domain.HeaderPair{{Name: "Authorization", Value: "Bearer abc"}},
	}
	d := r.request(Seed{
		Method:  "GET",
		URL:     "https://api.example.com/a",
		Headers: []domain.HeaderPair{{Name: "Authorization", Value: "Token hand-written"}},
		Auth:    &domain.Auth{Type: domain.AuthBearer},
	})
	d.Headers[0].Enabled = false

	if _, ok := rowOf(r.preview(t, d).Projected, domain.RowHeaders, "Authorization"); !ok {
		t.Error("a switched-off row took the projection with it")
	}
}

// A `{{token}}` in a scheme not in use is not a variable of this request.
func TestAnUnusedSchemesAnswersAreNotSubstituted(t *testing.T) {
	r := newRig(t)
	auth := domain.NewAuth(domain.AuthBasic).
		With("username", "user").With("password", "pass").
		With("token", "{{nothing-resolves-this}}")
	d := r.request(Seed{Method: "GET", URL: "https://api.example.com/a", Auth: &auth})

	for _, one := range r.preview(t, d).Missing {
		if one == "nothing-resolves-this" {
			t.Error("a variable in a scheme that is not in use was reported missing")
		}
	}
}

// A tab's draft is in no tree, so "Inherit" asks the scheme with "None" rather than guessing; a
// handed-in answer projects the same way a request's own does.
func TestAnInheritedAuthorizationIsProjectedFromWhatIsHandedIn(t *testing.T) {
	r := newRig(t)
	d := r.request(Seed{Method: "GET", URL: "https://api.example.com/a",
		Auth: &domain.Auth{Type: domain.AuthInherit}})

	preview := r.preview(t, d)
	if r.auth.shown[len(r.auth.shown)-1].Type != domain.AuthNone {
		t.Errorf("the scheme was asked with %q, want 'None'", r.auth.shown[len(r.auth.shown)-1].Type)
	}
	if len(preview.Projected) != 0 || preview.Inherited != nil {
		t.Errorf("preview = %+v, want nothing inherited", preview)
	}

	r.auth.answer = domain.AuthOutput{
		Headers: []domain.HeaderPair{{Name: "Authorization", Value: "Bearer from-above"}},
	}
	inherited := domain.NewAuth(domain.AuthBearer).With("token", "from-above")
	projected, err := r.projected(d, &inherited)
	if err != nil {
		t.Fatalf("projected: %v", err)
	}
	row, ok := rowOf(projected, domain.RowHeaders, "Authorization")
	if !ok || row.Value != "Bearer from-above" {
		t.Errorf("projected = %+v, want the row the inherited answer comes to", projected)
	}
	if got := r.auth.shown[len(r.auth.shown)-1].Answer("token"); got != "from-above" {
		t.Errorf("the scheme was asked with %q, want what the level above answered", got)
	}
}

// The rows the app adds by itself, in the order the wire carries them: the address's host, the name
// the app calls itself, the compression it asks for, the jar's cookies, and the body's length.
func TestAutoIsTheRowsTheAppAdds(t *testing.T) {
	r := newRig(t)
	r.jar.header = "session=abc"
	r.net = fakeNet{agent: "Schemo/2.4", gzip: true}

	draft := domain.Draft{
		ID: "cmd", Method: "POST", URL: "https://api.example.com/users",
		Body: `{"name":"ok"}`, BodyKind: domain.BodyJSON,
	}
	rows, err := r.autoRows(context.Background(), draft, nil, nil, r.jar.header)
	if err != nil {
		t.Fatalf("autoRows: %v", err)
	}

	want := []domain.ProjectedRow{
		{Target: domain.RowHeaders, Name: "Host", Value: "api.example.com"},
		{Target: domain.RowHeaders, Name: "User-Agent", Value: "Schemo/2.4"},
		{Target: domain.RowHeaders, Name: "Accept-Encoding", Value: "gzip"},
		{Target: domain.RowHeaders, Name: "Cookie", Value: "session=abc"},
		{Target: domain.RowHeaders, Name: "Content-Length", Value: "13"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v, want %+v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
}

// A body only a send can read — a file, or a form carrying one — is left without a length rather
// than given a guess, and nothing is opened to find out.
func TestAutoLeavesOutTheBodyOnlyASendCanRead(t *testing.T) {
	r := newRig(t)
	r.net = fakeNet{}

	draft := domain.Draft{
		ID: "cmd", Method: "POST", URL: "/upload", BodyKind: domain.BodyBinary,
		BodyFile: "/tmp/logo.png",
	}
	rows, err := r.autoRows(context.Background(), draft, nil, nil, r.jar.header)
	if err != nil {
		t.Fatalf("autoRows: %v", err)
	}
	for _, row := range rows {
		if row.Name == "Content-Length" {
			t.Errorf("rows = %+v, want no length for a body only a send can read", rows)
		}
	}
	if r.files.read != 0 {
		t.Errorf("files read = %d, want nothing opened for a preview", r.files.read)
	}
}

// A form without a file is text the way any other body is, and its length is counted as a send
// renders it.
func TestAutoCountsAFormBody(t *testing.T) {
	r := newRig(t)
	r.net = fakeNet{}

	draft := domain.Draft{
		ID: "cmd", Method: "POST", URL: "https://api.example.com/form", BodyKind: domain.BodyForm,
		Form: []domain.FormRow{{Name: "a", Value: "1", Enabled: true}},
	}
	rows, err := r.autoRows(context.Background(), draft, nil, nil, r.jar.header)
	if err != nil {
		t.Fatalf("autoRows: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.Name == "Content-Length" && row.Value != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("rows = %+v, want a length for a form without files", rows)
	}
}

// The address is drawn as it goes out, with a secret resolved to the mask rather than to its value:
// the row says the host is answered by a variable without saying what the variable holds.
func TestAutoMasksWhatTheAddressCarries(t *testing.T) {
	r := newRig(t)
	r.net = fakeNet{}

	draft := domain.Draft{
		ID: "cmd", Method: "GET", URL: "https://{{token}}.example.com/x",
	}
	rows, err := r.autoRows(context.Background(), draft, nil, nil, r.jar.header)
	if err != nil {
		t.Fatalf("autoRows: %v", err)
	}
	for _, row := range rows {
		if strings.Contains(row.Value, "abc123") {
			t.Fatalf("rows = %+v, want the secret not to travel in a value", rows)
		}
		if row.Name == "Host" && row.Value != fakeMask+".example.com" {
			t.Errorf("host = %q, want the masked name", row.Value)
		}
	}
}

// The case every body that is not text needs: a file system behind the command line.
func loadedWithFiles(t *testing.T) (*line, *fakeFiles) {
	t.Helper()
	l := newLine(t)
	return l, l.files
}

// Case-insensitive: two Content-Type rows written differently must not count as two headers.
func valueOf(headers []domain.HeaderPair, name string) (string, bool) {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value, true
		}
	}
	return "", false
}

// What the Body popover does when a segment is clicked.
func replaced(t *testing.T, drafts *line, kind domain.BodyKind, text string) {
	t.Helper()
	drafts.seed(Seed{Method: "POST", URL: "https://api.example.com/a", Body: text})
	drafts.d.BodyKind = kind
}

func prepared(t *testing.T, drafts *line) Prepared {
	t.Helper()
	return drafts.prepared(t, drafts.d)
}

// The format chosen in the popover is what the request declares itself as.
func TestTheKindBecomesAContentType(t *testing.T) {
	ctx := context.Background()
	for _, want := range []struct {
		kind  domain.BodyKind
		value string
	}{
		{domain.BodyJSON, "application/json"},
		{domain.BodyXML, "application/xml"},
	} {
		drafts := newLine(t)
		replaced(t, drafts, want.kind, "<a/>")
		value, ok := renderedHeader(prepared(t, drafts).Headers, "Content-Type")
		if !ok || value != want.value {
			t.Errorf("Content-Type for %s = %q (ok=%v), want %q", want.kind, value, ok, want.value)
		}
	}

	// Raw declares nothing, and that is the point of it: a body this app does not interpret says
	// nothing about itself, and naming a type here would be the window guessing at what it is.
	drafts := newLine(t)
	replaced(t, drafts, domain.BodyRaw, "plain")
	if value, ok := renderedHeader(prepared(t, drafts).Headers, "Content-Type"); ok {
		t.Errorf("Content-Type for raw = %q, want none at all", value)
	}
	_ = ctx
}

// What opening a saved form request does.
func renderedForm(t *testing.T, drafts *line, rows []domain.FormRow) {
	t.Helper()
	drafts.seed(Seed{Method: "POST", URL: "https://api.example.com/a", BodyKind: domain.BodyForm, Form: rows})
}

// A header the user wrote is the more precise answer, and it is the only way to send a type the
// kind cannot name.
func TestAWrittenContentTypeWins(t *testing.T) {
	drafts := newLine(t)
	replaced(t, drafts, domain.BodyJSON, "{}")
	withHeader(t, drafts, "content-type", "application/vnd.api+json")

	out := prepared(t, drafts)
	if value, _ := valueOf(out.Headers, "Content-Type"); value != "application/vnd.api+json" {
		t.Errorf("Content-Type = %q, want the one the user wrote", value)
	}
	if countHeaders(out.Headers, "Content-Type") != 1 {
		t.Errorf("headers = %+v, want one Content-Type and not two", out.Headers)
	}
}

// The user's own header is a text like any other, and it is resolved and masked on both renderings.
func TestAWrittenContentTypeIsResolvedAndMasked(t *testing.T) {
	drafts := newLine(t)
	replaced(t, drafts, domain.BodyRaw, "plain")
	withHeader(t, drafts, "Content-Type", "application/{{token}}")

	out := prepared(t, drafts)
	if value, _ := renderedHeader(out.Headers, "Content-Type"); value != "application/abc123" {
		t.Errorf("Content-Type = %q, want the token resolved", value)
	}
	if value, _ := renderedHeader(out.MaskedHeaders, "Content-Type"); value != "application/••••" {
		t.Errorf("masked Content-Type = %q, want the mask", value)
	}
}

// What the Headers chip does when a header is added.
func withHeader(t *testing.T, drafts *line, name, value string) {
	t.Helper()
	drafts.header(name, value)
}

// A form goes out as multipart, and the reader that takes it apart is the standard library's — not
// the writer that built it, which could agree with itself about anything.
func TestAFormBodyGoesOutAsMultipart(t *testing.T) {
	drafts, files := loadedWithFiles(t)
	renderedForm(t, drafts, []domain.FormRow{
		{Name: "title", Value: "Coffee grinder", Enabled: true},
		{Name: "off", Value: "no", Enabled: false},
		{Name: "photo", Src: "/tmp/logo.png", File: true, Enabled: true},
	})

	out := prepared(t, drafts)
	value, ok := renderedHeader(out.Headers, "Content-Type")
	if !ok || !strings.HasPrefix(value, "multipart/form-data; boundary=") {
		t.Fatalf("Content-Type = %q, want a multipart one", value)
	}

	reader := multipart.NewReader(strings.NewReader(out.Body), boundaryOf(t, value))
	seen := map[string]string{}
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("reading part %q: %v", part.FormName(), err)
		}
		seen[part.FormName()] = string(body)
	}
	if seen["title"] != "Coffee grinder" {
		t.Errorf("parts = %+v, want the text row", seen)
	}
	if _, present := seen["off"]; present {
		t.Error("a switched-off row went out")
	}
	if seen["photo"] != "PNGDATA" {
		t.Errorf("file part = %q, want the file's bytes", seen["photo"])
	}
	if files.read == 0 {
		t.Error("the file was never read")
	}
}

// One encoding, two renderings: the boundary the header names is the delimiter in both the body
// that goes out and the one that is written down. Two writers minting their own would break this.
func TestTheLiveAndMaskedFormsShareOneBoundary(t *testing.T) {
	drafts, _ := loadedWithFiles(t)
	renderedForm(t, drafts, []domain.FormRow{{Name: "token", Value: "{{token}}", Enabled: true}})

	out := prepared(t, drafts)
	value, _ := renderedHeader(out.Headers, "Content-Type")
	boundary := boundaryOf(t, value)
	if boundary == "" {
		t.Fatal("the header names no boundary")
	}
	if !strings.Contains(out.Body, "--"+boundary) {
		t.Error("the sent body is not delimited by the boundary the header names")
	}
	if !strings.Contains(out.MaskedBody, "--"+boundary) {
		t.Error("the written body is not delimited by the boundary the header names")
	}
	if !strings.Contains(out.Body, "abc123") {
		t.Error("the sent form did not resolve the token")
	}
	if !strings.Contains(out.MaskedBody, "••••") {
		t.Error("the written form did not mask the token")
	}
	// The masked copy is written from the same header, so it names the same boundary — the extension
	// is not a secret, and the record describes the request that went out.
	if masked, _ := renderedHeader(out.MaskedHeaders, "Content-Type"); masked != value {
		t.Errorf("masked Content-Type = %q, want %q", masked, value)
	}
}

// A record describes the upload rather than carrying a second copy of it.
func TestAFileFieldKeepsItsNameAndLosesItsBytes(t *testing.T) {
	drafts, files := loadedWithFiles(t)
	renderedForm(t, drafts, []domain.FormRow{{Name: "photo", Src: "/tmp/logo.png", File: true, Enabled: true}})

	before := files.read
	out := prepared(t, drafts)
	if !strings.Contains(out.MaskedBody, "logo.png") {
		t.Error("the written body does not name the file")
	}
	if strings.Contains(out.MaskedBody, "PNGDATA") {
		t.Error("the written body carries the file's bytes")
	}
	// Exactly one read for the whole rendering: the live copy opens the file, and the masked copy
	// runs after it and opens nothing — the bytes are not wanted, and a file that vanished between
	// the two must not fail the second one.
	if files.read != before+1 {
		t.Errorf("reads = %d, want exactly one more than %d", files.read, before)
	}
}

// A file that is gone at the moment of sending is a send that does not happen.
func TestAMissingFileFailsTheSend(t *testing.T) {
	drafts, _ := loadedWithFiles(t)
	replaced(t, drafts, domain.BodyBinary, "")
	ctx := context.Background()
	drafts.d.BodyFile = "/tmp/gone.bin"
	if _, err := drafts.Prepared(ctx, drafts.d, nil, nil); !errors.Is(err,
		domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// An extension nothing knows still declares the bytes rather than nothing at all. The assertion on
// .json is safe on every machine because it is in Go's own table; a type Windows reads out of the
// registry is not.
func TestAnUnknownExtensionGoesOutAsOctets(t *testing.T) {
	drafts, _ := loadedWithFiles(t)

	if got := typeOfFile("/tmp/x.zzz"); got != "application/octet-stream" {
		t.Errorf("an unknown extension = %q, want octets", got)
	}
	if got := typeOfFile("/tmp/x.json"); got != "application/json" {
		t.Errorf(".json = %q, want application/json", got)
	}
	_ = drafts
}

// A binary body goes out byte for byte, under the type its extension names.
func TestABinaryBodyIsTheFile(t *testing.T) {
	drafts, _ := loadedWithFiles(t)
	replaced(t, drafts, domain.BodyBinary, "")
	drafts.d.BodyFile = "/tmp/logo.png"
	out := prepared(t, drafts)
	if out.Body != "PNGDATA" {
		t.Errorf("body = %q, want the file's bytes", out.Body)
	}
	if _, ok := renderedHeader(out.Headers, "Content-Type"); !ok {
		t.Error("a binary body declared no type at all")
	}
	if out.MaskedBody != "" {
		t.Errorf("masked body = %q, want no bytes written down", out.MaskedBody)
	}
}

// A seed carries its format, which is how a followed link or a pasted command keeps what it
// declared.
func TestASeedCarriesItsKind(t *testing.T) {
	ctx := context.Background()
	drafts := newLine(t)

	out, err := drafts.Prepare(ctx, Seed{
		Method: "POST", URL: "https://api.example.com/a", Body: "{}", BodyKind: domain.BodyJSON,
	}, nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if value, _ := renderedHeader(out.Headers, "Content-Type"); value != "application/json" {
		t.Errorf("Content-Type = %q, want the seed's kind to decide it", value)
	}

	plain, err := drafts.Prepare(ctx,
		Seed{Method: "POST", URL: "https://api.example.com/a", Body: "{}"}, nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, ok := renderedHeader(plain.Headers, "Content-Type"); ok {
		t.Error("a seed that says nothing about its format declared one anyway")
	}
}

func countHeaders(headers []domain.HeaderPair, name string) int {
	count := 0
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			count++
		}
	}
	return count
}

func boundaryOf(t *testing.T, contentType string) string {
	t.Helper()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("Content-Type %q: %v", contentType, err)
	}
	return params["boundary"]
}

// authWith sets the command line's auth, which is what the chip does when it is clicked.
func authWith(t *testing.T, drafts *line, auth domain.Auth) {
	t.Helper()
	drafts.setAuth(t, auth)
}

func renderedHeader(headers []domain.HeaderPair, name string) (string, bool) {
	for _, header := range headers {
		if header.Name == name {
			return header.Value, true
		}
	}
	return "", false
}

// The answers in the Auth chip are texts like any other: a variable in one of them is filled in on
// the way out and left as a mask in the copy that is written down, and what the scheme makes of
// them is what the request goes out with.
func TestTheAuthFieldsReachTheSchemeResolved(t *testing.T) {
	ctx := context.Background()
	drafts := newLine(t)
	auth := drafts.auth

	drafts.seed(Seed{
		Method: "GET",
		URL:    "https://api.example.com/a",
	})

	auth.answer = domain.AuthOutput{
		Headers: []domain.HeaderPair{{Name: "Authorization", Value: "given"}},
	}
	authWith(t, drafts, domain.NewAuth(domain.AuthBearer).With("token", "{{token}}"))

	prepared, err := drafts.Prepared(ctx, drafts.d, nil, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	if got := auth.asked[0].Answer("token"); got != "abc123" {
		t.Errorf("the scheme was asked with %q, want the resolved variable", got)
	}
	// The record outlives the send, so the secret it carries is the mask the environment gave.
	if got := auth.asked[1].Answer("token"); got != "••••" {
		t.Errorf("the second pass asked with %q, want the mask rather than the value", got)
	}
	if value, ok := renderedHeader(prepared.Headers, "Authorization"); !ok || value != "given" {
		t.Errorf("headers = %+v, want what the scheme answered in them", prepared.Headers)
	}
	if value, _ := renderedHeader(prepared.MaskedHeaders, "Authorization"); value != "given" {
		t.Errorf("masked headers = %+v, want the masked answer in them", prepared.MaskedHeaders)
	}
}

// A header the user wrote by hand is the more precise answer, and two Authorization headers are a
// request no server reads the way the window drew it.
func TestAWrittenAuthorizationHeaderWins(t *testing.T) {
	ctx := context.Background()
	drafts := newLine(t)
	auth := drafts.auth
	auth.answer = domain.AuthOutput{
		Headers: []domain.HeaderPair{{Name: "Authorization", Value: "from-the-chip"}},
	}

	drafts.seed(Seed{
		Method:  "GET",
		URL:     "https://api.example.com/a",
		Headers: []domain.HeaderPair{{Name: "authorization", Value: "Token hand-written"}},
	})
	authWith(t, drafts, domain.NewAuth(domain.AuthBearer).With("token", "abc123"))

	prepared, err := drafts.Prepared(ctx, drafts.d, nil, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	if len(prepared.Headers) != 1 {
		t.Fatalf("headers = %+v, want the one row and nothing beside it", prepared.Headers)
	}
	if prepared.Headers[0].Value != "Token hand-written" {
		t.Errorf("authorization = %q, want what was written", prepared.Headers[0].Value)
	}
}

// A scheme told to travel in the query string puts its parameter on the address being sent — and
// not among the draft's rows, which are what the window edits and what the next keystroke in the
// address bar would rewrite out of it.
func TestAQueryParameterGoesOnTheAddressAndNotInTheRows(t *testing.T) {
	ctx := context.Background()
	drafts := newLine(t)
	auth := drafts.auth
	auth.answer = domain.AuthOutput{Query: []domain.HeaderPair{{Name: "X-API-Key", Value: "s3cret"}}}

	drafts.seed(Seed{
		Method: "GET",
		URL:    "https://api.example.com/a?include=author",
	})
	authWith(t, drafts, domain.NewAuth(domain.AuthAPIKey).With("key", "X-API-Key"))

	prepared, err := drafts.Prepared(ctx, drafts.d, nil, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	want := "https://api.example.com/a?include=author&X-API-Key=s3cret"
	if prepared.URL != want {
		t.Errorf("url = %q, want %q", prepared.URL, want)
	}

	if drafts.d.URL != "https://api.example.com/a?include=author" {
		t.Errorf("the draft's address = %q, want it untouched by the projection", drafts.d.URL)
	}
}

// A parameter the user already wrote under that name is the more precise answer, for the reason a
// written header is.
func TestAWrittenParameterWins(t *testing.T) {
	ctx := context.Background()
	drafts := newLine(t)
	auth := drafts.auth
	auth.answer = domain.AuthOutput{
		Query: []domain.HeaderPair{{Name: "X-API-Key", Value: "from-the-chip"}},
	}

	drafts.seed(Seed{
		Method: "GET",
		URL:    "https://api.example.com/a?X-API-Key=hand-written",
	})
	authWith(t, drafts, domain.NewAuth(domain.AuthAPIKey).With("key", "X-API-Key"))

	prepared, err := drafts.Prepared(ctx, drafts.d, nil, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	if prepared.URL != "https://api.example.com/a?X-API-Key=hand-written" {
		t.Errorf("url = %q, want the parameter the user wrote and only that one", prepared.URL)
	}
}

// A credential that cannot be put on the request yet travels to the engine beside it rather than on
// it: nothing above the engine has a header to show for a Digest challenge that has not arrived.
func TestACredentialTheRequestCannotCarryGoesToTheEngine(t *testing.T) {
	ctx := context.Background()
	line := newLine(t)
	line.auth.answer = domain.AuthOutput{
		Digest: &domain.DigestCredentials{Username: "user", Password: "pass"},
	}

	prepared, err := line.Prepared(ctx, line.d, nil, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	if prepared.Digest == nil || prepared.Digest.Username != "user" {
		t.Fatalf("digest = %+v, want the credential handed on", prepared.Digest)
	}
	if _, ok := renderedHeader(prepared.Headers, "Authorization"); ok {
		t.Errorf("headers = %+v, want no authorization: there is nothing to put there", prepared.Headers)
	}
}

// A row the user wrote under that name is the more precise answer, and a challenge answered over it
// would be an answer to a request nobody asked about.
func TestAWrittenAuthorizationHeaderTakesTheChallengeAway(t *testing.T) {
	ctx := context.Background()
	line := newLine(t)
	line.auth.answer = domain.AuthOutput{
		Digest: &domain.DigestCredentials{Username: "user", Password: "pass"},
	}
	line.seed(Seed{
		Method:  "GET",
		URL:     "https://api.example.com/a",
		Headers: []domain.HeaderPair{{Name: "Authorization", Value: "Token hand-written"}},
	})

	prepared, err := line.Prepared(ctx, line.d, nil, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	if prepared.Digest != nil {
		t.Errorf("digest = %+v, want none while a written header stands", prepared.Digest)
	}
}

// "Inherit" is answered by whoever knows the tree: the draft is told what the levels above it
// said, and a request with nothing above it sends no credentials at all.
func TestInheritedAuthIsWhatTheCallerResolved(t *testing.T) {
	ctx := context.Background()
	drafts := newLine(t)
	auth := drafts.auth
	auth.answer = domain.AuthOutput{
		Headers: []domain.HeaderPair{{Name: "Authorization", Value: "from-the-collection"}},
	}

	drafts.seed(Seed{
		Method: "GET",
		URL:    "https://api.example.com/a",
	})
	authWith(t, drafts, domain.NewAuth(domain.AuthInherit))

	inherited := domain.NewAuth(domain.AuthBearer).With("token", "from-the-collection")
	prepared, err := drafts.Prepared(ctx, drafts.d, &inherited, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	if got := auth.asked[0].Answer("token"); got != "from-the-collection" {
		t.Errorf("the scheme was asked with %q, want what the level above answered", got)
	}
	if value, ok := renderedHeader(prepared.Headers,
		"Authorization"); !ok || value != "from-the-collection" {
		t.Errorf("headers = %+v, want the inherited authorization in them", prepared.Headers)
	}

	// Nothing above is not "None" written down somewhere: the request simply goes out with nothing.
	auth.answer = domain.AuthOutput{}
	prepared, err = drafts.Prepared(ctx, drafts.d, nil, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	if auth.last().Type != domain.AuthNone {
		t.Errorf("auth = %q, want 'no' when there is nothing above to inherit", auth.last().Type)
	}
	if _, ok := renderedHeader(prepared.Headers, "Authorization"); ok {
		t.Errorf("headers = %+v, want no authorization when there is nothing above to inherit",
			prepared.Headers)
	}
}

// A run sends saved requests, and a request that names a variable nothing answers goes out with the
// name left empty: the window is what marks the names — in its own language, and about the row they
// stand in — and a `{{token}}` carried to a server is a path of braces.
func TestPrepareSendsWithANameNothingAnswersEmpty(t *testing.T) {
	ctx := context.Background()
	drafts := newLine(t)

	prepared, err := drafts.Prepare(ctx, Seed{
		Method: "GET",
		URL:    "https://{{host}}/{{var3}}/{{other}}",
	}, nil)
	if err != nil {
		t.Fatalf("Prepare = %v, want the request to go out", err)
	}
	if prepared.URL != "https://api.example.com//" {
		t.Errorf("url = %q, want the unanswered names empty and the known one filled in",
			prepared.URL)
	}
}

// A token a level above the request answers for is filled in from that level: a collection's own
// variable is what the run carries down to every request inside it.
func TestPrepareAsksWithTheNamesTheLevelsAboveAnswer(t *testing.T) {
	ctx := context.Background()
	drafts := newLine(t)

	above := []domain.Variable{
		{Name: "var3", Value: "three", Kind: domain.VariableText, Enabled: true},
	}
	seed := Seed{Method: "GET", URL: "https://{{host}}/{{var3}}"}
	prepared, err := drafts.Prepare(ctx, seed, above)
	if err != nil {
		t.Fatalf("Prepare = %v, want a request the collection answers for to go out", err)
	}
	if prepared.URL != "https://api.example.com/three" {
		t.Errorf("url = %q, want the level above filling its own name in", prepared.URL)
	}
}

// A request a script rewrote has already been filled in once: its braces are its own text by then,
// and this second rendering — the copy the record keeps — must not refuse a request that has
// already left over text one of its own values happened to carry.
func TestMaskKeepsTheBracesOfASubstitutedRequest(t *testing.T) {
	ctx := context.Background()
	drafts := newLine(t)

	prepared, err := drafts.Mask(ctx, Seed{Method: "GET", URL: "https://api.example.com/{{var3}}/a"})
	if err != nil {
		t.Fatalf("Mask: %v", err)
	}
	if prepared.MaskedURL != "https://api.example.com/{{var3}}/a" {
		t.Errorf("masked url = %q, want the text as it was handed over", prepared.MaskedURL)
	}
}
