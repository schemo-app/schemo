// Package jsonpointer reads and writes JSON Pointers, RFC 6901: the "/a/0/b" that names one value in
// a JSON document, and the same pointer spelled as a URI fragment ("#/a/0/b") in a $ref.
package jsonpointer

import (
	"errors"
	"net/url"
	"strings"
)

// ErrSyntax is a pointer that is not one: a non-empty pointer starts with a slash.
var ErrSyntax = errors.New("a JSON pointer is empty or starts with a slash")

// escaper and unescaper are single-pass on purpose: "~01" is "~1" (a tilde and a one), and a second
// pass over the output of the first would turn it into a slash.
var (
	escaper   = strings.NewReplacer("~", "~0", "/", "~1")
	unescaper = strings.NewReplacer("~1", "/", "~0", "~")
)

// Parse splits a pointer into its reference tokens, unescaped. The empty pointer names the whole
// document and has none; "/" has one, the empty key.
func Parse(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, ErrSyntax
	}
	tokens := strings.Split(pointer[1:], "/")
	for i, token := range tokens {
		tokens[i] = unescaper.Replace(token)
	}
	return tokens, nil
}

// ParseFragment reads a pointer written as the fragment of a URI reference, with or without the
// leading "#". The fragment is percent-decoded first (§6), so "#/definitions/My%20Model" reaches
// the key "My Model".
func ParseFragment(fragment string) ([]string, error) {
	fragment = strings.TrimPrefix(fragment, "#")
	decoded, err := url.PathUnescape(fragment)
	if err != nil {
		return nil, err
	}
	return Parse(decoded)
}

// Join writes tokens as a pointer, escaping the two characters a token may not carry as they are.
func Join(tokens []string) string {
	var pointer strings.Builder
	for _, token := range tokens {
		pointer.WriteByte('/')
		pointer.WriteString(escaper.Replace(token))
	}
	return pointer.String()
}

// Index reads a token as a position in an array of that length (§4): digits with no leading zero.
// The "-" that names the element after the last is not an element, so it is not a position.
func Index(token string, length int) (int, bool) {
	if token == "" || (len(token) > 1 && token[0] == '0') {
		return 0, false
	}
	position := 0
	for _, digit := range token {
		if digit < '0' || digit > '9' {
			return 0, false
		}
		position = position*10 + int(digit-'0')
		if position >= length {
			return 0, false
		}
	}
	return position, true
}
