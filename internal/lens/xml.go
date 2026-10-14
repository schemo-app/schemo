package lens

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"schemo/internal/domain"
)

// parseXML reads one XML document; an element's attributes come first among its children.
func parseXML(text string) (*node, error) {
	dec := xml.NewDecoder(strings.NewReader(text))
	// The body is already text; a declared charset changes nothing.
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }

	var root *node
	var stack []*node
	prev := 0
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", domain.ErrUnreadable, err)
		}
		end := int(dec.InputOffset())

		switch t := token.(type) {
		case xml.StartElement:
			start := strings.LastIndexByte(text[:end], '<')
			el := &node{kind: domain.NodeElement, name: t.Name.Local, start: start, keyStart: -1}
			if len(stack) > 0 {
				el.parent = stack[len(stack)-1]
				el.parent.kids = append(el.parent.kids, el)
			} else if root == nil {
				root = el
			} else {
				return nil, fmt.Errorf("%w: more than one root element", domain.ErrUnreadable)
			}
			addAttributes(el, t.Attr, text[start:end], start)
			stack = append(stack, el)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("%w: a closing tag with nothing open", domain.ErrUnreadable)
			}
			el := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			el.end = end
			if only := onlyText(el); only != nil {
				el.scalar = only.scalar
			}
		case xml.CharData:
			if len(stack) == 0 {
				break
			}
			run := string(t)
			if strings.TrimSpace(run) == "" {
				break
			}
			from := prev + len(text[prev:end]) - len(strings.TrimLeft(text[prev:end], " \t\r\n"))
			el := stack[len(stack)-1]
			el.kids = append(el.kids, &node{
				kind: domain.NodeText, parent: el, start: from, end: end, keyStart: -1,
				scalar: strings.TrimSpace(run),
			})
		}
		prev = end
	}
	if root == nil || len(stack) != 0 {
		return nil, fmt.Errorf("%w: no complete element", domain.ErrUnreadable)
	}
	return root, nil
}

// onlyText is the text an element consists of, when that is all besides attributes.
func onlyText(el *node) *node {
	var found *node
	for _, kid := range el.kids {
		switch kid.kind {
		case domain.NodeAttribute:
		case domain.NodeText:
			if found != nil {
				return nil
			}
			found = kid
		default:
			return nil
		}
	}
	return found
}

// addAttributes adds an attribute node per attribute, at its place in the tag, skipping xmlns.
func addAttributes(el *node, attrs []xml.Attr, tag string, base int) {
	spans := attributeSpans(tag)
	for i, a := range attrs {
		if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
			continue
		}
		from, to := el.start, el.start
		if i < len(spans) {
			from, to = base+spans[i][0], base+spans[i][1]
		}
		el.kids = append(el.kids, &node{
			kind: domain.NodeAttribute, name: a.Name.Local, parent: el, start: from, end: to,
			keyStart: -1, scalar: a.Value,
		})
	}
}

// attributeSpans finds each attribute's extent in a start tag, name to end of value.
func attributeSpans(tag string) [][2]int {
	var spans [][2]int
	i := 1
	for i < len(tag) && !strings.ContainsRune(" \t\r\n/>", rune(tag[i])) {
		i++
	}
	for i < len(tag) {
		for i < len(tag) && strings.ContainsRune(" \t\r\n", rune(tag[i])) {
			i++
		}
		if i >= len(tag) || tag[i] == '>' || tag[i] == '/' {
			break
		}
		nameStart := i
		for i < len(tag) && !strings.ContainsRune("= \t\r\n>/", rune(tag[i])) {
			i++
		}
		nameEnd := i
		for i < len(tag) && strings.ContainsRune(" \t\r\n", rune(tag[i])) {
			i++
		}
		if i >= len(tag) || tag[i] != '=' {
			// An attribute with no value is its name alone.
			spans = append(spans, [2]int{nameStart, nameEnd})
			continue
		}
		i++
		for i < len(tag) && strings.ContainsRune(" \t\r\n", rune(tag[i])) {
			i++
		}
		if i < len(tag) && tag[i] != '"' && tag[i] != '\'' {
			for i < len(tag) && !strings.ContainsRune(" \t\r\n>", rune(tag[i])) {
				i++
			}
			spans = append(spans, [2]int{nameStart, i})
			continue
		}
		if i >= len(tag) {
			break
		}
		quote := tag[i]
		i++
		for i < len(tag) && tag[i] != quote {
			i++
		}
		if i < len(tag) {
			i++
		}
		spans = append(spans, [2]int{nameStart, i})
	}
	return spans
}

// xpath writes a node's address, with [k] among same-name siblings.
func xpath(n *node) string {
	var parts []string
	for at := n; at != nil; at = at.parent {
		parts = append(parts, xpathStep(at))
	}
	var b strings.Builder
	for i := len(parts) - 1; i >= 0; i-- {
		b.WriteString("/" + parts[i])
	}
	return b.String()
}

func xpathStep(n *node) string {
	switch n.kind {
	case domain.NodeAttribute:
		return "@" + n.name
	case domain.NodeText:
		return "text()" + position(n, func(k *node) bool { return k.kind == domain.NodeText })
	}
	if n.parent == nil {
		return n.name
	}
	return n.name + position(n, func(k *node) bool {
		return k.kind == domain.NodeElement && k.name == n.name
	})
}

// position is "[k]" among matching siblings, or empty for an only child.
func position(n *node, same func(*node) bool) string {
	count, at := 0, 0
	for _, kid := range n.parent.kids {
		if same(kid) {
			count++
			if kid == n {
				at = count
			}
		}
	}
	if count < 2 {
		return ""
	}
	return "[" + strconv.Itoa(at) + "]"
}

func xmlForms(n *node) []domain.PathForm {
	return []domain.PathForm{{Syntax: "XPath", Text: xpath(n)}}
}

// findXPath resolves what xpath writes.
func findXPath(root *node, path string) (*node, error) {
	steps := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(steps) == 0 || steps[0] == "" {
		return nil, domain.ErrNotFound
	}
	name, _ := splitPosition(steps[0])
	if name != root.name {
		return nil, domain.ErrNotFound
	}
	at := root
	for _, raw := range steps[1:] {
		name, index := splitPosition(raw)
		var next *node
		count := 0
		for _, kid := range at.kids {
			if !matchesStep(kid, name) {
				continue
			}
			count++
			if count == index {
				next = kid
				break
			}
		}
		if next == nil {
			return nil, domain.ErrNotFound
		}
		at = next
	}
	return at, nil
}

// matchesStep reports whether a child is what a step names.
func matchesStep(kid *node, name string) bool {
	switch {
	case strings.HasPrefix(name, "@"):
		return kid.kind == domain.NodeAttribute && kid.name == name[1:]
	case name == "text()":
		return kid.kind == domain.NodeText
	default:
		return kid.kind == domain.NodeElement && kid.name == name
	}
}

// splitPosition splits "entry[2]" into the step and its 1-based place (default 1).
func splitPosition(step string) (string, int) {
	open := strings.IndexByte(step, '[')
	if open < 0 || !strings.HasSuffix(step, "]") {
		return step, 1
	}
	n, err := strconv.Atoi(step[open+1 : len(step)-1])
	if err != nil || n < 1 {
		return step, 1
	}
	return step[:open], n
}
