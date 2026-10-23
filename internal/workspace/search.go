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

// Find searches every area at once or the one that was asked for. It is not called Query because
// the name would repeat the argument's own type.
//
// The workspace is resolved once, here, and travels into every area as a parameter: an area that
// resolved it again could answer about a workspace the window has already left.
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
		// An empty field is not "everything": over every area the palette offers what the user was
		// doing last, and only the history keeps a time. Under a chosen chip the empty field means
		// every row, because the chip says "show me these".
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

// Query is what the palette asks: the words typed, and the one area the user narrowed to.
//
// No area is nil rather than an empty word, because a kind this build does not know is one a newer
// window asked for, and it should search nothing rather than everything. The rows a group draws are
// not a number here: how many fit under one heading is the answer's business.
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

// The tiers are: the whole name, then a name, then an address, then a value. The needle arrives
// folded.
//
// Comparison is case-insensitive the way a person expects, which is why it happens here and not in
// SQL: SQLite folds case for ASCII alone, so a LIKE would miss a Cyrillic title typed in lower
// case, and what is searched is whatever a person typed, in any script.
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

// rank orders one area's answer and cuts it to what the group draws, returning the rows and how
// many were found. The rows are sorted where they lie: the slice is the area's own and nobody else
// holds it.
//
// The best kind of match comes first and, inside a kind, the newest. A row with no time at all (a
// saved request, which never happened) keeps the order the area stored it in, which is why the sort
// is stable.
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

// Index is what the search reads: one method per area, all returning the same shape, so an area is
// a line beside the others rather than a branch inside the query.
//
// It is one port rather than a question put to each feature: a feature that had to answer one would
// grow a search of its own, and adding an area would touch three packages.
//
// The names start with Find because *db.Store implements every port and already has a
// Collections: one type cannot carry two methods with one name, so a new area's name is checked
// against internal/db before it is added.
//
// An area returns its whole set and is told nothing about the words: matching happens above,
// because SQLite folds case for ASCII alone and names can be in any script. The schema keeps that
// affordable, since a level is a document and history is kept to what retention leaves.
type Index interface {
	FindRequests(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
	FindCollections(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
	FindEnvironments(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
	FindHistory(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
}
