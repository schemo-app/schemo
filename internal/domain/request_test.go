package domain

import (
	"slices"
	"testing"
)

func TestMediaTypeOfIsTheTypeWithoutItsParameters(t *testing.T) {
	for contentType, want := range map[string]string{
		"application/json":                    "application/json",
		"Application/JSON; charset=UTF-8":     "application/json",
		"text/html;charset=utf-8":             "text/html",
		`multipart/form-data; boundary="a;b"`: "multipart/form-data",
		"text/html; charset":                  "text/html",
		"  text/csv ; header=present":         "text/csv",
		"":                                    "",
		"not a type":                          "not a type",
	} {
		if got := MediaTypeOf(contentType); got != want {
			t.Errorf("MediaTypeOf(%q) = %q, want %q", contentType, got, want)
		}
	}
}

// The suffixes of RFC 6839 and RFC 7303 name the notation a type is built on; a type that only
// contains the word does not.
func TestAMediaTypeIsAKindByItsNameOrItsSuffix(t *testing.T) {
	for _, c := range []struct {
		media          string
		json, xml, web bool
	}{
		{"application/json", true, false, false},
		{"application/vnd.api+json", true, false, false},
		{"application/problem+json", true, false, false},
		{"application/jsonp", false, false, false},
		{"application/json-seq", false, false, false},
		{"application/xml", false, true, false},
		{"text/xml", false, true, false},
		{"application/atom+xml", false, true, false},
		{"application/xhtml+xml", false, true, true},
		{"text/xml-external-parsed-entity", false, false, false},
		{"text/html", false, false, true},
		{"text/plain", false, false, false},
		{"", false, false, false},
	} {
		if got := IsJSONMedia(c.media); got != c.json {
			t.Errorf("IsJSONMedia(%q) = %v, want %v", c.media, got, c.json)
		}
		if got := IsXMLMedia(c.media); got != c.xml {
			t.Errorf("IsXMLMedia(%q) = %v, want %v", c.media, got, c.xml)
		}
		if got := IsHTMLMedia(c.media); got != c.web {
			t.Errorf("IsHTMLMedia(%q) = %v, want %v", c.media, got, c.web)
		}
	}
	if !IsCSVMedia("text/csv") || !IsCSVMedia("text/tab-separated-values") || IsCSVMedia("text/plain") {
		t.Error("IsCSVMedia misread the tabular types")
	}
}

func TestSplitQueryLeavesTheFragmentWithTheAddress(t *testing.T) {
	for _, c := range []struct{ address, base, query string }{
		{"https://x.test/a?b=1&c=2", "https://x.test/a", "b=1&c=2"},
		{"https://x.test/a", "https://x.test/a", ""},
		{"https://x.test/a?b=1#top", "https://x.test/a#top", "b=1"},
		{"https://x.test/a#top?not=query", "https://x.test/a#top?not=query", ""},
		{"https://x.test/a?", "https://x.test/a", ""},
		{"?only=query", "", "only=query"},
		{"https://x.test/{{base}}?id={{id}}", "https://x.test/{{base}}", "id={{id}}"},
	} {
		base, query := SplitQuery(c.address)
		if base != c.base || query != c.query {
			t.Errorf("SplitQuery(%q) = %q, %q, want %q, %q", c.address, base, query, c.base, c.query)
		}
	}
}

// url.Values would sort the names and group the repeats; a request is sent, and shown, as written.
func TestQueryPairsKeepTheOrderAndTheRepeats(t *testing.T) {
	got := QueryPairs("b=2&a=1&b=3&flag&&empty=&=x")
	want := []QueryPair{
		{"b", "2"}, {"a", "1"}, {"b", "3"}, {"flag", ""}, {"empty", ""}, {"", "x"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("QueryPairs = %v, want %v", got, want)
	}
	if QueryPairs("") != nil {
		t.Error("an empty query has pairs")
	}
}

func TestADecodedPairIsReadAsAFormReadsIt(t *testing.T) {
	for _, c := range []struct{ raw, name, value string }{
		{"q=a+b", "q", "a b"},
		{"q=a%20b", "q", "a b"},
		{"q=a%2Bb", "q", "a+b"},
		{"include=author,comments", "include", "author,comments"},
		{"filter%5Bid%5D=1", "filter[id]", "1"},
		{"bad=100%", "bad", "100%"},
		{"%zz=1", "%zz", "1"},
		{"id={{id}}", "id", "{{id}}"},
	} {
		pair := QueryPairs(c.raw)[0].Decoded()
		if pair.Name != c.name || pair.Value != c.value {
			t.Errorf("%q decoded to %q = %q, want %q = %q", c.raw, pair.Name, pair.Value, c.name, c.value)
		}
	}
}

// Path parameters are the :named segments of the path. A port, a scheme, a digit and anything after
// the query or fragment are not.
func TestPathParamsAreTheNamedSegmentsOfTheAddress(t *testing.T) {
	cases := []struct {
		address string
		want    []string
	}{
		{"{{baseUrl}}/products/:product", []string{"product"}},
		{"{{baseUrl}}/attributes/:attribute/enums/:enum", []string{"attribute", "enum"}},
		{"https://api.acme.com/v1/users/:id/posts/:id", []string{"id"}},
		{"/groups/:attribute_group/destroy", []string{"attribute_group"}},
		{"/tenants/:tenant-id/items", []string{"tenant-id"}},
		{"/files/:id.json", []string{"id"}},
		{"http://localhost:8080/api", nil},
		{"https://host:443", nil},
		{"{{host}}:8080/api", nil},
		{"/api/v1:beta/items", nil},
		{"/items/:1", nil},
		{"/items/:", nil},
		{"localhost:3000", nil},
		{"/items?next=/:cursor", nil},
		{"/items#/:anchor", nil},
	}
	for _, c := range cases {
		if got := PathParams(c.address); !slices.Equal(got, c.want) {
			t.Errorf("PathParams(%q) = %v, want %v", c.address, got, c.want)
		}
	}
}

func TestATypedVariableInFrontOfThePathDoesNotTakeItsParametersAway(t *testing.T) {
	for _, address := range []string{"{{/users/:id", "{{host}}/users/:id", "{{ho/users/:id"} {
		if got := PathParams(address); !slices.Equal(got, []string{"id"}) {
			t.Errorf("PathParams(%q) = %v, want the id: a half-typed variable is plain text", address, got)
		}
	}
}

// FillPath writes each row's value, escaped as one segment, in place of its :name. A row with no
// value leaves the name, and a {{variable}} in a value is left for the resolver.
func TestFillPathWritesTheValueInPlaceOfTheName(t *testing.T) {
	for _, c := range []struct {
		address string
		rows    []Row
		want    string
	}{
		{"{{baseUrl}}/attributes/:attribute/enums/:enum?lang=en",
			[]Row{{Name: "attribute", Value: "color"}, {Name: "enum", Value: "red"}},
			"{{baseUrl}}/attributes/color/enums/red?lang=en"},
		{"/attributes/:attribute/enums/:enum", []Row{{Name: "attribute"}, {Name: "other", Value: "x"}},
			"/attributes/:attribute/enums/:enum"},
		{"/p/:product", []Row{{Name: "product", Value: "a b/c"}}, "/p/a%20b%2Fc"},
		{"/p/:product", []Row{{Name: "product", Value: "{{sku}}/x y"}}, "/p/{{sku}}%2Fx%20y"},
		{"/a/:id/b/:id", []Row{{Name: "id", Value: "7"}}, "/a/7/b/7"},
	} {
		if got := FillPath(c.address, c.rows); got != c.want {
			t.Errorf("FillPath(%q) = %q, want %q", c.address, got, c.want)
		}
	}
}

func TestPathParamNameKeepsWhatAnAddressCanCarry(t *testing.T) {
	cases := map[string]string{
		"product":         "product",
		"attribute_group": "attribute_group",
		"tenant-id":       "tenant-id",
		"item.id":         "item_id",
		"a b  c":          "a_b_c",
		"1st":             "_1st",
		"":                "_",
		"{x}":             "_x_",
	}
	for raw, want := range cases {
		if got := PathParamName(raw); got != want {
			t.Errorf("PathParamName(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestReplacePathParamsLeavesTheRestOfTheAddress(t *testing.T) {
	got := ReplacePathParams("{{baseUrl}}/a/:x/b?next=/:y#/:z", func(name string) string {
		return "{" + name + "}"
	})
	if want := "{{baseUrl}}/a/{x}/b?next=/:y#/:z"; got != want {
		t.Errorf("ReplacePathParams = %q, want %q", got, want)
	}
}

func TestLoopbackHostIsOnlyThisMachine(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1":          true,
		"127.255.255.254":    true,
		"::1":                true,
		"[::1]":              true,
		"::ffff:127.0.0.1":   true,
		"localhost":          true,
		"LOCALHOST.":         true,
		"foo.localhost":      false,
		"127.evil.com":       false,
		"127.0.0.1.nip.io":   false,
		"localhost.evil.com": false,
		"10.0.0.1":           false,
		"192.168.1.1":        false,
		"2130706433":         false,
		"":                   false,
		"example.com":        false,
	} {
		if got := LoopbackHost(host); got != want {
			t.Errorf("LoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestLocalNameAddsTheNamesUnderLocalhost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost":          true,
		"api.localhost":      true,
		"API.Localhost.":     true,
		"127.0.0.1":          true,
		"127.evil.com":       false,
		"localhost.evil.com": false,
		"notlocalhost":       false,
	} {
		if got := LocalName(host); got != want {
			t.Errorf("LocalName(%q) = %v, want %v", host, got, want)
		}
	}
}
