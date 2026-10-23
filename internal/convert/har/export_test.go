package har

import (
	"encoding/json"
	"slices"
	"testing"

	"schemo/internal/domain"
)

func micros(us int64) *int64 { return &us }

func sample() Entry {
	return Entry{
		Record: domain.Record{
			RecordSummary: domain.RecordSummary{
				ID:          "rec-1",
				Source:      domain.SourceBrowser,
				Method:      "POST",
				URL:         "https://api.example.com/articles?include=author&page[size]=25",
				Status:      201,
				StatusText:  "201 Created",
				ContentType: "application/vnd.api+json",
				DurationUs:  120_000,
				StartedAt:   1_700_000_000_000,
				TabTitle:    "Blog demo",
			},
			WaitUs:        micros(90_000),
			DownloadUs:    micros(30_000),
			RequestBytes:  17,
			ResponseBytes: 42,
			RequestHeaders: []domain.HeaderPair{
				{Name: "Content-Type", Value: "application/json"},
				{Name: "Cookie", Value: "session=abc"},
			},
			ResponseHeaders: []domain.HeaderPair{
				{Name: "Set-Cookie", Value: "session=abc; Path=/; HttpOnly"},
			},
		},
		RequestBody:  `{"data":{"type":"articles"}}`,
		ResponseBody: `{"data":{"id":"1"}}`,
	}
}

// What a HAR file says about a request: the two halves, their bodies, and the phases the browser
// timed. Everything a reader needs to follow the request through, in the shape the format asks for.
func TestExportWritesTheEntry(t *testing.T) {
	data, err := Export("Schemo", "2.4.0", []Entry{sample()})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("the document is not JSON")
	}

	var doc logFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if doc.Log.Version != Version || doc.Log.Creator.Name != "Schemo" {
		t.Errorf("log = %+v, want the version and the app that wrote it", doc.Log)
	}
	if len(doc.Log.Entries) != 1 {
		t.Fatalf("entries = %d, want one", len(doc.Log.Entries))
	}

	entry := doc.Log.Entries[0]
	if entry.Request.Method != "POST" || entry.Request.PostData == nil ||
		entry.Request.PostData.Text != `{"data":{"type":"articles"}}` {
		t.Errorf("request = %+v, want the post with its body", entry.Request)
	}
	if entry.Response.Status != 201 || entry.Response.Content.Text != `{"data":{"id":"1"}}` {
		t.Errorf("response = %+v, want the answer with its body", entry.Response)
	}
	if len(entry.Response.Cookies) != 1 || entry.Response.Cookies[0].Name != "session" {
		t.Errorf("response cookies = %+v, want the set-cookie read back", entry.Response.Cookies)
	}
	if len(entry.Request.QueryString) != 2 {
		t.Errorf("query = %+v, want the two parameters of the address", entry.Request.QueryString)
	}
	if entry.Time != 120 {
		t.Errorf("time = %v ms, want the duration in milliseconds", entry.Time)
	}
}

// The query string of a HAR entry is the request's own: in the order it was sent, a name once for
// each time it was sent, and decoded.
func TestQueryStringKeepsTheOrderAndTheRepeats(t *testing.T) {
	entry := sample()
	entry.Record.URL = "https://api.example.com/search?b=2&a=1&b=3&q=a+b%21&flag#frag?x=1"

	data, err := Export("Schemo", "2.4.0", []Entry{entry})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var doc logFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	want := []nameValue{
		{Name: "b", Value: "2"}, {Name: "a", Value: "1"}, {Name: "b", Value: "3"},
		{Name: "q", Value: "a b!"}, {Name: "flag"},
	}
	if got := doc.Log.Entries[0].Request.QueryString; !slices.Equal(got, want) {
		t.Errorf("query = %+v, want %+v", got, want)
	}
}

// A Cookie header is read as RFC 6265 §4.2.1 has it, and one that is not a cookie list names none.
func TestCookiesOfReadsTheHeaderOfARequest(t *testing.T) {
	got := cookiesOf("a=1; b=two; a=3")
	want := []cookie{{Name: "a", Value: "1"}, {Name: "b", Value: "two"}, {Name: "a", Value: "3"}}
	if !slices.Equal(got, want) {
		t.Errorf("cookiesOf = %+v, want %+v", got, want)
	}
	if got := cookiesOf("not a cookie"); len(got) != 0 {
		t.Errorf("cookiesOf(garbage) = %+v, want none", got)
	}
}

// What nobody measured is not zero. A phase the app never timed gets HAR's -1, which says "this did
// not happen" — a zero would say it happened instantly, and the two are different claims.
func TestUnmeasuredPhasesAreMinusOne(t *testing.T) {
	data, err := Export("Schemo", "2.4.0", []Entry{sample()})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	var doc logFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("reading back: %v", err)
	}

	timings := doc.Log.Entries[0].Timings
	if timings.DNS != unknown || timings.Connect != unknown || timings.SSL != unknown {
		t.Errorf("timings = %+v, want -1 for the phases nothing measured", timings)
	}
	if timings.Blocked != unknown || timings.Send != unknown {
		t.Errorf("timings = %+v, want -1 for the phases nothing times at all", timings)
	}
	if timings.Wait != 90 || timings.Receive != 30 {
		t.Errorf("timings = %+v, want the two the browser timed", timings)
	}
}

// The reason phrase is the words after the code. A record this app sent kept the whole status line,
// and a file that repeated the number twice would be a file somebody has to correct by hand.
func TestStatusTextIsTheReasonAlone(t *testing.T) {
	for _, one := range []struct {
		statusText string
		status     int
		want       string
	}{
		{"200 OK", 200, "OK"},
		{"OK", 200, "OK"},
		{"", 0, ""},
		{"network error", 0, "network error"},
	} {
		if got := reasonPhrase(one.statusText, one.status); got != one.want {
			t.Errorf("reasonPhrase(%q, %d) = %q, want %q", one.statusText, one.status, got, one.want)
		}
	}
}

// A session with nothing in it is still a HAR file: an empty list of entries is what says "nothing
// was captured", and refusing to write one would leave the user with no file and no answer.
func TestExportOfNothingIsAnEmptyDocument(t *testing.T) {
	data, err := Export("Schemo", "2.4.0", nil)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	var doc logFile
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if doc.Log.Entries == nil || len(doc.Log.Entries) != 0 {
		t.Errorf("entries = %+v, want an empty list rather than null", doc.Log.Entries)
	}
	if data[len(data)-1] != '\n' {
		t.Error("the file does not end with a newline")
	}
}
