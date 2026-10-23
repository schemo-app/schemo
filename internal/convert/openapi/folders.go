package openapi

import (
	"strings"

	"schemo/internal/domain"
)

// folderNode is a folder while the document is walked: its name, its description, the requests
// that landed in it, and the folders under it in the order they were made.
type folderNode struct {
	name        string
	description string
	items       []domain.CollectionNode
	children    []*folderNode
	byName      map[string]*folderNode
}

func newFolderNode(name string) *folderNode {
	return &folderNode{name: name, byName: map[string]*folderNode{}}
}

// at walks to the folder the segments name, making the ones on the way.
func (f *folderNode) at(segments []string) *folderNode {
	node := f
	for _, segment := range segments {
		child, ok := node.byName[segment]
		if !ok {
			child = newFolderNode(segment)
			node.byName[segment] = child
			node.children = append(node.children, child)
		}
		node = child
	}
	return node
}

// collection is the tree as this app holds a collection: the requests and the folders in the order
// they were placed, each level numbered like a level is.
func (f *folderNode) collection() domain.Collection {
	collection := domain.Collection{Name: f.name, Description: f.description}
	for at := range f.items {
		f.items[at].Position = int64(at)
		collection.Items = append(collection.Items, f.items[at])
	}
	for at, child := range f.children {
		built := child.collection()
		built.Position = int64(at)
		collection.Children = append(collection.Children, built)
	}
	return collection
}

// tagSegments is one tag as the folders it names. Whether `/` splits at all is the document's
// answer (a marker, or the hierarchy the tags show), so a document with plain tag names keeps plain
// folders.
func tagSegments(tag string, shouldSplit bool) []string {
	if !shouldSplit {
		if tag == "" {
			return nil
		}
		return []string{tag}
	}
	out := []string{}
	for _, segment := range strings.Split(tag, "/") {
		if trimmed := strings.TrimSpace(segment); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// longestTagSegments is where a request goes: the deepest of the tags it carries, and the first of
// those where two run as deep — one placement, because one request is one row.
func longestTagSegments(tags []string, shouldSplit bool) []string {
	best := []string{}
	for _, tag := range tags {
		if segments := tagSegments(tag, shouldSplit); len(segments) > len(best) {
			best = segments
		}
	}
	return best
}

// sharedTagPathPrefix reports whether the tags, read as `/`-split segments, show a hierarchy: some
// tag is the strict beginning of another, or two tags begin with the same run of segments.
func sharedTagPathPrefix(tags []string) bool {
	splits := [][]string{}
	for _, tag := range tags {
		if segments := tagSegments(tag, true); len(segments) > 0 {
			splits = append(splits, segments)
		}
	}
	if len(splits) < 2 {
		return false
	}
	const sep = "\x00"
	full := map[string]bool{}
	for _, segments := range splits {
		full[strings.Join(segments, sep)] = true
	}
	for _, segments := range splits {
		for at := 1; at < len(segments); at++ {
			if full[strings.Join(segments[:at], sep)] {
				return true
			}
		}
	}
	seen := map[string]bool{}
	for _, segments := range splits {
		for at := 1; at < len(segments); at++ {
			prefix := strings.Join(segments[:at], sep)
			if seen[prefix] {
				return true
			}
			seen[prefix] = true
		}
	}
	return false
}
