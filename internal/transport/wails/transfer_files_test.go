package wails

import (
	"strings"
	"testing"

	"schemo/internal/usecase/transfer"
)

// Every format the dialogs offer has a filter of its own and an ending of its own: a save dialog
// that opened on *.json for a YAML document, or suggested a name that says nothing about the tool
// it is for, would be the window writing the wrong file.
func TestEveryFormatHasItsOwnFilterAndEnding(t *testing.T) {
	seen := map[string]transfer.Format{}
	for _, format := range []transfer.Format{
		transfer.FormatSchemo,
		transfer.FormatPostman,
		transfer.FormatOpenAPI,
		transfer.FormatInsomnia,
		transfer.FormatHAR,
	} {
		kind := kindOf(format)
		if kind.name == "" || kind.pattern == "" || kind.suffix == "" {
			t.Errorf("%s: kind = %+v, want a name, a pattern and an ending", format, kind)
		}
		if !strings.HasPrefix(kind.suffix, ".") {
			t.Errorf("%s: ending = %q, want a file ending", format, kind.suffix)
		}
		if previous, clash := seen[kind.suffix]; clash {
			t.Errorf("%s and %s share the ending %q", previous, format, kind.suffix)
		}
		seen[kind.suffix] = format

		filter := filterOf(format)
		if len(filter) != 1 || filter[0].Pattern != kind.pattern ||
			filter[0].DisplayName != kind.name {
			t.Errorf("%s: filter = %+v, want the format's own kind", format, filter)
		}
	}
}

// A paste of commands is not a file, and the cURL format names no kind of file at all.
func TestCurlOffersNoFileName(t *testing.T) {
	if kind := kindOf(transfer.FormatCurl); kind.suffix != "" {
		t.Errorf("ending = %q, want none for a paste", kind.suffix)
	}
}
