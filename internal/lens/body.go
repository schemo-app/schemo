package lens

import (
	"strings"

	"schemo/internal/domain"
)

// serverFields are the members a server writes itself; sent back they are noise or a refusal.
var serverFields = wordSet("created_at updated_at deleted_at createdat updatedat deletedat " +
	"created modified links _links _embedded")

// Body writes the node as an indented JSON request body, without serverFields when bare.
func (d *Document) Body(path string, bare bool) (string, error) {
	if d.kind != domain.DocJSON {
		return "", domain.ErrNotAllowed
	}
	n, err := d.find(path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	d.write(&b, n, 0, bare)
	return b.String(), nil
}

// write emits a node, one member to a line, skipping serverFields when bare.
func (d *Document) write(b *strings.Builder, n *node, depth int, bare bool) {
	indent := strings.Repeat("  ", depth+1)
	closing := strings.Repeat("  ", depth)

	switch n.kind {
	case domain.NodeObject:
		kept := n.kids[:0:0]
		for _, kid := range n.kids {
			if !bare || !serverFields[strings.ToLower(kid.name)] {
				kept = append(kept, kid)
			}
		}
		if len(kept) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, kid := range kept {
			b.WriteString(indent + quoted(kid.name) + ": ")
			d.write(b, kid, depth+1, bare)
			if i < len(kept)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(closing + "}")
	case domain.NodeArray:
		if len(n.kids) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, kid := range n.kids {
			b.WriteString(indent)
			d.write(b, kid, depth+1, bare)
			if i < len(n.kids)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString(closing + "]")
	default:
		b.WriteString(d.text[n.start:n.end])
	}
}
