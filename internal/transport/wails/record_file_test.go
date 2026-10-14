package wails

import (
	"strings"
	"testing"
)

// A session saved from a page the user was looking at opens its dialog on a name that page can
// be recognised by, and no file system refuses. `/` and `:` are legal in a host and not a file's.
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

// A session is offered as its own kind of file: a HAR is what devtools and proxies read by name,
// and a dialog that called it JSON would give the user a file their tools open as nothing.
func TestHarFileKind(t *testing.T) {
	if harKindName != "HAR" || harPattern != "*.har" {
		t.Errorf("the dialog offers %s (%s), want the file kind", harKindName, harPattern)
	}
	if len(harFilter) != 1 || harFilter[0].Pattern != harPattern {
		t.Errorf("filter = %+v, want one entry for the kind of file", harFilter)
	}
	if !strings.HasSuffix(harFileName("session"), harExtension) {
		t.Errorf("a suggested name does not end in %q", harExtension)
	}
}
