package command

import "strings"

// The characters that separate the words of a pasted command. Three things ask, and they have to
// give the same answer: the tokenizer, the trimming every reader does, and the regexes that name
// the command in the first place.

// spaceRunes is that set by code point. It is not Go's unicode.IsSpace: a paste out of a browser
// carries U+FEFF, which separates tokens here, and it does not carry U+0085, which Go would treat
// as a space. The set is written as code points because a literal tab or non-breaking space in the
// source is invisible, and one of these characters being wrong would be invisible too.
var spaceRunes = []rune{
	0x0009, 0x000a, 0x000b, 0x000c, 0x000d, 0x0020, 0x00a0, 0x1680,
	0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007,
	0x2008, 0x2009, 0x200a, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff,
}

var spaceSet = string(spaceRunes)

func trimSpace(s string) string { return strings.Trim(s, spaceSet) }

func isSpace(r rune) bool { return strings.ContainsRune(spaceSet, r) }

// spaceInner is the set as the body of a regexp character class, and spaceClass wraps it for a
// positive class. Written as ranges rather than as `reClass` used to build it: the class is read by
// eye far more often than it is built, and `\x{2000}-\x{200A}` says "the spaces" where twenty-five
// escapes in a row say nothing.
const (
	spaceInner = `\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}` +
		`\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`
	spaceClass = "[" + spaceInner + "]"
)

// Headers and JSON keys keep the order they were written in: a JS object would enumerate a
// numeric-looking key first, while the order headers go out in is the order somebody wrote them.
// The corpus says nothing about the difference, so fixtures_test.go is where the rule is
// written down.
