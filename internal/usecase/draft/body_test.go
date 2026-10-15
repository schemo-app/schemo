package draft

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"

	"strings"
	"testing"

	"schemo/internal/domain"
)

// The case every body that is not text needs: a file system behind the command line.
func loadedWithFiles(t *testing.T) (*UseCase, *fakeFiles) {
	t.Helper()
	uc, _, files := newUseCaseWithFiles()
	if err := uc.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return uc, files
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
func replaced(t *testing.T, uc *UseCase, kind domain.BodyKind, text string) {
	t.Helper()
	ctx := context.Background()
	if _, err := uc.Replace(ctx, domain.DraftCommandLine, Seed{
		Method: "POST", URL: "https://api.example.com/a", Body: text,
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if _, err := uc.SetBodyKind(ctx, domain.DraftCommandLine, kind); err != nil {
		t.Fatalf("SetBodyKind: %v", err)
	}
}

func prepared(t *testing.T, uc *UseCase) Prepared {
	t.Helper()
	out, err := uc.Prepared(context.Background(), domain.DraftCommandLine, nil, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	return out
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
		uc, _ := loaded(t)
		replaced(t, uc, want.kind, "<a/>")
		value, ok := headerOf(prepared(t, uc).Headers, "Content-Type")
		if !ok || value != want.value {
			t.Errorf("Content-Type for %s = %q (ok=%v), want %q", want.kind, value, ok, want.value)
		}
	}

	// Raw declares nothing, and that is the point of it: a body this app does not interpret says
	// nothing about itself, and naming a type here would be the window guessing at what it is.
	uc, _ := loaded(t)
	replaced(t, uc, domain.BodyRaw, "plain")
	if value, ok := headerOf(prepared(t, uc).Headers, "Content-Type"); ok {
		t.Errorf("Content-Type for raw = %q, want none at all", value)
	}
	_ = ctx
}

// What opening a saved form request does.
func formOf(t *testing.T, uc *UseCase, rows []domain.FormRow) {
	t.Helper()
	if _, err := uc.Replace(context.Background(), domain.DraftCommandLine, Seed{
		Method: "POST", URL: "https://api.example.com/a", BodyKind: domain.BodyForm, Form: rows,
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
}

// A header the user wrote is the more precise answer, and it is the only way to send a type the
// kind cannot name.
func TestAWrittenContentTypeWins(t *testing.T) {
	uc, _ := loaded(t)
	replaced(t, uc, domain.BodyJSON, "{}")
	withHeader(t, uc, "content-type", "application/vnd.api+json")

	out := prepared(t, uc)
	if value, _ := valueOf(out.Headers, "Content-Type"); value != "application/vnd.api+json" {
		t.Errorf("Content-Type = %q, want the one the user wrote", value)
	}
	if countHeaders(out.Headers, "Content-Type") != 1 {
		t.Errorf("headers = %+v, want one Content-Type and not two", out.Headers)
	}
}

// The user's own header is a text like any other, and it is resolved and masked on both renderings.
func TestAWrittenContentTypeIsResolvedAndMasked(t *testing.T) {
	uc, _ := loaded(t)
	replaced(t, uc, domain.BodyRaw, "plain")
	withHeader(t, uc, "Content-Type", "application/{{token}}")

	out := prepared(t, uc)
	if value, _ := headerOf(out.Headers, "Content-Type"); value != "application/abc123" {
		t.Errorf("Content-Type = %q, want the token resolved", value)
	}
	if value, _ := headerOf(out.MaskedHeaders, "Content-Type"); value != "application/••••" {
		t.Errorf("masked Content-Type = %q, want the mask", value)
	}
}

// What the Headers chip does when a header is added.
func withHeader(t *testing.T, uc *UseCase, name, value string) {
	t.Helper()
	ctx := context.Background()
	state, err := uc.AddRow(ctx, domain.DraftCommandLine, domain.RowHeaders)
	if err != nil {
		t.Fatalf("AddRow: %v", err)
	}
	id := state.Draft.Headers[len(state.Draft.Headers)-1].ID
	if _, err := uc.PatchRow(ctx, domain.DraftCommandLine, domain.RowHeaders, id, RowPatch{
		Name: strPtr(name), Value: strPtr(value),
	}); err != nil {
		t.Fatalf("PatchRow: %v", err)
	}
}

// A form goes out as multipart, and the reader that takes it apart is the standard library's — not
// the writer that built it, which could agree with itself about anything.
func TestAFormBodyGoesOutAsMultipart(t *testing.T) {
	uc, files := loadedWithFiles(t)
	formOf(t, uc, []domain.FormRow{
		{Name: "title", Value: "Coffee grinder", Enabled: true},
		{Name: "off", Value: "no", Enabled: false},
		{Name: "photo", Src: "/tmp/logo.png", File: true, Enabled: true},
	})

	out := prepared(t, uc)
	value, ok := headerOf(out.Headers, "Content-Type")
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
	uc, _ := loadedWithFiles(t)
	formOf(t, uc, []domain.FormRow{{Name: "token", Value: "{{token}}", Enabled: true}})

	out := prepared(t, uc)
	value, _ := headerOf(out.Headers, "Content-Type")
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
	if masked, _ := headerOf(out.MaskedHeaders, "Content-Type"); masked != value {
		t.Errorf("masked Content-Type = %q, want %q", masked, value)
	}
}

// A record describes the upload rather than carrying a second copy of it.
func TestAFileFieldKeepsItsNameAndLosesItsBytes(t *testing.T) {
	uc, files := loadedWithFiles(t)
	formOf(t, uc, []domain.FormRow{{Name: "photo", Src: "/tmp/logo.png", File: true, Enabled: true}})

	before := files.read
	out := prepared(t, uc)
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
	uc, _ := loadedWithFiles(t)
	replaced(t, uc, domain.BodyBinary, "")
	ctx := context.Background()
	if _, err := uc.SetBodyFile(ctx, domain.DraftCommandLine, "/tmp/gone.bin"); err != nil {
		t.Fatalf("SetBodyFile: %v", err)
	}
	if _, err := uc.Prepared(ctx, domain.DraftCommandLine, nil, nil); !errors.Is(err,
		domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// An extension nothing knows still declares the bytes rather than nothing at all. The assertion on
// .json is safe on every machine because it is in Go's own table; a type Windows reads out of the
// registry is not.
func TestAnUnknownExtensionGoesOutAsOctets(t *testing.T) {
	uc, _ := loadedWithFiles(t)

	if got := typeOfFile("/tmp/x.zzz"); got != "application/octet-stream" {
		t.Errorf("an unknown extension = %q, want octets", got)
	}
	if got := typeOfFile("/tmp/x.json"); got != "application/json" {
		t.Errorf(".json = %q, want application/json", got)
	}
	_ = uc
}

// A binary body goes out byte for byte, under the type its extension names.
func TestABinaryBodyIsTheFile(t *testing.T) {
	uc, _ := loadedWithFiles(t)
	replaced(t, uc, domain.BodyBinary, "")
	ctx := context.Background()
	if _, err := uc.SetBodyFile(ctx, domain.DraftCommandLine, "/tmp/logo.png"); err != nil {
		t.Fatalf("SetBodyFile: %v", err)
	}
	out := prepared(t, uc)
	if out.Body != "PNGDATA" {
		t.Errorf("body = %q, want the file's bytes", out.Body)
	}
	if _, ok := headerOf(out.Headers, "Content-Type"); !ok {
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
	uc, _ := loaded(t)

	out, err := uc.Prepare(ctx, Seed{
		Method: "POST", URL: "https://api.example.com/a", Body: "{}", BodyKind: domain.BodyJSON,
	}, nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if value, _ := headerOf(out.Headers, "Content-Type"); value != "application/json" {
		t.Errorf("Content-Type = %q, want the seed's kind to decide it", value)
	}

	plain, err := uc.Prepare(ctx,
		Seed{Method: "POST", URL: "https://api.example.com/a", Body: "{}"}, nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, ok := headerOf(plain.Headers, "Content-Type"); ok {
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
