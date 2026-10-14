package search

import (
	"testing"

	"schemo/internal/domain"
)

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
