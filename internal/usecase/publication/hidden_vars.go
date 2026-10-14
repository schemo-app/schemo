package publication

// Which variable values a page may carry, and why the rest may not.
//
// The rule has no exception and no switch: a value the app already hides is hidden here too, and a
// name that reads like a credential's is hidden beside it. A person is shown the list — «Не попадут
// на страницу» — but is not offered a way to put one back.

import (
	"regexp"
	"strings"
	"unicode"

	"schemo/internal/domain"
)

// secretWords are the words that make a name a warning in itself. They are matched as whole words
// of a spelled-out name, so `api_key` and `apiKey` are both read as one, and `monkey` is not read
// as `key`: the point is to catch what a person meant, not to catch every letter in order.
var secretWords = map[string]bool{
	"token": true, "secret": true, "password": true, "passwd": true, "pass": true, "pwd": true,
	"key": true, "apikey": true, "auth": true, "authorization": true, "bearer": true,
	"session": true, "cookie": true, "signature": true, "credential": true, "credentials": true,
	"private": true,
}

// variableToken is a `{{name}}` as a value is written in terms of one. The grammar is the app's —
// a name is letters, digits and underscores — and anything else between braces is left alone.
var variableToken = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// hiddenNames is every name whose value a page may not carry, with the reason for each. The scopes
// are read together and not one by one: a name is hidden wherever it is used, so a global secret
// hides a collection variable that is written in terms of it, and so does one of another level.
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

	// A variable written in terms of a hidden one carries that value wherever it is used, so the
	// hiding closes over the references until nothing more is reachable from what is already
	// hidden.
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
	for _, word := range wordsOf(name) {
		if secretWords[word] {
			return true
		}
	}
	return false
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

// maskText replaces every `{{name}}` a hidden variable answers with the mask this app already uses
// for a value that must not leave it. The name goes with the value: a token names a variable, so a
// secret's name is part of the secret, and a page read by a stranger is no place for it.
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

// wordsOf splits a name into the words it is made of, on the separators people write names with
// and on the case changes they write them across: `apiKey`, `API-KEY` and `api_key` are the same
// two words.
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
