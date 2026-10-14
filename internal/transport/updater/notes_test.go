package updater

import (
	"slices"
	"testing"
)

// TestReleaseNotesTakesOnlyTheBullets verifies that only bulleted lines ('-' and '*')
// are extracted, while headings, plain prose, and unspaced dashes are ignored.
func TestReleaseNotesTakesOnlyTheBullets(t *testing.T) {
	body := `## Что нового

- Инспектор диффов: подсветка внутри массивов.
* Экспорт сохраняет порядок папок.

-
Обычный абзац, который ничего не перечисляет.
-Последняя строка без пробела после дефиса.`

	want := []string{
		"Инспектор диффов: подсветка внутри массивов.",
		"Экспорт сохраняет порядок папок.",
	}
	if got := releaseNotes(body); !slices.Equal(got, want) {
		t.Errorf("releaseNotes = %q, want %q", got, want)
	}
}

// TestReleaseNotesOnAProseOnlyBodyIsEmpty ensures that bodies containing only plain text
// without bullet points return no notes.
func TestReleaseNotesOnAProseOnlyBodyIsEmpty(t *testing.T) {
	if got := releaseNotes("Выпуск с исправлениями.\n\nПодробности в трекере."); got != nil {
		t.Errorf("releaseNotes = %q, want nothing", got)
	}
}

func TestReleaseNotesOnAnEmptyBody(t *testing.T) {
	if got := releaseNotes(""); got != nil {
		t.Errorf("releaseNotes = %q, want nothing", got)
	}
}
