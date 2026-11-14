package workspace

import (
	"cmp"
	"context"
	"fmt"
	"schemo/internal/domain"
	"slices"
	"strings"
)

type Search struct {
	index Index
	scope domain.Scope
}

func NewSearch(index Index, scope domain.Scope) *Search {
	return &Search{index: index, scope: scope}
}

// Find searches every area or the one asked for. The workspace is resolved once so every area
// answers about the same one.
func (s *Search) Find(ctx context.Context, in Query) (domain.SearchResult, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return domain.SearchResult{}, err
	}

	needle := strings.TrimSpace(in.Text)
	narrowed := in.Kind != nil
	limit := perGroupAll
	if narrowed {
		limit = perGroupOne
	}

	result := domain.SearchResult{Groups: []domain.SearchGroup{}}
	for _, area := range s.areas() {
		if narrowed && area.kind != *in.Kind {
			continue
		}
		// An empty query shows recent history across all areas, but every row under a chosen area.
		if needle == "" && !narrowed && area.kind != domain.SearchHistory {
			continue
		}

		hits, err := area.find(ctx, workspace)
		if err != nil {
			return domain.SearchResult{}, fmt.Errorf("searching %s: %w", area.kind, err)
		}
		rows := matched(needle, hits)
		if len(rows) == 0 {
			continue
		}
		ordered, total := rank(rows, limit)
		result.Groups = append(result.Groups,
			domain.SearchGroup{Kind: area.kind, Total: total, Hits: ordered})
	}
	return result, nil
}

// area is one entry of the index: the kind of row it answers with, and where to ask for it. It is a
// method value rather than a branch so that adding an area is a line beside the others.
type area struct {
	kind domain.SearchKind
	find func(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
}

// The areas, in the order the palette draws them.
func (s *Search) areas() []area {
	return []area{
		{domain.SearchRequest, s.index.FindRequests},
		{domain.SearchCollection, s.index.FindCollections},
		{domain.SearchEnvironment, s.index.FindEnvironments},
		{domain.SearchHistory, s.index.FindHistory},
	}
}

// Query is the palette's words and optional area. An unknown area searches nothing, not everything.
type Query struct {
	Text string             `json:"text"`
	Kind *domain.SearchKind `json:"kind"`
}

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

// matchFolded tiers matches: whole name, name, address, value. Folding happens here because SQLite
// folds ASCII only.
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

const (
	// Two numbers because the two modes differ: every area at once is a glance and five rows fit under
	// one heading, while a chosen area is a list the user asked for and gets the room.
	perGroupAll = 5
	perGroupOne = 20
)

// rank sorts in place, best match then newest (stable, so timeless rows keep their order), and cuts
// to limit, returning the total found.
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

// Index is what search reads, one Find method per area. Matching happens above, since SQLite folds
// ASCII only. Names start with Find because *db.Store implements every port.
type Index interface {
	FindRequests(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
	FindCollections(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
	FindEnvironments(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
	FindHistory(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
}
