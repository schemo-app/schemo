package lens

import (
	"strings"
	"testing"

	"schemo/internal/domain"
)

// A table is read the way RFC 4180 reads it: a line break is a carriage return and a line feed, a
// quoted field may hold line breaks, and the byte order mark Excel writes is not part of the first
// name.
func TestATableFromASpreadsheetIsReadWhole(t *testing.T) {
	text := string(rune(0xfeff)) + "id,note\r\n1,\"line one\r\nline two\"\r\n2,plain\r\n"
	doc := mustOpen(t, "text/csv", text)

	note, err := doc.Node("$[0].note")
	if err != nil || !strings.Contains(note.Preview, "line one") || !strings.Contains(note.Preview, "line two") {
		t.Fatalf("note = %+v, %v, want the two lines in one cell", note, err)
	}
	if id, err := doc.Node("$[1].id"); err != nil || id.Preview != "2" {
		t.Errorf("id = %+v, %v, want the second row's id", id, err)
	}

	onSecondLine := doc.At(indexOf(text, "line two") + 1)
	if onSecondLine.Path != "$[0].note" {
		t.Errorf("a click in the second line of a cell = %q, want the cell", onSecondLine.Path)
	}
	onPlain := doc.At(indexOf(text, "plain") + 1)
	if onPlain.Path != "$[1].note" {
		t.Errorf("a click on %q = %q, want the cell of the last row", "plain", onPlain.Path)
	}
}

func TestACellKeepsTheBytesItLiesIn(t *testing.T) {
	const text = "name;city\n\"Ann\";Oslo\nBob;\"Rome\"\n"
	doc := mustOpen(t, "text/csv", text)

	for click, want := range map[string]string{
		"Ann":  "$[0].name",
		"Oslo": "$[0].city",
		"Bob":  "$[1].name",
		"Rome": "$[1].city",
	} {
		if got := doc.At(indexOf(text, click) + 1).Path; got != want {
			t.Errorf("a click on %q = %q, want %q", click, got, want)
		}
	}
	if city, _ := doc.Node("$[1].city"); city.Preview != "Rome" {
		t.Errorf("city = %+v, want the quotes taken off", city)
	}
}

// People write `id, name` for a table, and a name that starts with a space is not what they meant.
func TestABlankAfterTheDelimiterIsNotPartOfTheField(t *testing.T) {
	doc := mustOpen(t, "text/csv", "id, name, email\n1, Ann, ann@x.io\n2, Bob, bob@x.io\n")

	name, err := doc.Node("$[1].name")
	if err != nil || name.Preview != "Bob" {
		t.Errorf("name = %+v, %v, want the cell without its leading space", name, err)
	}
}

// A quote the RFC would refuse is text here: the response is read as it came.
func TestAQuoteInsideAnUnquotedFieldIsText(t *testing.T) {
	doc := mustOpen(t, "text/csv", "id,size\n1,5\" nail\n2,6\" nail\n")

	size, err := doc.Node("$[0].size")
	if err != nil || size.Preview != "5\" nail" {
		t.Errorf("size = %+v, %v, want the quote kept in the text", size, err)
	}
}

func TestATabSeparatedTableIsRead(t *testing.T) {
	doc := mustOpen(t, "text/tab-separated-values", "id\tname\n1\tAnn\n2\tBob\n")
	if name, err := doc.Node("$[1].name"); err != nil || name.Preview != "Bob" || name.Kind != domain.NodeString {
		t.Errorf("name = %+v, %v", name, err)
	}
}
