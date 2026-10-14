package environment

import (
	"testing"

	"schemo/internal/domain"
)

// fakeLookup is the environment these tests resolve against: one plain variable, one global and one
// secret, so that "unknown", "global" and "masked" are all reachable.
func fakeLookup(name string) (domain.Resolution, bool) {
	switch name {
	case "host":
		return domain.Resolution{Value: "api.example.com", Source: "env", Kind: domain.VariableText}, true
	case "page":
		return domain.Resolution{Value: "2", Source: "global", Kind: domain.VariableText}, true
	case "token":
		return domain.Resolution{Value: "s3cret", Source: "env", Kind: domain.VariableSecret}, true
	}
	return domain.Resolution{}, false
}

func TestTokens(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []token
	}{
		{"empty", "", nil},
		{"prose", "no tokens here", nil},
		{"one", "{{host}}", []token{{start: 0, end: 8, name: "host", raw: "{{host}}"}}},
		{
			"spaces inside",
			"before {{ host }} after",
			[]token{{start: 7, end: 17, name: "host", raw: "{{ host }}"}},
		},
		{
			"two, offsets past the first",
			"{{host}}{{page}}",
			[]token{
				{start: 0, end: 8, name: "host", raw: "{{host}}"},
				{start: 8, end: 16, name: "page", raw: "{{page}}"},
			},
		},
		{
			"escaped then real",
			`\{{host}} {{page}}`,
			[]token{{start: 10, end: 18, name: "page", raw: "{{page}}"}},
		},
		{"only an escape", `\{{host}}`, nil},
		{"a JSON payload is not a token", `{{"a": 1}}`, nil},
		{"empty name", "{{}}", nil},
		{"dash in the name", "{{a-b}}", nil},
		{"leading digit", "{{9a}}", nil},
		{"unclosed", "{{host", nil},
		{
			"unclosed after a real token",
			"{{host}} and {{page",
			[]token{{start: 0, end: 8, name: "host", raw: "{{host}}"}},
		},
		{
			"a payload before a real token",
			`{{"a": 1}} {{host}}`,
			[]token{{start: 11, end: 19, name: "host", raw: "{{host}}"}},
		},
		{
			"the name is trimmed, newlines included",
			"{{\nhost\n}}",
			[]token{{start: 0, end: 10, name: "host", raw: "{{\nhost\n}}"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertTokens(t, tokens(tc.text), tc.want)
		})
	}
}

// assertTokens compares the whole token sequence: offsets decide where the highlight layer draws,
// so a token that is found but misplaced is a failure too.
func assertTokens(t *testing.T, got, want []token) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("tokens = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestInterpolate(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"plain text is returned as it is", "no tokens", "no tokens"},
		{"known", "{{host}}/articles", "api.example.com/articles"},
		{"the name is trimmed", "{{ host }}", "api.example.com"},
		{"a secret is substituted too: this is the send path", "Bearer {{token}}", "Bearer s3cret"},
		{
			// A request goes out either way: the window says which names came to nothing, and the
			// address carries a value rather than a name in braces.
			"unknown leaves nothing behind",
			"Bearer {{nope}}",
			"Bearer ",
		},
		{
			"an escaped opener is not a token",
			`\{{host}}`,
			`\{{host}}`,
		},
		{"a JSON payload travels untouched", `{"a": "{{host}}"}`, `{"a": "api.example.com"}`},
		{"a payload with no name is untouched", `{{"a": 1}}`, `{{"a": 1}}`},
		{"several", "{{host}}:{{page}}?", "api.example.com:2?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := interpolate(tc.text, fakeLookup, false); got != tc.want {
				t.Errorf("interpolate(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestInterpolateMasksSecrets(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"known text value", "{{host}}", "api.example.com"},
		{"a global value is not a secret", "{{page}}", "2"},
		{"a secret leaves as the mask", "Bearer {{token}}", "Bearer " + domain.SecretMask},
		{"unknown leaves nothing behind", "{{nope}}", ""},
		{"escaped", `\{{token}}`, `\{{token}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := interpolate(tc.text, fakeLookup, true); got != tc.want {
				t.Errorf("interpolate(%q, mask) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestMissing(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"nothing missing", "{{host}}", []string{}},
		{"one", "{{nope}}", []string{"nope"}},
		{
			"deduplicated in order of first appearance",
			"{{b}} {{a}} {{b}} {{c}} {{a}}",
			[]string{"b", "a", "c"},
		},
		{"known names are not reported", "{{nope}} {{host}}", []string{"nope"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := missing(tc.text, fakeLookup)
			if len(got) != len(tc.want) {
				t.Fatalf("missing(%q) = %v, want %v", tc.text, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("missing(%q) = %v, want %v (index %d)", tc.text, got, tc.want, i)
				}
			}
		})
	}
}
