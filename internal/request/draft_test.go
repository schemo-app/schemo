package request

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"schemo/internal/convert/command"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// rig is a draft service over a temporary store, with the boundaries faked: the variables, the
// files, the schemes and the jar. Their own working is tested where each lives.
type rig struct {
	*Drafts
	files *fakeFiles
	auth  *fakeAuth
	jar   *fakeJar
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{
		files: &fakeFiles{files: map[string]string{
			"/tmp/logo.png":  "PNGDATA",
			"/tmp/notes.txt": "hello",
		}},
		auth: &fakeAuth{},
		jar:  &fakeJar{},
	}
	r.Drafts = NewDrafts(DraftDeps{
		Store: dbtest.New(t),
		Scope: domain.WorkspaceScope(domain.WorkspacePersonalID),
		Vars: fakeVars{
			values: map[string]string{"host": "api.example.com", "token": "abc123", "nothing": ""},
			secret: map[string]bool{"token": true},
		},
		Jar:   r.jar,
		Files: r.files,
		Auth:  r.auth,
		Net:   fakeNet{},
		IDs:   platform.NewIDGen(),
	})
	return r
}

// request is a seed as the window would hold it once pasted: rows with ids, the authorization
// normalized.
func (r *rig) request(seed Seed) domain.Draft {
	d := r.draftOfSeed(seed)
	d.ID = "tab:test"
	return d
}

func (r *rig) preview(t *testing.T, d domain.Draft) Preview {
	t.Helper()
	preview, err := r.Preview(context.Background(), d)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	return preview
}

func (r *rig) prepared(t *testing.T, d domain.Draft) Prepared {
	t.Helper()
	prepared, err := r.Prepared(context.Background(), d, nil, nil)
	if err != nil {
		t.Fatalf("Prepared: %v", err)
	}
	return prepared
}

// fakeMask is the literal a faked secret leaves as, written here the way the other fakes in this
// package write it: the draft never learns what the mask is made of.
const fakeMask = "••••"

// fakeVars is the environment feature as the draft sees it: a value per name it knows, a secret
// that leaves as its mask, and a blunt `{{name}}` scan — the grammar is the environment's own.
type fakeVars struct {
	values map[string]string
	secret map[string]bool
}

// known is what both calls answer from: the fake's values plus the levels above, nearest winning.
// The list arrives outermost first, and both calls read the same map, so they cannot disagree.
func (f fakeVars) known(above []domain.Variable) map[string]string {
	values := map[string]string{}
	for name, value := range f.values {
		values[name] = value
	}
	for _, v := range above {
		if v.Enabled && v.Name != "" {
			values[v.Name] = v.Value
		}
	}
	return values
}

func (f fakeVars) Missing(
	_ context.Context,
	above []domain.Variable,
	texts []string,
	_ string,
) ([]string, error) {
	values := f.known(above)
	out, seen := []string{}, map[string]bool{}
	for _, text := range texts {
		for _, name := range f.mentioned(text) {
			if _, isKnown := values[name]; isKnown || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

func (f fakeVars) SubstituteTexts(
	_ context.Context,
	above []domain.Variable,
	texts []string,
	mask bool,
	_ string,
) ([]string, error) {
	values := f.known(above)
	out := make([]string, len(texts))
	for i, text := range texts {
		for name, value := range values {
			if mask && f.secret[name] {
				value = fakeMask
			}
			text = strings.ReplaceAll(text, "{{"+name+"}}", value)
		}
		// A name nothing answers goes out as nothing — the same rule the real resolver keeps.
		for _, name := range f.mentioned(text) {
			text = strings.ReplaceAll(text, "{{"+name+"}}", "")
		}
		out[i] = text
	}
	return out, nil
}

// ReferenceTexts is the export's rendering: a secret resolves to the shell variable it goes out
// under rather than to its value, and a value the text spells out is masked, as the resolver does.
func (f fakeVars) ReferenceTexts(
	_ context.Context,
	above []domain.Variable,
	texts []string,
	_ string,
) ([]string, error) {
	values := f.known(above)
	out := make([]string, len(texts))
	for i, text := range texts {
		for name, value := range values {
			if f.secret[name] {
				value = "$SCHEMO_" + strings.ToUpper(name)
			}
			text = strings.ReplaceAll(text, "{{"+name+"}}", value)
		}
		for _, name := range f.mentioned(text) {
			text = strings.ReplaceAll(text, "{{"+name+"}}", "")
		}
		for name, value := range f.values {
			if f.secret[name] && value != "" {
				text = strings.ReplaceAll(text, value, fakeMask)
			}
		}
		out[i] = text
	}
	return out, nil
}

// MaskTexts is the other side of a request that has already been filled in: the fake hides its
// secret values and resolves nothing.
func (f fakeVars) MaskTexts(
	_ context.Context,
	_ []domain.Variable,
	texts []string,
	_ string,
) ([]string, error) {
	out := make([]string, len(texts))
	for i, text := range texts {
		for name, value := range f.values {
			if f.secret[name] {
				text = strings.ReplaceAll(text, value, fakeMask)
			}
		}
		out[i] = text
	}
	return out, nil
}

// mentioned is the names a text writes between braces, in the order they appear.
func (f fakeVars) mentioned(text string) []string {
	out := []string{}
	for i := 0; i+1 < len(text); i++ {
		if text[i] != '{' || text[i+1] != '{' {
			continue
		}
		rest := text[i+2:]
		end := strings.Index(rest, "}}")
		if end == -1 {
			break
		}
		out = append(out, strings.TrimSpace(rest[:end]))
		i += end + 3
	}
	return out
}

// fakeFiles is the file system as the draft sees it. read is the count of reads, so a test can say
// that rendering the masked copy did not open anything.
type fakeFiles struct {
	files map[string]string
	fail  error
	read  int
}

func (f *fakeFiles) Read(path string) ([]byte, error) {
	f.read++
	if f.fail != nil {
		return nil, f.fail
	}
	data, ok := f.files[path]
	if !ok {
		return nil, fmt.Errorf("file %s: %w", path, domain.ErrNotFound)
	}
	return []byte(data), nil
}

// fakeNet is the engine's own answers as the draft reads them: how a typed address is completed,
// what the app calls itself, and whether the server is allowed to compress. The rules themselves
// are the engine's and tested where they live; what these tests are about is that a draft asks.
type fakeNet struct {
	agent string
	gzip  bool
}

func (fakeNet) CompleteURL(raw string) string { return raw }

func (f fakeNet) WireDefaults() (string, bool) { return f.agent, f.gzip }

// fakeJar stands in for the cookie jar: the header one address goes out with. Its own working is
// tested where it lives; what this package must get right is that a request reads it.
type fakeJar struct {
	header string
	// got is what the last read was asked for, so a test can say the request asked about its own
	// address rather than about whatever was in the URL before the variables were filled in.
	got struct {
		workspace string
		url       string
	}
	fail error
}

func (f *fakeJar) CookieHeader(
	_ context.Context,
	workspace, rawURL string,
) (string, error) {
	f.got.workspace, f.got.url = workspace, rawURL
	if f.fail != nil {
		return "", f.fail
	}
	return f.header, nil
}

// fakeAuth stands in for the schemes; their own working is tested where it lives. What this package
// must get right: a field reaches the scheme resolved, masked on the second pass, burying no row.
type fakeAuth struct {
	// answer is what the fake hands back, so a test can watch any output travel the pipeline.
	answer domain.AuthOutput
	// token is what a scheme that fetches is reported to hold, so a test can say what the window is
	// told without a provider anywhere near it.
	token domain.AuthToken
	// asked is what it was asked while sending: a draft is prepared twice, values then masks.
	// shown is the same question with nobody sending — the rows the window draws — kept apart.
	asked []domain.Auth
	shown []domain.Auth
}

func (f *fakeAuth) Materialize(
	_ context.Context,
	auth domain.Auth,
	_ domain.AuthRequest,
) (domain.AuthOutput, error) {
	f.asked = append(f.asked, auth)
	return f.answer, nil
}

func (f *fakeAuth) Project(auth domain.Auth, _ domain.AuthRequest) (domain.AuthOutput, error) {
	f.shown = append(f.shown, auth)
	return f.answer, nil
}

// Obtain and Forget do nothing here: what these tests are about is the pipeline, and a token
// somebody else issues is tested where it is fetched and kept.
func (f *fakeAuth) Obtain(context.Context, domain.Auth) error { return nil }
func (f *fakeAuth) Forget(domain.Auth)                        {}
func (f *fakeAuth) Held(domain.Auth) domain.AuthToken         { return f.token }

func (f *fakeAuth) last() domain.Auth {
	if len(f.asked) == 0 {
		return domain.Auth{}
	}
	return f.asked[len(f.asked)-1]
}

// What goes out, and what is kept beside it: the same request twice, once with the values and once
// with a secret left as its mask.
func TestPreparedFillsAndMasks(t *testing.T) {
	r := newRig(t)
	// A cookie's value is a text like any other: a `{{token}}` in one is filled in on the way out and
	// masked in the copy that is written down.
	r.jar.header = "s={{token}}"

	prepared := r.prepared(t, r.request(Seed{
		Method: "GET",
		URL:    "https://{{host}}/a?x=1",
		Headers: []domain.HeaderPair{
			{Name: "Authorization", Value: "Bearer {{token}}"},
			{Name: "X-Off", Value: "{{nothing}}"},
		},
		Body: "body {{token}}",
	}))

	if prepared.URL != "https://api.example.com/a?x=1" {
		t.Errorf("url = %q, want the variables filled in", prepared.URL)
	}
	if prepared.Body != "body abc123" {
		t.Errorf("body = %q", prepared.Body)
	}
	if len(prepared.Headers) != 3 {
		t.Fatalf("headers = %+v, want the two and the Cookie the jar became", prepared.Headers)
	}
	if prepared.Headers[0].Value != "Bearer abc123" {
		t.Errorf("authorization = %q, want the secret's value on the way out", prepared.Headers[0].Value)
	}
	if prepared.Headers[2].Name != "Cookie" || prepared.Headers[2].Value != "s=abc123" {
		t.Errorf("cookie header = %+v, want the jar in it", prepared.Headers[2])
	}
	if prepared.MaskedURL != "https://api.example.com/a?x=1" {
		t.Errorf("masked url = %q", prepared.MaskedURL)
	}
	if prepared.MaskedBody != "body "+fakeMask {
		t.Errorf("masked body = %q, want the secret replaced", prepared.MaskedBody)
	}
	if prepared.MaskedHeaders[0].Value != "Bearer "+fakeMask {
		t.Errorf("masked authorization = %q", prepared.MaskedHeaders[0].Value)
	}
	if prepared.MaskedHeaders[2].Value != "s="+fakeMask {
		t.Errorf("masked cookie header = %q", prepared.MaskedHeaders[2].Value)
	}
	// The jar is asked about the address the request goes to, not the one with a variable in it.
	if r.jar.got.url != "https://api.example.com/a?x=1" {
		t.Errorf("jar was asked about %q, want the resolved address", r.jar.got.url)
	}
}

// The preview names what nothing answers, each once and in the order written; an empty value is a
// value.
func TestPreviewNamesWhatNothingAnswers(t *testing.T) {
	r := newRig(t)

	if missing := r.preview(t, r.request(Seed{URL: "https://{{host}}/a"})).Missing; len(missing) != 0 {
		t.Errorf("missing = %q, want nothing", missing)
	}
	got := r.preview(t, r.request(Seed{
		URL: "https://x/{{nothing}}", Body: "{{nope}} and {{body}} and {{nope}}",
	})).Missing
	if want := []string{"nope", "body"}; !slices.Equal(got, want) {
		t.Errorf("missing = %q, want %q", got, want)
	}
}

// A tab's draft is written down and read back; one never written starts empty; a discarded one is
// gone.
func TestATabDraftIsKeptUntilDiscarded(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	fresh, err := r.Open(ctx, "tab:x")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if fresh.ID != "tab:x" || fresh.Method != "GET" || fresh.URL != "" {
		t.Errorf("fresh = %+v, want an empty GET under the tab's id", fresh)
	}

	d := r.request(Seed{Method: "POST", URL: "/a", Headers: []domain.HeaderPair{{Name: "A", Value: "1"}}})
	d.ID, d.EnvironmentID = "tab:x", "env-1"
	d.Params = []domain.Row{{Name: "page", Value: "2", Enabled: true}}
	if err := r.Save(ctx, d); err != nil {
		t.Fatalf("Save: %v", err)
	}
	read, err := r.Open(ctx, "tab:x")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if read.Method != "POST" || read.URL != "/a" || read.EnvironmentID != "env-1" ||
		len(read.Headers) != 1 {
		t.Errorf("read = %+v, want what was saved", read)
	}
	if len(read.Params) != 1 || read.Params[0].ID == "" {
		t.Errorf("params = %+v, want the row given an id", read.Params)
	}

	if err := r.Discard(ctx, "tab:x"); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	again, err := r.Open(ctx, "tab:x")
	if err != nil || again.URL != "" {
		t.Errorf("after Discard = %+v (%v), want a fresh draft", again, err)
	}
}

// Only a tab's draft is written down: a saved request's draft becomes the request through SaveNode.
func TestSaveRefusesADraftThatIsNotATabs(t *testing.T) {
	r := newRig(t)
	d := r.request(Seed{URL: "/a"})
	d.ID = "node-1"
	if err := r.Save(context.Background(), d); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("Save = %v, want a refusal", err)
	}
}

// A saved request opens as a draft that inherits when it has no authorization of its own, and the
// other direction keeps what the node does not let the editor own.
func TestANodeAndItsDraftAreTheSameRequest(t *testing.T) {
	node := domain.CollectionNode{ID: "n", Name: "List", Method: "GET", URL: "/a"}
	d := FromNode(node)
	if d.ID != "n" || d.Auth.Type != domain.AuthInherit {
		t.Fatalf("draft = %+v, want the node's id and Inherit", d)
	}
	d.Method = " POST "
	back := nodeFromDraft(node, d)
	if back.Method != "POST" || back.Name != "List" || back.Auth == nil ||
		back.Auth.Type != domain.AuthInherit {
		t.Errorf("node = %+v, want the edit, the name kept and Inherit chosen", back)
	}
}

// line is a rig with one draft in hand, edited in place the way the window edits it.
type line struct {
	*rig
	d domain.Draft
}

func newLine(t *testing.T) *line {
	t.Helper()
	r := newRig(t)
	return &line{rig: r, d: r.request(Seed{Method: "GET", URL: "https://api.example.com/a"})}
}

// seed replaces the draft with a whole request, the way a paste does.
func (l *line) seed(s Seed) { l.d = l.request(s) }

// header adds an enabled header row.
func (l *line) header(name, value string) {
	l.d.Headers = append(l.d.Headers, domain.Row{ID: l.ids(), Name: name, Value: value, Enabled: true})
}

// setAuth is the Auth chip's choice, normalized as the window keeps it.
func (l *line) setAuth(t *testing.T, auth domain.Auth) {
	t.Helper()
	l.d.Auth = auth.Normalized()
}

// copyAs is a copy of the draft in hand, with what the levels above answer handed in.
func (l *line) copyAs(inherits *domain.Auth, above []domain.Variable, format command.Format) (Copy, error) {
	ctx := context.Background()
	cookie, err := l.jarCookies(ctx, l.d)
	if err != nil {
		return Copy{}, err
	}
	return l.renderCommand(ctx, l.d, inherits, above, format, cookie, true)
}

type quiet struct{}

func (quiet) Publish(string, any) {}

// A history record opened for editing becomes a tab's draft with everything the request went out
// with, and a copy of that draft outlives it.
func TestARecordIsLaidOutAsAnIndependentDraft(t *testing.T) {
	ctx := t.Context()
	store := dbtest.New(t)
	ids := platform.NewIDGen()
	records := NewHistory(HistoryDeps{Store: store, Scope: store, Notifier: quiet{}, Retention: store,
		IDs: ids})
	drafts := NewDrafts(DraftDeps{Store: store, Scope: store, IDs: ids, History: records})
	headers := []domain.HeaderPair{{Name: "Content-Type", Value: "application/json"},
		{Name: "Authorization", Value: "Bearer captured-token"}}
	captured, err := records.Ingest(ctx, IngestInput{
		Method: "POST", URL: "https://example.com/items?page=2", RequestHeaders: headers,
		RequestBody: `{"name":"example"}`, Status: 201,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := drafts.FromRecord(ctx, "tab:seeded", captured.ID); err != nil {
		t.Fatalf("FromRecord: %v", err)
	}
	seeded, err := drafts.Open(ctx, "tab:seeded")
	if err != nil {
		t.Fatal(err)
	}
	if seeded.Method != "POST" || seeded.URL != captured.URL || seeded.Body != `{"name":"example"}` {
		t.Fatalf("seeded draft lost content: %+v", seeded)
	}
	var got []domain.HeaderPair
	for _, header := range seeded.Headers {
		if !header.Enabled || header.ID == "" {
			t.Fatalf("seeded header = %+v, want it enabled with an id", header)
		}
		got = append(got, domain.HeaderPair{Name: header.Name, Value: header.Value})
	}
	if !slices.Equal(got, headers) {
		t.Fatalf("headers = %+v, want %+v", got, headers)
	}

	if err := drafts.Copy(ctx, "tab:seeded", "tab:copy"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if err := drafts.Discard(ctx, "tab:seeded"); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	copied, err := drafts.Open(ctx, "tab:copy")
	if err != nil || copied.URL != captured.URL || len(copied.Headers) != len(headers) {
		t.Fatalf("copy = %+v, %v, want it to outlive the original", copied, err)
	}
}
