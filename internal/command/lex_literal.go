package command

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

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
// its place — the one thing a `\u` escape cannot carry across, and nothing spells half a pair of
// surrogates on purpose.
func codeUnit(v uint16) string { return string(rune(v)) }
