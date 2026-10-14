package domain

// SearchKind names one area of the index. The window draws a group and a chip per kind, and the
// words for both come from its catalogue: a kind is a word Go can say, a heading is not.
//
// The set is open on purpose — an area is a method on the index and a value here, and nothing else
// in the domain or in the transport learns about it.
type SearchKind string

const (
	SearchRequest     SearchKind = "request"
	SearchCollection  SearchKind = "collection"
	SearchEnvironment SearchKind = "environment"
	SearchHistory     SearchKind = "history"
	SearchAction      SearchKind = "action"
)

// SearchMatch is where the query was found, which is what the order is built on: the design puts an
// exact name first, then a name, then an address, and a variable that merely holds the word last.
//
// It is a tier rather than a score because the tiers are the design's, while a score would be a
// number nobody could argue with. What breaks a tie inside a tier is SearchHit.At.
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

// SearchNoteKind is what a row says on the right, as a shape rather than a sentence. The window
// words it from its catalogue — "current", "8 requests" — because the words are the catalogue's and
// the counting is not.
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

// SearchTarget names what activating a row does. The window switches on it, so it is a fixed
// vocabulary and not a sentence: a target this build does not know is a row that does nothing,
// which is what a newer build's answer should be to an older window.
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

// SearchHit is one row of the palette. Everything it draws is decided here except the words: the
// title and the path are the user's own text, and the note is a shape the window words.
//
// Only where the query was found is carried, not the matched text: highlighting is the window's
// either way, and not shipping the match keeps a variable's value out of the answer even when the
// value is what answered.
type SearchHit struct {
	Kind  SearchKind `json:"kind"`
	ID    string     `json:"id"`
	Title string     `json:"title"`
	// Path is what the row sits inside, outermost first: the collections above a request. The window
	// joins it with the separator the design draws, so the separator is not a word Go has to own.
	Path  []string    `json:"path"`
	Badge string      `json:"badge,omitempty"`
	Note  SearchNote  `json:"note"`
	Match SearchMatch `json:"match"`
	// At is when the row happened, in milliseconds, or 0 for a row that did not happen — a saved
	// request has no time, and the window draws none rather than one it made up.
	At   int64      `json:"at"`
	Open SearchOpen `json:"open"`

	// MatchText is what a row is searched by besides what it draws: a variable's value. It never
	// travels — the window draws the name and has no business with the value that answered, which
	// is a secret's whenever the variable is one.
	MatchText string `json:"-"`
}

// SearchGroup is one area's answer: the rows the window draws, and how many the area found before
// the list was cut. The two are separate because the heading counts matches, not shown rows —
// "Requests · 8" over a list of five is the design's own wording.
type SearchGroup struct {
	Kind  SearchKind  `json:"kind"`
	Total int         `json:"total"`
	Hits  []SearchHit `json:"hits"`
}

// SearchResult is the whole answer, one group per area that found something, in the order the
// design draws them. An area that found nothing is absent rather than empty: a heading over no rows
// is a heading the window would have to know to skip.
type SearchResult struct {
	Groups []SearchGroup `json:"groups"`
}
