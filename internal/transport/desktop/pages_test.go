package desktop

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The pages this package opens are the entries Vite builds: a name in one list and not the other is
// a window that opens blank, and nothing but running the app would say so. The file is read as
// text, as the catalogue is in catalogue_test.go — what it holds is something this side cannot ask.
func TestEveryWindowPageIsBuilt(t *testing.T) {
	entries := viteEntries(t)
	for _, page := range []string{pageAbout, pageUpdate, pageSettings} {
		name := strings.TrimSuffix(strings.TrimPrefix(page, "/"), ".html")
		if !entries[name] {
			t.Errorf("%s: opened as a window and not built by Vite (frontend/vite.config.ts)", page)
		}
		if _, err := os.Stat("../../../frontend/" + name + ".html"); err != nil {
			t.Errorf("%s: the page itself is missing: %v", page, err)
		}
	}
}

// viteEntries is the `input` map of frontend/vite.config.ts: the pages the window is built from.
func viteEntries(t *testing.T) map[string]bool {
	t.Helper()

	held, err := os.ReadFile("../../../frontend/vite.config.ts")
	if err != nil {
		t.Fatalf("reading vite.config.ts: %v", err)
	}
	block := regexp.MustCompile(`(?s)input:\s*\{(.*?)\}`).FindStringSubmatch(string(held))
	if block == nil {
		t.Fatal("vite.config.ts has no `input:` map: this test cannot say what is built")
	}

	out := map[string]bool{}
	for _, one := range regexp.MustCompile(`(\w+)\s*:`).FindAllStringSubmatch(block[1], -1) {
		out[one[1]] = true
	}
	if len(out) < 3 {
		t.Fatalf("read %d entries out of vite.config.ts: too few to be its list", len(out))
	}
	return out
}
