package lens

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"

	"schemo/internal/domain"
)

// voidTags never hold content, so are never open.
var voidTags = wordSet("area base br col embed hr img input link meta param source track wbr")

// closedBy lists the open elements a start tag closes implicitly.
var closedBy = map[string][]string{
	"li": {"li"}, "dt": {"dt", "dd"}, "dd": {"dt", "dd"}, "tr": {"tr", "td", "th"},
	"td": {"td", "th"}, "th": {"td", "th"}, "option": {"option"}, "optgroup": {"optgroup", "option"},
	"thead": {"tbody", "tfoot", "thead"}, "tbody": {"thead", "tbody", "tfoot"},
	"tfoot": {"thead", "tbody"},
	"p":     {"p"},
}

func init() {
	for _, block := range strings.Fields("address article aside blockquote details div dl fieldset " +
		"figure footer form h1 h2 h3 h4 h5 h6 header hr main nav ol pre section table ul") {
		closedBy[block] = append(closedBy[block], "p")
	}
}

// parseHTML reads a page as roughly the tree a browser builds, with positions. Unclosed tags close
// where the next implies it; stray end tags are dropped.
func parseHTML(text string) (*node, error) {
	z := html.NewTokenizer(strings.NewReader(text))
	root := &node{kind: domain.NodeElement, name: "#document", start: 0, end: len(text), keyStart: -1}
	stack := []*node{root}
	pos := 0

	closeTo := func(depth, at int) {
		for len(stack) > depth {
			stack[len(stack)-1].end = at
			stack = stack[:len(stack)-1]
		}
	}

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		raw := z.Raw()
		start := pos
		pos += len(raw)

		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			var attrs [][2]string
			for hasAttr {
				var key, value []byte
				key, value, hasAttr = z.TagAttr()
				attrs = append(attrs, [2]string{string(key), string(value)})
			}

			for len(stack) > 1 && slices.Contains(closedBy[tag], stack[len(stack)-1].name) {
				closeTo(len(stack)-1, start)
			}
			el := &node{
				kind: domain.NodeElement, name: tag, parent: stack[len(stack)-1], start: start,
				end: pos, keyStart: -1,
			}
			el.parent.kids = append(el.parent.kids, el)
			addHTMLAttributes(el, attrs, text[start:pos], start)
			if tt == html.StartTagToken && !voidTags[tag] && len(stack) < maxDepth {
				stack = append(stack, el)
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			for depth := len(stack) - 1; depth >= 1; depth-- {
				if stack[depth].name == string(name) {
					closeTo(depth+1, start)
					stack[depth].end = pos
					stack = stack[:depth]
					break
				}
			}
		case html.TextToken:
			run := strings.TrimSpace(string(z.Text()))
			if run == "" {
				break
			}
			lead := len(raw) - len(strings.TrimLeft(string(raw), " \t\r\n"))
			at := stack[len(stack)-1]
			at.kids = append(at.kids, &node{
				kind: domain.NodeText, parent: at, start: start + lead, end: pos, keyStart: -1, scalar: run,
			})
		}
	}
	closeTo(1, len(text))

	if len(root.kids) == 0 {
		return nil, fmt.Errorf("%w: no elements", domain.ErrUnreadable)
	}
	settle(root)
	// A page is rooted at `html`; a fragment stays under the synthetic root.
	if len(root.kids) == 1 && root.kids[0].kind == domain.NodeElement && root.kids[0].name == "html" {
		page := root.kids[0]
		page.parent = nil
		return page, nil
	}
	return root, nil
}

// settle gives an element of only text that text as its value.
func settle(n *node) {
	for _, kid := range n.kids {
		settle(kid)
	}
	if n.kind == domain.NodeElement {
		if only := onlyText(n); only != nil {
			n.scalar = only.scalar
		}
	}
}

// addHTMLAttributes adds an attribute node per attribute, at its place in the tag.
func addHTMLAttributes(el *node, attrs [][2]string, tag string, base int) {
	spans := attributeSpans(tag)
	for i, a := range attrs {
		from, to := el.start, el.end
		if i < len(spans) {
			from, to = base+spans[i][0], base+spans[i][1]
		}
		el.kids = append(el.kids, &node{
			kind: domain.NodeAttribute, name: a[0], parent: el, start: from, end: to, keyStart: -1,
			scalar: a[1],
		})
	}
}

var cssIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// selector writes an element's CSS address from the nearest ancestor with an id, by tag and, among
// same-tag siblings, :nth-of-type.
func selector(n *node) string {
	var steps []string
	for at := n; at != nil; at = at.parent {
		if at.kind != domain.NodeElement || at.name == "#document" {
			continue
		}
		if id := at.attr("id"); id != "" && cssIdent.MatchString(id) {
			steps = append(steps, "#"+id)
			break
		}
		step := at.name
		if at.parent != nil {
			if k := position(at, func(s *node) bool {
				return s.kind == domain.NodeElement && s.name == at.name
			}); k != "" {
				step += ":nth-of-type(" + strings.Trim(k, "[]") + ")"
			}
		}
		steps = append(steps, step)
	}
	for i, j := 0, len(steps)-1; i < j; i, j = i+1, j-1 {
		steps[i], steps[j] = steps[j], steps[i]
	}
	return strings.Join(steps, " > ")
}

// attr is an attribute's value, or empty.
func (n *node) attr(name string) string {
	for _, kid := range n.kids {
		if kid.kind == domain.NodeAttribute && kid.name == name {
			return kid.scalar
		}
	}
	return ""
}

// htmlForms is the node's address as XPath, CSS and JavaScript.
func htmlForms(n *node) []domain.PathForm {
	forms := []domain.PathForm{{Syntax: "XPath", Text: xpath(n)}}
	element := n
	suffix := ""
	switch n.kind {
	case domain.NodeAttribute:
		element, suffix = n.parent, ".getAttribute("+quoted(n.name)+")"
	case domain.NodeText:
		element, suffix = n.parent, ".textContent"
	}
	css := selector(element)
	if n.kind == domain.NodeAttribute {
		forms = append(forms, domain.PathForm{Syntax: "CSS", Text: css + "[" + n.name + "]"})
	} else {
		forms = append(forms, domain.PathForm{Syntax: "CSS", Text: css})
	}
	forms = append(forms, domain.PathForm{
		Syntax: "JavaScript", Text: "document.querySelector(" + quoted(css) + ")" + suffix,
	})
	return forms
}
