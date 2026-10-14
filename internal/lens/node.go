// Package lens reads a response body as a tree of positions and says what each is: its address,
// what its value reads as, and its type. Pure: text in, facts out.
package lens

import (
	"unicode/utf8"

	"schemo/internal/domain"
)

// node is one position of a document. It holds offsets into the text, not copies of it.
type node struct {
	kind   domain.NodeKind
	name   string
	parent *node
	kids   []*node

	// start and end delimit the node's text in bytes, brackets and quotes included.
	// keyStart is where the member's key begins, or -1.
	start, end, keyStart int

	// scalar is the decoded string, or a number's or literal's text as written.
	scalar string

	// header holds the column names of a table, on its root.
	header []string
}

// head is where the node's member begins: at its key when it has one.
func (n *node) head() int {
	if n.keyStart >= 0 {
		return n.keyStart
	}
	return n.start
}

// index is the node's position among its parent's children.
func (n *node) index() int {
	if n.parent == nil {
		return 0
	}
	for i, k := range n.parent.kids {
		if k == n {
			return i
		}
	}
	return 0
}

// Document is a body that has been read.
type Document struct {
	kind  domain.DocKind
	text  string
	root  *node
	ascii bool
}

// Kind is what the body was read as.
func (d *Document) Kind() domain.DocKind { return d.kind }

// unitsUpTo counts the UTF-16 units of text[:byteOffset].
func unitsUpTo(text string, byteOffset int) int {
	units := 0
	for _, r := range text[:byteOffset] {
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	return units
}

// byteAt is the byte offset of a UTF-16 unit offset, clamped to the text.
func byteAt(text string, units int) int {
	if units <= 0 {
		return 0
	}
	count := 0
	for i, r := range text {
		if count >= units {
			return i
		}
		if r >= 0x10000 {
			count += 2
		} else {
			count++
		}
	}
	return len(text)
}

// isASCII reports whether bytes and UTF-16 units coincide.
func isASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
