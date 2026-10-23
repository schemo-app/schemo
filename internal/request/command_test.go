package request

import (
	"context"
	"schemo/internal/convert/command"
	"schemo/internal/domain"
	"strings"
	"testing"
)

func TestPastingSomethingElseLeavesTheDraftAlone(t *testing.T) {
	drafts := newLine(t)
	for _, text := range []string{"https://api.example.com/articles", "plain text", ""} {
		pasted := drafts.Paste(text)
		if pasted.Reading.Kind != command.KindNone {
			t.Errorf("PasteCommand(%q) = %q, want none", text, pasted.Reading.Kind)
		}
		if pasted.Draft != nil {
			t.Errorf("PasteCommand(%q) replaced the draft with %+v", text, pasted.Draft)
		}
	}
}

// A command is a whole request rather than an edit to one: it goes over the draft, and the reading
// comes back so the window can say so.

func TestPastingACommandReplacesTheDraft(t *testing.T) {
	drafts := newLine(t)
	pasted := drafts.Paste(`curl -X POST 'https://api.example.com/articles' -H 'X-A: 1' --data-raw '{"a":1}'`)
	if pasted.Reading.Kind != command.KindOK {
		t.Fatalf("reading = %+v, want a POST read as curl", pasted.Reading)
	}
	if pasted.Draft == nil {
		t.Fatal("a command that was read left the draft as it was")
	}
	if pasted.Draft.Method != "POST" ||
		pasted.Draft.URL != "https://api.example.com/articles" {
		t.Errorf("draft = %s %s, want the command that was pasted",
			pasted.Draft.Method, pasted.Draft.URL)
	}
	if pasted.Draft.Body != `{"a":1}` {
		t.Errorf("body = %q, want the body the command carried", pasted.Draft.Body)
	}
}

// A credential pasted as a header reaches the Auth chip. Every tool that copies a request out of a
// browser writes it that way — devtools writes `-H 'Authorization: …'` and never `-u` — so a header
// left as a header would mean the chip stayed on "None" for the shape of command people actually
// paste, and the fields behind it unfilled.

// A saved request is written out as a command for an export.
func TestCopySavedWritesTheSavedRequest(t *testing.T) {
	drafts := newLine(t)
	ctx := context.Background()

	copied, err := drafts.CopySaved(ctx, domain.ResolvedRequest{
		Node: domain.CollectionNode{ID: "n1", Method: "GET", URL: "https://api.example.com/saved"},
	}, command.FormatCurl, true)
	if err != nil {
		t.Fatalf("CopySaved: %v", err)
	}
	if !strings.Contains(copied.Text, "/saved") || !strings.Contains(copied.Text, "curl") {
		t.Errorf("text = %q, want the saved request written out", copied.Text)
	}
}

// Secrets are as the caller asks: a command meant to be run carries the values, and one meant to be
// handed on leaves each secret as a reference to itself.
func TestCopySavedRendersSecretsAsAsked(t *testing.T) {
	drafts := newLine(t)
	ctx := context.Background()

	from := domain.ResolvedRequest{
		Node: domain.CollectionNode{
			ID: "n1", Method: "GET", URL: "https://{{host}}/users",
			Headers: []domain.Row{{Name: "X-Key", Value: "{{token}}", Enabled: true}},
		},
	}

	withValues, err := drafts.CopySaved(ctx, from, command.FormatCurl, true)
	if err != nil {
		t.Fatalf("CopySaved: %v", err)
	}
	if !strings.Contains(withValues.Text, "abc123") {
		t.Errorf("text = %q, want the secret's value in a copy meant to be run", withValues.Text)
	}

	named, err := drafts.CopySaved(ctx, from, command.FormatCurl, false)
	if err != nil {
		t.Fatalf("CopySaved: %v", err)
	}
	if strings.Contains(named.Text, "abc123") {
		t.Errorf("text = %q, want no secret value in a copy meant to be handed on", named.Text)
	}
	if !strings.Contains(named.Text, "$SCHEMO_TOKEN") {
		t.Errorf("text = %q, want the secret named where its value would be", named.Text)
	}
}

// A paste of commands is the requests it holds, one per command, and a `# name` line above one
// names it — which is how this app's own copy writes them down.
func TestReadCommandsReadsEachCommandWithItsName(t *testing.T) {
	nodes, err := ReadCommands(`# List users
curl 'https://api.example.com/users?page=2' -H 'Accept: application/json'

# Create a user
curl -X POST https://api.example.com/users -H 'Content-Type: application/json' \
  --data-raw '{"name":"Ada"}'
`)
	if err != nil {
		t.Fatalf("ReadCommands: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d, want both commands", len(nodes))
	}

	list := nodes[0]
	if list.Name != "List users" || list.Method != "GET" ||
		list.URL != "https://api.example.com/users?page=2" {
		t.Errorf("first = %+v, want the first command as it was pasted", list)
	}
	if len(list.Params) != 1 || list.Params[0].Name != "page" || !list.Params[0].Enabled {
		t.Errorf("params = %+v, want the query as rows beside the address", list.Params)
	}

	created := nodes[1]
	if created.Name != "Create a user" || created.Method != "POST" ||
		created.BodyKind != domain.BodyJSON || created.Body != `{"name":"Ada"}` {
		t.Errorf("second = %+v, want the second command with its body", created)
	}
	if len(created.Headers) != 1 || created.Headers[0].Name != "Content-Type" {
		t.Errorf("headers = %+v, want what the command carried", created.Headers)
	}
}

// Text with no command in it is refused: an import has nothing to write, and what a person pastes
// into the window's own field is left untouched by that field's own rule.
func TestReadCommandsRefusesAPasteThatIsNotACommand(t *testing.T) {
	if _, err := ReadCommands("GET https://api.example.com/users\n"); domain.CodeOf(err) !=
		domain.CodeNotCommand {
		t.Errorf("ReadCommands = %v, want notCommand", err)
	}
}

func TestParamsFromURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want []domain.Row
	}{
		{
			name: "a token and an encoded letter",
			url:  "https://api.example.com/a?x={{b}}&c=%C3%A9",
			want: []domain.Row{
				{Name: "x", Value: "{{b}}", Enabled: true},
				{Name: "c", Value: "é", Enabled: true},
			},
		},
		{
			name: "order is the order of the URL",
			url:  "/a?b=2&a=1",
			want: []domain.Row{
				{Name: "b", Value: "2", Enabled: true},
				{Name: "a", Value: "1", Enabled: true},
			},
		},
		{
			name: "a name with no value, and a value with no name",
			url:  "/a?flag&=orphan",
			want: []domain.Row{
				{Name: "flag", Value: "", Enabled: true},
				{Name: "", Value: "orphan", Enabled: true},
			},
		},
		{
			name: "a plus is a space on the way in",
			url:  "/a?q=one+two",
			want: []domain.Row{{Name: "q", Value: "one two", Enabled: true}},
		},
		{
			name: "a trailing separator is not a parameter",
			url:  "/a?x=1&",
			want: []domain.Row{{Name: "x", Value: "1", Enabled: true}},
		},
		{name: "nothing to read", url: "/a", want: nil},
		{name: "an empty query", url: "/a?", want: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := paramsFromURL(tc.url)
			if len(got) != len(tc.want) {
				t.Fatalf("paramsFromURL(%q) = %+v, want %+v", tc.url, got, tc.want)
			}
			for i := range got {
				if got[i].Name != tc.want[i].Name || got[i].Value != tc.want[i].Value || !got[i].Enabled {
					t.Errorf("row %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// The characters this app's URLs are made of, and the ones that have to be escaped. `,` and the
// brackets are the deliberate difference from what both Go and the browser would produce: they are
// legal in a query, and encoding them would rewrite a pasted JSON:API URL into one nobody
// recognises.
func TestEncodeQueryKeepsWhatTheUrlIsMadeOf(t *testing.T) {
	cases := map[string]string{
		"1,2":             "1,2",
		"page[number]":    "page[number]",
		"filter[id][in]":  "filter[id][in]",
		"{{token}}":       "{{token}}",
		"one two":         "one+two",
		"é":               "%C3%A9",
		"a&b=c":           "a%26b%3Dc",
		"100%":            "100%25",
		"a/b?c":           "a%2Fb%3Fc",
		"include":         "include",
		"a-b_c.d*e":       "a-b_c.d*e",
		"tilde~and'quote": "tilde~and%27quote",
		"a\nb":            "a%0Ab",
		"":                "",
	}
	for in, want := range cases {
		if got := encodeQuery(in); got != want {
			t.Errorf("encodeQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

// The jar wins the Cookie header. A request holding both — a `Cookie` row somebody typed and the
// cookies the jar has for the same host — sends the jar's: cookies leave from one place, and two
// rows of the same name are a request no server can read.
func TestTheJarWinsTheCookieHeader(t *testing.T) {
	draft := domain.Draft{
		Headers: []domain.Row{{ID: "h1", Name: "Cookie", Value: "typed=1", Enabled: true}},
	}

	if got := collectParts(draft, "jar=2").headers; len(got) != 0 {
		t.Errorf("headers = %+v, want the typed Cookie row dropped", got)
	}
	// With nothing in the jar there is nothing to win over, and the row is what the request sends.
	got := collectParts(draft, "").headers
	if len(got) != 1 || got[0].Name != "Cookie" || got[0].Value != "typed=1" {
		t.Errorf("headers = %+v, want the typed Cookie kept", got)
	}
}
