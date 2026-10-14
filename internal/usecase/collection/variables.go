package collection

// The `{{tokens}}` a collection answers for everything inside it.

import (
	"context"
	"fmt"
	"strings"

	"schemo/internal/domain"
)

// Above is what the levels over a request answer for it: the authorization one of them gave, and
// the `{{tokens}}` they answer for, outermost first. It is one answer because it is one walk —
// whoever needs one of them needs the other at that moment, and reading the tree twice for it would
// be reading it twice on every keystroke of a card.
type Above struct {
	Auth      *domain.Auth
	Variables []domain.Variable
}

// SaveVariables writes what a collection answers for the requests inside it. The whole set arrives
// as the editor holds it, in order, and comes back trimmed and numbered from one — the shape the
// environments keep theirs in, a row with no name included. The «+» of the editor is what makes a
// row, and a store that dropped it on arrival would be a button doing nothing.
//
// A secret is refused here, and the refusal is the point: a collection is what gets exported,
// duplicated and handed to somebody else, and the export is the collection's own JSON. A value the
// window promises never to put in a DTO cannot live in a thing that leaves the machine. Secrets
// belong to the environment, which is where the sentence for this refusal sends the user.
func (u *UseCase) SaveVariables(
	ctx context.Context,
	id string,
	variables []domain.Variable,
) ([]domain.Collection, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	collection, ok, err := u.collection(ctx, workspace, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("collection %s: %w", id, domain.ErrNotFound)
	}

	cleaned := make([]domain.Variable, 0, len(variables))
	for _, variable := range variables {
		// The name is trimmed and never dropped: a row the user has just added has none yet, and the
		// row is what the next keystroke is typed into.
		variable.Name = strings.TrimSpace(variable.Name)
		if variable.Kind == domain.VariableSecret {
			return nil, domain.Refuse(domain.CodeVariableSecret, domain.ErrNotAllowed, nil)
		}
		// A row the editor has just added arrives without one: ids are minted on this side everywhere,
		// and an id the window invented would be a second kind of name for the same thing.
		if variable.ID == "" {
			variable.ID = u.ids()
		}
		variable.Position = len(cleaned) + 1
		// What the window draws beside a value it was not given: here the value travelled with the
		// set, so "there is something" is simply whether there is.
		variable.HasValue = variable.Value != ""
		cleaned = append(cleaned, variable)
	}

	collection.Variables = cleaned
	if err := u.store.SaveCollection(ctx, workspace, collection); err != nil {
		return nil, err
	}
	return u.store.Collections(ctx, workspace)
}
