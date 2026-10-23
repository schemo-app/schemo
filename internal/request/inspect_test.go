package request

import (
	"context"
	"errors"
	"schemo/internal/domain"
	"testing"
)

func TestABodyIsOpenedOnceAndAskedAboutByHandle(t *testing.T) {
	inspector := NewInspector(nil, nil)
	opened, err := inspector.Open(`{"a": {"b": 1}}`, "application/json")
	if err != nil || opened.ID == "" || opened.Kind != domain.DocJSON {
		t.Fatalf("Open = %+v, %v, want a JSON handle", opened, err)
	}
	again, _ := inspector.Open(`{"a": {"b": 1}}`, "application/json")
	if again.ID != opened.ID {
		t.Errorf("the same body opened twice got %q and %q, want one handle", opened.ID, again.ID)
	}

	view, err := inspector.At(opened.ID, 12)
	if err != nil || view.Path != "$.a.b" {
		t.Errorf("At = %+v, %v, want $.a.b", view, err)
	}
}

func TestATextIsNotADocumentAndThatIsNotAnError(t *testing.T) {
	opened, err := NewInspector(nil, nil).Open("plain words", "text/plain")
	if err != nil || opened.ID != "" {
		t.Errorf("Open = %+v, %v, want no handle and no error", opened, err)
	}
}

func TestTheOldestDocumentIsForgottenAndAskedForAgainByOpening(t *testing.T) {
	inspector := NewInspector(nil, nil)
	first, _ := inspector.Open(`{"n": 0}`, "application/json")
	for i := 1; i <= kept; i++ {
		if _, err := inspector.Open(`{"n": `+string(rune('0'+i))+`}`, "application/json"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := inspector.At(first.ID, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("At on an evicted handle = %v, want ErrNotFound", err)
	}
}

// fakeTree is the collections of the workspace in hand: where a value is looked for a request to fit.
type fakeTree struct {
	held []domain.Collection
	err  error
}

func (t fakeTree) Collections(context.Context, string) ([]domain.Collection, error) {
	return t.held, t.err
}

// fixedScope is the workspace the window is showing, which is one name.
type fixedScope string

func (s fixedScope) ActiveWorkspace(context.Context) (string, error) { return string(s), nil }

// A value that says what it identifies is offered to the requests that take one: the address a
// `{{id}}` sits in is the map, so no specification is read and none is needed.
func TestAValueIsOfferedWhereItFits(t *testing.T) {
	inspector := NewInspector(
		fakeTree{held: []domain.Collection{{Items: []domain.CollectionNode{
			{ID: "n1", Name: "List authors", Method: "GET", URL: "https://api.example.com/authors"},
			{ID: "n2", Name: "Get an author", Method: "GET", URL: "https://api.example.com/authors/{{id}}"},
		}}}},
		fixedScope("space-1"),
	)
	opened, err := inspector.Open(`{"author_id": "42"}`, "application/json")
	if err != nil || opened.ID == "" {
		t.Fatalf("Open = %+v, %v, want a handle", opened, err)
	}

	found, err := inspector.Suggest(context.Background(), opened.ID, "$.author_id")
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(found) != 1 || found[0].NodeID != "n2" {
		t.Fatalf("Suggest = %+v, want the one request that takes an author", found)
	}
	if found[0].Filled != "https://api.example.com/authors/42" {
		t.Errorf("filled = %q, want the value put in the hole", found[0].Filled)
	}
}

// A value that identifies nothing is offered to nobody, and that is an answer rather than a
// failure: most of what a response holds is not somebody's identifier.
func TestAValueNothingIdentifiesIsNotOffered(t *testing.T) {
	inspector := NewInspector(fakeTree{}, fixedScope("space-1"))
	opened, err := inspector.Open(`{"title": "Dune"}`, "application/json")
	if err != nil || opened.ID == "" {
		t.Fatalf("Open = %+v, %v, want a handle", opened, err)
	}

	found, err := inspector.Suggest(context.Background(), opened.ID, "$.title")
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("Suggest = %+v, want nothing offered", found)
	}
}

// A tree that cannot be read is the answer to this question, and not a silence: an empty list would
// say there is nowhere to offer the value, which is not what happened.
func TestATreeThatCannotBeReadIsNotAnEmptyAnswer(t *testing.T) {
	inspector := NewInspector(fakeTree{err: errors.New("no database")}, fixedScope("space-1"))
	opened, _ := inspector.Open(`{"author_id": "42"}`, "application/json")

	if _, err := inspector.Suggest(context.Background(), opened.ID, "$.author_id"); err == nil {
		t.Fatal("Suggest returned no error for a tree that could not be read")
	}
}
