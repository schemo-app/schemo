package search

import (
	"cmp"
	"slices"

	"schemo/internal/domain"
)

const (
	// Two numbers because the two modes differ: every area at once is a glance and five rows fit under
	// one heading, while a chosen area is a list the user asked for and gets the room.
	perGroupAll = 5
	perGroupOne = 20
)

// rank orders one area's answer and cuts it to what the group draws, answering the rows and how
// many were found. The rows are sorted where they lie: the slice is the area's own and nobody else
// holds it.
//
// The order is the design's: the best kind of match first, and inside a kind the newest. A row with
// no time at all — a saved request, which never happened — keeps the order the area stored it in,
// which is why the sort is stable.
func rank(hits []domain.SearchHit, limit int) ([]domain.SearchHit, int) {
	total := len(hits)

	slices.SortStableFunc(hits, func(a, b domain.SearchHit) int {
		if a.Match != b.Match {
			return cmp.Compare(a.Match, b.Match)
		}
		switch {
		case a.At == b.At:
			return 0
		case a.At == 0:
			return 1
		case b.At == 0:
			return -1
		default:
			return cmp.Compare(b.At, a.At)
		}
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, total
}
