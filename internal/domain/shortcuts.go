package domain

import (
	"slices"
	"strconv"
	"strings"
)

// ShortcutNone is what a command is left with when somebody takes its combination for another one:
// a command that does nothing is a state the window draws, and "not assigned" is a word for it
// rather than a missing row.
const ShortcutNone = "none"

// shortcutModifiers are the names a combination is written with, in the only order it is kept in.
// `mod` is the platform's own: ⌘ on macOS, Ctrl everywhere else.
var shortcutModifiers = []string{"mod", "alt", "shift"}

// shortcutKeys are the keys a combination may end with, beyond a single letter or digit: the named
// ones and the punctuation the recording of a combination writes down.
var shortcutKeys = map[string]bool{
	"enter": true, ",": true, ".": true, "/": true, ";": true, "'": true,
	"[": true, "]": true, "-": true, "=": true, "`": true,
}

// ValidShortcut reports whether a combination is one this app can carry out: modifiers in the
// canonical order, each at most once, and one key — a letter, a digit, a named key, or an F-key.
// The string is what a window stores and compares, and a shape nobody writes is one no keyboard can
// ever match.
func ValidShortcut(combo string) bool {
	if combo == ShortcutNone {
		return true
	}
	parts := strings.Split(combo, "+")
	key := parts[len(parts)-1]
	if !validShortcutKey(key) {
		return false
	}
	if len(parts) == 1 {
		// A bare key is one only the function keys are: an unmodified letter would swallow typing.
		return isFunctionKey(key)
	}

	// The modifiers are looked up strictly left to right, so one that is out of order, sent twice,
	// or not a modifier at all leaves the walk where it cannot go on.
	next := 0
	for _, mod := range parts[:len(parts)-1] {
		at := slices.Index(shortcutModifiers[next:], mod)
		if at < 0 {
			return false
		}
		next += at + 1
	}
	return true
}

func isFunctionKey(key string) bool {
	rest, ok := strings.CutPrefix(key, "F")
	if !ok {
		return false
	}
	n, err := strconv.Atoi(rest)
	return err == nil && n >= 1 && n <= 24
}

func validShortcutKey(key string) bool {
	if len(key) == 1 {
		switch {
		case key >= "A" && key <= "Z", key >= "0" && key <= "9", shortcutKeys[key]:
			return true
		}
		return false
	}
	return isFunctionKey(key) || shortcutKeys[key]
}
