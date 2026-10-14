package desktop

import (
	"net/url"
	"testing"
)

// The link a page offers names the page, escaped as a query value carries it: what the window is
// told is the address as it stands, because the side that fetches it is the side that has to know
// what it is.
func TestAnImportLinkNamesThePageItCarries(t *testing.T) {
	parsed, err := url.Parse("schemo://import?url=" +
		url.QueryEscape("https://pages.example/petstore"))
	if err != nil {
		t.Fatalf("parsing the link: %v", err)
	}

	if page := importedPage(parsed); page != "https://pages.example/petstore" {
		t.Errorf("page = %q, want the address the link carried", page)
	}
}

// Every other link this app answers is not an import: a tab opened from outside must not arrive as
// a collection to take in.
func TestALinkOfAnotherKindCarriesNoPage(t *testing.T) {
	for _, raw := range []string{"schemo://open?tab=2", "schemo://import",
		"schemo://import?url=", "schemo://import?tab=2"} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parsing %s: %v", raw, err)
		}
		if page := importedPage(parsed); page != "" {
			t.Errorf("%s: page = %q, want nothing to import from it", raw, page)
		}
	}
}
