package publication

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"schemo/internal/domain"
)

const (
	maxDepth        = 16
	maxFolders      = 1000
	maxRequests     = 5000
	maxVariables    = 500
	maxParams       = 100
	maxFields       = 200
	maxResponses    = 50
	maxTitleRunes   = 200
	maxNameRunes    = 300
	maxTextRunes    = 8000
	maxExampleRunes = 32 << 10
	// What a page may be called from and what it may say has moved at it. The server holds a document
	// to the same three, and a page refused here is one that would have been refused there.
	maxEnvironments = 20
	maxReleases     = 200
	maxReleaseItems = 200
)

func checkName(name string, most int) error {
	if utf8.RuneCountInString(name) > most {
		return domain.Refuse(domain.CodeNameTooLong, domain.ErrNotAllowed,
			domain.Args{"max": strconv.Itoa(most)})
	}
	return nil
}

func tooMany(what string, held, most int) error {
	if held <= most {
		return nil
	}
	return domain.Refuse(domain.CodeTooManyItems, domain.ErrNotAllowed,
		domain.Args{"what": what, "max": strconv.Itoa(most)})
}

func clip(text string, most int) (string, bool) {
	if utf8.RuneCountInString(text) <= most {
		return text, false
	}
	return string([]rune(text)[:most]), true
}

func methodOf(method string) (string, error) {
	held := strings.ToUpper(strings.TrimSpace(method))
	if held == "" {
		return "GET", nil
	}
	for _, char := range held {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '-' {
			return "", domain.Refuse(domain.CodeMethodUnsupported, domain.ErrNotAllowed,
				domain.Args{"method": method})
		}
	}
	if len(held) > 16 {
		return "", domain.Refuse(domain.CodeMethodUnsupported, domain.ErrNotAllowed,
			domain.Args{"method": method})
	}
	return held, nil
}
