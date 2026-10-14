package domain

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

// WorkspaceAccess is what the account says this person may do in a space: the role they hold, and
// the three acts the window draws a control for. It is the account's answer and never this device's
// — a device that kept its own would be a second opinion about who may write — so only the round
// that reads the account's list writes it. Nothing here decides anything: the server judges every
// change on its own, and these are what a window draws before it asks.
type WorkspaceAccess struct {
	RoleName string `json:"roleName,omitempty"`
	Update   bool   `json:"update"`
	Delete   bool   `json:"delete"`
	// Write is whether this person may put anything into the space at all, and a reader whose
	// changes would be refused and parked is owed the warning before it.
	Write bool `json:"write"`
}

// OwnAccess is a space nobody else answers for: one this device holds alone, or one the account has
// not described. Every act in it is the person's, because a device that held its work back on an
// answer it never got would quietly stop syncing, and the server refuses what it must regardless.
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
	// Access is what the account says about this person's place in the space, kept and not asked
	// for on every draw: the sheet showing a space is opened from the list the round already read.
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
