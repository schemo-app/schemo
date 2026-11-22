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
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Color       string                `json:"color,omitempty"`
	Readonly    bool                  `json:"readonly"`
	Position    int                   `json:"position"`
	Vars        []Variable            `json:"vars"`
	Collections map[string][]Variable `json:"collections"`
}

// Block is what the environment holds for one collection; nil when it holds nothing.
func (e Environment) Block(collection string) []Variable {
	if collection == "" {
		return nil
	}
	return e.Collections[collection]
}

// EnvState is everything the environments screen and the request preview need. Globals are the
// scope that applies whatever the active environment is.
type EnvState struct {
	Environments []Environment `json:"environments"`
	Globals      []Variable    `json:"globals"`
	// ActiveID is empty when no environment is selected.
	ActiveID string `json:"activeId,omitempty"`
}

// Find is the environment with the id; false when there is none.
func (s EnvState) Find(id string) (Environment, bool) {
	for _, environment := range s.Environments {
		if environment.ID == id {
			return environment, true
		}
	}
	return Environment{}, false
}

// EnvScope names where a variable lives: one environment, one collection's block in it, or the
// globals that always apply.
type EnvScope struct {
	// Environment is the environment's id; empty means the globals scope.
	Environment string `json:"environment,omitempty"`
	// Collection narrows an environment to its block for one collection; it means nothing without one.
	Collection string `json:"collection,omitempty"`
}

// Variables returns the values in one scope; the zero scope is the workspace globals.
func (s EnvState) Variables(scope EnvScope) []Variable {
	if scope.Environment == "" {
		return s.Globals
	}
	env, _ := s.Find(scope.Environment)
	if scope.Collection != "" {
		return env.Collections[scope.Collection]
	}
	return env.Vars
}

// VarLevels is what the levels over a request answer for its `{{tokens}}`: the collection it is in
// (whose block in the environment applies) and the variables of every level, outermost first.
type VarLevels struct {
	Collection string     `json:"collection,omitempty"`
	Variables  []Variable `json:"variables,omitempty"`
}

// VarImport is what bringing variables into a set did: names added, and names it already held,
// whose values were left as they were.
type VarImport struct {
	Added int `json:"added"`
	Kept  int `json:"kept"`
}

// EnvImport is what a document's environments did to the list: the environments it had to make,
// by name, and the variables it added or found already there.
type EnvImport struct {
	Created []string `json:"created"`
	Added   int      `json:"added"`
	Kept    int      `json:"kept"`
	State   EnvState `json:"state"`
}

// Imported is the tree after an import, and the collection each of the file's collections now
// answers to in the environments: itself at the top level, the collection it went into otherwise.
type Imported struct {
	Tree    []Collection
	Targets []string
}

// Resolution is what a `{{token}}` resolves to, with where it came from so the tooltip can say it.
type Resolution struct {
	Value  string       `json:"value,omitempty"`
	Source string       `json:"source"` // "global" | "collection" | "env" | "block"
	Kind   VariableKind `json:"kind"`
	// HasValue is false when the value was withheld from the answer (a secret) or is empty.
	HasValue bool `json:"hasValue"`
}

// Scripts run around a request. Nil means not set here; levels do not override each other, every
// level's script runs, outermost first.
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
	// BodyKind lets re-masking rebuild form and file bodies; scripts cannot change it.
	BodyKind BodyKind `json:"bodyKind,omitempty"`
}

// ScriptPass is one attempt as its scripts see it; internal only, hence no tags.
type ScriptPass struct {
	Run      string
	RecordID string
	NodeID   string
	Request  *ScriptRequest
	Response *Response

	// Ran carries pre-request reports to the post-request half, since the record is written after the answer.
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

// VarStore holds script variables. `variables` reads run scope, environment, then globals; the others
// read one scope. Errors are distinct from "no value".
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
