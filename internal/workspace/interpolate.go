package workspace

// The `{{name}}` grammar. Other features ask through a port so the grammar stays in one place.

import (
	"strings"
	"unicode/utf8"

	"schemo/internal/domain"
)

// lookup answers what a name comes to, and whether it came to anything at all.
type lookup func(name string) (domain.Resolution, bool)

// token is one `{{name}}` in a text: where it starts, where it ends, what it is called, and how it
// was written.
type token struct {
	start int
	end   int
	name  string
	raw   string
}

// tokens finds every `{{name}}`. `\{{` is literal and an unclosed `{{` is text; names are trimmed
// with strings.TrimSpace.
func tokens(text string) []token {
	out := []token{}
	i := 0
	for i < len(text) {
		if text[i] == '\\' && strings.HasPrefix(text[i:], `\{{`) {
			i += 3
			continue
		}
		if !strings.HasPrefix(text[i:], "{{") {
			i++
			continue
		}

		close := strings.Index(text[i+2:], "}}")
		if close == -1 {
			break
		}
		close += i + 2
		name := strings.TrimSpace(text[i+2 : close])
		if !domain.IsVariableName(name) {
			i += 2
			continue
		}

		out = append(out, token{start: i, end: close + 2, name: name, raw: text[i : close+2]})
		i = close + 2
	}
	return out
}

// maxDepth is how deep a value that names other variables (`{{host}}:{{port}}`) is followed.
const maxDepth = 10

// interpolate fills a text's variables; with maskSecrets a secret becomes its mask.
func interpolate(text string, resolve lookup, maskSecrets bool) string {
	return expand(text, resolve, maskSecrets, map[string]bool{}, 0)
}

func expand(text string, resolve lookup, maskSecrets bool, open map[string]bool, depth int) string {
	found := tokens(text)
	if len(found) == 0 {
		return text
	}

	var out strings.Builder
	last := 0
	for _, t := range found {
		out.WriteString(text[last:t.start])
		switch r, ok := resolve(t.name); {
		case !ok, open[t.name], depth >= maxDepth:
			out.WriteString(t.raw)
		case maskSecrets && r.Kind == domain.VariableSecret:
			out.WriteString(domain.SecretMask)
		default:
			open[t.name] = true
			out.WriteString(expand(r.Value, resolve, maskSecrets, open, depth+1))
			delete(open, t.name)
		}
		last = t.end
	}
	out.WriteString(text[last:])
	return out.String()
}

// shortestRedacted is the shortest value searched for: shorter ones appear in every address.
const shortestRedacted = 6

// redact masks secret values a script substituted in, where no braces are left to find. Longest
// first, so overlapping values are not half-masked.
func redact(text string, secrets []string) string {
	for _, secret := range secrets {
		if utf8.RuneCountInString(secret) < shortestRedacted {
			continue
		}
		text = strings.ReplaceAll(text, secret, domain.SecretMask)
	}
	return text
}

// missing lists the names a text mentions that came to nothing, in the order they were written and
// each of them once.
func missing(text string, resolve lookup) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range tokens(text) {
		if _, ok := resolve(t.name); ok || seen[t.name] {
			continue
		}
		seen[t.name] = true
		out = append(out, t.name)
	}
	return out
}
