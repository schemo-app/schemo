package members

import (
	"schemo/internal/domain"
)

// The shape that exists only for the window: the team of the space on screen. A member row is a
// domain type and is not here.

// Team is who is in one space and what is waiting to let more in. Every change answers the whole of
// it again, so a window is always drawn from an answer the server has just given rather than from a
// list it patched itself.
type Team struct {
	// SpaceID is the space here, which is what the window asked with.
	SpaceID string `json:"spaceId"`
	// AccountID is the space's id in the account, empty for a space that has never been linked.
	AccountID string `json:"accountId,omitempty"`
	// Personal says the space is somebody's own: nobody may be put in it, and there is nothing for
	// the window to offer — the server refuses every put, and the window says why.
	Personal bool `json:"personal"`
	// Members are the people in it, the one reading marked among them.
	Members []domain.Member `json:"members"`
	// Invites are the links that have not been used up or taken back, for this space only.
	Invites []domain.Invite `json:"invites"`
	// Roles are the roles that may be handed out here: the workspace ones of the organisation the
	// space stands in.
	Roles []domain.Role `json:"roles"`
}
