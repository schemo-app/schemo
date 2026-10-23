// Package domain holds the app's entities and their rules, with no database, window or HTTP. Every
// type that crosses the IPC boundary is declared here so the binding generator emits one model
// file.
package domain

import (
	"context"
	"strings"
)

// WorkspaceKind defines the operational scope of a workspace (personal or shared team).
type WorkspaceKind string

const (
	WorkspacePersonal WorkspaceKind = "personal"
	WorkspaceTeam     WorkspaceKind = "team"
)

// Valid reports whether the workspace kind is supported.
func (k WorkspaceKind) Valid() bool {
	return k == WorkspacePersonal || k == WorkspaceTeam
}

// WorkspaceAccess is what the account says this person may do in a workspace: the role and the three
// acts the window draws a control for. It is the account's answer, never this device's, so only the
// round that reads the account's list writes it. The server judges every change itself.
type WorkspaceAccess struct {
	RoleName string `json:"roleName,omitempty"`
	Update   bool   `json:"update"`
	Delete   bool   `json:"delete"`
	// Write is whether this person may put anything into the workspace at all, and a reader whose
	// changes would be refused and parked is owed the warning before it.
	Write bool `json:"write"`
}

// OwnAccess is a workspace nobody else answers for: one this device holds alone or one the account has
// not described. Every act is allowed, since holding work back on an answer that never came would
// silently stop syncing.
func OwnAccess() WorkspaceAccess {
	return WorkspaceAccess{Update: true, Delete: true, Write: true}
}

// Workspace represents the primary entity container for collections, environments, and history.
type Workspace struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Kind      WorkspaceKind `json:"kind"`
	Color     string        `json:"color"`
	CreatedAt int64         `json:"createdAt"`
	UpdatedAt int64         `json:"updatedAt"`
	RemoteID  string        `json:"remoteId,omitempty"`
	// Access is what the account says about this person's place in the workspace, kept and not asked
	// for on every draw: the sheet showing a workspace is opened from the list the round already read.
	Access WorkspaceAccess `json:"access"`
}

// WorkspacePersonalID is the immutable default personal workspace ID.
const WorkspacePersonalID = "personal"

// IsPersonal reports whether the given ID corresponds to the default personal workspace.
func IsPersonal(id string) bool { return id == WorkspacePersonalID }

// SettingAccountContext defines the key storing the active account context string.
const SettingAccountContext = "sync.account"

// SettingAccountUser defines the key prefix storing user account ID associations.
const SettingAccountUser = "sync.user:"

// SettingActiveWorkspace defines the key storing currently selected workspace ID.
const SettingActiveWorkspace = "workspace.activeId"

// WorkspaceState holds the full list of workspaces, current active ID, and item counts.
type WorkspaceState struct {
	Workspaces []Workspace                `json:"workspaces"`
	ActiveID   string                     `json:"activeId"`
	PersonalID string                     `json:"personalId"`
	Counts     map[string]WorkspaceCounts `json:"counts,omitempty"`
}

// WorkspaceCounts holds total counts of contained entities within a workspace.
type WorkspaceCounts struct {
	Collections  int `json:"collections"`
	Environments int `json:"environments"`
	Runs         int `json:"runs"`
}

// NewWorkspace constructs a new Workspace instance.
func NewWorkspace(id string, name string, kind WorkspaceKind, color string, now int64) Workspace {
	return Workspace{
		ID:        id,
		Name:      name,
		Kind:      kind,
		Color:     color,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Strip is the window's tabs in one workspace: the tabs in their order and the one on screen.
// The window owns the rules of the strip and saves it whole after every change.
type Strip struct {
	Workspace string `json:"workspace"`
	Tabs      []Tab  `json:"tabs"`
	Active    string `json:"active"`
}

// Tab is one tab of the strip.
type Tab struct {
	ID      string  `json:"id"`
	Kind    TabKind `json:"kind"`
	Ref     string  `json:"ref,omitempty"`
	DraftID DraftID `json:"draftId,omitempty"`
	Record  string  `json:"record,omitempty"`
	Pinned  bool    `json:"pinned"`
	Preview bool    `json:"preview"`
}

// TabKind is what a tab is about: a request being composed, a collection's page, or one of the four
// single-instance pages (overview, history, capture and published collections).
type TabKind string

const (
	TabKindRequest     TabKind = "request"
	TabKindCollections TabKind = "collections"
	TabKindWelcome     TabKind = "welcome"
	TabKindHistory     TabKind = "history"
	TabKindCapture     TabKind = "capture"
	TabKindPubs        TabKind = "pubs"
)

// Valid reports whether the kind is one the strip knows how to draw.
func (k TabKind) Valid() bool {
	switch k {
	case TabKindRequest, TabKindCollections, TabKindWelcome, TabKindHistory, TabKindCapture,
		TabKindPubs:
		return true
	}
	return false
}

// Page reports whether the kind is a page: it shows the same thing however it was reached, so a
// strip holds at most one of each.
func (k TabKind) Page() bool {
	switch k {
	case TabKindWelcome, TabKindHistory, TabKindCapture, TabKindPubs:
		return true
	}
	return false
}

// TabDraftPrefix names the drafts the window mints for its request tabs.
const TabDraftPrefix = "tab:"

// IsTabDraft reports whether a draft belongs to the strip, and is therefore written down: a
// collection node's draft is a proposal until it is saved, and a tab's is what a restart restores.
func IsTabDraft(id DraftID) bool {
	return strings.HasPrefix(string(id), TabDraftPrefix)
}

// A team is the people in a workspace and the links waiting to let more in. It is the cloud's side of a
// workspace, never stored in the local database. A personal workspace has a team of one and no way to
// invite; the server refuses every put.

// Member is one person in a workspace.
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
	// them from their own workspace.
	Self bool `json:"self,omitempty"`
	// Gone says the account behind this membership was deleted. The row stays: a workspace somebody
	// worked in does not lose its history because they left.
	Gone bool `json:"gone,omitempty"`
}

// ScopeWorkspace is what a role may be written in: one workspace of it. The application hands roles out
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
	// WorkspaceID is the workspace the link also lets somebody into, empty for one into the organisation
	// alone. A workspace's own invitations are the organisation's narrowed to the ones that name it.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// Email is the address the invitation is for, empty for a link anybody holding it may use.
	Email    string `json:"email,omitempty"`
	RoleID   string `json:"roleId"`
	RoleName string `json:"roleName,omitempty"`
	// Status is the server's own word: "pending", "expired", "exhausted" or "revoked".
	Status string `json:"status"`
	// CreatedAt is when the link was made, in Unix milliseconds. The window shows it beside the
	// address so a stale invitation can be told from a recent one.
	CreatedAt int64 `json:"createdAt,omitempty"`
	// ExpiresAt is when the link stops working, in Unix milliseconds.
	ExpiresAt int64 `json:"expiresAt,omitempty"`
	// MaxUses of 0 is no limit, and UsedCount is how many have taken it up.
	MaxUses   int32 `json:"maxUses,omitempty"`
	UsedCount int32 `json:"usedCount,omitempty"`
}

// Invitation is what a caller asks for when inviting somebody: who it is for, what they get and how
// long the link lives. It is a struct so the address and the role cannot be swapped by a caller.
type Invitation struct {
	// Email is the address to invite, empty for a link to hand over by hand. A letter goes out when
	// it is named, and only that address may accept the link.
	Email string
	// RoleID is what the person is given in the workspace. Empty takes the organisation's default.
	RoleID string
	// ExpiresHours of 0 takes the server's default.
	ExpiresHours int32
	// Lang is the language the letter is written in — the one the inviter is reading the window in.
	Lang string
}

// Scope answers which workspace a call works in: the one the window is showing. It is a port because
// features never import each other, declared once for the eight that need it.
type Scope interface {
	ActiveWorkspace(ctx context.Context) (string, error)
}

// WorkspaceScope addresses one workspace without changing the desktop selection.
type WorkspaceScope string

func (s WorkspaceScope) ActiveWorkspace(context.Context) (string, error) {
	return string(s), nil
}
