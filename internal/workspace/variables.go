package workspace

// The `{{tokens}}` a collection answers for everything inside it.

import (
	"strings"

	"schemo/internal/domain"
)

// Above is the auth and variables the levels over a request answer, read in one walk.
type Above struct {
	Auth   *domain.Auth
	Levels domain.VarLevels
}

// cleanVariables trims and numbers collection variables, keeping unnamed rows. Secrets are refused:
// collections are shared, secrets belong to environments.
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
