package domain

// ResolvedRequest is one saved request as a copy of it needs to see it: the request whole, the
// authorization the nearest level above answers and the variables of the levels above it. The tree
// the window draws carries neither bodies nor those answers, so the walk that knows them hands them
// over in this shape.
type ResolvedRequest struct {
	Node      CollectionNode `json:"node"`
	Auth      *Auth          `json:"auth,omitempty"`
	Variables []Variable     `json:"variables,omitempty"`
}
