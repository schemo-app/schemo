package collection

// The example answers a request carries: what an imported file brought in beside it, and what the
// window saves from a live answer.

import (
	"context"

	"schemo/internal/domain"
)

// SaveExamples writes the answers kept beside a request. The whole set arrives as the pane holds
// it, in order; only a request has them, so a collection that was asked answers not-found the way
// any unknown id does. Nothing in an example is refused — a status the server never sent and a
// body that is not JSON are both things a person may write down.
func (u *UseCase) SaveExamples(
	ctx context.Context,
	id string,
	examples []domain.ResponseExample,
) (domain.CollectionNode, error) {
	node, err := u.store.Node(ctx, id)
	if err != nil {
		return domain.CollectionNode{}, err
	}

	node.Responses = domain.OrEmpty(examples)
	if err := u.store.SaveNode(ctx, node); err != nil {
		return domain.CollectionNode{}, err
	}
	return u.store.Node(ctx, id)
}
