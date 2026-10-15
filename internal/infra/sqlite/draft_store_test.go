package sqlite

import (
	"context"
	"errors"
	"testing"

	"schemo/internal/domain"
)

func TestDraftRoundTrip(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if _, err := store.Draft(ctx, ws, domain.DraftCommandLine); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a draft that was never saved = %v, want ErrNotFound", err)
	}

	saved := sampleDraft()
	if err := store.SaveDraft(ctx, ws, saved); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	got, err := store.Draft(ctx, ws, domain.DraftCommandLine)
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if got.Method != saved.Method || got.URL != saved.URL || got.Body != saved.Body ||
		got.Revision != 3 {
		t.Errorf("draft = %+v, want the saved one", got)
	}
	if got.EnvironmentID != "env-1" {
		t.Errorf("environmentID = %q, want the pin it was saved with", got.EnvironmentID)
	}
	if len(got.Params) != 2 || got.Params[1].ID != "p2" || got.Params[1].Enabled {
		t.Errorf("params = %+v, want both rows with what they carried", got.Params)
	}
	if len(got.Headers) != 1 || got.Headers[0].Value != "Bearer {{token}}" {
		t.Errorf("headers = %+v, want the token as it was typed", got.Headers)
	}
	if len(got.PathParams) != 1 || got.PathParams[0].Value != "{{id}}" ||
		got.PathParams[0].Description != "which user" {
		t.Errorf("path params = %+v, want the value and the description kept", got.PathParams)
	}
	if got.Auth.Type != domain.AuthBearer {
		t.Errorf("auth = %+v", got.Auth)
	}
}

func TestDraftRoundTripKeepsTheBodyFormat(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	saved := sampleDraft()
	saved.BodyKind = domain.BodyForm
	saved.Body = ""
	saved.Form = []domain.FormRow{
		{ID: "f1", Name: "title", Value: "Coffee grinder", Enabled: true},
		{ID: "f2", Name: "photo", Src: `C:\pics\logo.png`, File: true, Enabled: false},
	}
	if err := store.SaveDraft(ctx, ws, saved); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	got, err := store.Draft(ctx, ws, domain.DraftCommandLine)
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if got.BodyKind != domain.BodyForm {
		t.Errorf("bodyKind = %q, want the saved one", got.BodyKind)
	}
	if len(got.Form) != 2 {
		t.Fatalf("form = %+v, want both rows", got.Form)
	}
	if got.Form[0].Value != "Coffee grinder" {
		t.Errorf("form[0] = %+v, want the text that was typed", got.Form[0])
	}
	// The path is kept apart from the text so that switching a row back to a text field finds what
	// was under it — and it survives a round trip on a Windows path.
	if !got.Form[1].File || got.Form[1].Src != `C:\pics\logo.png` {
		t.Errorf("form[1] = %+v, want the file row and its path", got.Form[1])
	}
	if got.Form[1].Enabled {
		t.Error("a switched-off row came back switched on")
	}
}

// The draft is one row: saving twice replaces what the window was composing before.
func TestSaveDraftReplacesTheOneBefore(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveDraft(ctx, ws, sampleDraft()); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	next := sampleDraft()
	next.Revision = 4
	next.Method = "DELETE"
	next.Params = nil
	if err := store.SaveDraft(ctx, ws, next); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	got, err := store.Draft(ctx, ws, domain.DraftCommandLine)
	if err != nil {
		t.Fatalf("Draft: %v", err)
	}
	if got.Method != "DELETE" || got.Revision != 4 {
		t.Errorf("draft = %+v, want the second save", got)
	}
	if len(got.Params) != 0 {
		t.Errorf("params = %+v, want the empty list the second save had", got.Params)
	}

	var rows int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM drafts`).Scan(&rows); err != nil {
		t.Fatalf("counting drafts: %v", err)
	}
	if rows != 1 {
		t.Errorf("drafts = %d, want one", rows)
	}
}
