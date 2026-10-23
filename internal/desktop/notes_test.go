package desktop

import (
	"slices"
	"testing"
)

// Release notes are the bulleted lines of a release body; headings and prose are left out.
func TestReleaseNotesAreTheBullets(t *testing.T) {
	body := "## What's new\n\n- Diff inspector: highlighting inside arrays.\n* Export preserves folder order.\n\n-\n" +
		"Plain paragraph, without a list.\n-Last line without a space after the dash."
	want := []string{"Diff inspector: highlighting inside arrays.", "Export preserves folder order."}
	if got := ParseNotes(body); !slices.Equal(got, want) {
		t.Errorf("ParseNotes = %q, want %q", got, want)
	}
	for _, empty := range []string{"", "Bug fix release.\n\nDetails in the issue tracker."} {
		if got := ParseNotes(empty); got != nil {
			t.Errorf("ParseNotes(%q) = %q, want nothing", empty, got)
		}
	}
}
