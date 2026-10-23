package domain

import (
	"encoding/json"
	"strconv"
	"strings"
)

// UntitledRequest names a request that an imported file names nowhere.
const UntitledRequest = "Untitled Request"

type Collection struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Position    int64            `json:"position"`
	CreatedAt   int64            `json:"createdAt"`
	UpdatedAt   int64            `json:"updatedAt"`
	ParentID    string           `json:"parentId,omitempty"`
	Items       []CollectionNode `json:"items"`
	Children    []Collection     `json:"children"`
	Auth        *Auth            `json:"auth,omitempty"`
	Variables   []Variable       `json:"variables,omitempty"`
	// A level's own code. It is written only where a whole tree is written (import, duplicate):
	// SaveCollection works from shallow rows that carry no code, so writing it there would erase
	// the code on every rename.
	Scripts *Scripts `json:"scripts,omitempty"`
}

type LevelRow struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Method string `json:"method,omitempty"`
	URL    string `json:"url,omitempty"`
}

type LevelEntry struct {
	Node       *CollectionNode
	Collection *Collection
}

func (c *Collection) Level() []LevelEntry {
	out := make([]LevelEntry, 0, len(c.Items)+len(c.Children))
	i, j := 0, 0
	for i < len(c.Items) || j < len(c.Children) {
		if j >= len(c.Children) || (i < len(c.Items) && c.Items[i].Position <= c.Children[j].Position) {
			out = append(out, LevelEntry{Node: &c.Items[i]})
			i++
			continue
		}
		out = append(out, LevelEntry{Collection: &c.Children[j]})
		j++
	}
	return out
}

type CollectionNode struct {
	ID            string    `json:"id"`
	CollectionID  string    `json:"collectionId"`
	Name          string    `json:"name"`
	Position      int64     `json:"position"`
	Method        string    `json:"method,omitempty"`
	URL           string    `json:"url,omitempty"`
	Params        []Row     `json:"params,omitempty"`
	Headers       []Row     `json:"headers,omitempty"`
	PathParams    []Row     `json:"pathParams,omitempty"`
	Body          string    `json:"body,omitempty"`
	BodyKind      BodyKind  `json:"bodyKind,omitempty"`
	Form          []FormRow `json:"form,omitempty"`
	BodyFile      string    `json:"bodyFile,omitempty"`
	EnvironmentID string    `json:"environmentId,omitempty"`
	Auth          *Auth     `json:"auth,omitempty"`
	Scripts       *Scripts  `json:"scripts,omitempty"`
	Description   string    `json:"description,omitempty"`
	// Responses are example answers carried by a file (a Postman example, a declared response).
	// They are read-only: a request sent from the app composes its own answer.
	Responses []ResponseExample `json:"responses,omitempty"`
	CreatedAt int64             `json:"createdAt"`
	UpdatedAt int64             `json:"updatedAt"`
}

// ResponseExample is one saved answer: what it is called, the status it carried, its headers and
// its body as they were written down.
type ResponseExample struct {
	Name    string       `json:"name,omitempty"`
	Status  int          `json:"status,omitempty"`
	Body    string       `json:"body,omitempty"`
	Headers []HeaderPair `json:"headers,omitempty"`
}

type CollectionRun struct {
	ID           string                `json:"id"`
	CollectionID string                `json:"collectionId"`
	NodeID       string                `json:"nodeId,omitempty"`
	Environment  string                `json:"environment,omitempty"`
	StartedAt    int64                 `json:"startedAt"`
	FinishedAt   int64                 `json:"finishedAt,omitempty"`
	Passed       int                   `json:"passed"`
	Failed       int                   `json:"failed"`
	DurationUs   int64                 `json:"durationUs"`
	Results      []CollectionRunResult `json:"results"`
}

type CollectionRunResult struct {
	Status           *int     `json:"status,omitempty"`
	Failure          *Failure `json:"failure,omitempty"`
	NodeID           string   `json:"nodeId"`
	Error            string   `json:"error,omitempty"`
	RecordID         string   `json:"recordId,omitempty"`
	Position         int64    `json:"position"`
	DurationUs       int64    `json:"durationUs"`
	AssertionsPassed int      `json:"assertionsPassed"`
	AssertionsTotal  int      `json:"assertionsTotal"`
	OK               bool     `json:"ok"`
	Skipped          bool     `json:"skipped,omitempty"`
}

// Document is what a foreign file holds in this app's terms: collections for the tree and
// environments for the environment list. A format may carry only one of the two.
type Document struct {
	Collections  []Collection       `json:"collections,omitempty"`
	Environments []EnvironmentDraft `json:"environments,omitempty"`
	Skipped      []Skip             `json:"skipped,omitempty"`
}

// EnvironmentDraft is an environment described by a file. Ids, positions and the active environment
// are decided where the draft is written.
type EnvironmentDraft struct {
	Name      string     `json:"name"`
	Color     string     `json:"color,omitempty"`
	Variables []Variable `json:"variables,omitempty"`
}

// Skip is one thing an import deliberately left out, counted rather than carried: a code the window
// has a sentence for, and how many of them there were. A count, never the things themselves.
type Skip struct {
	Code  Code `json:"code"`
	Count int  `json:"count"`
}

// LeftOut records that something of a kind was not carried over. Readers call it while walking a
// file, so the final report reflects what the file actually held.
func (d *Document) LeftOut(code Code, count int) {
	if count <= 0 {
		return
	}
	for index := range d.Skipped {
		if d.Skipped[index].Code == code {
			d.Skipped[index].Count += count
			return
		}
	}
	d.Skipped = append(d.Skipped, Skip{Code: code, Count: count})
}

// ValueText renders a file value as substitution text: a string as is, anything else as JSON, so
// `2` and `"2"` both answer `{{page}}`.
func ValueText(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// SearchKind names one area of the index. The window draws a group and a chip per kind and words
// both from its catalogue. The set is open: an area is a method on the index plus a value here.
type SearchKind string

const (
	SearchRequest     SearchKind = "request"
	SearchCollection  SearchKind = "collection"
	SearchEnvironment SearchKind = "environment"
	SearchHistory     SearchKind = "history"
	SearchAction      SearchKind = "action"
)

// SearchMatch is where the query was found, which orders the results: exact name, then name, then
// address, then a variable that merely holds the word. It is a tier rather than a score;
// SearchHit.At breaks ties within a tier.
type SearchMatch int

const (
	// MatchNone is the zero value, and it is deliberately not a tier: a row that matched nothing must
	// not read as one that matched exactly when somebody forgets to look at the flag beside it.
	MatchNone SearchMatch = iota
	// MatchExact is the whole name, character for character. It is separated from MatchName because
	// it is the one case where the user has already typed what they want and the row is the answer.
	MatchExact
	MatchName
	MatchPath
	MatchValue
)

// SearchNoteKind is what a row says on its right, as a shape rather than a sentence ("current", "8
// requests"). The window words it from its catalogue.
type SearchNoteKind string

// The zero value is the row with no note, so it needs no constant of its own.
const (
	NoteActive   SearchNoteKind = "active"
	NoteRequests SearchNoteKind = "requests"
)

// SearchNote is that line: which shape it is, and the number it counts when it counts one.
type SearchNote struct {
	Kind  SearchNoteKind `json:"kind"`
	Count int            `json:"count,omitempty"`
}

// SearchTarget names what activating a row does. The window switches on it; a target this build
// does not know is a row that does nothing, which is what a newer build's answer should be to an
// older window.
type SearchTarget string

const (
	TargetRequest     SearchTarget = "request"
	TargetCollection  SearchTarget = "collection"
	TargetEnvironment SearchTarget = "environment"
	TargetVariable    SearchTarget = "variable"
	TargetHistory     SearchTarget = "history"
)

// SearchOpen is where a row takes the window: the verb, the id, and — for a variable, which is the
// one thing that is addressed through the environment holding it — the scope to open.
type SearchOpen struct {
	Target SearchTarget `json:"target"`
	ID     string       `json:"id"`
	Scope  string       `json:"scope,omitempty"`
}

// SearchHit is one row of the palette. Everything but the words is decided here: title and path are
// the user's text and the note is a shape the window words. Only where the query was found is
// carried, not the matched text, which keeps a variable's value out of the answer.
type SearchHit struct {
	Kind  SearchKind `json:"kind"`
	ID    string     `json:"id"`
	Title string     `json:"title"`
	// Path is what the row sits inside, outermost first: the collections above a request. The window
	// joins it with its own separator, so the separator is not a word Go has to own.
	Path  []string    `json:"path"`
	Badge string      `json:"badge,omitempty"`
	Note  SearchNote  `json:"note"`
	Match SearchMatch `json:"match"`
	// At is when the row happened, in milliseconds, or 0 for a row that did not happen — a saved
	// request has no time, and the window draws none rather than one it made up.
	At   int64      `json:"at"`
	Open SearchOpen `json:"open"`

	// MatchText is what a row is searched by besides what it draws: a variable's value. It never
	// travels, since it may be a secret.
	MatchText string `json:"-"`
}

// SearchGroup is one area's answer: the rows to draw and how many the area found before the list
// was cut. The heading counts matches, not shown rows.
type SearchGroup struct {
	Kind  SearchKind  `json:"kind"`
	Total int         `json:"total"`
	Hits  []SearchHit `json:"hits"`
}

// SearchResult is the whole answer, one group per area that found something, in display order. An
// area that found nothing is absent rather than empty.
type SearchResult struct {
	Groups []SearchGroup `json:"groups"`
}

// CleanName returns a trimmed, non-empty name within max. The ceiling is the caller's; the two
// refusals are catalogue errors, so every window words them the same way.
func CleanName(raw string, max int) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", Refuse(CodeNameEmpty, ErrNotAllowed, nil)
	}
	if len([]rune(name)) > max {
		return "", Refuse(CodeNameTooLong, ErrNotAllowed, Args{"max": strconv.Itoa(max)})
	}
	return name, nil
}

// NameTaken is the refusal for a name already worn by another thing of its kind. What the kind is,
// is the caller's.
func NameTaken() error {
	return Refuse(CodeNameTaken, ErrConflict, nil)
}

// OrEmpty returns a list that is never nil. A nil slice crosses the IPC boundary as `null`, which
// every list the window draws would otherwise have to treat as a second kind of empty.
func OrEmpty[T any](xs []T) []T {
	if xs == nil {
		return []T{}
	}
	return xs
}
