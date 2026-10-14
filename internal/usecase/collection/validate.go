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

// maxDescriptionLength is what still reads as a description and not as a document pasted into a
// header. It is generous: a description is markdown now, and a page about what a collection is for
// is a page, not a paragraph.
const maxDescriptionLength = 2000

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

// validDocs trims the prose a collection carries and holds each note to the same ceiling a
// description has: all four are written in the same editor and read as the same kind of page. An
// empty note is an answer — the page draws the three headings it has something under.
func validDocs(docs domain.Docs) (domain.Docs, error) {
	held := domain.Docs{
		Authorization: strings.TrimSpace(docs.Authorization),
		Errors:        strings.TrimSpace(docs.Errors),
		Limits:        strings.TrimSpace(docs.Limits),
	}
	for _, note := range []string{held.Authorization, held.Errors, held.Limits} {
		if len([]rune(note)) > maxDescriptionLength {
			return domain.Docs{}, domain.Refuse(domain.CodeDescriptionTooLong, domain.ErrNotAllowed,
				domain.Args{"max": strconv.Itoa(maxDescriptionLength)})
		}
	}
	return held, nil
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
