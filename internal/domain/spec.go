package domain

import (
	"encoding/json"
	"regexp"
	"strings"
)

// SpecKind is where a linked API description is read from.
type SpecKind string

const (
	SpecFile SpecKind = "file"
	SpecURL  SpecKind = "url"
)

// SpecSource is the description a collection is updated from; ReadAt is the last update, in ms.
type SpecSource struct {
	Kind     SpecKind `json:"kind"`
	Location string   `json:"location"`
	ReadAt   int64    `json:"readAt"`
}

// SpecLink is where this device reads a collection's description. It stays here: a path means
// nothing elsewhere.
type SpecLink struct {
	CollectionID string     `json:"collectionId"`
	Source       SpecSource `json:"source"`
}

// TreeUpdate is written whole or not at all; Levels are new folders, parents first. Snapshot is what
// the description said, kept with Link's collection so that it syncs. Link is kept when it has a
// location to read again.
type TreeUpdate struct {
	Levels   []Collection
	Patches  []LevelPatch
	Requests []CollectionNode
	Deleted  []string
	Link     SpecLink
	Snapshot json.RawMessage
}

// LevelPatch is what a saved collection or folder answers for after an update.
type LevelPatch struct {
	ID          string
	Name        string
	Description string
	Auth        *Auth
	Variables   []Variable
}

var braceHole = regexp.MustCompile(`\{\{[^{}]*\}\}|\{[^{}]*\}`)

// OperationKey names an operation of an API description. Parameter names are left out: two paths
// that differ only in them are one path (OpenAPI §4.8.8).
func OperationKey(method, path string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + PathShape(path)
}

// PathShape is a path with every parameter, however it is written, as `{}`.
func PathShape(path string) string {
	path = pathSlot.ReplaceAllString(path, "/{}")
	path = braceHole.ReplaceAllString(path, "{}")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

// AddressPath is what an address names after its base: the scheme and host, or a leading
// `{{variable}}`. The query and fragment are left out.
func AddressPath(address string) string {
	base, _ := SplitQuery(address)
	base, _, _ = strings.Cut(base, "#")
	if _, rest, found := strings.Cut(base, "://"); found {
		return afterHost(rest)
	}
	if strings.HasPrefix(base, "/") {
		return base
	}
	head, _, _ := strings.Cut(base, "/")
	if strings.Contains(head, "{{") || strings.ContainsAny(head, ".:") || strings.EqualFold(head, "localhost") {
		return afterHost(base)
	}
	return "/" + base
}

func afterHost(address string) string {
	if at := strings.IndexByte(address, '/'); at >= 0 {
		return address[at:]
	}
	return "/"
}
