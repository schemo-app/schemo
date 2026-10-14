package workspace

import (
	"schemo/internal/domain"
)

// The shapes that exist only for the window: what making a space asks for, what it answers with,
// an invitation on its way out, and what became of one. A space and its members are domain types.

// CreateInput is what the create card sends: what the space is called, how it looks, and what it is
// for. The kind is the design's second half — "Personal" or "Team" — and it is a word this app
// keeps and travels with the space, not a claim about who may enter: who is in a space is
// membership, and membership is the cloud panel's to hand out.
type CreateInput struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	// Kind is "personal" or "team", and empty is the personal one: a space nobody said anything about
	// is somebody's own.
	Kind string `json:"kind,omitempty"`
	// Invites are the people written in the create form, with the role each is to be given here. They
	// are sent the moment the space exists in the account, because there is nothing to invite anybody
	// into before that.
	Invites []Invitation `json:"invites,omitempty"`
}

// Created is what making a space came to: the state a window draws, and what became of each address
// written in the form. The two are one answer rather than two calls, because the space exists
// either way once the account has it: a letter that was refused does not undo it, and a person is
// owed the one that failed rather than a sentence about all of them.
type Created struct {
	// The state is the whole set as every other call answers it — a window draws the switcher and the
	// form from one answer — with what became of the invitations beside it.
	domain.WorkspaceState
	// Invites are the outcomes, in the order the addresses were written. A space made without any is
	// an empty list and not a nil one, which is what a window draws nothing for.
	Invites []InviteOutcome `json:"invites"`
}

// Patch is a partial change: a nil field is left as it is.
type Patch struct {
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
	// Kind is "personal" or "team": what a space is for changes when the person working in it decides
	// it is somebody else's too.
	Kind *string `json:"kind,omitempty"`
}

// Invitation is one address written in the create form, and the role it will be given. An empty
// role is the organisation's default, which is the server's to choose.
type Invitation struct {
	Email  string `json:"email"`
	RoleID string `json:"roleId,omitempty"`
}

// InviteOutcome is what became of one address. A refusal is the server's own — "no seats left"
// and "no such address" are different things to tell somebody — and the window words it.
type InviteOutcome struct {
	Email   string          `json:"email"`
	Failure *domain.Failure `json:"failure,omitempty"`
}

// Changed is what a window receives when the workspace on screen moves.
type Changed struct {
	Workspace domain.Workspace `json:"workspace"`
}
