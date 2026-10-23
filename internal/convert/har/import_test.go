package har

import (
	"errors"
	"os"
	"testing"

	"schemo/internal/domain"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return data
}

// An entry is a request: the method, the address as it was sent, the headers without HTTP/2's own
// pseudo-headers, and the query both in the address and in the rows the editor draws.
func TestImportReadsAnEntryAsARequest(t *testing.T) {
	doc, err := Import(fixture(t, "session.har"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(doc.Collections) != 1 {
		t.Fatalf("collections = %d, want one", len(doc.Collections))
	}
	collection := doc.Collections[0]
	// One flat level: every entry is a row of the collection itself, in the order the log recorded
	// them, and the host is read off each address rather than repeated as a folder.
	if len(collection.Children) != 0 || len(collection.Items) != 4 {
		t.Fatalf("collection = %+v, want the four entries flat", collection)
	}

	list := collection.Items[0]
	if list.Method != "GET" ||
		list.URL != "https://api.example.com/v1/products?page[size]=25&include=offers" {
		t.Errorf("request = %+v, want the method and the address as sent", list)
	}
	if list.Name != list.URL {
		t.Errorf("name = %q, want the address as the name", list.Name)
	}
	if len(list.Headers) != 2 || list.Headers[0].Name != "Accept" || list.Headers[1].Name != "Cookie" {
		t.Errorf("headers = %+v, want the pseudo-headers dropped", list.Headers)
	}
	if len(list.Params) != 2 || !list.Params[0].Enabled || list.Params[0].Name != "page[size]" {
		t.Errorf("params = %+v, want what the address carried", list.Params)
	}
}

// A body the log recorded as text arrives as the text it was, in the kind its media type says.
func TestImportReadsAPostedBody(t *testing.T) {
	doc, err := Import(fixture(t, "session.har"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	created := doc.Collections[0].Items[1]
	if created.Body != "{\"data\":{\"type\":\"cart-items\"}}" || created.BodyKind != domain.BodyRaw {
		t.Errorf("body = %+v, want the text the log holds", created)
	}
	if created.Name != "https://api.example.com/v1/cart/items" {
		t.Errorf("name = %q, want the address as the name", created.Name)
	}
}

// A form body becomes the form it means, and a field that carried a file keeps its name with the
// file left behind: a path in a log is where the browser read it, not something this app can send.
func TestImportReadsAMultipartBody(t *testing.T) {
	doc, err := Import(fixture(t, "session.har"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	upload := doc.Collections[0].Items[2]
	if upload.BodyKind != domain.BodyForm || len(upload.Form) != 2 {
		t.Fatalf("body = %+v, want the two form fields", upload)
	}
	if upload.Form[0].Name != "note" || upload.Form[0].Value != "hello" || upload.Form[0].File {
		t.Errorf("field = %+v, want a plain value", upload.Form[0])
	}
	if upload.Form[1].Name != "report" || !upload.Form[1].File || upload.Form[1].Value != "" {
		t.Errorf("field = %+v, want the file field kept as its name", upload.Form[1])
	}

	var binary, cookies bool
	for _, skip := range doc.Skipped {
		switch skip.Code {
		case domain.CodeSkippedBinaryFile:
			binary = true
		case domain.CodeSkippedCookieJar:
			cookies = true
		}
	}
	if !binary || !cookies {
		t.Errorf("skipped = %+v, want the file and the cookies counted", doc.Skipped)
	}
}

// A body stored as base64 is left out rather than decoded into something a request would send as
// text, and the file says what was left behind.
func TestImportLeavesABase64BodyOut(t *testing.T) {
	doc, err := Import(fixture(t, "session.har"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	raw := doc.Collections[0].Items[3]
	if raw.Body != "" {
		t.Errorf("body = %q, want the encoded body left out", raw.Body)
	}
	var encoded bool
	for _, skip := range doc.Skipped {
		encoded = encoded || skip.Code == domain.CodeSkippedBase64Body
	}
	if !encoded {
		t.Errorf("skipped = %+v, want the encoded body counted", doc.Skipped)
	}
}

func TestImportRefusesAFileThatIsNotAHAR(t *testing.T) {
	for name, data := range map[string]string{
		"some json": `{"openapi": "3.0.0"}`,
		"not json":  `curl -X GET https://example.com`,
	} {
		if _, err := Import([]byte(data)); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("%s: Import = %v, want ErrNotAllowed", name, err)
		}
	}
	if _, err := Import(fixture(t, "session.har")); err != nil {
		t.Fatalf("the fixture must read: %v", err)
	}
}

// The collection is named the constant Hoppscotch names it by, whatever the log says about itself:
// a log has no name of its own, and the tool that wrote it named nothing either.
func TestImportNamesTheCollectionTheSameEveryTime(t *testing.T) {
	one := []byte(`{"log":{"version":"1.2","creator":{"name":"Charles"},"entries":[
		{"request":{"method":"GET","url":"https://only.example.com/a","headers":[],"queryString":[]}}]}}`)
	doc, err := Import(one)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got := doc.Collections[0].Name; got != "Imported from HAR" {
		t.Errorf("name = %q, want the constant", got)
	}
}
