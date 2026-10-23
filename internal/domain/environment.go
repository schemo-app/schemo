package domain

import (
	"strings"
)

// VariableKind separates a value that may be shown from one that must never leave the app
// unmasked: exports, logs and the request preview all go through the mask.
type VariableKind string

const (
	VariableText   VariableKind = "text"
	VariableSecret VariableKind = "secret"
)

// BaseURLVariable is the variable addresses are written against. The OpenAPI reader declares it
// from a document's servers and the first environment starts with it.
const BaseURLVariable = "baseUrl"

// SecretMask is what a value the app must not show is replaced by wherever it would be written
// down — the preview, a record, an export, and the header the engine computed for a Digest.
const SecretMask = "••••••••••••"

// Variable is one `{{name}}` value. A secret's value is stored like any other (the database keeps
// plain text); masking keeps it out of everything the user can see or copy.
type Variable struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Value    string       `json:"value,omitempty"`
	Kind     VariableKind `json:"kind"`
	Position int          `json:"position"`
	Enabled  bool         `json:"enabled"`
	HasValue bool         `json:"hasValue"`
}

// Environment is a named set of variables, one of which is active at a time.
type Environment struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Color    string     `json:"color,omitempty"`
	Readonly bool       `json:"readonly"`
	Position int        `json:"position"`
	Vars     []Variable `json:"vars"`
}

// EnvState is everything the environments screen and the request preview need. Globals are the
// scope that applies whatever the active environment is.
type EnvState struct {
	Environments []Environment `json:"environments"`
	Globals      []Variable    `json:"globals"`
	// ActiveID is empty when no environment is selected.
	ActiveID string `json:"activeId,omitempty"`
}

// EnvScope names where a variable lives: one environment, or the globals that always apply.
type EnvScope struct {
	// Environment is the environment's id; empty means the globals scope.
	Environment string `json:"environment,omitempty"`
}

// Resolution is what a `{{token}}` resolves to, with where it came from so the tooltip can say it.
type Resolution struct {
	Value  string       `json:"value,omitempty"`
	Source string       `json:"source"` // "env" | "global"
	Kind   VariableKind `json:"kind"`
	// HasValue is false when the value was withheld from the answer (a secret) or is empty.
	HasValue bool `json:"hasValue"`
}

// Scripts is the code that runs around a request: before it goes out and after it comes back. On a
// node it is a pointer like Auth (nil means not set here), but levels do not override each other:
// every level with a script runs, outermost first.
type Scripts struct {
	Pre  string `json:"pre,omitempty"`
	Post string `json:"post,omitempty"`
	// A half can be switched off without losing its code. An off half is still an answer about its
	// level, so it keeps the level in the chain.
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

// ScriptInput is one script about to run: the code, when it runs and what it sees, which is the
// request and, once answered, the response.
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

// ScriptRequest is a request as a script sees it: the address it goes to, the headers as sent and
// the body as written. Not the draft, which still holds `{{tokens}}`.
type ScriptRequest struct {
	Method  string       `json:"method"`
	URL     string       `json:"url"`
	Headers []HeaderPair `json:"headers"`
	Body    string       `json:"body"`
	// BodyKind lets re-masking render the body a script changed: form bodies and file paths cannot
	// be rebuilt from the text a script saw. The prelude neither reads nor writes it, so a script
	// can rewrite a JSON body but not change its kind.
	BodyKind BodyKind `json:"bodyKind,omitempty"`
}

// ScriptPass is one attempt as the scripts around it see it: the run, the record the reports are
// written against, the originating node and the request, which a pre-request script may change. It
// has no tags: it is the shape two features hand each other inside the app.
type ScriptPass struct {
	Run      string
	RecordID string
	NodeID   string
	Request  *ScriptRequest
	Response *Response

	// Ran carries the pre-request scripts' results to the other half of the same pass. Reports hang
	// off the record, which is written only after the answer, so they wait here; whoever sends the
	// request carries them and never reads them.
	Ran []ScriptRun
}

// VarScope names one of the three places a script's variables live, by the name the script uses. It
// is not an EnvScope: the active environment is a place a script only knows exists.
type VarScope string

const (
	// ScopeRun lives as long as the run it belongs to and disappears with it.
	ScopeRun         VarScope = "variables"
	ScopeEnvironment VarScope = "environment"
	ScopeGlobals     VarScope = "globals"
)

// VarStore is where a script's variables live. Reading `variables` reads the run scope, then the
// environment, then the globals; reading either of the other two scopes reads that one alone. Reads
// return an error rather than nothing: `no value` and `could not be read` are different answers.
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
