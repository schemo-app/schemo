package cloud

import (
	"regexp"
	"schemo/internal/domain"
	"slices"
	"strings"
	"unicode"
)

// Which variable values a page may carry. A value the app hides, or a name that reads like a
// credential, is never published, and there is no override.

// secretWords make a name look like a credential. They match whole words, so `api_key` and `apiKey`
// match and `monkey` does not.
var secretWords = map[string]bool{
	"token": true, "secret": true, "password": true, "passwd": true, "pass": true, "pwd": true,
	"key": true, "apikey": true, "auth": true, "authorization": true, "bearer": true,
	"session": true, "cookie": true, "signature": true, "credential": true, "credentials": true,
	"private": true,
}

// variableToken is a `{{name}}` as a value is written in terms of one; anything else between braces
// is left alone.
var variableToken = regexp.MustCompile(`\{\{\s*(` + domain.VariableNamePattern + `)\s*\}\}`)

// hiddenNames is every name a page may not carry, with the reason. Scopes are read together, since a
// variable referencing a secret anywhere is hidden too.
func hiddenNames(scopes ...[]domain.Variable) map[string]domain.HiddenReason {
	var every []domain.Variable
	for _, scope := range scopes {
		every = append(every, scope...)
	}

	hidden := map[string]domain.HiddenReason{}
	value := map[string]string{}
	for _, variable := range every {
		value[variable.Name] = variable.Value
		if _, held := hidden[variable.Name]; held {
			continue
		}
		switch {
		case variable.Kind == domain.VariableSecret:
			hidden[variable.Name] = domain.HiddenSecret
		case looksLikeSecret(variable.Name):
			hidden[variable.Name] = domain.HiddenLooksSecret
		}
	}

	// Close over references until nothing new is hidden.
	for changed := true; changed; {
		changed = false
		for _, variable := range every {
			if _, held := hidden[variable.Name]; held {
				continue
			}
			for _, name := range referenced(value[variable.Name]) {
				if _, held := hidden[name]; held {
					hidden[variable.Name] = domain.HiddenLinked
					changed = true
					break
				}
			}
		}
	}
	return hidden
}

// looksLikeSecret is whether a name reads like one a credential is kept in.
func looksLikeSecret(name string) bool {
	return slices.ContainsFunc(wordsOf(name), func(word string) bool { return secretWords[word] })
}

// referenced is the names a value is written in terms of.
func referenced(value string) []string {
	matches := variableToken.FindAllStringSubmatch(value, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match[1])
	}
	return out
}

// maskText masks every `{{name}}` of a hidden variable, name included: a secret's name is part of it.
func maskText(text string, hidden map[string]domain.HiddenReason) string {
	if len(hidden) == 0 || !strings.Contains(text, "{{") {
		return text
	}
	return variableToken.ReplaceAllStringFunc(text, func(token string) string {
		name := variableToken.FindStringSubmatch(token)[1]
		if _, held := hidden[name]; held {
			return domain.SecretMask
		}
		return token
	})
}

// wordsOf splits a name on separators and case changes: `apiKey`, `API-KEY` and `api_key` are the same words.
func wordsOf(name string) []string {
	var out []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			out = append(out, strings.ToLower(string(word)))
			word = nil
		}
	}

	runes := []rune(name)
	for index, char := range runes {
		switch {
		case char == '_' || char == '-' || char == '.' || char == ' ':
			flush()
		case unicode.IsUpper(char) && index > 0 &&
			(unicode.IsLower(runes[index-1]) || unicode.IsDigit(runes[index-1])):
			flush()
			word = append(word, char)
		default:
			word = append(word, char)
		}
	}
	flush()
	return out
}
