package members

import (
	"context"
	"testing"

	"schemo/internal/domain"
)

// The team of a space: who is in it, what is waiting to let more in, and the one rule that is about
// the space rather than about the caller — a space of somebody's own holds one person.
//
// The fakes answer the way the server does, refusals and all: this feature decides nothing, so
// what is tested here is what it asks for, what it does with the answer, and which row it calls the
// reader's own.

const (
	spaceID  = "space-1"
	account  = "00000000-0000-0000-0000-0000000000aa"
	orgID    = "org-1"
	me       = "user-me"
	other    = "user-other"
	roleEdit = "role-editor"
)

func TestTheTeamOfASpaceIsItsPeopleItsLinksAndItsRoles(t *testing.T) {
	uc, _ := newUseCase()

	held, err := uc.Show(context.Background(), spaceID)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}

	if held.SpaceID != spaceID || held.AccountID != account {
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
	// An invitation into another space is not this space's, whatever organisation made it.
	if len(held.Invites) != 1 || held.Invites[0].Email != "ann@acme.com" {
		t.Errorf("invites = %+v, want only the ones that name this space", held.Invites)
	}
}

// A space that has never been linked has no team: there is no account for anybody to have been
// invited through, and the window is told that rather than handed an empty list to draw.
func TestASpaceThatIsNotInTheAccountHasNoTeam(t *testing.T) {
	uc, _ := newUseCase()
	uc.store = &fakeStore{space: domain.Workspace{ID: spaceID, Kind: domain.WorkspaceTeam}}

	held, err := uc.Show(context.Background(), spaceID)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if held.AccountID != "" || len(held.Members) != 0 {
		t.Errorf("team = %+v, want nothing for a space nobody has linked", held)
	}

	// And inviting somebody into it is refused rather than made in an account it is not in.
	_, err = uc.Invite(context.Background(), spaceID, domain.Invitation{Email: "ann@acme.com"})
	if domain.CodeOf(err) != domain.CodeWorkspaceMissing {
		t.Errorf("refusal = %q, want a space with no account refused", domain.CodeOf(err))
	}
}

// A space of somebody's own holds one person. It is read from the account's answer and not from the
// row here: what a space is for is the server's word, and a window that drew controls on its own
// opinion would draw the ones the server then refuses.
func TestASpaceOfOnesOwnIsSaidToBe(t *testing.T) {
	uc, server := newUseCase()
	server.kind = domain.WorkspacePersonal

	held, err := uc.Show(context.Background(), spaceID)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if !held.Personal {
		t.Error("a space of somebody's own is not marked as one")
	}
}

// Inviting somebody is one call naming the organisation the space stands in — the account's answer
// and not anything kept on this device — and it answers the team again.
func TestInvitingSomebodyNamesTheOrganisationOfTheSpace(t *testing.T) {
	uc, server := newUseCase()

	held, err := uc.Invite(context.Background(), spaceID, domain.Invitation{
		Email: "ann@acme.com", RoleID: roleEdit,
	})
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if len(server.invited) != 1 {
		t.Fatalf("invited = %+v, want one invitation made", server.invited)
	}
	made := server.invited[0]
	if made.org != orgID || made.space != account || made.asked.Email != "ann@acme.com" {
		t.Errorf("invited = %+v, want it made in the organisation the space stands in", made)
	}
	if len(held.Invites) != 1 {
		t.Errorf("invites = %+v, want the answer to carry the link that was made", held.Invites)
	}
}

// And what the server refuses is handed back as it stands: this feature words nothing of its own,
// and a caller who may not administer the space is answered by the server and not guessed at here.
func TestARefusalTravels(t *testing.T) {
	uc, server := newUseCase()
	server.refuse = domain.Refuse(domain.CodeNotAllowed, domain.ErrNotAllowed, nil)

	_, err := uc.SetRole(context.Background(), spaceID, other, roleEdit)
	if domain.CodeOf(err) != domain.CodeNotAllowed {
		t.Errorf("refusal = %q, want the server's own code", domain.CodeOf(err))
	}
}

// The fake store: one space, with the account's id or without it.
type fakeStore struct {
	space domain.Workspace
}

func (f *fakeStore) Workspace(_ context.Context, id string) (domain.Workspace, error) {
	if f.space.ID != id {
		return domain.Workspace{}, domain.ErrNotFound
	}
	return f.space, nil
}

// The fake server: one space of one organisation, two people, and the links made for it.
type fakeServer struct {
	kind    domain.WorkspaceKind
	refuse  error
	invited []invitation
}

type invitation struct {
	org, space string
	asked      domain.Invitation
}

func (f *fakeServer) Spaces(
	context.Context, domain.Caller,
) (domain.AccountSpaces, error) {
	if f.refuse != nil {
		return domain.AccountSpaces{}, f.refuse
	}
	return domain.AccountSpaces{
		OwnOrgID: orgID,
		Spaces: []domain.RemoteSpace{{
			ID: account, OrgID: orgID, Kind: f.kind, OwnOrg: true,
		}},
	}, nil
}

func (f *fakeServer) Members(
	context.Context, domain.Caller, string,
) ([]domain.Member, error) {
	return []domain.Member{
		{UserID: me, Email: "me@acme.com", RoleID: "role-owner", RoleName: "Owner"},
		{UserID: other, Email: "ann@acme.com", RoleID: roleEdit, RoleName: "Editor"},
	}, nil
}

func (f *fakeServer) SetMember(
	context.Context, domain.Caller, string, string, string,
) (domain.Member, error) {
	return domain.Member{}, nil
}

func (f *fakeServer) ChangeRole(
	context.Context, domain.Caller, string, string, string,
) (domain.Member, error) {
	return domain.Member{}, nil
}

func (f *fakeServer) RemoveMember(context.Context, domain.Caller, string, string) error {
	return nil
}

func (f *fakeServer) Invites(
	context.Context, domain.Caller, string,
) ([]domain.Invite, error) {
	return []domain.Invite{
		{ID: "invite-1", WorkspaceID: account, Email: "ann@acme.com", RoleID: roleEdit,
			RoleName: "Editor", Status: "pending"},
		// Somebody else's space in the same organisation: the link is real and it is not this one's.
		{ID: "invite-2", WorkspaceID: "another-space", Email: "bob@acme.com", Status: "pending"},
	}, nil
}

func (f *fakeServer) Invite(
	_ context.Context,
	_ domain.Caller,
	orgID, workspaceID string,
	asked domain.Invitation,
) (domain.Invite, error) {
	if f.refuse != nil {
		return domain.Invite{}, f.refuse
	}
	f.invited = append(f.invited, invitation{org: orgID, space: workspaceID, asked: asked})
	return domain.Invite{ID: "invite-3", WorkspaceID: workspaceID, Email: asked.Email,
		Status: "pending"}, nil
}

func (f *fakeServer) RevokeInvite(
	context.Context, domain.Caller, string, string,
) (domain.Invite, error) {
	return domain.Invite{}, f.refuse
}

func (f *fakeServer) Roles(
	_ context.Context, _ domain.Caller, _, scope string,
) ([]domain.Role, error) {
	if scope != domain.ScopeWorkspace {
		return nil, domain.ErrNotFound
	}
	return []domain.Role{{ID: roleEdit, Name: "Editor", BuiltIn: true}}, nil
}

// The fake session: whoever this device is signed in as.
type fakeSession struct{}

func (fakeSession) Caller(context.Context) (domain.Caller, error) {
	return domain.Caller{Address: "schemo.local", Access: "token", SessionID: "s-1"}, nil
}

func (fakeSession) Me(context.Context) (domain.Account, error) {
	return domain.Account{UserID: me, Email: "me@acme.com"}, nil
}

// newUseCase is one space of a team's, in an account, with two people in it.
func newUseCase() (*UseCase, *fakeServer) {
	store := &fakeStore{space: domain.Workspace{
		ID: spaceID, Name: "Payments", Kind: domain.WorkspaceTeam, RemoteID: account,
	}}
	server := &fakeServer{kind: domain.WorkspaceTeam}
	return NewUseCase(store, server, fakeSession{}), server
}
