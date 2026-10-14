package search

import (
	"strings"

	"schemo/internal/domain"
)

// matched is the rows of one area that answer, each carrying where in it the words were found.
func matched(needle string, hits []domain.SearchHit) []domain.SearchHit {
	folded := strings.ToLower(needle)

	var out []domain.SearchHit
	for _, hit := range hits {
		at, ok := matchFolded(folded, hit)
		if !ok {
			continue
		}
		hit.Match = at
		out = append(out, hit)
	}
	return out
}

// The tiers are the design's: the whole name, then a name, then an address, then a value. The
// needle arrives folded.
//
// Comparison is case-insensitive the way a person expects, which is why it happens here and not in
// SQL: SQLite folds case for ASCII alone, so a LIKE would leave a Cyrillic title unfindable by the
// same title typed in lower case — what is searched is whatever a person typed, and can be in any
// script.
func matchFolded(needle string, hit domain.SearchHit) (domain.SearchMatch, bool) {
	title := strings.ToLower(hit.Title)
	switch {
	case title == needle:
		return domain.MatchExact, true
	case strings.Contains(title, needle):
		return domain.MatchName, true
	}
	for _, step := range hit.Path {
		if strings.Contains(strings.ToLower(step), needle) {
			return domain.MatchPath, true
		}
	}
	if hit.MatchText != "" && strings.Contains(strings.ToLower(hit.MatchText), needle) {
		return domain.MatchValue, true
	}
	return domain.MatchNone, false
}
