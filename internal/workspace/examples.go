package workspace

// The example answers a request carries: what an imported file brought in beside it, and what the
// window saves from a live answer.

import (
	"context"

	"schemo/internal/domain"
)

// SaveExamples writes a request's examples in order; any status or body is accepted.
func (s *Collections) SaveExamples(
	ctx context.Context,
	id string,
	examples []domain.ResponseExample,
) (domain.CollectionNode, error) {
	node, err := s.store.Node(ctx, id)
	if err != nil {
		return domain.CollectionNode{}, err
	}

	node.Responses = domain.OrEmpty(examples)
	if err := s.store.SaveNode(ctx, node); err != nil {
		return domain.CollectionNode{}, err
	}
	return s.store.Node(ctx, id)
}
