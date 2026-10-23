package cloud

import (
	"context"
	"testing"

	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
)

// The team of a workspace: who is in it, what is waiting to let more in, and the one rule that is about
// the workspace rather than the caller: a workspace of somebody's own holds one person.
//
// The fakes answer the way the server does, refusals and all. This feature decides nothing, so what
// is tested is what it asks for, what it does with the answer, and which row it calls the reader's
// own.

const (
	workspaceID = "space-1"
	account     = "00000000-0000-0000-0000-0000000000aa"
	orgID       = "org-1"
	me          = "user-me"
	other       = "user-other"
	roleEdit    = "role-editor"
)

func TestTheTeamOfAWorkspaceIsItsPeopleItsLinksAndItsRoles(t *testing.T) {
	members, _ := newMembers(t)

	held, err := members.Team(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}

	if held.WorkspaceID != workspaceID || held.AccountID != account {
		t.Errorf("team = %+v, want it named by both ids", held)
	}
	if len(held.Members) != 2 {
		t.Fatalf("members = %+v, want the two the cloud answered", held.Members)
	}
	// Which of them is the person reading is this device's to say, and the server answers no such
	// field: the row marked is the one the window does not offer to remove.
	if !held.Members[0].Self || held.Members[1].Self {
		t.Errorf("members = %+v, want the reader marked and nobody else", held.Members)
	}
	if len(held.Roles) != 1 || held.Roles[0].Name != "Editor" {
		t.Errorf("roles = %+v, want the workspace roles of the organisation", held.Roles)
	}
	// An invitation into another workspace is not this workspace's, whatever organisation made it.
	if len(held.Invites) != 1 || held.Invites[0].Email != "ann@acme.com" {
		t.Errorf("invites = %+v, want only the ones that name this workspace", held.Invites)
	}
}

// A workspace that has never been linked has no team: there is no account for anybody to have been
// invited through, and the window is told that rather than handed an empty list to draw.
func TestAWorkspaceThatIsNotInTheAccountHasNoTeam(t *testing.T) {
	members, _ := newMembers(t)
	if _, err := members.store.DB().Exec(`UPDATE workspaces SET remote_workspace_id = '' WHERE id = ?`,
		workspaceID); err != nil {
		t.Fatal(err)
	}

	held, err := members.Team(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if held.AccountID != "" || len(held.Members) != 0 {
		t.Errorf("team = %+v, want nothing for a workspace nobody has linked", held)
	}

	// And inviting somebody into it is refused rather than made in an account it is not in.
	_, err = members.Invite(context.Background(), workspaceID, "ann@acme.com", "")
	if domain.CodeOf(err) != domain.CodeWorkspaceMissing {
		t.Errorf("refusal = %q, want a workspace with no account refused", domain.CodeOf(err))
	}
}

// A workspace of somebody's own holds one person. It is read from the account's answer and not from the
// row here: what a workspace is for is the server's word, and a window that drew controls on its own
// opinion would draw the ones the server then refuses.
func TestAWorkspaceOfOnesOwnIsSaidToBe(t *testing.T) {
	members, server := newMembers(t)
	server.kind = domain.WorkspacePersonal

	held, err := members.Team(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if !held.Personal {
		t.Error("a workspace of somebody's own is not marked as one")
	}
}

// Inviting somebody is one call naming the organisation the workspace stands in — the account's answer
// and not anything kept on this device — and it answers the team again.
func TestInvitingSomebodyNamesTheOrganisationOfTheWorkspace(t *testing.T) {
	members, server := newMembers(t)

	held, err := members.Invite(context.Background(), workspaceID, "ann@acme.com", roleEdit)
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if len(server.invited) != 1 {
		t.Fatalf("invited = %+v, want one invitation made", server.invited)
	}
	made := server.invited[0]
	if made.org != orgID || made.workspace != account || made.asked.Email != "ann@acme.com" {
		t.Errorf("invited = %+v, want it made in the organisation the workspace stands in", made)
	}
	if len(held.Invites) != 1 {
		t.Errorf("invites = %+v, want the answer to carry the link that was made", held.Invites)
	}
}

// And what the server refuses is handed back as it stands: this feature words nothing of its own,
// and a caller who may not administer the workspace is answered by the server and not guessed at here.
func TestARefusalTravels(t *testing.T) {
	members, server := newMembers(t)
	server.refuse = domain.Refuse(domain.CodeNotAllowed, domain.ErrNotAllowed, nil)

	_, err := members.SetRole(context.Background(), workspaceID, other, roleEdit)
	if domain.CodeOf(err) != domain.CodeNotAllowed {
		t.Errorf("refusal = %q, want the server's own code", domain.CodeOf(err))
	}
}

// The fake server: one workspace of one organisation, two people, and the links made for it.
type fakeMemberServer struct {
	kind    domain.WorkspaceKind
	refuse  error
	invited []invitation
}

type invitation struct {
	org, workspace string
	asked          domain.Invitation
}

func (f *fakeMemberServer) Workspaces(
	context.Context, domain.Caller,
) (domain.AccountWorkspaces, error) {
	if f.refuse != nil {
		return domain.AccountWorkspaces{}, f.refuse
	}
	return domain.AccountWorkspaces{
		OwnOrgID: orgID,
		Workspaces: []domain.RemoteWorkspace{{
			ID: account, OrgID: orgID, Kind: f.kind, OwnOrg: true,
		}},
	}, nil
}

func (f *fakeMemberServer) Members(
	context.Context, domain.Caller, string,
) ([]domain.Member, error) {
	return []domain.Member{
		{UserID: me, Email: "me@acme.com", RoleID: "role-owner", RoleName: "Owner"},
		{UserID: other, Email: "ann@acme.com", RoleID: roleEdit, RoleName: "Editor"},
	}, nil
}

func (f *fakeMemberServer) SetMember(
	context.Context, domain.Caller, string, string, string,
) (domain.Member, error) {
	return domain.Member{}, nil
}

func (f *fakeMemberServer) ChangeRole(
	context.Context, domain.Caller, string, string, string,
) (domain.Member, error) {
	return domain.Member{}, nil
}

func (f *fakeMemberServer) RemoveMember(context.Context, domain.Caller, string, string) error {
	return nil
}

func (f *fakeMemberServer) Invites(
	context.Context, domain.Caller, string,
) ([]domain.Invite, error) {
	return []domain.Invite{
		{ID: "invite-1", WorkspaceID: account, Email: "ann@acme.com", RoleID: roleEdit,
			RoleName: "Editor", Status: "pending"},
		// Somebody else's workspace in the same organisation: the link is real and it is not this one's.
		{ID: "invite-2", WorkspaceID: "another-space", Email: "bob@acme.com", Status: "pending"},
	}, nil
}

func (f *fakeMemberServer) Invite(
	_ context.Context,
	_ domain.Caller,
	orgID, workspaceID string,
	asked domain.Invitation,
) (domain.Invite, error) {
	if f.refuse != nil {
		return domain.Invite{}, f.refuse
	}
	f.invited = append(f.invited, invitation{org: orgID, workspace: workspaceID, asked: asked})
	return domain.Invite{ID: "invite-3", WorkspaceID: workspaceID, Email: asked.Email,
		Status: "pending"}, nil
}

func (f *fakeMemberServer) RevokeInvite(
	context.Context, domain.Caller, string, string,
) (domain.Invite, error) {
	return domain.Invite{}, f.refuse
}

func (f *fakeMemberServer) Roles(
	_ context.Context, _ domain.Caller, _, scope string,
) ([]domain.Role, error) {
	if scope != domain.ScopeWorkspace {
		return nil, domain.ErrNotFound
	}
	return []domain.Role{{ID: roleEdit, Name: "Editor", BuiltIn: true}}, nil
}

// The fake session: whoever this device is signed in as.
type memberSession struct{}

func (memberSession) Caller(context.Context) (domain.Caller, error) {
	return domain.Caller{Address: "schemo.local", Access: "token", SessionID: "s-1"}, nil
}

func (memberSession) Me(context.Context) (domain.Account, error) {
	return domain.Account{UserID: me, Email: "me@acme.com"}, nil
}

// newMembers is one workspace of a team's, in an account, with two people in it.
func newMembers(t *testing.T) (*Members, *fakeMemberServer) {
	t.Helper()
	store := dbtest.New(t)
	if err := store.SaveWorkspace(context.Background(),
		domain.NewWorkspace(workspaceID, "Payments", domain.WorkspaceTeam, "", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE workspaces SET remote_workspace_id = ? WHERE id = ?`,
		account, workspaceID); err != nil {
		t.Fatal(err)
	}
	server := &fakeMemberServer{kind: domain.WorkspaceTeam}
	return NewMembers(store, server, memberSession{}), server
}
