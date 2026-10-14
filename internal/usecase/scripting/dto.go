package scripting

import (
	"schemo/internal/domain"
)

// The shape that exists only for the window: one level of the inheritance chain, as the pane draws
// it. What a level's scripts come to is a domain type and is not here.

// Level is one step of a chain: a collection, a collection inside one, or a request, and what it
// runs.
type Level struct {
	NodeID  string         `json:"nodeId"`
	Name    string         `json:"name"`
	Kind    string         `json:"kind"`
	Scripts domain.Scripts `json:"scripts"`
}
