package jsonpointer

import (
	"errors"
	"slices"
	"testing"
)

// The examples of RFC 6901 §5, against the document it gives them for.
func TestParseReadsTheExamplesOfTheRFC(t *testing.T) {
	for pointer, want := range map[string][]string{
		"":       nil,
		"/foo":   {"foo"},
		"/foo/0": {"foo", "0"},
		"/":      {""},
		"/a~1b":  {"a/b"},
		"/c%d":   {"c%d"},
		"/e^f":   {"e^f"},
		"/g|h":   {"g|h"},
		"/i\\j":  {"i\\j"},
		"/k\"l":  {"k\"l"},
		"/ ":     {" "},
		"/m~0n":  {"m~n"},
		"/~01":   {"~1"},
		"//a":    {"", "a"},
		"/a//b":  {"a", "", "b"},
		"/a/b/":  {"a", "b", ""},
	} {
		got, err := Parse(pointer)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Parse(%q) = %q, %v, want %q", pointer, got, err, want)
		}
	}
}

func TestParseRefusesWhatIsNotAPointer(t *testing.T) {
	for _, pointer := range []string{"foo", "a/b", "#/a"} {
		if _, err := Parse(pointer); !errors.Is(err, ErrSyntax) {
			t.Errorf("Parse(%q) error = %v, want a syntax error", pointer, err)
		}
	}
}

// The fragment forms of RFC 6901 §6.
func TestParseFragmentDecodesTheURI(t *testing.T) {
	for fragment, want := range map[string][]string{
		"#":                               nil,
		"#/foo/0":                         {"foo", "0"},
		"#/a~1b":                          {"a/b"},
		"#/c%25d":                         {"c%d"},
		"#/e%5Ef":                         {"e^f"},
		"#/ ":                             {" "},
		"#/m~0n":                          {"m~n"},
		"#/components/schemas/My%20Model": {"components", "schemas", "My Model"},
		"#/paths/~1users~1%7Bid%7D":       {"paths", "/users/{id}"},
		"/no/hash":                        {"no", "hash"},
	} {
		got, err := ParseFragment(fragment)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("ParseFragment(%q) = %q, %v, want %q", fragment, got, err, want)
		}
	}
	if _, err := ParseFragment("#/bad%zz"); err == nil {
		t.Error("a bad percent escape was accepted")
	}
}

func TestJoinIsTheInverseOfParse(t *testing.T) {
	for _, tokens := range [][]string{{"a", "b"}, {"a/b", "m~n", "~1"}, {""}, {"", ""}, {"x y", "é"}} {
		got, err := Parse(Join(tokens))
		if err != nil || !slices.Equal(got, tokens) {
			t.Errorf("Parse(Join(%q)) = %q, %v", tokens, got, err)
		}
	}
	if Join(nil) != "" {
		t.Errorf("Join(nil) = %q, want the whole document", Join(nil))
	}
}

func TestIndexIsDigitsWithoutALeadingZero(t *testing.T) {
	for _, c := range []struct {
		token  string
		length int
		want   int
		ok     bool
	}{
		{"0", 3, 0, true},
		{"2", 3, 2, true},
		{"3", 3, 0, false},
		{"01", 3, 0, false},
		{"+1", 3, 0, false},
		{"-1", 3, 0, false},
		{"-", 3, 0, false},
		{"", 3, 0, false},
		{"1e0", 3, 0, false},
		{"99999999999999999999", 3, 0, false},
	} {
		got, ok := Index(c.token, c.length)
		if got != c.want || ok != c.ok {
			t.Errorf("Index(%q, %d) = %d, %v, want %d, %v", c.token, c.length, got, ok, c.want, c.ok)
		}
	}
}
