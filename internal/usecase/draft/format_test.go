package draft

import (
	"testing"

	"schemo/internal/domain"
)

// A formatter that re-types a document loses what the person wrote: the digits of a number the
// window's parser rounds, the `1.0` it writes as `1`, the order of the keys. Here the bytes of
// every value are the bytes that came in, and only the whitespace between them changes.
func TestFormatJSONKeepsEveryValueAsWritten(t *testing.T) {
	in := `{"id":12345678901234567890,"price":1.0,"tags":[],"nested":{"b":2,"a":1}}`
	want := "{\n  \"id\": 12345678901234567890,\n  \"price\": 1.0,\n  \"tags\": [],\n" +
		"  \"nested\": {\n    \"b\": 2,\n    \"a\": 1\n  }\n}"

	if got := FormatBody(domain.BodyJSON, in, domain.IndentTwo); got != want {
		t.Errorf("formatted =\n%s\nwant\n%s", got, want)
	}
}

func TestFormatUsesTheIndentTheEditorIsSetTo(t *testing.T) {
	in := `{"a":[1]}`

	if got, want := FormatBody(domain.BodyJSON, in, domain.IndentFour),
		"{\n    \"a\": [\n        1\n    ]\n}"; got != want {
		t.Errorf("four spaces = %q, want %q", got, want)
	}
	if got, want := FormatBody(domain.BodyJSON, in, domain.IndentTab),
		"{\n\t\"a\": [\n\t\t1\n\t]\n}"; got != want {
		t.Errorf("tab = %q, want %q", got, want)
	}
}

// A body that does not parse is the person's to finish: it comes back exactly as it went in.
func TestFormatLeavesWhatItCannotReadAlone(t *testing.T) {
	cases := map[string]struct {
		kind domain.BodyKind
		text string
	}{
		"json cut short":   {domain.BodyJSON, `{"a": [1, 2`},
		"json with a typo": {domain.BodyJSON, `{a: 1}`},
		"json empty":       {domain.BodyJSON, ""},
		"xml unclosed":     {domain.BodyXML, `<a><b></a>`},
		"xml with no tag":  {domain.BodyXML, "just words"},
		"xml empty":        {domain.BodyXML, ""},
		"a raw body":       {domain.BodyRaw, `{"a":1}`},
	}
	for name, c := range cases {
		if got := FormatBody(c.kind, c.text, domain.IndentTwo); got != c.text {
			t.Errorf("%s: got %q, want it untouched", name, got)
		}
	}
}

func TestFormatXMLIndentsByDepthAndKeepsATextElementOnOneLine(t *testing.T) {
	in := `<?xml version="1.0"?><root><item id="1"><name>Ada</name><empty/></item><item/></root>`
	want := "<?xml version=\"1.0\"?>\n<root>\n  <item id=\"1\">\n    <name>Ada</name>\n" +
		"    <empty/>\n  </item>\n  <item/>\n</root>"

	if got := FormatBody(domain.BodyXML, in, domain.IndentTwo); got != want {
		t.Errorf("formatted =\n%s\nwant\n%s", got, want)
	}
}

// Everything that is not whitespace between tags is written back as it was read: a prefix, a quote
// style, a comment, a CDATA section. The old regular expression broke on every one of these.
func TestFormatXMLRewritesNothingButWhitespace(t *testing.T) {
	in := "<s:Envelope xmlns:s='http://x'>  <!-- a > b -->\n<s:Body><![CDATA[<raw & text>]]>" +
		`</s:Body><note a="x>y"/></s:Envelope>`
	want := "<s:Envelope xmlns:s='http://x'>\n  <!-- a > b -->\n" +
		"  <s:Body><![CDATA[<raw & text>]]></s:Body>\n  <note a=\"x>y\"/>\n</s:Envelope>"

	if got := FormatBody(domain.BodyXML, in, domain.IndentTwo); got != want {
		t.Errorf("formatted =\n%s\nwant\n%s", got, want)
	}
}

// Formatting a formatted document changes nothing, so the button can be pressed twice.
func TestFormatIsIdempotent(t *testing.T) {
	for kind, in := range map[domain.BodyKind]string{
		domain.BodyJSON: `{"a":[1,{"b":null}]}`,
		domain.BodyXML:  `<a><b>x</b><c><d/></c></a>`,
	} {
		once := FormatBody(kind, in, domain.IndentFour)
		if twice := FormatBody(kind, once, domain.IndentFour); twice != once {
			t.Errorf("%s: formatting twice gave\n%s\nwant\n%s", kind, twice, once)
		}
	}
}
