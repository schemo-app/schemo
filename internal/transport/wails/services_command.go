package wails

import (
	"context"
	"errors"

	"schemo/internal/command"
	"schemo/internal/domain"
	"schemo/internal/usecase/collection"
	"schemo/internal/usecase/draft"
)

// CommandService is the command boundary: the request being composed, written as a command a person
// can carry away. Resolving `{{tokens}}` is done here, because their values never reach the window.
type CommandService struct {
	drafts draftSource
	tree   treeSource
}

// draftSource is the draft feature as a copy of the request being composed needs it: the request
// written as a command for one tool, with what the notation could not carry.
type draftSource interface {
	CopyCommand(
		ctx context.Context,
		id domain.DraftID,
		inherits *domain.Auth,
		above []domain.Variable,
		format command.Format,
	) (draft.Copy, error)
}

// treeSource is the collections feature as the same copy needs it: what the levels above a request
// answer for its «Inherit» and its tokens. A draft outside every tree has nothing above it.
type treeSource interface {
	Above(ctx context.Context, id domain.DraftID) (collection.Above, error)
}

func NewCommandService(drafts draftSource, tree treeSource) *CommandService {
	return &CommandService{drafts: drafts, tree: tree}
}

// ExportDraft writes the request being composed out as a command for one tool, with the tree above
// resolved and the real credentials on it — a copy is copied whole and asks for no token.
func (s *CommandService) ExportDraft(
	ctx context.Context,
	id domain.DraftID,
	format command.Format,
) (draft.Copy, error) {
	above, err := s.tree.Above(ctx, id)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return draft.Copy{}, err
	}
	return s.drafts.CopyCommand(ctx, id, above.Auth, above.Variables, format)
}
