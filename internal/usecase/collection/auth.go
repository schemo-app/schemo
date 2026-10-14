package collection

// The auth walk the card and the run both ask: the nearest level above a request that answered.

import (
	"context"

	"schemo/internal/domain"
)

// SaveAuth writes what a level authorizes its requests with. The tab sends its whole state, so
// «None» over a filled-in Bearer is an answer about who authorizes, not an erasure — that
// distinction is domain.Auth.Stored's to draw, which is why it lives there.
func (u *UseCase) SaveAuth(
	ctx context.Context,
	id string,
	auth domain.Auth,
) ([]domain.Collection, error) {
	// Normalized like a draft's: the answers arrive whole, and the scheme may have changed.
	saved := auth.Normalized().Stored()

	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	if collection, ok, err := u.collection(ctx, workspace, id); err != nil {
		return nil, err
	} else if ok {
		collection.Auth = saved
		if err := u.store.SaveCollection(ctx, workspace, collection); err != nil {
			return nil, err
		}
		return u.store.Collections(ctx, workspace)
	}

	node, err := u.store.Node(ctx, id)
	if err != nil {
		return nil, err
	}
	node.Auth = saved
	if err := u.store.SaveNode(ctx, node); err != nil {
		return nil, err
	}
	return u.store.Collections(ctx, workspace)
}

// Above is everything the levels over a request answer for it, read in one walk because both are
// read at the same moment by the same caller: the authorization the nearest level that gave one
// answered with, and the `{{tokens}}` they answer for, outermost first.
//
// The command line's draft is in no tree, and a node that has been deleted is in none either —
// neither is a failure, because "there is nothing above this request" is what the answer is, not
// that the question was wrong.
func (u *UseCase) Above(ctx context.Context, id domain.DraftID) (Above, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return Above{}, err
	}
	tree, err := u.store.Collections(ctx, workspace)
	if err != nil {
		return Above{}, err
	}
	for _, collection := range tree {
		if found, ok := aboveUnder([]domain.Collection{collection}, string(id), nil, nil); ok {
			return found, nil
		}
	}
	return Above{}, nil
}

// aboveUnder descends one level at a time, carrying what the levels over the request answered.
func aboveUnder(
	collections []domain.Collection,
	id string,
	inherited *domain.Auth,
	above []domain.Variable,
) (Above, bool) {
	for _, collection := range collections {
		at := answerOf(collection.Auth, inherited)
		variables := appendLevel(above, collection.Variables)

		for _, node := range collection.Items {
			if node.ID == id {
				return Above{Auth: answerOf(node.Auth, at), Variables: variables}, true
			}
		}
		if found, ok := aboveUnder(collection.Children, id, at, variables); ok {
			return found, true
		}
	}
	return Above{}, false
}

// Both walks over the tree go through here, so «None» means the same thing in a run as in a card.
func answerOf(level *domain.Auth, inherited *domain.Auth) *domain.Auth {
	if level.Answered() {
		return level
	}
	return inherited
}

// A nil auth stays nil — a level nobody touched; «None» with no answers behind it is stored as
// nothing, so the level still inherits.
func storedAuth(auth *domain.Auth) *domain.Auth {
	if auth == nil {
		return nil
	}
	return auth.Stored()
}
