package dotenv

import (
	"regexp"
	"strings"
)

type Entry struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

var secretHint = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|KEY|AUTH)`)

// SecretName guesses from a name whether what it holds is a secret. A file of another tool does not
// say so — Postman does, Insomnia does not — and the name is the only thing left to read.
func SecretName(name string) bool { return secretHint.MatchString(name) }

func Parse(text string) []Entry {
	var out []Entry

	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		body := line
		if rest, found := strings.CutPrefix(body, "export "); found {
			body = strings.TrimSpace(rest)
		}

		eq := strings.Index(body, "=")
		if eq == -1 {
			continue
		}
		name := strings.TrimSpace(body[:eq])
		if name == "" {
			continue
		}

		value := strings.TrimSpace(body[eq+1:])

		if len(value) >= 2 {
			first, last := value[0], value[len(value)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		out = append(out, Entry{Name: name, Value: value, Secret: SecretName(name)})
	}

	return out
}
