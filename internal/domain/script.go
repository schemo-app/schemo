package domain

import "strings"

// Scripts is the code that runs around a request: before it goes out, and after it comes back.
//
// On a node it is a pointer for the reason Auth is — nil means "not set here" and a value means
// "this is what this level says". It does not win by being closest, though: every level that has a
// script runs, outermost first, so a collection can count requests and a request can assert about
// its own response without either of them replacing the other.
type Scripts struct {
	Pre  string `json:"pre,omitempty"`
	Post string `json:"post,omitempty"`
	// A half can be switched off without being thrown away: the code stays where it is written while
	// it stops running, which is the difference between a script somebody is working on and one they
	// have deleted. It is the code that says a level has something to say — an off half is still an
	// answer about this level, so it keeps the level in the chain.
	PreOff  bool `json:"preOff,omitempty"`
	PostOff bool `json:"postOff,omitempty"`
	// Captures run before the level's own code, so the code can read what they store.
	Captures []Capture `json:"captures,omitempty"`
}

// Off is whether a half is switched off for this level.
func (s *Scripts) Off(scope ScriptScope) bool {
	if s == nil {
		return false
	}
	if scope == ScriptPre {
		return s.PreOff
	}
	return s.PostOff
}

// Empty is the difference between a level that has nothing to say and one that says nothing.
func (s *Scripts) Empty() bool {
	if s == nil {
		return true
	}
	return strings.TrimSpace(s.Pre) == "" && strings.TrimSpace(s.Post) == "" && len(s.Captures) == 0
}

// ScriptScope is when a script ran: before the request was sent, or after the answer came back.
type ScriptScope string

const (
	ScriptPre  ScriptScope = "pre"
	ScriptPost ScriptScope = "post"
)

// ScriptInput is one script about to run: the code, when it runs, and everything it can see. What a
// script is handed is a request, and — after the answer came back — the response to it, because
// those are what it counts, changes and asserts about.
type ScriptInput struct {
	Scope  ScriptScope
	Source string

	// Request is what a script may change: a pre-request script edits the request that is about to go
	// out, and what it leaves here is what is sent.
	Request *ScriptRequest
	// Response is nil before the request has been sent, which is what a pre-request script finds when
	// it looks at pm.response.
	Response *Response

	// Variables is where the script reads and writes. The engine keeps no storage of its own: a value
	// a script writes goes back through here, so whether it outlives the run is decided in one place.
	Variables VarStore
}

// ScriptRequest is a request as a script sees it: the address it goes to, the headers as they will
// be sent, the body as it is written. Not the draft: a script counts what actually goes out, and a
// draft still holds `{{tokens}}` where the values will be.
type ScriptRequest struct {
	Method  string       `json:"method"`
	URL     string       `json:"url"`
	Headers []HeaderPair `json:"headers"`
	Body    string       `json:"body"`
	// BodyKind is here so that re-masking a request a script changed can render the body it actually
	// has: a form body and a file path cannot be rebuilt from the text a script was shown. The
	// prelude neither reads nor writes it, so a script can rewrite a JSON body and cannot turn one
	// kind into another.
	BodyKind BodyKind `json:"bodyKind,omitempty"`
}

// ScriptPass is one attempt as the scripts around it see it: the run it belongs to, the record
// the reports are written against, the node the request came from, and the request itself, which
// a pre-request script may change.
//
// No tags: it is the shape two features hand each other inside the app, so neither imports the
// other.
type ScriptPass struct {
	Run      string
	RecordID string
	NodeID   string
	Request  *ScriptRequest
	Response *Response

	// Ran is what the pre-request scripts did, carried to the other half of the same pass. A report
	// hangs off the record it ran around, and the record is written only once the request has been
	// answered — so the reports of the first half wait here for the record to exist. Whoever sends the
	// request carries them and never reads them.
	Ran []ScriptRun
}

// VarScope names one of the three places a script's variables live, by the name the script itself
// uses. It is not an EnvScope: "the active environment" is a place a script only knows exists, and
// which environment that is is the environments screen's business.
type VarScope string

const (
	// ScopeRun lives as long as the run it belongs to and disappears with it.
	ScopeRun         VarScope = "variables"
	ScopeEnvironment VarScope = "environment"
	ScopeGlobals     VarScope = "globals"
)

// VarStore is where a script's variables live. Reading `variables` reads the run's own scope and,
// through it, the environment and the globals — the order a request is resolved in; reading either
// of the other two reads that one alone, which is what tells a script whether a name is set in the
// environment or only borrowed from a run.
//
// Reading answers with an error and not with nothing: `no value` and `could not be read` are
// different answers, and a script that asks is entitled to tell them apart.
type VarStore interface {
	Lookup(scope VarScope, name string) (string, bool, error)
	Set(scope VarScope, name string, value string) error
}

// ScriptRun is one execution of one script: what ran, whether it went through, what it printed and
// what it asserted. A run that failed is still a run — the failure is what the tab has to show.
type ScriptRun struct {
	ID          string       `json:"id"`
	RecordID    string       `json:"recordId,omitempty"`
	NodeID      string       `json:"nodeId,omitempty"`
	Scope       ScriptScope  `json:"scope"`
	Error       string       `json:"error,omitempty"`
	Logs        []ScriptLog  `json:"logs"`
	Tests       []TestResult `json:"tests"`
	DurationUs  int64        `json:"durationUs"`
	CreatedAt   int64        `json:"createdAt"`
	OK          bool         `json:"ok"`
	SkipRequest bool         `json:"skipRequest,omitempty"`
}

// ScriptLog is one line a script printed. The level is the call it came from, so a warning does not
// have to be spelled out in the text.
type ScriptLog struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// TestResult is one assertion a script made. The name is the script's own — `pm.test('...')` — and
// a failure carries the reason beside it, because "it failed" without why is not a report.
type TestResult struct {
	Name       string `json:"name"`
	Passed     bool   `json:"passed"`
	Error      string `json:"error,omitempty"`
	DurationUs int64  `json:"durationUs"`
}
