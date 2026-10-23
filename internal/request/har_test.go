package request

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"schemo/internal/domain"
)

// A saved session holds the traffic of one tab and nobody else's: the file is what somebody
// attaches to a report about a page, and requests from the tabs beside it are a different story.
func TestExportHARTakesOneTab(t *testing.T) {
	history, store, _, _ := newHistory(t)
	ctx := context.Background()

	first := browserRecord("rec-1", 7, "https://api.example.com/first")
	first.ResponseBody = bodyRef(`{"data":[]}`, false)
	second := browserRecord("rec-2", 7, "https://api.example.com/second")
	second.StartedAt++
	second.ResponseBody = bodyRef(`{"data":[1]}`, false)
	store.put(first, second, browserRecord("other", 8, "https://api.example.com/elsewhere"))

	data, name, err := history.har(ctx, "7")
	if err != nil {
		t.Fatalf("ExportHAR: %v", err)
	}
	if name != "api.example.com" {
		t.Errorf("name = %q, want the page's host", name)
	}

	var doc struct {
		Log struct {
			Entries []struct {
				Request struct{ URL string } `json:"request"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if len(doc.Log.Entries) != 2 {
		t.Fatalf("entries = %d, want the two of that tab", len(doc.Log.Entries))
	}
	// Oldest first: a session is a file about the order things happened in, and the list it is built
	// from is newest-first because that is what a panel shows.
	if doc.Log.Entries[0].Request.URL != "https://api.example.com/first" {
		t.Errorf("first entry = %q, want the earliest request", doc.Log.Entries[0].Request.URL)
	}
}

// The seventh tab of another device is not the seventh tab of this one: a tab's number counts only
// where the record was captured, so an export of one device's session holds its own traffic alone.
func TestAnExportOfATabLeavesAnotherDevicesTabAlone(t *testing.T) {
	history, store, _, _ := newHistory(t)
	ctx := context.Background()

	mine := browserRecord("mine", 7, "https://api.example.com/first")
	theirs := browserRecord("theirs", 7, "https://api.example.com/elsewhere")
	theirs.IsRemote = true
	mine.ResponseBody = bodyRef(`{"data":[]}`, false)
	theirs.ResponseBody = bodyRef(`{"data":[]}`, false)
	store.put(mine, theirs)

	entries := func(key string) []string {
		t.Helper()
		data, _, err := history.har(ctx, key)
		if err != nil {
			t.Fatalf("ExportHAR(%q): %v", key, err)
		}
		var doc harDocument
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("reading back: %v", err)
		}
		var urls []string
		for _, entry := range doc.Log.Entries {
			var one struct {
				Request struct{ URL string } `json:"request"`
			}
			if err := json.Unmarshal(entry, &one); err != nil {
				t.Fatalf("reading an entry back: %v", err)
			}
			urls = append(urls, one.Request.URL)
		}
		return urls
	}

	if urls := entries("7"); len(urls) != 1 || urls[0] != "https://api.example.com/first" {
		t.Errorf("this device's tab seven holds %v, want its own request alone", urls)
	}
	if urls := entries("remote:7"); len(urls) != 1 ||
		urls[0] != "https://api.example.com/elsewhere" {
		t.Errorf("their tab seven holds %v, want the request their device captured", urls)
	}
}

// A body this device does not hold is fetched as the file is written: an export of a session that
// lives on the server is a file about the session, not a file of records with nothing in them.
func TestAnExportFetchesABodyItDoesNotHold(t *testing.T) {
	source := &fakeSource{fetched: domain.RecordFetched{
		Bodies: map[domain.BodySide]domain.BodyRef{
			domain.SideResponse: {Inline: `{"data":[1]}`, Size: 12},
		},
	}}
	history, store := newFetchingHistory(t, source)
	theirs := browserRecord("theirs", 9, "https://api.example.com/elsewhere")
	theirs.IsRemote = true
	store.put(theirs)

	data, _, err := history.har(context.Background(), "remote:9")
	if err != nil {
		t.Fatalf("ExportHAR: %v", err)
	}
	if len(source.asked) != 1 || source.asked[0] != "theirs" {
		t.Fatalf("asked %v, want the body fetched for the record held elsewhere", source.asked)
	}
	if !strings.Contains(string(data), `{\"data\":[1]}`) {
		t.Errorf("the file does not carry the fetched body: %s", data)
	}
}

// A tab with no traffic is a file with no entries rather than a refusal: the button was pressed,
// and an empty session is an answer about what was captured.
func TestExportHAROfATabWithNothing(t *testing.T) {
	history, store, _, _ := newHistory(t)
	store.put(browserRecord("rec-1", 7, "https://api.example.com/first"))

	data, _, err := history.har(context.Background(), "8")
	if err != nil {
		t.Fatalf("ExportHAR: %v", err)
	}

	var doc harDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if len(doc.Log.Entries) != 0 {
		t.Errorf("entries = %d, want none for a tab that captured nothing", len(doc.Log.Entries))
	}
}

// The smallest shape of the document this test reads: what it asserts is the count, and the rest of
// the file is the writer's business.
type harDocument struct {
	Log struct {
		Entries []json.RawMessage `json:"entries"`
	} `json:"log"`
}

func browserRecord(id string, tabID int, url string) domain.Record {
	return domain.Record{
		RecordSummary: domain.RecordSummary{
			ID: id, Source: domain.SourceBrowser, Method: "GET", URL: url, Status: 200,
			StartedAt: 1_700_000_000_000, TabID: tabID, TabTitle: "Blog demo",
			TabURL: "https://api.example.com/articles",
		},
	}
}

// A session saved from a page opens its dialog on a name that page can be recognised by.
func TestHarFileName(t *testing.T) {
	for name, want := range map[string]string{
		"api.example.com": "api.example.com.har",
		"localhost:8080":  "localhost-8080.har",
		"API/Prod: users": "API-Prod- users.har",
		"   ":             "session.har",
		`a\b*c?d"e<f>g|h`: "a-b-c-d-e-f-g-h.har",
	} {
		if got := harFileName(name); got != want {
			t.Errorf("harFileName(%q) = %q, want %q", name, got, want)
		}
	}
}
