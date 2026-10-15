package draft

import (
	"context"
	"testing"

	"schemo/internal/command"
	"schemo/internal/domain"
)

func TestPastingSomethingElseLeavesTheDraftAlone(t *testing.T) {
	uc, _ := newUseCase()
	for _, text := range []string{"https://api.example.com/articles", "plain text", ""} {
		pasted, err := uc.PasteCommand(context.Background(), domain.DraftCommandLine, text)
		if err != nil {
			t.Fatalf("PasteCommand(%q): %v", text, err)
		}
		if pasted.Reading.Kind != command.KindNone {
			t.Errorf("PasteCommand(%q) = %q, want none", text, pasted.Reading.Kind)
		}
		if pasted.State != nil {
			t.Errorf("PasteCommand(%q) replaced the draft with %+v", text, pasted.State)
		}
	}
}

// A command is a whole request rather than an edit to one: it goes over the draft, and the reading
// comes back so the window can say so.

func TestPastingACommandReplacesTheDraft(t *testing.T) {
	uc, _ := newUseCase()
	pasted, err := uc.PasteCommand(context.Background(), domain.DraftCommandLine,
		`curl -X POST 'https://api.example.com/articles' -H 'X-A: 1' --data-raw '{"a":1}'`)
	if err != nil {
		t.Fatalf("PasteCommand: %v", err)
	}
	if pasted.Reading.Kind != command.KindOK {
		t.Fatalf("reading = %+v, want a POST read as curl", pasted.Reading)
	}
	if pasted.State == nil {
		t.Fatal("a command that was read left the draft as it was")
	}
	if pasted.State.Draft.Method != "POST" ||
		pasted.State.Draft.URL != "https://api.example.com/articles" {
		t.Errorf("draft = %s %s, want the command that was pasted",
			pasted.State.Draft.Method, pasted.State.Draft.URL)
	}
	if pasted.State.Draft.Body != `{"a":1}` {
		t.Errorf("body = %q, want the body the command carried", pasted.State.Draft.Body)
	}
}

// A credential pasted as a header reaches the Auth chip. Every tool that copies a request out of a
// browser writes it that way — devtools writes `-H 'Authorization: …'` and never `-u` — so a header
// left as a header would mean the chip stayed on «None» for the shape of command people actually
// paste, and the fields behind it unfilled.
