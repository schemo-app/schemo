package request

import (
	"errors"
	"strings"
	"testing"

	"schemo/internal/convert/command"
	"schemo/internal/domain"
)

// copyLine is the copy the menu asks for: the command line's draft, in one notation, with nothing
// above it — the shape a request composed on the line has.
func copyLine(t *testing.T, drafts *line, format command.Format) Copy {
	t.Helper()
	out, err := drafts.copyAs(nil, nil, format)
	if err != nil {
		t.Fatalf("copyAs: %v", err)
	}
	return out
}

// A command is the one place this app hands a secret's value over, so
// a copy carries the values themselves and never the masks a record is written with.
func TestACopyCarriesTheValuesRatherThanTheMasks(t *testing.T) {
	drafts := newLine(t)
	drafts.seed(Seed{Method: "GET", URL: "https://api.example.com/a?t={{token}}"})

	out := copyLine(t, drafts, command.FormatURL)
	if out.Text != "https://api.example.com/a?t=abc123" {
		t.Errorf("copy = %q, want the value rather than the mask", out.Text)
	}
	if len(out.Omitted) != 0 {
		t.Errorf("omitted = %+v, want nothing left out", out.Omitted)
	}
}

// A send may fetch the credential its authorization carries, and a copy must not: nobody who asked
// for a command asked to be signed in. A token already held still lands on the request.
func TestACopyOfAFetchingSchemeAsksTheNetworkForNothing(t *testing.T) {
	line := newLine(t)
	line.auth.token = domain.AuthToken{Held: true}
	line.auth.answer = domain.AuthOutput{
		Headers: []domain.HeaderPair{{Name: "Authorization", Value: "Bearer held"}},
	}
	authWith(t, line, domain.NewAuth(domain.AuthOAuth2))

	// Every edit answers with the rows the window draws, which is a projection of its own: what the
	// copy adds is the one beyond that.
	before := len(line.auth.shown)
	out := copyLine(t, line, command.FormatCurl)
	if len(line.auth.asked) != 0 {
		t.Errorf("a copy materialized %v, want no conversation while nothing is being sent",
			line.auth.asked)
	}
	if len(line.auth.shown) != before+1 {
		t.Errorf("the copy projected %d times, want once", len(line.auth.shown)-before)
	}
	if !strings.Contains(out.Text, "Bearer held") {
		t.Errorf("copy = %q, want the token that is held on it", out.Text)
	}
	if len(out.Omitted) != 0 {
		t.Errorf("omitted = %+v, want nothing left out of a request that is whole", out.Omitted)
	}
}

// A token nobody obtained is neither a reason to refuse the copy nor a reason to go and get one:
// the command is written without the credential, and the omission is how the window can say so.
func TestACopyReportsATokenNobodyObtained(t *testing.T) {
	line := newLine(t)
	authWith(t, line, domain.NewAuth(domain.AuthOAuth2))

	out := copyLine(t, line, command.FormatCurl)
	if len(line.auth.asked) != 0 {
		t.Errorf("a copy materialized %v, want no conversation", line.auth.asked)
	}
	if strings.Contains(out.Text, "Authorization") {
		t.Errorf("copy = %q, want no authorization: there is nothing to write", out.Text)
	}
	if len(out.Omitted) != 1 || out.Omitted[0].Kind != command.OmissionToken {
		t.Errorf("omitted = %+v, want the token it could not write", out.Omitted)
	}
}

// A Digest credential is a hash over a nonce only the server can send, so there is nothing this
// side can put in a command — and a command that quietly lost a credential would be one that lies.
func TestACopyReportsADigestCredential(t *testing.T) {
	line := newLine(t)
	line.auth.answer = domain.AuthOutput{
		Digest: &domain.DigestCredentials{Username: "user", Password: "pass"},
	}
	authWith(t, line, domain.NewAuth(domain.AuthDigest))

	out := copyLine(t, line, command.FormatCurl)
	if len(out.Omitted) != 1 || out.Omitted[0].Kind != command.OmissionDigest {
		t.Errorf("omitted = %+v, want the Digest credential it could not write", out.Omitted)
	}
	if strings.Contains(out.Text, "user") {
		t.Errorf("copy = %q, want no credential in it", out.Text)
	}
}

// A row the user wrote under that name is the more precise answer, and it is a header like any
// other: it goes into the command, and nothing is reported as left out.
func TestACopyOfAWrittenAuthorizationReportsNoCredentialLeftOut(t *testing.T) {
	line := newLine(t)
	line.auth.answer = domain.AuthOutput{
		Digest: &domain.DigestCredentials{Username: "user", Password: "pass"},
	}
	authWith(t, line, domain.NewAuth(domain.AuthDigest))
	line.seed(Seed{
		Method:  "GET",
		URL:     "https://api.example.com/a",
		Headers: []domain.HeaderPair{{Name: "Authorization", Value: "Token hand-written"}},
	})

	out := copyLine(t, line, command.FormatCurl)
	if !strings.Contains(out.Text, "Token hand-written") {
		t.Errorf("copy = %q, want the row the user wrote", out.Text)
	}
	if len(out.Omitted) != 0 {
		t.Errorf("omitted = %+v, want nothing left out", out.Omitted)
	}
}

// The file a body is read from is not read: a copy writes the reference and leaves the bytes where
// they lie. A file that has gone missing in the meantime is a copy that still works.
func TestACopyDoesNotOpenTheFileABodyIsReadFrom(t *testing.T) {
	drafts := newLine(t)
	files := drafts.files
	files.fail = errors.New("the file is gone")
	drafts.seed(Seed{Method: "POST", URL: "https://api.example.com/a"})
	drafts.d.BodyKind, drafts.d.BodyFile = domain.BodyBinary, "/tmp/logo.png"

	out := copyLine(t, drafts, command.FormatCurl)
	if files.read != 0 {
		t.Errorf("the copy opened the file %d times, want none", files.read)
	}
	if !strings.Contains(out.Text, "--data-binary @'/tmp/logo.png'") {
		t.Errorf("copy = %q, want the file as a reference curl reads", out.Text)
	}
	if len(out.Omitted) != 0 {
		t.Errorf("omitted = %+v, want nothing: curl can write this one", out.Omitted)
	}
}

// A form with a file is a body the tool builds itself: `-F name=@file` is where the file is read,
// and not here. The type is the tool's too — an empty boundary in a header names nothing.
func TestACopyOfAFormWithAFileWritesFieldsAndOpensNothing(t *testing.T) {
	drafts := newLine(t)
	files := drafts.files
	files.fail = errors.New("the file is gone")
	drafts.seed(Seed{
		Method:   "POST",
		URL:      "https://api.example.com/a",
		BodyKind: domain.BodyForm,
		Form: []domain.FormRow{
			{Name: "note", Value: "hello", Enabled: true},
			{Name: "logo", Src: "/tmp/logo.png", File: true, Enabled: true},
		},
	})

	out := copyLine(t, drafts, command.FormatCurl)
	if files.read != 0 {
		t.Errorf("the copy opened the file %d times, want none", files.read)
	}
	want := "curl -X POST -F 'note=hello' -F 'logo=@/tmp/logo.png' 'https://api.example.com/a'"
	if out.Text != want {
		t.Errorf("copy = %q, want %q", out.Text, want)
	}
	if strings.Contains(out.Text, "multipart/form-data") {
		t.Errorf("copy = %q, want no type: the boundary would be the tool's own", out.Text)
	}
}

// A form that carries no file is text like any other, and it is rendered here exactly as a send
// renders it: the header's boundary and the body's are one, or the header describes nothing.
func TestACopyOfAFormWithoutAFileKeepsTheBoundaryItWrote(t *testing.T) {
	drafts := newLine(t)
	drafts.seed(Seed{
		Method:   "POST",
		URL:      "https://api.example.com/a",
		BodyKind: domain.BodyForm,
		Form:     []domain.FormRow{{Name: "note", Value: "hello", Enabled: true}},
	})

	out := copyLine(t, drafts, command.FormatCurl)
	_, after, ok := strings.Cut(out.Text, "boundary=")
	if !ok {
		t.Fatalf("copy = %q, want the boundary its type names", out.Text)
	}
	boundary, _, _ := strings.Cut(after, "'")
	if boundary == "" {
		t.Fatalf("copy = %q, want a boundary rather than an empty parameter", out.Text)
	}
	if !strings.Contains(out.Text, "--"+boundary) {
		t.Errorf("copy = %q, want the body written with the boundary %q", out.Text, boundary)
	}
}

// A name nothing answers leaves the copy empty and is named beside it: the command is what the
// request would be, and a name in braces is one no server reads.
func TestACopyNamesTheVariablesNothingAnswers(t *testing.T) {
	line := newLine(t)
	line.d.Body = `{"id": "{{nope}}"}`

	out := copyLine(t, line, command.FormatCurl)
	if strings.Contains(out.Text, "{{nope}}") {
		t.Errorf("copy = %q, want nothing where the name stood", out.Text)
	}
	if !strings.Contains(out.Text, `{"id": ""}`) {
		t.Errorf("copy = %q, want the empty value the request would carry", out.Text)
	}
	if len(out.Omitted) != 1 || out.Omitted[0].Kind != command.OmissionVariable ||
		out.Omitted[0].Name != "nope" {
		t.Errorf("omitted = %+v, want the name nothing answers", out.Omitted)
	}
}

// A link is the address alone, so a name the body carries is not a name the link left out — and the
// address is written whatever the rest of the request holds.
func TestALinkIsCopiedThoughItsBodyNamesNothing(t *testing.T) {
	line := newLine(t)
	line.d.Body = `{"id": "{{nope}}"}`

	out := copyLine(t, line, command.FormatURL)
	if out.Text != "https://api.example.com/a" {
		t.Errorf("link = %q, want the address", out.Text)
	}
	if len(out.Omitted) != 0 {
		t.Errorf("omitted = %+v, want nothing: the link carries no body", out.Omitted)
	}
}

// A command is the request as it would go out, and what the jar would send goes with it: a copied
// login is a request that still works.
func TestACopyCarriesTheJarsCookie(t *testing.T) {
	drafts := newLine(t)
	jar := drafts.jar
	jar.header = "sid=1"
	drafts.seed(Seed{Method: "GET", URL: "https://api.example.com/a"})

	out := copyLine(t, drafts, command.FormatCurl)
	if !strings.Contains(out.Text, "-H 'Cookie: sid=1'") {
		t.Errorf("copy = %q, want the jar's cookie on it", out.Text)
	}
}

// "Inherit" is answered by whoever knows the tree, and a copy is prepared through the same question
// a send is: what the caller resolved is the authorization the command carries.
func TestACopyTakesTheAuthorizationTheTreeAboveAnswered(t *testing.T) {
	line := newLine(t)
	authWith(t, line, domain.NewAuth(domain.AuthInherit))
	inherited := domain.NewAuth(domain.AuthBearer).With("token", "abc123")

	before := len(line.auth.shown)
	out, err := line.copyAs(&inherited, nil, command.FormatCurl)
	if err != nil {
		t.Fatalf("copyAs: %v", err)
	}
	if len(line.auth.shown) != before+1 {
		t.Fatalf("the copy projected %d times, want once", len(line.auth.shown)-before)
	}
	if got := line.auth.shown[before].Type; got != domain.AuthBearer {
		t.Errorf("projected %q, want the authorization the tree above holds", got)
	}
	if len(out.Omitted) != 0 {
		t.Errorf("omitted = %+v, want nothing", out.Omitted)
	}
}
