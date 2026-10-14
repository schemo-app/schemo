package domain

// A team: the people in a space, and the links that are waiting to let more in.
//
// This is the cloud's side of a space rather than this device's. Who is in a space is decided there
// and by roles the organisation writes, so what is here is what a window draws and what a caller
// asks — none of it is kept in the database beside the collections.
//
// A space of somebody's own has a team of one and no way to invite anybody: the server refuses
// every put, and the window says so rather than drawing controls that answer with a refusal.

// Member is one person in a space.
type Member struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
	// Name is what they are called, empty when they have never given one: the address is then the
	// name, and the window falls back to it rather than drawing a blank row.
	Name string `json:"name,omitempty"`
	// RoleID and RoleName are what they hold here. Both travel because the window draws the name and
	// a change sends the id back.
	RoleID   string `json:"roleId"`
	RoleName string `json:"roleName"`
	// Self says this is the person reading — the window marks the row rather than offering to remove
	// them from their own space.
	Self bool `json:"self,omitempty"`
	// Gone says the account behind this membership was deleted. The row stays: a space somebody
	// worked in does not lose its history because they left.
	Gone bool `json:"gone,omitempty"`
}

// ScopeWorkspace is what a role may be written in: one space of it. The application hands roles out
// and never writes one — a role is the organisation's own decision, made in the panel.
const ScopeWorkspace = "workspace"

// Role is a named set of permissions. The application does not write these — an organisation does,
// in the panel — so what is here is enough to draw a list and to send one back.
type Role struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Description is what the role is for, which the window may show under the name.
	Description string `json:"description,omitempty"`
	// BuiltIn is one of the four the product ships. They are held out for reading and handing out,
	// never edited, and the window draws them as words rather than as somebody's own naming.
	BuiltIn bool `json:"builtIn,omitempty"`
}

// Invite is a link that lets somebody in, as the window draws it. The token is never here: it
// exists in the answer that made it and in the letter it was sent in.
type Invite struct {
	ID string `json:"id"`
	// WorkspaceID is the space the link also lets somebody into, empty for one into the organisation
	// alone. A space's own invitations are the organisation's narrowed to the ones that name it.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// Email is the address the invitation is for, empty for a link anybody holding it may use.
	Email    string `json:"email,omitempty"`
	RoleID   string `json:"roleId"`
	RoleName string `json:"roleName,omitempty"`
	// Status is the server's own word: "pending", "expired", "exhausted" or "revoked".
	Status string `json:"status"`
	// CreatedAt is when the link was made, in Unix milliseconds. The window draws it beside the
	// address: an invitation nobody has taken up is a row that has been standing there a while, and
	// how long is the whole of what says whether to wait or to ask again.
	CreatedAt int64 `json:"createdAt,omitempty"`
	// ExpiresAt is when the link stops working, in Unix milliseconds.
	ExpiresAt int64 `json:"expiresAt,omitempty"`
	// MaxUses of 0 is no limit, and UsedCount is how many have taken it up.
	MaxUses   int32 `json:"maxUses,omitempty"`
	UsedCount int32 `json:"usedCount,omitempty"`
}

// Invitation is what a caller asks for when they invite somebody: who it is for, what they get
// and how long the link lives. A struct rather than a list of strings because the two that matter
// most — the address and the role — would otherwise sit beside each other as two strings, and a
// caller that swapped them would be a caller that compiles.
type Invitation struct {
	// Email is the address to invite, empty for a link to hand over by hand. A letter goes out when
	// it is named, and only that address may accept the link.
	Email string
	// RoleID is what the person is given in the space. Empty takes the organisation's default.
	RoleID string
	// ExpiresHours of 0 takes the server's default.
	ExpiresHours int32
	// Lang is the language the letter is written in — the one the inviter is reading the window in.
	Lang string
}
