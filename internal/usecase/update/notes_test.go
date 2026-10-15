package update

import (
	"slices"
	"testing"
)

// TestReleaseNotesTakesOnlyTheBullets verifies that only bulleted lines ('-' and '*')
// are extracted, while headings, plain prose, and unspaced dashes are ignored.
func TestReleaseNotesTakesOnlyTheBullets(t *testing.T) {
	body := `## What's new

- Diff inspector: highlighting inside arrays.
* Export preserves folder order.

-
Plain paragraph, without a list.
-Last line without a space after the dash.`

	want := []string{
		"Diff inspector: highlighting inside arrays.",
		"Export preserves folder order.",
	}
	if got := ParseNotes(body); !slices.Equal(got, want) {
		t.Errorf("releaseNotes = %q, want %q", got, want)
	}
}

// TestReleaseNotesOnAProseOnlyBodyIsEmpty ensures that bodies containing only plain text
// without bullet points return no notes.
func TestReleaseNotesOnAProseOnlyBodyIsEmpty(t *testing.T) {
	if got := ParseNotes("Bug fix release.\n\nDetails in the issue tracker."); got != nil {
		t.Errorf("releaseNotes = %q, want nothing", got)
	}
}

func TestReleaseNotesOnAnEmptyBody(t *testing.T) {
	if got := ParseNotes(""); got != nil {
		t.Errorf("releaseNotes = %q, want nothing", got)
	}
}
