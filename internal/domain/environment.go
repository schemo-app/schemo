package domain

// VariableKind separates a value that may be shown from one that must never leave the app
// unmasked: exports, logs and the request preview all go through the mask.
type VariableKind string

const (
	VariableText   VariableKind = "text"
	VariableSecret VariableKind = "secret"
)

// BaseURLVariable is the name of the variable an address is written against — the one the OpenAPI
// reader declares out of a document's servers, and the one the first environment arrives with. It
// is a name rather than a port because the readers and the environments both speak it.
const BaseURLVariable = "baseUrl"

// SecretMask is what a value the app must not show is replaced by wherever it would be written
// down — the preview, a record, an export, and the header the engine computed for a Digest.
const SecretMask = "••••••••••••"

// Variable is one `{{name}}` value. A secret's value is stored here like any other — the database
// keeps it as plain text, which is what portability costs — so masking is what keeps it out of
// everything the user can see or copy.
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
