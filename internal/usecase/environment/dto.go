package environment

import (
	"schemo/internal/domain"
)

// The shapes that exist only for the window: what a new environment is asked for, the patch that
// changes one, and the same pair for its variables. A stored row is a domain type, not here.

// Draft is what a new environment is asked for: its name, the colour its dot and avatar
// are drawn with, and — when it is made from an existing one — the environment it starts as a copy
// of. A draft rather than three more parameters: the create form asks for all three at once, and a
// signature that grows an argument every time the form gains a field is one nobody can read.
type Draft struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
	// StartFrom is an environment id, empty for a blank one.
	StartFrom string `json:"startFrom,omitempty"`
}

// Patch is a partial update: a nil field is left as it is.
type Patch struct {
	Name     *string `json:"name,omitempty"`
	Color    *string `json:"color,omitempty"`
	Readonly *bool   `json:"readonly,omitempty"`
}

// Created is the state after an environment appeared, with the id it was given: the sheet opens on
// the new one, and an id the window matched out of a before-and-after would be a guess.
type Created struct {
	domain.EnvState
	ID string `json:"id"`
}

// VariableDraft is a variable on its way in: the sheet's "add row" leaves it empty, the .env
// dialog fills it in, and both go through one call.
type VariableDraft struct {
	Name  string              `json:"name"`
	Kind  domain.VariableKind `json:"kind"`
	Value string              `json:"value,omitempty"`
}

// VariablePatch edits one variable. SetValue is what keeps a secret's value when only its name or
// its enabled flag is being changed: the sheet never has the old value to send back.
type VariablePatch struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Kind     domain.VariableKind `json:"kind"`
	Value    string              `json:"value,omitempty"`
	Enabled  bool                `json:"enabled"`
	SetValue bool                `json:"setValue"`
}
