package transfer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"schemo/internal/command"
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

// A paste that is not a command is not a failure, and it is not a draft either: the window has to
// be able to put the text in the field, which it cannot do if this side has already replaced the
// draft with something. Most pastes into an address field are exactly this.

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
// input is — no panic, and an `ok` result that carries a method and a URL — because everything
// else, down to the byte, is pinned by the corpus in testdata/parse. A fuzz-only assertion about,
// say, the header list would be a second, weaker specification of the same thing.
//
// The second half is what the fuzzer earned its keep on: `curl -X "" URL` used to come back as
// `ok` with an empty method, exactly as the TypeScript does. The reading answers it instead — see
// TestEveryOKResultIsUsable.

func FuzzParse(f *testing.F) {
	paths, err := filepath.Glob(filepath.Join("../../command/testdata", "parse", "*.cmd"))
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
