// Package search owns the one index over everything the app keeps, and answers what the palette
// draws when the user types into it.
package search

import (
	"context"
	"fmt"
	"strings"

	"schemo/internal/domain"
)

type UseCase struct {
	index Index
	scope domain.Scope
}

func NewUseCase(index Index, scope domain.Scope) *UseCase {
	return &UseCase{index: index, scope: scope}
}

// Find searches every area at once or the one that was asked for. It is not called Query: the
// name would repeat the argument's own type, and the type is the better half of the two.
//
// The workspace is resolved once, here, and travels into every area as a parameter: an area that
// resolved it again could answer about a space the window has already left.
func (u *UseCase) Find(ctx context.Context, in Query) (domain.SearchResult, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
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
	for _, area := range u.areas() {
		if narrowed && area.kind != *in.Kind {
			continue
		}
		// An empty field is not "everything": over every area the palette offers what the user was doing
		// last, and only the history keeps a time. Under a chosen chip the empty field means every row,
		// because the chip says "show me these".
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

// The areas, in the order the design draws them.
func (u *UseCase) areas() []area {
	return []area{
		{domain.SearchRequest, u.index.FindRequests},
		{domain.SearchCollection, u.index.FindCollections},
		{domain.SearchEnvironment, u.index.FindEnvironments},
		{domain.SearchHistory, u.index.FindHistory},
	}
}
