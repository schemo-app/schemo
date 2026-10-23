package workspace

// The `{{tokens}}` a collection answers for everything inside it.

import (
	"strings"

	"schemo/internal/domain"
)

// Above is what the levels over a request answer for it: the authorization one of them gave and the
// `{{tokens}}` they answer for, outermost first. It is one answer because it is one walk: whoever
// needs one needs the other at that moment, and reading the tree twice would double the work on
// every keystroke of a card.
type Above struct {
	Auth      *domain.Auth
	Variables []domain.Variable
}

// cleanVariables trims and numbers a collection's variables, keeping rows without a name (the
// editor's "+" makes one). A secret is refused: a collection is exported and shared, and secrets
// belong to environments.
func (s *Collections) cleanVariables(variables []domain.Variable) ([]domain.Variable, error) {
	cleaned := make([]domain.Variable, 0, len(variables))
	for _, variable := range variables {
		variable.Name = strings.TrimSpace(variable.Name)
		if variable.Kind == domain.VariableSecret {
			return nil, domain.Refuse(domain.CodeVariableSecret, domain.ErrNotAllowed, nil)
		}
		if variable.ID == "" {
			variable.ID = s.ids()
		}
		variable.Position = len(cleaned) + 1
		variable.HasValue = variable.Value != ""
		cleaned = append(cleaned, variable)
	}
	return cleaned, nil
}
