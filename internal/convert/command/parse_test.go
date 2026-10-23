package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"schemo/internal/convert/command"
	"schemo/internal/domain"
)

func TestEveryOKResultIsUsable(t *testing.T) {
	t.Run("a command with no URL is not ok", func(t *testing.T) {
		for _, text := range []string{`curl ''`, `curl`, `curl --url '' -X POST`} {
			got := command.Parse(text)
			if got.Kind != command.KindError || got.Reason != command.ReasonNoURL {
				t.Errorf("Parse(%q) = %+v, want kind error with reason %q",
					text, got, command.ReasonNoURL)
			}
		}
	})

	t.Run("an explicitly empty method falls back to GET", func(t *testing.T) {
		got := command.Parse(`curl -X "" https://api.example.com/articles`)
		if got.Kind != command.KindOK {
			t.Fatalf(`Parse(curl -X "") = %+v, want kind ok`, got)
		}
		if got.Request.Method != "GET" {
			t.Errorf("method = %q, want GET", got.Request.Method)
		}
		if got.Request.URL != "https://api.example.com/articles" {
			t.Errorf("url = %q, want the URL that was written", got.Request.URL)
		}
	})
}

// A plain `-F` field is a multipart form and not a form-encoded body: the rows are the fields, the
// draft composes the multipart body from them, and the command goes out as it was meant to. (A
// value naming a file is refused instead — see the fixtures: this app cannot send an upload from a
// path, and a field quietly emptied is worse than a sentence saying so.)
func TestParseReadsPlainFormFieldsAsMultipart(t *testing.T) {
	got := command.Parse(`curl -F 'title=Hello' -F 'tag=news' https://api.example.com/articles`)
	if got.Kind != command.KindOK {
		t.Fatalf("Parse = %+v, want ok", got)
	}
	req := got.Request
	if req.BodyKind != domain.BodyForm || len(req.Form) != 2 {
		t.Fatalf("kind = %q with form %+v, want the fields as a multipart form", req.BodyKind, req.Form)
	}
	if req.Form[0].Name != "title" || req.Form[0].Value != "Hello" || !req.Form[0].Enabled {
		t.Errorf("form = %+v, want the first field enabled with its value", req.Form)
	}
	if req.Body != "" {
		t.Errorf("body = %q, want the fields and no text body", req.Body)
	}
	var contentType string
	for _, header := range req.Headers {
		if strings.EqualFold(header.Name, "Content-Type") {
			contentType = header.Value
		}
	}
	if contentType != "multipart/form-data" {
		t.Errorf("content-type = %q, want the form's", contentType)
	}
}

// curl reads `-XPOST`, `-uuser:pass` and `-sG` as flags written together: single-dash letters are
// flags one after another, and the first that takes a value takes the rest of the word. Dropping
// them would still come back ok, as a request the command never described.
func TestParseReadsFlagsWrittenTogether(t *testing.T) {
	for _, tc := range []struct {
		what   string
		text   string
		method string
		url    string
		body   string
	}{
		{"a method fused to its flag", `curl -XPUT https://api.example.com/a`, "PUT", "https://api.example.com/a", ""},
		{"a method after switches", `curl -sSLX DELETE https://api.example.com/a`, "DELETE", "https://api.example.com/a", ""},
		{"a method fused after switches", `curl -sSLXPATCH https://api.example.com/a`, "PATCH", "https://api.example.com/a", ""},
		{"data fused to its flag", `curl -d'a=1' https://api.example.com/a`, "POST", "https://api.example.com/a", "a=1"},
		{"switches that move the data into the query", `curl -sG https://api.example.com/a --data-urlencode 'q=x y'`,
			"GET", "https://api.example.com/a?q=x%20y", ""},
		{"a head request among switches", `curl -sIL https://api.example.com/a`, "HEAD", "https://api.example.com/a", ""},
	} {
		t.Run(tc.what, func(t *testing.T) {
			got := command.Parse(tc.text)
			if got.Kind != command.KindOK {
				t.Fatalf("Parse(%q) = %+v, want ok", tc.text, got)
			}
			req := got.Request
			if req.Method != tc.method || req.URL != tc.url || req.Body != tc.body {
				t.Errorf("Parse(%q) = %s %s %q, want %s %s %q",
					tc.text, req.Method, req.URL, req.Body, tc.method, tc.url, tc.body)
			}
		})
	}
}

// The value of a flag is never taken for the address, whichever flag it belongs to.
func TestParseSkipsTheValuesOfFlagsItDoesNotUse(t *testing.T) {
	got := command.Parse(`curl -D headers.txt -sSr 0-99 --max-redirs 3 https://api.example.com/a`)
	if got.Kind != command.KindOK || got.Request.URL != "https://api.example.com/a" {
		t.Errorf("Parse = %+v, want ok with the address alone", got)
	}
}

// A header and a login written against their flags reach the request like the spaced forms.
func TestParseReadsFusedHeaderAndLogin(t *testing.T) {
	got := command.Parse(`curl -H'Accept: text/plain' -uuser:secret https://api.example.com/a`)
	if got.Kind != command.KindOK {
		t.Fatalf("Parse = %+v, want ok", got)
	}
	var accept string
	for _, header := range got.Request.Headers {
		if strings.EqualFold(header.Name, "Accept") {
			accept = header.Value
		}
	}
	if accept != "text/plain" {
		t.Errorf("Accept = %q, want the header that was written", accept)
	}
	auth := got.Request.Auth
	if auth == nil || auth.Type != domain.AuthBasic ||
		auth.Answer("username") != "user" || auth.Answer("password") != "secret" {
		t.Errorf("auth = %+v, want basic with the login that was written", auth)
	}
}

func TestAPastedCredentialReachesTheChip(t *testing.T) {
	for _, tc := range []struct {
		what string
		text string
		want domain.Auth
	}{
		{
			what: "basic, whose value is the two answers encoded together",
			text: `curl -H 'Authorization: Basic MTIzOjMyMQ==' https://api.example.com/articles`,
			want: domain.NewAuth(domain.AuthBasic).With("username", "123").With("password", "321"),
		},
		{
			what: "bearer, and case does not matter",
			text: `curl -H 'authorization: bearer written' https://api.example.com/articles`,
			want: domain.NewAuth(domain.AuthBearer).With("token", "written"),
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			got := command.Parse(tc.text)
			if got.Kind != command.KindOK {
				t.Fatalf("Parse = %+v, want ok", got)
			}
			if got.Request.Auth == nil {
				t.Fatal("auth = nil, want the scheme the header named")
			}
			if got.Request.Auth.Type != tc.want.Type {
				t.Errorf("auth type = %q, want %q", got.Request.Auth.Type, tc.want.Type)
			}
			for key, value := range tc.want.Fields {
				if got.Request.Auth.Answer(key) != value {
					t.Errorf("auth %s = %q, want %q", key, got.Request.Auth.Answer(key), value)
				}
			}
			// The row goes with it: left beside the scheme it would win over the chip, and editing
			// the chip would change nothing that goes out.
			for _, header := range got.Request.Headers {
				if strings.EqualFold(header.Name, "Authorization") {
					t.Errorf("headers still carry %q beside the scheme", header.Name)
				}
			}
		})
	}
}

// FuzzParse feeds arbitrary pastes to the reader. It asserts only what has to hold whatever the
// input is (no panic, and an `ok` result that carries a method and a URL), because everything else,
// down to the byte, is pinned by the corpus in testdata/parse. A fuzz-only assertion about, say,
// the header list would be a second, weaker specification of the same thing.
//
// The second half is what the fuzzer found: `curl -X "" URL` must not come back `ok` with an empty
// method. The reading answers it instead; see TestEveryOKResultIsUsable.
func FuzzParse(f *testing.F) {
	paths, err := filepath.Glob(filepath.Join("testdata", "parse", "*.cmd"))
	if err != nil {
		f.Fatalf("globbing parse fixtures: %v", err)
	}
	if len(paths) == 0 {
		f.Fatal("no parse fixtures to seed the fuzzer with")
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatalf("reading %s: %v", path, err)
		}
		f.Add(string(data))
	}

	f.Fuzz(func(t *testing.T, text string) {
		got := command.Parse(text)
		if got.Kind != command.KindOK {
			return
		}
		if got.Request.Method == "" {
			t.Fatalf("Parse(%q) = ok with an empty method", text)
		}
		if got.Request.URL == "" {
			t.Fatalf("Parse(%q) = ok with an empty URL", text)
		}
	})
}
