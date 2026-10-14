package command

// The lexer: the words a curl command comes apart into, and the quoting it comes apart by.

import (
	"strings"
	"unicode/utf8"
)

// optString keeps `null` (the flag had no value) apart from `""` (its value was empty): the TS
// decides the two differently — an empty `--url` is not a missing one — and the port follows.
type optString struct {
	value string
	set   bool
}

// splitFlag separates `--flag=value`. The inline value is nil when the token carries none, and for
// a token that is not a flag at all.
func splitFlag(token string) (string, *string) {
	if !strings.HasPrefix(token, "-") {
		return token, nil
	}
	eq := strings.Index(token, "=")
	if eq > 0 {
		inline := token[eq+1:]
		return token[:eq], &inline
	}
	return token, nil
}

// The TS's taker(): the inline value, else the next token, advancing the cursor past what it took.
func take(tokens []string, cursor *int, inline *string) optString {
	if inline != nil {
		return optString{value: *inline, set: true}
	}
	if *cursor+1 < len(tokens) {
		*cursor++
		return optString{value: tokens[*cursor], set: true}
	}
	return optString{}
}

// A curl command copied on Windows is continued with a caret as often as with a backslash, so both
// count, and the spaces and tabs before the newline go with them.
func lineJoin(input string, at int) int {
	ch := input[at]
	if ch != '\\' && ch != '^' {
		return -1
	}
	j := at + 1
	for j < len(input) && (input[j] == ' ' || input[j] == '\t') {
		j++
	}
	if j < len(input) && input[j] == '\n' {
		return j + 1
	}
	return -1
}

// byteAt is `input[i]` in the TS: an index past the end reads as the zero byte, which no comparison
// in the reader matches, so the checks stay one-liners instead of bounds tests.
func byteAt(s string, i int) byte {
	if i < 0 || i >= len(s) {
		return 0
	}
	return s[i]
}

type readResult struct {
	text string
	next int
}

func readQuoted(text string, at int) (readResult, bool) {
	if text[at] == '\'' {
		return readSingle(text, at)
	}
	return readDouble(text, at)
}

// readSingle reads a '…' string. The close-escape-reopen dance a shell uses to embed a quote —
// `'\”` — is understood, because a command copied from a browser or a terminal is full of it.
func readSingle(input string, at int) (readResult, bool) {
	i := at + 1
	var text strings.Builder
	for i < len(input) {
		if input[i] == '\'' {
			if byteAt(input, i+1) == '\\' && byteAt(input, i+2) == '\'' && byteAt(input, i+3) == '\'' {
				text.WriteByte('\'')
				i += 4
				continue
			}
			return readResult{text: text.String(), next: i + 1}, true
		}
		size := runeSize(input, i)
		text.WriteString(input[i : i+size])
		i += size
	}
	return readResult{}, false
}

// readDouble reads a "…" string. Only a quote, a backslash and a dollar may be escaped: anything
// else keeps the backslash as written, which is how `"C:\Users"` survives.
func readDouble(input string, at int) (readResult, bool) {
	i := at + 1
	var text strings.Builder
	for i < len(input) {
		c := input[i]
		if c == '\\' {
			if i+1 >= len(input) {
				// A trailing escape has nothing to escape, which reads as unclosed.
				return readResult{}, false
			}
			nx := input[i+1]
			if nx == '\n' {
				i += 2
				continue
			}
			if nx == '"' || nx == '\\' || nx == '$' || nx == '`' {
				text.WriteByte(nx)
				i += 2
				continue
			}
			text.WriteByte(c)
			i++
			continue
		}
		if c == '"' {
			return readResult{text: text.String(), next: i + 1}, true
		}
		size := runeSize(input, i)
		text.WriteString(input[i : i+size])
		i += size
	}
	return readResult{}, false
}

// tokenize splits a command's arguments, reading quotes as it goes. The bool is false when a quote
// never closes, which the caller reports as bad-quotes rather than guessing at a split.
func tokenize(input string) ([]string, bool) {
	out := []string{}
	i := 0
	for i < len(input) {
		if join := lineJoin(input, i); join != -1 {
			i = join
			continue
		}
		r, size := utf8.DecodeRuneInString(input[i:])
		if isSpace(r) || input[i] == ';' {
			i += size
			continue
		}

		var token strings.Builder
		for i < len(input) {
			if join := lineJoin(input, i); join != -1 {
				i = join
				continue
			}
			size := runeSize(input, i)
			r, _ := utf8.DecodeRuneInString(input[i:])
			if isSpace(r) || input[i] == ';' {
				break
			}
			c := input[i]
			switch {
			case c == '\'' || c == '"':
				res, ok := readQuoted(input, i)
				if !ok {
					return nil, false
				}
				token.WriteString(res.text)
				i = res.next
			case c == '$' && byteAt(input, i+1) == '\'':
				res, ok := readAnsiC(input, i)
				if !ok {
					return nil, false
				}
				token.WriteString(res.text)
				i = res.next
			case c == '\\':
				if i+1 >= len(input) {
					return nil, false
				}
				token.WriteString(input[i+1 : i+1+runeSize(input, i+1)])
				i += 1 + runeSize(input, i+1)
			default:
				token.WriteString(input[i : i+size])
				i += size
			}
		}
		out = append(out, token.String())
	}
	return out, true
}
