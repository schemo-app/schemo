package wails

import (
	"context"
	"testing"

	"schemo/internal/command"
	"schemo/internal/domain"
	"schemo/internal/usecase/collection"
	"schemo/internal/usecase/draft"
)

// fakeDrafts is the draft feature as the copy sees it: the answer it hands back, and what it was
// asked on the way.
type fakeDrafts struct {
	answer   draft.Copy
	id       domain.DraftID
	format   command.Format
	inherits *domain.Auth
	above    []domain.Variable
}

func (f *fakeDrafts) CopyCommand(
	_ context.Context,
	id domain.DraftID,
	inherits *domain.Auth,
	above []domain.Variable,
	format command.Format,
) (draft.Copy, error) {
	f.id, f.inherits, f.above, f.format = id, inherits, above, format
	return f.answer, nil
}

// fakeTree answers what the levels above a request hold, or that the request is in no tree.
type fakeTree struct {
	above collection.Above
	err   error
}

func (f fakeTree) Above(context.Context, domain.DraftID) (collection.Above, error) {
	return f.above, f.err
}

// The copy is written on the side that holds the values, and what it came to crosses back whole:
// the text for the clipboard, and what the notation could not write, which the window words.
func TestExportDraftAnswersWithWhatTheCopyCameTo(t *testing.T) {
	drafts := &fakeDrafts{answer: draft.Copy{
		Text:    "curl 'https://api.example.com/roles'",
		Omitted: []command.Omission{{Kind: command.OmissionDigest}},
	}}
	service := NewCommandService(drafts, fakeTree{err: domain.ErrNotFound})

	answer, err := service.ExportDraft(
		context.Background(), domain.DraftCommandLine, command.FormatCurl)
	if err != nil {
		t.Fatalf("ExportDraft: %v", err)
	}
	if answer.Text != "curl 'https://api.example.com/roles'" {
		t.Errorf("text = %q, want the command the copy wrote", answer.Text)
	}
	if len(answer.Omitted) != 1 || answer.Omitted[0].Kind != command.OmissionDigest {
		t.Errorf("omitted = %+v, want what the notation could not write", answer.Omitted)
	}
	if drafts.format != command.FormatCurl || drafts.id != domain.DraftCommandLine {
		t.Errorf("asked for %q of %q, want the format and the draft the window named",
			drafts.format, drafts.id)
	}
}

// A request inside a collection is copied with what the levels above it answer: «Inherit» is theirs
// to resolve, and a copy that skipped them would be a request that is never sent.
func TestExportDraftHandsTheTreeAboveToTheCopy(t *testing.T) {
	inherited := &domain.Auth{Type: domain.AuthBearer}
	above := collection.Above{
		Auth:      inherited,
		Variables: []domain.Variable{{Name: "host", Value: "api.example.com", Enabled: true}},
	}
	drafts := &fakeDrafts{}
	service := NewCommandService(drafts, fakeTree{above: above})

	if _, err := service.ExportDraft(context.Background(), "node-1", command.FormatCurl); err != nil {
		t.Fatalf("ExportDraft: %v", err)
	}
	if drafts.inherits != inherited {
		t.Errorf("inherits = %v, want the authorization the tree above holds", drafts.inherits)
	}
	if len(drafts.above) != 1 || drafts.above[0].Name != "host" {
		t.Errorf("above = %+v, want the variables the tree above holds", drafts.above)
	}
}

// A draft outside every tree is copied all the same: having nothing above it is not an error, and
// what the copy is told is that there is nothing there.
func TestExportDraftOutsideEveryTreeIsCopiedAllTheSame(t *testing.T) {
	drafts := &fakeDrafts{answer: draft.Copy{Text: "curl 'https://api.example.com/roles'"}}
	service := NewCommandService(drafts, fakeTree{err: domain.ErrNotFound})

	if _, err := service.ExportDraft(context.Background(), domain.DraftCommandLine,
		command.FormatCurl); err != nil {
		t.Fatalf("ExportDraft: %v", err)
	}
	if drafts.inherits != nil || len(drafts.above) != 0 {
		t.Errorf("inherits = %v, above = %+v, want nothing above the command line",
			drafts.inherits, drafts.above)
	}
}
