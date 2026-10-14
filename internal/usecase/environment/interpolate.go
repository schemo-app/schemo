package environment

// The `{{name}}` grammar: what a token is, which names are worth reading, and what a text comes to
// once the values are in it.
//
// It lives here because it is this feature's rule and nobody else's: a name names a variable of an
// environment, and "nothing came of it" means what blocks a send. The draft, which paints tokens as
// it draws an address field, asks through the port it declares for itself rather than reading the
// grammar — so what a token is stays in one place.

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

func validName(name string) bool { return domain.IsVariableName(name) }

// tokens finds every `{{name}}` worth resolving in a text. `\{{` is an escape and stays literal,
// and a `{{` that never closes is text — the rest of the line is read as it was written.
//
// The name is trimmed the way Go trims: JS's own set differs from it in two characters, U+FEFF and
// U+0085, and neither is something anybody puts inside the braces. The set is kept in the command
// lexer instead, where a BOM pasted between words is a case that really happens.
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
		if !validName(name) {
			// `{{a b}}` is not a name, so it is text. The scan resumes at the second brace rather
			// than past the token, so that a real one written inside it is still found.
			i += 2
			continue
		}

		out = append(out, token{start: i, end: close + 2, name: name, raw: text[i : close+2]})
		i = close + 2
	}
	return out
}

// interpolate writes a text out with its variables filled in. maskSecrets is the difference between
// the request that goes out and everything that outlives it: the preview, an export, the record — a
// secret leaves those as its mask and nothing else does.
//
// A name that came to nothing is written as nothing: the window says so beside the request line,
// and a `{{token}}` carried to a server is a name in braces. Which names those were is a separate
// question, asked by `Missing`.
func interpolate(text string, resolve lookup, maskSecrets bool) string {
	found := tokens(text)
	if len(found) == 0 {
		return text
	}

	var out strings.Builder
	last := 0
	for _, t := range found {
		out.WriteString(text[last:t.start])
		switch r, ok := resolve(t.name); {
		case !ok:
			// Nothing: a name nothing answers leaves nothing behind.
		case maskSecrets && r.Kind == domain.VariableSecret:
			out.WriteString(domain.SecretMask)
		default:
			out.WriteString(r.Value)
		}
		last = t.end
	}
	out.WriteString(text[last:])
	return out.String()
}

// shortestRedacted is the floor under which a value is not searched for. The search replaces plain
// text that was written for other reasons, and a value of a character or two occurs in every
// address: hiding it would leave a record describing a request nobody wrote, which is worse than a
// value no server would take as a credential. Six runes is where a value is long enough to be one.
const shortestRedacted = 6

// redact replaces a secret's value with its mask wherever the text still carries it. The values
// arrive longest first, so one that contains another leaves neither half-written.
//
// It is the half of masking a rewritten request cannot do without. A token is recognised by its
// braces; a value that has already been substituted in has none, and a pre-request script is handed
// the request with its values in it and hands one back. Everything that outlives the send is
// rendered from that text, so the value is all that is left to find.
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
