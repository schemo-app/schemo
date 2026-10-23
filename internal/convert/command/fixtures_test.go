package command_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"schemo/internal/convert/command"
	"schemo/internal/domain"
)

// The corpus in testdata is pinned byte for byte: a case made to pass by editing its expectation
// is a regression.

// wantParse is the shape a `.want.json` has: a request on `ok`, a reason on `error`, nothing on
// `none`.
type wantParse struct {
	Kind    string     `json:"kind"`
	Format  string     `json:"format"`
	Method  string     `json:"method"`
	URL     string     `json:"url"`
	Headers [][]string `json:"headers"`
	Body    string     `json:"body"`
	Reason  string     `json:"reason"`
	// Auth is the scheme a command's flags come to, which is not a header and is not in `headers`:
	// `<type>` and the answers to its fields, as the fixture writes them out.
	Auth *wantAuth `json:"auth"`
}

type wantAuth struct {
	Type   string            `json:"type"`
	Fields map[string]string `json:"fields"`
}

type wantExport struct {
	Format  string     `json:"format"`
	Method  string     `json:"method"`
	URL     string     `json:"url"`
	Headers [][]string `json:"headers"`
	Body    string     `json:"body"`
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
}

// TestParseFixtures reads every paste in testdata/parse and compares the result: the kind, and
// on `ok` the format, method, URL, body and the header sequence — names, values and order together.
func TestParseFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "parse", "*.cmd"))
	if err != nil {
		t.Fatalf("globbing parse fixtures: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no parse fixtures found")
	}

	counts := map[string]int{}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".cmd")
		// The input is read as bytes: the corpus contains CRLF, a BOM and $'…', and normalising any
		// of them here would test something other than the parser.
		input, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var want wantParse
		readJSON(t, filepath.Join("testdata", "parse", name+".want.json"), &want)
		counts[want.Kind]++

		t.Run(name, func(t *testing.T) {
			got := command.Parse(string(input))
			switch want.Kind {
			case "none":
				if got.Kind != command.KindNone {
					t.Fatalf("Parse(%q) = %+v, want kind none", input, got)
				}
			case "ok":
				if got.Kind != command.KindOK {
					t.Fatalf("Parse(%q) = %+v, want kind ok", input, got)
				}
				// The reader knows one tool, and a fixture of another format surviving the cut to curl is
				// a regression in `detect` rather than a corpus that needs editing.
				if want.Format != "curl" {
					t.Fatalf("%s: a %q fixture is still expected to parse", name, want.Format)
				}
				assertRequest(t, got.Request, want)
			case "error":
				if got.Kind != command.KindError {
					t.Fatalf("Parse(%q) = %+v, want kind error", input, got)
				}
				if string(got.Reason) != want.Reason {
					t.Errorf("Parse(%q) reason = %q, want %q", input, got.Reason, want.Reason)
				}
			default:
				t.Fatalf("%s has an unknown kind %q", name, want.Kind)
			}
		})
	}
	t.Logf("parse fixtures: %d ok, %d none, %d error", counts["ok"], counts["none"], counts["error"])
}

func assertRequest(t *testing.T, got command.Request, want wantParse) {
	t.Helper()
	if got.Method != want.Method {
		t.Errorf("method = %q, want %q", got.Method, want.Method)
	}
	if got.URL != want.URL {
		t.Errorf("url = %q, want %q", got.URL, want.URL)
	}
	if got.Body != want.Body {
		t.Errorf("body = %q, want %q", got.Body, want.Body)
	}
	assertHeaders(t, got.Headers, want.Headers)
	assertAuth(t, got.Auth, want.Auth)
}

// A fixture that names no scheme means no credential; a parse that invents one fails here.
func assertAuth(t *testing.T, got *domain.Auth, want *wantAuth) {
	t.Helper()
	if (got == nil) != (want == nil) {
		t.Fatalf("auth = %+v, want %+v", got, want)
	}
	if got == nil {
		return
	}
	if string(got.Type) != want.Type {
		t.Errorf("auth type = %q, want %q", got.Type, want.Type)
	}
	for key, value := range want.Fields {
		if got.Answer(key) != value {
			t.Errorf("auth %s = %q, want %q", key, got.Answer(key), value)
		}
	}
}

// Two requests with the same headers in a different order are not the same request.
func assertHeaders(t *testing.T, got []domain.HeaderPair, want [][]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("headers = %s, want %s", formatHeaders(got), formatWantHeaders(want))
	}
	for i := range want {
		if len(want[i]) != 2 {
			t.Fatalf("fixture header %d is not a name/value pair: %v", i, want[i])
		}
		if got[i].Name != want[i][0] || got[i].Value != want[i][1] {
			t.Errorf("header %d = %q: %q, want %q: %q",
				i, got[i].Name, got[i].Value, want[i][0], want[i][1])
		}
	}
}

func formatHeaders(hs []domain.HeaderPair) string {
	parts := make([]string, 0, len(hs))
	for _, h := range hs {
		parts = append(parts, h.Name+": "+h.Value)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func formatWantHeaders(hs [][]string) string {
	parts := make([]string, 0, len(hs))
	for _, h := range hs {
		parts = append(parts, strings.Join(h, ": "))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// The expected text is taken as it is — no trailing newline added or trimmed.
func TestExportFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "export", "*.in.json"))
	if err != nil {
		t.Fatalf("globbing export fixtures: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no export fixtures found")
	}

	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".in.json")
		var in wantExport
		readJSON(t, path, &in)
		want, err := os.ReadFile(filepath.Join("testdata", "export", name+".want.txt"))
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}

		t.Run(name, func(t *testing.T) {
			got, omitted := command.Render(command.Format(in.Format), requestFrom(in))
			if got != string(want) {
				t.Errorf("Render(%s) =\n%q\nwant\n%q", in.Format, got, string(want))
			}
			// Every case in the corpus is text and nothing else, so a command that could not write one
			// of them whole would mean the writer is dropping bodies it should write.
			if len(omitted) != 0 {
				t.Errorf("Render(%s) omitted %v, want nothing left out", in.Format, omitted)
			}
		})
	}
}

// Keeps the header order the fixture lists.
func requestFrom(in wantExport) command.Request {
	req := command.Request{Method: in.Method, URL: in.URL, Body: in.Body}
	for _, pair := range in.Headers {
		if len(pair) != 2 {
			continue
		}
		req.Headers = append(req.Headers, domain.HeaderPair{Name: pair[0], Value: pair[1]})
	}
	return req
}

// TestRoundTrip exports every command the corpus parses and parses it back: the two halves are each
// other's inverse, which keeps exporter and parser from drifting apart.
func TestRoundTrip(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "parse", "*.cmd"))
	if err != nil {
		t.Fatalf("globbing parse fixtures: %v", err)
	}

	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".cmd")
		input, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}

		t.Run(name, func(t *testing.T) {
			first := command.Parse(string(input))
			if first.Kind != command.KindOK {
				// A paste that is not a command has nothing to round-trip.
				return
			}

			exported, _ := command.Render(command.FormatCurl, first.Request)
			second := command.Parse(exported)
			if second.Kind != command.KindOK {
				t.Fatalf("re-parsing the export of %s = %+v; export was %q", name, second, exported)
			}
			if second.Request.Method != first.Request.Method {
				t.Errorf("method = %q, want %q (export was %q)",
					second.Request.Method, first.Request.Method, exported)
			}
			if second.Request.URL != first.Request.URL {
				t.Errorf("url = %q, want %q (export was %q)",
					second.Request.URL, first.Request.URL, exported)
			}
			if second.Request.Body != first.Request.Body {
				t.Errorf("body = %q, want %q (export was %q)",
					second.Request.Body, first.Request.Body, exported)
			}
			if len(second.Request.Headers) != len(first.Request.Headers) {
				t.Fatalf("headers = %s, want %s (export was %q)",
					formatHeaders(second.Request.Headers), formatHeaders(first.Request.Headers), exported)
			}
			for i := range first.Request.Headers {
				if second.Request.Headers[i] != first.Request.Headers[i] {
					t.Errorf("header %d = %v, want %v (export was %q)",
						i, second.Request.Headers[i], first.Request.Headers[i], exported)
				}
			}
		})
	}
}

// Headers and JSON keys keep their written order, names that look like numbers included. The corpus
// has no such case, so this test is the only place the rule is written down.
func TestWrittenOrder(t *testing.T) {
	parsed := command.Parse("curl -H '2: x' -H '10: y' -H '1: z' -H 'A: w' " +
		"https://api.example.com/articles")
	if parsed.Kind != command.KindOK {
		t.Fatalf("Parse = %+v, want ok", parsed)
	}
	wantHeaders := [][]string{{"2", "x"}, {"10", "y"}, {"1", "z"}, {"A", "w"}}
	assertHeaders(t, parsed.Request.Headers, wantHeaders)

	exported, _ := command.Render(command.FormatCurl, parsed.Request)
	wantExport := "curl -H '2: x' -H '10: y' -H '1: z' -H 'A: w' 'https://api.example.com/articles'"
	if exported != wantExport {
		t.Errorf("Export = %q, want %q", exported, wantExport)
	}

	// The rule survives the round trip: what the reader put in that order is what the command
	// writes back out, so a copy of a copy is the same request.
	again := command.Parse(exported)
	if again.Kind != command.KindOK {
		t.Fatalf("Parse(%q) = %+v, want ok", exported, again)
	}
	assertHeaders(t, again.Request.Headers, wantHeaders)
}

// TestExportCollapsesRepeatedHeaders pins that a name appearing twice keeps the position of its
// first occurrence and the value of its last.
func TestExportCollapsesRepeatedHeaders(t *testing.T) {
	req := command.Request{
		Method: "GET",
		URL:    "https://api.example.com/articles",
		Headers: []domain.HeaderPair{
			{Name: "Accept", Value: "application/json"},
			{Name: "X-B", Value: "2"},
			{Name: "Accept", Value: "application/vnd.api+json"},
		},
	}
	want := "curl -H 'Accept: application/vnd.api+json' -H 'X-B: 2' " +
		"'https://api.example.com/articles'"
	if got, _ := command.Render(command.FormatCurl, req); got != want {
		t.Errorf("Export = %q, want %q", got, want)
	}
}
