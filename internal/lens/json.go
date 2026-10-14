package lens

import (
	"encoding/json"
	"fmt"
	"strings"

	"schemo/internal/domain"
)

// maxDepth bounds recursion, so that a hostile body cannot exhaust the stack.
const maxDepth = 256

// parseJSON reads one JSON value and keeps where each part lies; encoding/json does not.
func parseJSON(text string) (*node, error) {
	p := &jsonParser{s: text}
	p.skip()
	root, err := p.value(nil, 0)
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.i != len(p.s) {
		return nil, p.fail("text after the value")
	}
	return root, nil
}

type jsonParser struct {
	s string
	i int
}

func (p *jsonParser) fail(what string) error {
	return fmt.Errorf("%w: %s at byte %d", domain.ErrUnreadable, what, p.i)
}

func (p *jsonParser) skip() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jsonParser) value(parent *node, depth int) (*node, error) {
	if depth > maxDepth {
		return nil, p.fail("nested too deeply")
	}
	if p.i >= len(p.s) {
		return nil, p.fail("the text ends")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object(parent, depth)
	case c == '[':
		return p.array(parent, depth)
	case c == '"':
		return p.stringNode(parent)
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number(parent)
	default:
		return p.literal(parent)
	}
}

func (p *jsonParser) object(parent *node, depth int) (*node, error) {
	n := &node{kind: domain.NodeObject, parent: parent, start: p.i, keyStart: -1}
	p.i++
	p.skip()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		n.end = p.i
		return n, nil
	}
	for {
		p.skip()
		if p.i >= len(p.s) || p.s[p.i] != '"' {
			return nil, p.fail("a key was expected")
		}
		keyStart := p.i
		key, err := p.rawString()
		if err != nil {
			return nil, err
		}
		p.skip()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, p.fail("':' was expected")
		}
		p.i++
		p.skip()
		kid, err := p.value(n, depth+1)
		if err != nil {
			return nil, err
		}
		kid.name, kid.keyStart = key, keyStart
		n.kids = append(n.kids, kid)
		p.skip()
		if p.i >= len(p.s) {
			return nil, p.fail("the object is not closed")
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			n.end = p.i
			return n, nil
		default:
			return nil, p.fail("',' or '}' was expected")
		}
	}
}

func (p *jsonParser) array(parent *node, depth int) (*node, error) {
	n := &node{kind: domain.NodeArray, parent: parent, start: p.i, keyStart: -1}
	p.i++
	p.skip()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		n.end = p.i
		return n, nil
	}
	for {
		p.skip()
		kid, err := p.value(n, depth+1)
		if err != nil {
			return nil, err
		}
		kid.keyStart = -1
		n.kids = append(n.kids, kid)
		p.skip()
		if p.i >= len(p.s) {
			return nil, p.fail("the array is not closed")
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			n.end = p.i
			return n, nil
		default:
			return nil, p.fail("',' or ']' was expected")
		}
	}
}

// rawString reads the string token at the cursor, unescaped.
func (p *jsonParser) rawString() (string, error) {
	start := p.i
	p.i++
	escaped := false
	for p.i < len(p.s) {
		switch c := p.s[p.i]; {
		case c == '\\':
			escaped = true
			p.i += 2
			continue
		case c == '"':
			p.i++
			if !escaped {
				return p.s[start+1 : p.i-1], nil
			}
			var out string
			if err := json.Unmarshal([]byte(p.s[start:p.i]), &out); err != nil {
				return "", p.fail("a bad escape in a string")
			}
			return out, nil
		case c < 0x20:
			return "", p.fail("a control character in a string")
		}
		p.i++
	}
	return "", p.fail("the string is not closed")
}

func (p *jsonParser) stringNode(parent *node) (*node, error) {
	n := &node{kind: domain.NodeString, parent: parent, start: p.i, keyStart: -1}
	text, err := p.rawString()
	if err != nil {
		return nil, err
	}
	n.scalar, n.end = text, p.i
	return n, nil
}

func (p *jsonParser) number(parent *node) (*node, error) {
	n := &node{kind: domain.NodeNumber, parent: parent, start: p.i, keyStart: -1}
	if p.s[p.i] == '-' {
		p.i++
	}
	digits := 0
	for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
		digits++
	}
	if digits == 0 {
		return nil, p.fail("a number was expected")
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
		}
	}
	n.scalar, n.end = p.s[n.start:p.i], p.i
	return n, nil
}

func (p *jsonParser) literal(parent *node) (*node, error) {
	for _, word := range []string{"true", "false", "null"} {
		if !strings.HasPrefix(p.s[p.i:], word) {
			continue
		}
		kind := domain.NodeBoolean
		if word == "null" {
			kind = domain.NodeNull
		}
		n := &node{kind: kind, parent: parent, start: p.i, keyStart: -1, scalar: word}
		p.i += len(word)
		n.end = p.i
		return n, nil
	}
	return nil, p.fail("a value was expected")
}
