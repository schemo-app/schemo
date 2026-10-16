package collection

// What a name and a description may be. Both are refused with the code the window words the
// sentence from, so an empty name and an overlong one are not the same news.

import (
	"strconv"
	"strings"

	"schemo/internal/domain"
)

// maxNameLength is the same ceiling the environments screen uses, so a name that is too long means
// the same thing wherever it is typed.
const maxNameLength = 120

// maxDescriptionLength accommodates the complete Markdown document, including public sections.
const maxDescriptionLength = 8000

func validName(name string) (string, error) {
	return domain.CleanName(name, maxNameLength)
}

// validDescription trims what was typed and keeps it within the ceiling. An empty description is a
// real answer — the header shows a placeholder for it — so it is not refused the way an empty name
// is.
func validDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	if len([]rune(description)) > maxDescriptionLength {
		return "", domain.Refuse(domain.CodeDescriptionTooLong, domain.ErrNotAllowed,
			domain.Args{"max": strconv.Itoa(maxDescriptionLength)})
	}
	return description, nil
}

// clip keeps a name inside the ceiling. Duplicating at the limit is a thing a user does, and
// refusing it would be the app's problem, not theirs.
func clip(name string) string {
	runes := []rune(name)
	if len(runes) <= maxNameLength {
		return name
	}
	return string(runes[:maxNameLength])
}
