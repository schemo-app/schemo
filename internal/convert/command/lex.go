package command

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// The lexer: the words a curl command comes apart into, and the quoting it comes apart by.

// optString keeps a flag that had no value apart from one whose value was empty: an empty `--url`
// is not a missing one.
type optString struct {
	value string
	set   bool
}

// splitCluster reads a word of single-dash letters the way curl does: `-sSL` is three flags, and the
// first that takes a value takes the rest of the word with it, as in `-XPOST` or `-uuser:pass`. Any
// other token comes back as it is.
func splitCluster(token string) []string {
	if len(token) < 3 || token[0] != '-' || token[1] == '-' {
		return []string{token}
	}

	var flags []string
	for offset, letter := range token[1:] {
		flag := "-" + string(letter)
		flags = append(flags, flag)
		if !curlFlags[flag] {
			continue
		}
		if rest := token[1+offset+utf8.RuneLen(letter):]; rest != "" {
			flags = append(flags, rest)
		}
		break
	}
	return flags
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

// take is a flag's value: the inline one, else the next token, advancing the cursor past what it took.
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

// byteAt is `s[i]` where an index past the end reads as the zero byte, which no comparison in the
// reader matches, so the checks stay one-liners instead of bounds tests.
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

// The string literals a command carries: `$'…'` with its C escapes, and the `\uXXXX` a literal may
// spell a character with.

// `$'…'` escapes are C's: only the four a pasted command ever carries are translated, and anything
// else stands for the character that follows the backslash.
func readAnsiC(input string, at int) (readResult, bool) {
	i := at + 2
	var text strings.Builder
	for i < len(input) {
		c := input[i]
		if c == '\'' {
			return readResult{text: text.String(), next: i + 1}, true
		}
		if c != '\\' {
			size := runeSize(input, i)
			text.WriteString(input[i : i+size])
			i += size
			continue
		}
		if i+1 >= len(input) {
			return readResult{}, false
		}
		nx := input[i+1]
		switch nx {
		case 'n':
			text.WriteByte('\n')
		case 't':
			text.WriteByte('\t')
		case 'r':
			text.WriteByte('\r')
		case 'u':
			// Four characters after the `\u`, clamped at the end of the string.
			end := i + 6
			if end > len(input) {
				end = len(input)
			}
			text.WriteString(codeUnit(ansiCodeUnit(input[i+2 : end])))
		default:
			text.WriteByte(nx)
		}
		if nx == 'u' {
			i += 6
		} else {
			i += 2
		}
	}
	return readResult{}, false
}

// runeSize is the width of the character at i, so that a reader can copy text through without
// splitting a multi-byte character. A byte that is not valid UTF-8 comes through as itself.
func runeSize(s string, i int) int {
	if s[i] < utf8.RuneSelf {
		return 1
	}
	_, size := utf8.DecodeRuneInString(s[i:])
	return size
}

// Anything that is not hex digits reads as zero, which is where a `\u` with nothing usable lands.
func ansiCodeUnit(s string) uint16 {
	n, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return 0
	}
	return uint16(n)
}

// codeUnit is one UTF-16 code unit as text. Go cannot hold a lone surrogate and writes U+FFFD in
// its place, which a `\u` escape cannot carry across.
func codeUnit(v uint16) string { return string(rune(v)) }

// spaceRunes are the characters that separate the words of a pasted command, shared by the
// tokenizer, the trimming and the detection regexes. It is not unicode.IsSpace: a browser paste
// carries U+FEFF, which separates here, and not U+0085. They are written as code points because a
// literal tab or non-breaking space is invisible.
var spaceRunes = []rune{
	0x0009, 0x000a, 0x000b, 0x000c, 0x000d, 0x0020, 0x00a0, 0x1680,
	0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007,
	0x2008, 0x2009, 0x200a, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff,
}

var spaceSet = string(spaceRunes)

func trimSpace(s string) string { return strings.Trim(s, spaceSet) }

func isSpace(r rune) bool { return strings.ContainsRune(spaceSet, r) }

// spaceInner is the set as the body of a regexp character class and spaceClass wraps it. Ranges
// read better than twenty-five escapes in a row.
const (
	spaceInner = `\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}` +
		`\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`
	spaceClass = "[" + spaceInner + "]"
)
