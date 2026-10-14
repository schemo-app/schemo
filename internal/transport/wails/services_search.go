package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/usecase/search"
)

// SearchService is the palette: one question, one answer, and no state kept between them. The
// window asks on every pause in typing, so the call carries no id it would have to be matched
// against — what is stale is decided by the window, which knows which question it is still waiting
// for.
type SearchService struct {
	search *search.UseCase
}

func NewSearchService(uc *search.UseCase) *SearchService {
	return &SearchService{search: uc}
}

func (s *SearchService) Find(ctx context.Context, in search.Query) (domain.SearchResult, error) {
	return s.search.Find(ctx, in)
}
