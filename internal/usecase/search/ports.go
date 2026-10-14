package search

import (
	"context"

	"schemo/internal/domain"
)

// Index is what the search reads: one method per area, all answering the same shape, so an area is
// a line beside the others rather than a branch inside the query.
//
// One port rather than a question put to each feature: a feature that had to answer one would have
// to grow a search of its own, and adding an area would touch three packages.
//
// The names start with Find because *sqlite.Store implements every port and already has a
// Collections: one type cannot carry two methods with one name, so a new area's name is checked
// against internal/infra/sqlite before it is added.
//
// An area answers with its whole set and is told nothing about the words: matching happens above,
// because SQLite folds case for ASCII alone and the names are Russian. The schema is what
// makes that affordable — a level is a document, and the history is kept to what retention leaves.
type Index interface {
	FindRequests(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
	FindCollections(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
	FindEnvironments(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
	FindHistory(ctx context.Context, workspaceID string) ([]domain.SearchHit, error)
}
