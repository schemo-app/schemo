package workspace

// The auth walk the card and the run both ask: the nearest level above a request that answered.

import (
	"context"
	"fmt"

	"schemo/internal/domain"
)

// Above is everything the levels over a request answer for it, read in one walk because the same
// caller needs both at the same moment: the authorization the nearest level that gave one answered
// with, and the `{{tokens}}` they answer for, outermost first.
//
// The command line's draft is in no tree, and a deleted node is in none either. Neither is a
// failure: "there is nothing above this request" is the answer.
//
//wails:ignore
func (s *Collections) Above(ctx context.Context, id domain.DraftID) (Above, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return Above{}, err
	}
	tree, err := s.store.Collections(ctx, workspace)
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
		at := effectiveAuth(collection.Auth, inherited)
		variables := appendLevel(above, collection.Variables)

		for _, node := range collection.Items {
			if node.ID == id {
				return Above{Auth: effectiveAuth(node.Auth, at), Variables: variables}, true
			}
		}
		if found, ok := aboveUnder(collection.Children, id, at, variables); ok {
			return found, true
		}
	}
	return Above{}, false
}

// Both walks over the tree go through here, so "None" means the same thing in a run as in a card.
func effectiveAuth(level *domain.Auth, inherited *domain.Auth) *domain.Auth {
	if level.Answered() {
		return level
	}
	return inherited
}

// A nil auth stays nil — a level nobody touched; "None" with no answers behind it is stored as
// nothing, so the level still inherits.
func storedAuth(auth *domain.Auth) *domain.Auth {
	if auth == nil {
		return nil
	}
	return auth.Stored()
}

// runnable is one request of a run together with the authorization the walk found above it: the
// tree is what knows about inheritance, and the sender is told the answer rather than the tree.
type runnable struct {
	node domain.CollectionNode
	auth *domain.Auth
	// What the levels above answer for `{{tokens}}`, outermost first: the walk down the tree is where
	// it is collected, and the sender is handed it because by then the tree is out of reach.
	variables []domain.Variable
}

// A request is a list of one, so running a saved request and running a collection are the same
// call. A node the collection does not have is a deletion the window has not heard about yet, not
// an empty run.
//
// The tree is walked from the root rather than from the collection the window named, because a
// folder two levels down still inherits from the levels between it and the root: a detached subtree
// would silently lose their authorization and variables.
func runnablesUnder(tree []domain.Collection, collectionID, nodeID string) ([]runnable, error) {
	anchor, inherited, above, ok := findCollectionInherited(tree, collectionID, nil, nil)
	if !ok {
		return nil, collectionNotFound(collectionID)
	}
	if nodeID == "" || nodeID == collectionID {
		return runnablesIn(anchor, inherited, above), nil
	}

	// What the run was started from is a level inside the anchor, and the anchor is one of the levels
	// above it — so the pair handed to the walk is the anchor's own, not the one it was given.
	at := effectiveAuth(anchor.Auth, inherited)
	variables := appendLevel(above, anchor.Variables)

	if nested, nAt, nAbove, ok := findCollectionInherited(anchor.Children, nodeID, at, variables); ok {
		return runnablesIn(nested, nAt, nAbove), nil
	}
	node, auth, nAbove, ok := findNodeInherited(anchor, nodeID, at, variables)
	if !ok {
		return nil, fmt.Errorf("node %s: %w", nodeID, domain.ErrNotFound)
	}
	return []runnable{{node: node, auth: auth, variables: nAbove}}, nil
}

// findCollectionInherited is a collection and what the levels above it answer — the pair requestsIn
// needs to carry the walk down, and the same walk aboveUnder does for a card.
func findCollectionInherited(
	collections []domain.Collection,
	id string,
	inherited *domain.Auth,
	above []domain.Variable,
) (domain.Collection, *domain.Auth, []domain.Variable, bool) {
	for _, collection := range collections {
		if collection.ID == id {
			return collection, inherited, above, true
		}
		at := effectiveAuth(collection.Auth, inherited)
		variables := appendLevel(above, collection.Variables)
		if found, fAt, fAbove, ok := findCollectionInherited(collection.Children, id, at, variables); ok {
			return found, fAt, fAbove, true
		}
	}
	return domain.Collection{}, nil, nil, false
}

// findNodeInherited is the same walk for a request inside a collection: it answers with the level
// the request itself goes out under, which is what a runnable carries.
func findNodeInherited(
	collection domain.Collection,
	id string,
	inherited *domain.Auth,
	above []domain.Variable,
) (domain.CollectionNode, *domain.Auth, []domain.Variable, bool) {
	for _, node := range collection.Items {
		if node.ID == id {
			return node, effectiveAuth(node.Auth, inherited), above, true
		}
	}
	for _, nested := range collection.Children {
		at := effectiveAuth(nested.Auth, inherited)
		variables := appendLevel(above, nested.Variables)
		if node, nAt, nAbove, ok := findNodeInherited(nested, id, at, variables); ok {
			return node, nAt, nAbove, true
		}
	}
	return domain.CollectionNode{}, nil, nil, false
}

// appendLevel copies rather than appending to what it was handed: the walk branches, and two
// branches appending to one slice would give the second one the first one's answers.
func appendLevel(above, level []domain.Variable) []domain.Variable {
	out := make([]domain.Variable, 0, len(above)+len(level))
	out = append(out, above...)
	return append(out, level...)
}

// The order is the level's own, not requests-then-collections, and the answer of the levels above
// is carried down.
func runnablesIn(
	collection domain.Collection,
	inherited *domain.Auth,
	above []domain.Variable,
) []runnable {
	at := effectiveAuth(collection.Auth, inherited)
	// A level's own variables stand over the ones above it, so they are appended: the reader takes the
	// last answer for a name, which is the nearest level.
	variables := appendLevel(above, collection.Variables)

	out := []runnable{}
	for _, entry := range collection.Level() {
		if entry.Collection != nil {
			out = append(out, runnablesIn(*entry.Collection, at, variables)...)
			continue
		}
		out = append(out, runnable{
			node:      *entry.Node,
			auth:      effectiveAuth(entry.Node.Auth, at),
			variables: variables,
		})
	}
	return out
}

// findCollection looks a collection up by id, the ones inside collections included: a collection
// answers to an id wherever it sits, because running one and dropping something into it are the
// same kind of question.
func findCollection(tree []domain.Collection, id string) (domain.Collection, bool) {
	for _, collection := range tree {
		if collection.ID == id {
			return collection, true
		}
		if found, ok := findCollection(collection.Children, id); ok {
			return found, true
		}
	}
	return domain.Collection{}, false
}
