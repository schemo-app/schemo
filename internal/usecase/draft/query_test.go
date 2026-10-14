package draft

import (
	"testing"

	"schemo/internal/domain"
)

func TestParamsFromURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want []domain.Row
	}{
		{
			name: "a token and an encoded letter",
			url:  "https://api.example.com/a?x={{b}}&c=%D0%B0",
			want: []domain.Row{
				{Name: "x", Value: "{{b}}", Enabled: true},
				{Name: "c", Value: "а", Enabled: true},
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

// A URL that has been through the rows has to come back byte for byte. Sorting the names, escaping
// the braces of a token or encoding the comma of `include=author,comments` would all show up here.
func TestJoinURLTextRoundTrip(t *testing.T) {
	for _, raw := range []string{
		"/a?x={{b}}&c=%D0%B0",
		"/a?b=2&a=1",
		"/a?q=one+two",
		"https://api.example.com/articles?include=author,comments&page[number]=2",
		"/a?filter[id][in]=1,2",
		"/a#frag?no=1",
	} {
		got := joinURL(baseOf(t, raw), paramsFromURL(raw))
		if got != raw {
			t.Errorf("round trip of %q = %q", raw, got)
		}
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
		"а":               "%D0%B0",
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

func baseOf(t *testing.T, raw string) string {
	t.Helper()
	base, _ := queryOf(raw)
	return base
}

func TestJoinURLDropsWhatIsNotSent(t *testing.T) {
	rows := []domain.Row{
		{Name: "a", Value: "1", Enabled: true},
		{Name: "off", Value: "2", Enabled: false},
		{Name: "  ", Value: "3", Enabled: true},
		{Name: "empty", Value: "", Enabled: true},
	}
	if got := joinURL("/x", rows); got != "/x?a=1&empty=" {
		t.Errorf("joinURL = %q, want only the enabled and named rows", got)
	}
	if got := joinURL("/x", nil); got != "/x" {
		t.Errorf("joinURL with no rows = %q, want the URL as it was", got)
	}
}

// A row that is still in the text keeps its identity: an editor open on it, and a patch on its way
// to it, both hold an id and not a position.
func TestReconcileKeepsIdentity(t *testing.T) {
	existing := []domain.Row{
		{ID: "p1", Name: "a", Value: "1", Enabled: true},
		{ID: "p2", Name: "b", Value: "2", Enabled: true},
		{ID: "p3", Name: "parked", Value: "3", Enabled: false},
	}

	parsed := []domain.Row{
		{Name: "a", Value: "1", Enabled: true},
		{Name: "c", Value: "3", Enabled: true},
	}

	got := reconcile(parsed, existing)
	if len(got) != 3 {
		t.Fatalf("reconciled to %+v, want the two from the URL and the parked row", got)
	}
	if got[0].ID != "p1" {
		t.Errorf("row a = %q, want the id it had", got[0].ID)
	}
	if got[1].ID != "" {
		t.Errorf("row c = %q, want no id yet — the caller mints one", got[1].ID)
	}
	if got[2].ID != "p3" || got[2].Enabled {
		t.Errorf("row parked = %+v, want it kept and still off", got[2])
	}
}

// The jar wins the Cookie header. A request holding both — a `Cookie` row somebody typed and the
// cookies the jar has for the same host — sends the jar's: cookies leave from one place, and two
// rows of the same name are a request no server can read.
func TestTheJarWinsTheCookieHeader(t *testing.T) {
	draft := domain.Draft{
		Headers: []domain.Row{{ID: "h1", Name: "Cookie", Value: "typed=1", Enabled: true}},
	}

	if got := collect(draft, "jar=2").headers; len(got) != 0 {
		t.Errorf("headers = %+v, want the typed Cookie row dropped", got)
	}
	// With nothing in the jar there is nothing to win over, and the row is what the request sends.
	got := collect(draft, "").headers
	if len(got) != 1 || got[0].Name != "Cookie" || got[0].Value != "typed=1" {
		t.Errorf("headers = %+v, want the typed Cookie kept", got)
	}
}
