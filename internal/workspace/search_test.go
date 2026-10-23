package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"schemo/internal/domain"
	"strings"
	"testing"
)

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
	result, err := NewSearch(index, domain.WorkspaceScope(domain.WorkspacePersonalID)).Find(context.Background(), in)
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
		collections: []domain.SearchHit{{Kind: domain.SearchCollection, ID: "c1", Title: "Users"}},
		requests:    []domain.SearchHit{{Kind: domain.SearchRequest, ID: "r1", Title: "/orders"}},
	}, Query{Text: "user"})

	if len(result.Groups) != 1 {
		t.Fatalf("groups = %+v, want the collection alone", result.Groups)
	}
	if result.Groups[0].Kind != domain.SearchCollection {
		t.Fatalf("kind = %s, want collection", result.Groups[0].Kind)
	}
}

// The heading counts what was found, not what is drawn: "Requests · 8" over five rows is what the
// heading says, and a group cut to five must still say eight.
func TestQueryCountsEverythingItFound(t *testing.T) {
	hits := []domain.SearchHit{}
	for _, name := range []string{"user 1", "user 2", "user 3", "user 4", "user 5", "user 6",
		"user 7"} {
		hits = append(hits, domain.SearchHit{Kind: domain.SearchCollection, ID: name, Title: name})
	}

	found := group(t, search(t, &fakeIndex{collections: hits}, Query{Text: "user"}),
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
				Title: "Users"})
	}
	index := &fakeIndex{collections: hits}

	narrow := group(t, search(t, index, Query{Text: "user", Kind: narrowed(domain.SearchCollection)}),
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
		collections: []domain.SearchHit{{Kind: domain.SearchCollection, ID: "c1", Title: "Users"}},
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
			{Kind: domain.SearchCollection, ID: "c1", Title: "Users"},
			{Kind: domain.SearchCollection, ID: "c2", Title: "Orders"},
		},
	}
	result := search(t, index, Query{Kind: narrowed(domain.SearchCollection)})

	if got := group(t, result, domain.SearchCollection).Total; got != 2 {
		t.Fatalf("total = %d, want every collection of the area", got)
	}
}

// The workspace is resolved once and every area is asked in it: an area resolving it again could
// answer about a workspace the window has already left.
func TestQuerySearchesTheWorkspaceTheWindowIsIn(t *testing.T) {
	index := &fakeIndex{history: []domain.SearchHit{
		{Kind: domain.SearchHistory, ID: "h1", Title: "/users"},
	}}
	if _, err := NewSearch(index,
		domain.WorkspaceScope("team-1")).Find(context.Background(), Query{}); err != nil {
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
	_, err := NewSearch(&fakeIndex{fail: domain.SearchHistory}, domain.WorkspaceScope(domain.WorkspacePersonalID)).
		Find(context.Background(), Query{Text: "Users"})
	if err == nil {
		t.Fatal("want a failure")
	}
}

// The tiers are pinned here: a whole name beats a name, a name
// beats the trail it sits in, and the trail beats a value that merely holds the word.
func TestMatchPicksTheTier(t *testing.T) {
	cases := []struct {
		name  string
		query string
		hit   domain.SearchHit
		want  domain.SearchMatch
		ok    bool
	}{
		{
			name:  "the whole name",
			query: "Users",
			hit:   domain.SearchHit{Title: "Users", Path: []string{"Users"}},
			want:  domain.MatchExact,
			ok:    true,
		},
		{
			name:  "part of a name",
			query: "ser",
			hit:   domain.SearchHit{Title: "Users"},
			want:  domain.MatchName,
			ok:    true,
		},
		{
			name:  "somewhere in the trail",
			query: "Catalog",
			hit:   domain.SearchHit{Title: "/users/{id}", Path: []string{"Catalog API", "Users"}},
			want:  domain.MatchPath,
			ok:    true,
		},
		{
			name:  "only a value holds it",
			query: "admin",
			hit:   domain.SearchHit{Title: "role", MatchText: "admin"},
			want:  domain.MatchValue,
			ok:    true,
		},
		{
			name:  "nowhere",
			query: "nothing",
			hit: domain.SearchHit{Title: "Users", Path: []string{"Catalog API"},
				MatchText: "admin"},
			want: 0,
			ok:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := matchFolded(strings.ToLower(tc.query), tc.hit)
			if ok != tc.ok {
				t.Fatalf("found = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("tier = %v, want %v", got, tc.want)
			}
		})
	}
}

// SQLite LIKE folds ASCII only; matching must also fold non-ASCII names.
func TestMatchIgnoresUnicodeCase(t *testing.T) {
	hit := domain.SearchHit{Title: "Équipe", Path: []string{"Résumé API"}}
	for _, query := range []string{"ÉQUIPE", "équipe", "ÉqUiPe", "RÉSUMÉ"} {
		if _, ok := matchFolded(strings.ToLower(query), hit); !ok {
			t.Fatalf("%q did not find %q", query, hit.Title)
		}
	}
}

// The window draws the name of a variable and nothing else, so a value that answered the query must
// not travel with the answer: it has nowhere to be drawn, and when the variable is a secret it is
// the one thing in this database that may not leave.
func TestHitDoesNotCarryWhatMatched(t *testing.T) {
	encoded, err := json.Marshal(domain.SearchHit{
		Kind:      domain.SearchEnvironment,
		Title:     "token",
		MatchText: "s3cr3t-value",
	})
	if err != nil {
		t.Fatalf("encoding the hit: %v", err)
	}
	if strings.Contains(string(encoded), "s3cr3t-value") {
		t.Fatalf("the value that matched travelled: %s", encoded)
	}
}

// The tier comes before the time: a row whose whole name was typed is the answer even when it was
// saved long ago, and a row that merely holds the word does not overtake it by being new.
func TestRankPutsTheTierAboveTheTime(t *testing.T) {
	hits := []domain.SearchHit{
		{ID: "value-new", Match: domain.MatchValue, At: 900},
		{ID: "path-new", Match: domain.MatchPath, At: 800},
		{ID: "name-old", Match: domain.MatchName, At: 100},
		{ID: "exact-oldest", Match: domain.MatchExact, At: 1},
	}

	ordered, total := rank(hits, 10)

	want := []string{"exact-oldest", "name-old", "path-new", "value-new"}
	for i, id := range want {
		if ordered[i].ID != id {
			t.Fatalf("row %d = %s, want %s (whole answer: %+v)", i, ordered[i].ID, id, ordered)
		}
	}
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
}

// Inside one tier the newest comes first, and a row that never happened follows the ones that did
// rather than being shuffled among them. The two never mix in practice — an area keeps a time or it
// does not — so this is the rule for the day one does.
func TestRankPutsTheNewestFirstInsideATier(t *testing.T) {
	hits := []domain.SearchHit{
		{ID: "never", Match: domain.MatchName},
		{ID: "old", Match: domain.MatchName, At: 100},
		{ID: "new", Match: domain.MatchName, At: 300},
	}

	ordered, _ := rank(hits, 10)

	want := []string{"new", "old", "never"}
	for i, id := range want {
		if ordered[i].ID != id {
			t.Fatalf("row %d = %s, want %s", i, ordered[i].ID, id)
		}
	}
}

// Rows with no time at all keep the order they were read in, which is the order the tree keeps them
// — a stable sort, so that a keystroke does not reshuffle rows that are otherwise equal.
func TestRankKeepsTheOrderOfRowsThatNeverHappened(t *testing.T) {
	hits := []domain.SearchHit{
		{ID: "first", Match: domain.MatchName},
		{ID: "second", Match: domain.MatchName},
		{ID: "third", Match: domain.MatchName},
	}

	ordered, _ := rank(hits, 10)

	want := []string{"first", "second", "third"}
	for i, id := range want {
		if ordered[i].ID != id {
			t.Fatalf("row %d = %s, want %s", i, ordered[i].ID, id)
		}
	}
}
