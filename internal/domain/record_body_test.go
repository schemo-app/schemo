package domain

import (
	"strings"
	"testing"
)

func TestBinaryBodyTellsBytesFromText(t *testing.T) {
	// A long Cyrillic text cut by the sample inside a two-byte letter is still text.
	cut := strings.Repeat("a", BodySniffBytes-1) + "ж"
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"empty", "", false},
		{"json", `{"data":[]}`, false},
		{"cyrillic", "привет, мир", false},
		{"cut at the sample", cut, false},
		{"mp4 header", "\x00\x00\x00\x20ftypisom", true},
		{"not UTF-8", "caf\xe9", true},
		{"nul inside text", "a\x00b", true},
	}
	for _, c := range cases {
		if got := BinaryBody(c.body); got != c.want {
			t.Errorf("%s: BinaryBody = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestABinaryBodyKeepsItsBytesOutOfInline(t *testing.T) {
	bytes := "\x00\x01\xff\xfe"
	body := BodyOf(bytes, false)
	if !body.Binary || body.Inline != "" || body.Content() != bytes || body.Size != 4 {
		t.Errorf("BodyOf = %+v, want the bytes kept but not inline", body)
	}

	text := BodyOf(`{"a":1}`, true)
	if text.Binary || text.Inline != `{"a":1}` || text.Content() != `{"a":1}` || !text.Truncated {
		t.Errorf("BodyOf = %+v, want the text inline", text)
	}
}
