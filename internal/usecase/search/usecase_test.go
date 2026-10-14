package search

import (
	"context"
	"errors"
	"testing"

	"schemo/internal/domain"
)

// fakeScope answers which workspace the window is showing. The index below answers about one
// workspace and ignores the id, but records every one it was asked in: the id the scope answers
// with has to be the id the index is asked in, and that is what asked is for.
type fakeScope struct{ id string }

func (f fakeScope) ActiveWorkspace(context.Context) (string, error) {
	if f.id == "" {
		return domain.WorkspacePersonalID, nil
	}
	return f.id, nil
}

// fakeIndex is every area at once, so that a test can fill the one it is about and leave the rest
// empty. Failures are per area for the same reason.
type fakeIndex struct {
	requests     []domain.SearchHit
	collections  []domain.SearchHit
	environments []domain.SearchHit
	history      []domain.SearchHit
	fail         domain.SearchKind
	asked        []string
}

func (f *fakeIndex) note(workspaceID string, kind domain.SearchKind) error {
	f.asked = append(f.asked, workspaceID)
	if f.fail == kind {
		return errors.New("the index is down")
	}
	return nil
}

func (f *fakeIndex) FindRequests(
	_ context.Context,
	workspaceID string,
) ([]domain.SearchHit, error) {
	return f.requests, f.note(workspaceID, domain.SearchRequest)
}

func (f *fakeIndex) FindCollections(
	_ context.Context,
	workspaceID string,
) ([]domain.SearchHit, error) {
	return f.collections, f.note(workspaceID, domain.SearchCollection)
}

func (f *fakeIndex) FindEnvironments(
	_ context.Context,
	workspaceID string,
) ([]domain.SearchHit, error) {
	return f.environments, f.note(workspaceID, domain.SearchEnvironment)
}

func (f *fakeIndex) FindHistory(_ context.Context, workspaceID string) ([]domain.SearchHit, error) {
	return f.history, f.note(workspaceID, domain.SearchHistory)
}

// narrowed is an area chosen: the query takes a pointer, and no area at all is nil.
func narrowed(kind domain.SearchKind) *domain.SearchKind { return &kind }

func search(t *testing.T, index Index, in Query) domain.SearchResult {
	t.Helper()
	result, err := NewUseCase(index, fakeScope{}).Find(context.Background(), in)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	return result
}

// group pulls the one group a test is asking about out of the answer.
func group(t *testing.T, result domain.SearchResult, kind domain.SearchKind) domain.SearchGroup {
	t.Helper()
	for _, g := range result.Groups {
		if g.Kind == kind {
			return g
		}
	}
	t.Fatalf("no %s group in %+v", kind, result.Groups)
	return domain.SearchGroup{}
}

// An area that found nothing is absent rather than empty: a heading over no rows is a heading the
// window would have to know to skip.
func TestQueryLeavesOutTheAreasThatFoundNothing(t *testing.T) {
	result := search(t, &fakeIndex{
		collections: []domain.SearchHit{{Kind: domain.SearchCollection, ID: "c1", Title: "Пользователи"}},
		requests:    []domain.SearchHit{{Kind: domain.SearchRequest, ID: "r1", Title: "/users"}},
	}, Query{Text: "польз"})

	if len(result.Groups) != 1 {
		t.Fatalf("groups = %+v, want the collection alone", result.Groups)
	}
	if result.Groups[0].Kind != domain.SearchCollection {
		t.Fatalf("kind = %s, want collection", result.Groups[0].Kind)
	}
}

// The heading counts what was found, not what is drawn: "Requests · 8" over five rows is the
// design's own wording, and a group cut to five must still say eight.
func TestQueryCountsEverythingItFound(t *testing.T) {
	hits := []domain.SearchHit{}
	for _, name := range []string{"польз 1", "польз 2", "польз 3", "польз 4", "польз 5", "польз 6",
		"польз 7"} {
		hits = append(hits, domain.SearchHit{Kind: domain.SearchCollection, ID: name, Title: name})
	}

	found := group(t, search(t, &fakeIndex{collections: hits}, Query{Text: "польз"}),
		domain.SearchCollection)
	if found.Total != 7 {
		t.Fatalf("total = %d, want 7", found.Total)
	}
	if len(found.Hits) != perGroupAll {
		t.Fatalf("drawn = %d, want %d", len(found.Hits), perGroupAll)
	}
}

// A chosen area is a list the user asked for, and it gets the room every area at once does not
// have.
func TestQueryWidensTheAreaThatWasChosen(t *testing.T) {
	hits := []domain.SearchHit{}
	for i := range perGroupOne + 3 {
		hits = append(hits,
			domain.SearchHit{Kind: domain.SearchCollection, ID: string(rune('a' + i)),
				Title: "пользователи"})
	}
	index := &fakeIndex{collections: hits}

	narrow := group(t, search(t, index, Query{Text: "польз", Kind: narrowed(domain.SearchCollection)}),
		domain.SearchCollection)
	if len(narrow.Hits) != perGroupOne {
		t.Fatalf("drawn = %d, want %d", len(narrow.Hits), perGroupOne)
	}

	// Only the area that was asked for is read at all.
	if len(index.asked) != 1 {
		t.Fatalf("areas read = %d, want 1", len(index.asked))
	}
}

// An empty field is not "everything". It is the palette offering what the user was doing last, and
// only the history has an answer to that — every other area would answer with its whole contents.
func TestQueryAnswersAnEmptyFieldWithHistoryAlone(t *testing.T) {
	index := &fakeIndex{
		collections: []domain.SearchHit{{Kind: domain.SearchCollection, ID: "c1", Title: "Пользователи"}},
		requests:    []domain.SearchHit{{Kind: domain.SearchRequest, ID: "r1", Title: "/users"}},
		history: []domain.SearchHit{
			{Kind: domain.SearchHistory, ID: "h2", Title: "/articles", At: 200},
			{Kind: domain.SearchHistory, ID: "h1", Title: "/users", At: 100},
		},
	}

	result := search(t, index, Query{})
	if len(result.Groups) != 1 || result.Groups[0].Kind != domain.SearchHistory {
		t.Fatalf("groups = %+v, want the history alone", result.Groups)
	}
	if got := result.Groups[0].Hits[0].ID; got != "h2" {
		t.Fatalf("first row = %s, want the newest", got)
	}
}

// An empty field over one area is a different question from an empty field over all of them: the
// chip says "show me these", and there is nothing to narrow by yet.
func TestQueryAnswersAnEmptyFieldInTheAreaThatWasChosen(t *testing.T) {
	index := &fakeIndex{
		collections: []domain.SearchHit{
			{Kind: domain.SearchCollection, ID: "c1", Title: "Пользователи"},
			{Kind: domain.SearchCollection, ID: "c2", Title: "Заказы"},
		},
	}
	result := search(t, index, Query{Kind: narrowed(domain.SearchCollection)})

	if got := group(t, result, domain.SearchCollection).Total; got != 2 {
		t.Fatalf("total = %d, want every collection of the area", got)
	}
}

// The workspace is resolved once and every area is asked in it: an area resolving it again could
// answer about a space the window has already left.
func TestQuerySearchesTheWorkspaceTheWindowIsIn(t *testing.T) {
	index := &fakeIndex{history: []domain.SearchHit{
		{Kind: domain.SearchHistory, ID: "h1", Title: "/users"},
	}}
	if _, err := NewUseCase(index,
		fakeScope{id: "team-1"}).Find(context.Background(), Query{}); err != nil {
		t.Fatalf("querying: %v", err)
	}

	for _, asked := range index.asked {
		if asked != "team-1" {
			t.Fatalf("asked in %q, want team-1", asked)
		}
	}
}

// An area that fails fails the whole answer: half a palette drawn from a database that is not
// answering is worse than the window saying so.
func TestQueryFailsWhenAnAreaDoes(t *testing.T) {
	_, err := NewUseCase(&fakeIndex{fail: domain.SearchHistory}, fakeScope{}).
		Find(context.Background(), Query{Text: "пользователи"})
	if err == nil {
		t.Fatal("want a failure")
	}
}
