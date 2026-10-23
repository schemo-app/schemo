package workspace

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"schemo/internal/db"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// workspaces reads and writes the real store behind the service, for setting a test up and checking it.
type workspaces struct {
	t  *testing.T
	db *db.Store
}

func (s workspaces) list() []domain.Workspace {
	s.t.Helper()
	list, err := s.db.Workspaces(s.t.Context())
	if err != nil {
		s.t.Fatal(err)
	}
	return list
}

func (s workspaces) active() string {
	s.t.Helper()
	id, err := s.db.ActiveWorkspace(s.t.Context())
	if err != nil {
		s.t.Fatal(err)
	}
	return id
}

func (s workspaces) has(id string) bool {
	_, err := s.db.Workspace(s.t.Context(), id)
	return err == nil
}

func (s workspaces) Workspace(ctx context.Context, id string) (domain.Workspace, error) {
	return s.db.Workspace(ctx, id)
}

// lastInAccount is the workspace made most recently, given an account id so it counts as one the account has.
func (s workspaces) lastInAccount(remote string) string {
	s.t.Helper()
	list := s.list()
	w := list[len(list)-1]
	if err := s.db.Link(s.t.Context(), w.ID, domain.RemoteWorkspace{ID: remote, Kind: w.Kind}, ""); err != nil {
		s.t.Fatal(err)
	}
	return w.ID
}

func (s workspaces) setReopen(on bool) {
	s.t.Helper()
	prefs, err := s.db.Preferences(s.t.Context())
	if err != nil {
		s.t.Fatal(err)
	}
	prefs.ReopenWorkspace = on
	if err := s.db.SavePreferences(s.t.Context(), prefs); err != nil {
		s.t.Fatal(err)
	}
}

// recorder keeps what was published, so a test can say the windows were told and what about.
type recorder struct {
	topics   []string
	payloads []any
}

func (f *recorder) Publish(topic string, payload any) {
	f.topics = append(f.topics, topic)
	f.payloads = append(f.payloads, payload)
}

func newWorkspaces(t *testing.T) (*Workspaces, workspaces, *recorder) {
	store := workspaces{t, dbtest.New(t)}
	notifier := &recorder{}
	return NewWorkspaces(store.db, platform.NewIDGen(), notifier, &fakeRemote{},
		&fakeLinker{}, &fakeInviter{}), store, notifier
}

// fakeLinker is the account's side of making a workspace there and then, which records what it was
// asked and answers what a test tells it to. A workspace of somebody's own never asks.
type fakeLinker struct {
	linked []string
	refuse error
}

func (l *fakeLinker) LinkNow(_ context.Context, workspaceID string) error {
	l.linked = append(l.linked, workspaceID)
	return l.refuse
}

// fakeInviter is the team of a workspace, as far as making one is concerned.
type fakeInviter struct {
	sent   []string
	refuse error
}

func (i *fakeInviter) Invite(_ context.Context, workspaceID, email, roleID string) error {
	i.sent = append(i.sent, workspaceID+" "+email+" "+roleID)
	return i.refuse
}

// fakeRemote is the account's side of a workspace, which records what it was asked and answers what a
// test tells it to. A workspace with no account behind it never asks.
type fakeRemote struct {
	ended  []string
	kinds  []string
	left   []string
	refuse error
}

// Delete is the account's deletion. It takes the id and nothing else — the account answers about
// the workspace from its own row, so a name this device happens to draw cannot be refused over.
func (r *fakeRemote) Delete(_ context.Context, workspaceID string) error {
	r.ended = append(r.ended, workspaceID)
	return r.refuse
}

func (r *fakeRemote) ChangeKind(
	_ context.Context,
	workspaceID string,
	kind domain.WorkspaceKind,
) error {
	r.kinds = append(r.kinds, workspaceID+"="+string(kind))
	return r.refuse
}

// Leave is the account's other half of an exit: the place in the workspace, and not the workspace itself.
func (r *fakeRemote) Leave(_ context.Context, workspaceID string) error {
	r.left = append(r.left, workspaceID)
	return r.refuse
}

// made is the workspace a test just created, which is the one after the default the store starts
// with.
func made(t *testing.T, state domain.WorkspaceState) domain.Workspace {
	t.Helper()
	if len(state.Workspaces) < 2 {
		t.Fatalf("workspaces = %+v, want the default and the new one", state.Workspaces)
	}
	return state.Workspaces[len(state.Workspaces)-1]
}

// withRemote is the engine with the account's side of a workspace in hand: the tests about a workspace that
// exists in both places need to see what was said there.
func withRemote(t *testing.T) (*Workspaces, workspaces, *fakeRemote) {
	store := workspaces{t, dbtest.New(t)}
	remote := &fakeRemote{}
	return NewWorkspaces(store.db, platform.NewIDGen(), &recorder{}, remote,
		&fakeLinker{}, &fakeInviter{}), store, remote
}

func TestCreateAppendsToList(t *testing.T) {
	u, store, _ := newWorkspaces(t)
	ctx := context.Background()

	state, err := u.Create(ctx, WorkspaceInput{Name: "Internal catalog of API", Color: "purple"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(state.Workspaces) != 2 {
		t.Fatalf("workspaces = %d, want the default and the new one", len(state.Workspaces))
	}

	// The workspace the app is born with keeps the first row: the list is drawn in the order the workspaces
	// were made.
	if state.Workspaces[0].ID != domain.WorkspacePersonalID {
		t.Errorf("the first workspace = %+v, want the one the schema wrote", state.Workspaces[0])
	}

	added := made(t, state.WorkspaceState)
	if added.Name != "Internal catalog of API" || added.Color != "purple" {
		t.Errorf("the new workspace = %+v, want the name and the colour it was given", added)
	}
	// The form has no kind: a workspace made here is a personal one, and the row says so.
	if added.Kind != domain.WorkspacePersonal {
		t.Errorf("kind = %q, want a personal workspace", added.Kind)
	}
	if added.ID == "" || added.ID == domain.WorkspacePersonalID {
		t.Errorf("id = %q, want one of its own", added.ID)
	}
	// A new workspace is empty, so the window is left where it was rather than moved into it.
	if state.ActiveID != domain.WorkspacePersonalID {
		t.Errorf("activeId = %q, want the window left where it was", state.ActiveID)
	}
	if len(store.list()) != 2 {
		t.Errorf("the store holds %d workspace(s), want the two", len(store.list()))
	}
}

// The workspace the app is born with may not be deleted, renamed or turned into a team's, and it says
// so before the store is asked anything. The rule is about what this workspace is, not how many workspaces
// are left: a device's own workspace in the account is paired with it, and a rename or a deletion would
// take the pair away.
func TestTheWorkspaceTheAppIsBornWithIsPermanent(t *testing.T) {
	u, store, _ := newWorkspaces(t)
	ctx := context.Background()

	other, err := u.Create(ctx, WorkspaceInput{Name: "Work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// A second workspace, so that a count-based rule would have allowed this: what is refused is the
	// deletion itself and not the number that is left.
	if _, err := u.Delete(ctx, made(t, other.WorkspaceState).ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	name, team := "Mine", string(domain.WorkspaceTeam)
	for _, one := range []struct {
		what string
		do   func() error
	}{
		{"delete", func() error {
			_, err := u.Delete(ctx, domain.WorkspacePersonalID)
			return err
		}},
		{"rename", func() error {
			_, err := u.Update(ctx, domain.WorkspacePersonalID, WorkspacePatch{Name: &name})
			return err
		}},
		{"change kind", func() error {
			_, err := u.Update(ctx, domain.WorkspacePersonalID, WorkspacePatch{Kind: &team})
			return err
		}},
	} {
		err := one.do()
		if !errors.Is(err, domain.ErrNotAllowed) || domain.CodeOf(err) != domain.CodeWorkspacePermanent {
			t.Errorf("%s = %v, want it refused as permanent", one.what, err)
		}
	}
	// The refusals are the service's, and none of them reaches the store: the only deletion there is
	// the second workspace this test made and took away.
	if !store.has(domain.WorkspacePersonalID) {
		t.Errorf("the personal workspace is gone, want it never touched")
	}

	state, err := u.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Workspaces) != 1 || state.ActiveID != domain.WorkspacePersonalID {
		t.Errorf("after the refusals = %+v, want the one workspace and the pointer at it", state)
	}
	if state.Workspaces[0].Name != "" || state.Workspaces[0].Kind != domain.WorkspacePersonal {
		t.Errorf("the workspace = %+v, want its name and its kind untouched", state.Workspaces[0])
	}

	// Its colour is its own to change: nothing is found by a colour.
	colour := "purple"
	if _, err := u.Update(ctx, domain.WorkspacePersonalID, WorkspacePatch{Color: &colour}); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

// Deleting the newest workspace leaves the window on the one that has been there longest — the same
// rule the born-with row obeys, said from the other side.
func TestDeleteFallsBackToTheOldestLeft(t *testing.T) {
	u, store, notifier := newWorkspaces(t)
	ctx := context.Background()

	created, err := u.Create(ctx, WorkspaceInput{Name: "Team"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	target := made(t, created.WorkspaceState).ID
	if _, err := u.Switch(ctx, target); err != nil {
		t.Fatalf("Switch: %v", err)
	}

	state, err := u.Delete(ctx, target)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if state.ActiveID != domain.WorkspacePersonalID {
		t.Errorf("activeId = %q, want the pointer back on the default", state.ActiveID)
	}
	if len(state.Workspaces) != 1 {
		t.Errorf("workspaces = %+v, want the default alone", state.Workspaces)
	}
	if store.active() != domain.WorkspacePersonalID {
		t.Errorf("the stored pointer = %q, want the default written down", store.active())
	}

	// The window is told, and about the workspace it landed in rather than the one that went away.
	if len(notifier.payloads) == 0 {
		t.Fatal("nothing was published")
	}
	last := notifier.payloads[len(notifier.payloads)-1]
	changed, ok := last.(Changed)
	if !ok {
		t.Fatalf("the last event carried %T, want Changed", last)
	}
	if changed.Workspace.ID != domain.WorkspacePersonalID {
		t.Errorf("the last event named %q, want the default", changed.Workspace.ID)
	}
	if topic := notifier.topics[len(notifier.topics)-1]; topic != TopicChanged {
		t.Errorf("the last topic = %q, want %q", topic, TopicChanged)
	}
}

// Where a launch starts: the workspace the app was left in, unless the user has turned that off, in
// which case the pointer is moved to the oldest one. It is written rather than answered, so
// nothing below this launch has to know the preference exists.
func TestStartPointsAtTheOldestWhenReopeningIsOff(t *testing.T) {
	u, store, _ := newWorkspaces(t)
	ctx := context.Background()

	created, err := u.Create(ctx, WorkspaceInput{Name: "Team"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	second := made(t, created.WorkspaceState).ID
	if _, err := u.Switch(ctx, second); err != nil {
		t.Fatalf("Switch: %v", err)
	}

	if err := u.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if store.active() != second {
		t.Errorf("with reopening on the pointer = %q, want %q — the workspace last left in",
			store.active(), second)
	}

	store.setReopen(false)
	if err := u.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if store.active() != domain.WorkspacePersonalID {
		t.Errorf("with reopening off the pointer = %q, want the oldest workspace", store.active())
	}

	// Turning the preference back on does not undo the move: it says what the *next* launch does, and
	// until the user switches somewhere the app opens where this one left it.
	store.setReopen(true)
	if err := u.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if store.active() != domain.WorkspacePersonalID {
		t.Errorf("the pointer = %q, want it left where the last launch put it", store.active())
	}
}

func TestSwitchStoresAndPublishes(t *testing.T) {
	u, store, notifier := newWorkspaces(t)
	ctx := context.Background()

	created, err := u.Create(ctx, WorkspaceInput{Name: "Team"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	target := made(t, created.WorkspaceState).ID

	state, err := u.Switch(ctx, target)
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if store.active() != target {
		t.Errorf("the stored pointer = %q, want %q", store.active(), target)
	}
	if state.ActiveID != target {
		t.Errorf("activeId = %q, want %q", state.ActiveID, target)
	}

	if len(notifier.topics) != 1 || notifier.topics[0] != TopicChanged {
		t.Fatalf("topics = %v, want the one %q", notifier.topics, TopicChanged)
	}
	changed, ok := notifier.payloads[0].(Changed)
	if !ok {
		t.Fatalf("the event carried %T, want Changed", notifier.payloads[0])
	}
	if changed.Workspace.ID != target {
		t.Errorf("the event named %q, want %q", changed.Workspace.ID, target)
	}

	// Switching to a row that is not there changes nothing and says so.
	if _, err := u.Switch(ctx, "not-found"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("switching to a workspace that is not there = %v, want ErrNotFound", err)
	}
	if store.active() != target {
		t.Errorf("the pointer moved to %q on a failed switch, want it left at %q", store.active(), target)
	}
}

// A workspace is made with the kind the form chose ("Personal" or "Team") and one
// nobody said anything about is somebody's own.
func TestAWorkspaceIsMadeWithTheKindItWasChosenWith(t *testing.T) {
	u, _, _ := newWorkspaces(t)
	ctx := context.Background()

	state, err := u.Create(ctx, WorkspaceInput{Name: "Development team", Kind: "team"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := made(t, state.WorkspaceState).Kind; got != domain.WorkspaceTeam {
		t.Errorf("kind = %q, want the one that was chosen", got)
	}

	state, err = u.Create(ctx, WorkspaceInput{Name: "Mine"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := made(t, state.WorkspaceState).Kind; got != domain.WorkspacePersonal {
		t.Errorf("kind = %q, want a workspace nobody said anything about to be somebody's own", got)
	}
}

// And a build sending a word this one does not have is refused rather than given one of the two: a
// workspace made with the wrong kind is a workspace somebody has to notice and rename.
func TestAWorkspaceIsNotMadeWithAWordThisBuildDoesNotHave(t *testing.T) {
	u, store, _ := newWorkspaces(t)

	_, err := u.Create(context.Background(), WorkspaceInput{Name: "Tests", Kind: "shared"})
	if domain.CodeOf(err) != domain.CodeUnknownField {
		t.Errorf("refusal = %q, want the kind named as what is wrong", domain.CodeOf(err))
	}
	if len(store.list()) != 1 {
		t.Errorf("workspaces = %+v, want nothing made", store.list())
	}
}

// What a workspace is for is not fixed at the moment it is made: the same patch that names it changes
// it, and the workspace keeps the word everywhere it is. The one exception is the workspace the app is born
// with, whose kind is as fixed as its name — see the test above.
func TestTheKindOfAWorkspaceCanBeChanged(t *testing.T) {
	u, _, _ := newWorkspaces(t)
	ctx := context.Background()

	made, err := u.Create(ctx, WorkspaceInput{Name: "Work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	target := made.Workspaces[len(made.Workspaces)-1].ID

	team := string(domain.WorkspaceTeam)
	state, err := u.Update(ctx, target, WorkspacePatch{Kind: &team})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := held(state, target).Kind; got != domain.WorkspaceTeam {
		t.Errorf("kind = %q, want it changed", got)
	}
	if got := held(state, target).Color; got != "" {
		t.Errorf("color = %q, want the field the patch left out left as it was", got)
	}

	unknown := "shared"
	_, err = u.Update(ctx, target, WorkspacePatch{Kind: &unknown})
	if domain.CodeOf(err) != domain.CodeUnknownField {
		t.Errorf("refusal = %q, want the kind refused", domain.CodeOf(err))
	}
}

// held is one workspace out of a state, which is what a test about one row asks for.
func held(state domain.WorkspaceState, id string) domain.Workspace {
	for _, one := range state.Workspaces {
		if one.ID == id {
			return one
		}
	}
	return domain.Workspace{}
}

func TestUpdateRenamesAndRecolors(t *testing.T) {
	u, _, notifier := newWorkspaces(t)
	ctx := context.Background()

	created, err := u.Create(ctx, WorkspaceInput{Name: "Work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	target := made(t, created.WorkspaceState).ID
	// The workspace being renamed is the one on screen, so this is also the case where the windows have
	// to hear about it.
	if _, err := u.Switch(ctx, target); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	notifier.topics = nil

	name := "Payments"
	state, err := u.Update(ctx, target, WorkspacePatch{Name: &name})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := held(state, target).Name; got != name {
		t.Errorf("name = %q, want %q", got, name)
	}
	// A field the patch leaves out is left as it is, which is what makes the patch partial.
	if got := held(state, target).Color; got != "" {
		t.Errorf("color = %q, want it left as it was", got)
	}

	color := "purple"
	state, err = u.Update(ctx, target, WorkspacePatch{Color: &color})
	if err != nil {
		t.Fatalf("Update (colour): %v", err)
	}
	got := held(state, target)
	if got.Color != "purple" || got.Name != name {
		t.Errorf("after the second patch = %+v, want both changes kept", got)
	}
	if got.UpdatedAt == 0 {
		t.Error("updatedAt was not written")
	}

	if len(notifier.topics) != 2 || notifier.topics[0] != TopicChanged {
		t.Errorf("topics = %v, want one %q per change of the workspace on screen",
			notifier.topics, TopicChanged)
	}

	if _, err := u.Update(ctx, "not-found", WorkspacePatch{Name: &name}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("updating a workspace that is not there = %v, want ErrNotFound", err)
	}
}

func TestNameValidation(t *testing.T) {
	u, store, _ := newWorkspaces(t)
	ctx := context.Background()

	for _, bad := range []string{"", "   ", strings.Repeat("x", maxWorkspaceName+1)} {
		if _, err := u.Create(ctx, WorkspaceInput{Name: bad}); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("Create(%q) = %v, want domain.ErrNotAllowed", bad, err)
		}
	}
	// The two refusals are told apart by their code, because the window words them differently.
	if _, err := u.Create(ctx, WorkspaceInput{Name: ""}); domain.CodeOf(err) != domain.CodeNameEmpty {
		t.Errorf("an empty name = %q, want %q", domain.CodeOf(err), domain.CodeNameEmpty)
	}
	tooLong := strings.Repeat("x", maxWorkspaceName+1)
	if _, err := u.Create(ctx,
		WorkspaceInput{Name: tooLong}); domain.CodeOf(err) != domain.CodeNameTooLong {
		t.Errorf("a name past the limit = %q, want %q", domain.CodeOf(err), domain.CodeNameTooLong)
	}
	// Nothing was made by any of the refusals.
	if len(store.list()) != 1 {
		t.Errorf("the store holds %d workspace(s), want the default alone", len(store.list()))
	}

	// The limit counts characters rather than bytes, including non-ASCII letters.
	if _, err := u.Create(ctx, WorkspaceInput{Name: strings.Repeat("é", maxWorkspaceName)}); err != nil {
		t.Errorf("a name of exactly %d characters = %v, want it accepted", maxWorkspaceName, err)
	}
	// A name is trimmed rather than refused for the space around it.
	state, err := u.Create(ctx, WorkspaceInput{Name: "  Team  "})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := made(t, state.WorkspaceState).Name; got != "Team" {
		t.Errorf("name = %q, want it trimmed", got)
	}

	// A patch is held to the same rule as a create. The workspace being renamed is the one the last
	// create made, because the workspace the app is born with may not be renamed at all.
	target := made(t, state.WorkspaceState).ID
	empty := "   "
	if _, err := u.Update(ctx, target,
		WorkspacePatch{Name: &empty}); domain.CodeOf(err) != domain.CodeNameEmpty {
		t.Errorf("renaming to an empty name = %q, want %q", domain.CodeOf(err), domain.CodeNameEmpty)
	}
}

// A workspace the app did not come with is one somebody made, and it goes like any other.
func TestDeleteTakesAWorkspaceSomebodyMade(t *testing.T) {
	u, store, _ := newWorkspaces(t)
	ctx := context.Background()

	created, err := u.Create(ctx, WorkspaceInput{Name: "Work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	going := made(t, created.WorkspaceState).ID

	state, err := u.Delete(ctx, going)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(state.Workspaces) != 1 || state.Workspaces[0].ID != domain.WorkspacePersonalID {
		t.Errorf("workspaces = %+v, want the one that is left", state.Workspaces)
	}
	if store.has(going) {
		t.Errorf("the workspace %q is still stored, want it gone", going)
	}
}

// A workspace that is also in the account is ended there first: a copy deleted only here would be
// handed back by the next round, and what was in it would be gone from the place it was deleted.
func TestAWorkspaceTheAccountHasIsEndedThereToo(t *testing.T) {
	u, store, remote := withRemote(t)
	ctx := context.Background()

	if _, err := u.Create(ctx, WorkspaceInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := store.lastInAccount("00000000-0000-0000-0000-0000000000aa")

	if _, err := u.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The account's own id travels, and nothing else: the workspace is named there, and a name sent from
	// here would be the one this device happens to draw.
	want := "00000000-0000-0000-0000-0000000000aa"
	if len(remote.ended) != 1 || remote.ended[0] != want {
		t.Errorf("ended = %v, want the account's own id %q", remote.ended, want)
	}
}

// Leaving is the exit of somebody who is only a member: the place in the account goes, and the
// workspace goes from here with it. A member who may not end a workspace for everybody would otherwise
// have no way out of one at all.
func TestLeaveStepsOutOfAWorkspaceSomebodyElseHas(t *testing.T) {
	u, store, remote := withRemote(t)
	ctx := context.Background()

	if _, err := u.Create(ctx, WorkspaceInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := store.lastInAccount("00000000-0000-0000-0000-0000000000aa")

	state, err := u.Leave(ctx, id)
	if err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if got := "00000000-0000-0000-0000-0000000000aa"; len(remote.left) != 1 || remote.left[0] != got {
		t.Errorf("left = %v, want the account's own id %q", remote.left, got)
	}
	if _, err := store.Workspace(ctx, id); err == nil {
		t.Error("the workspace stayed here although the person stepped out of it")
	}
	if len(remote.ended) != 0 {
		t.Errorf("ended = %v, want the workspace itself left standing", remote.ended)
	}
	if len(state.Workspaces) == 0 {
		t.Error("no state was handed back to the window")
	}
}

// The account is asked first, and its refusal is the whole answer: a workspace dropped here while the
// place in it still stood would be made again by the next round, which finds the person in it.
func TestAWorkspaceTheAccountWillNotLetThisPersonOutOfStaysHere(t *testing.T) {
	u, store, remote := withRemote(t)
	ctx := context.Background()

	if _, err := u.Create(ctx, WorkspaceInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := store.lastInAccount("00000000-0000-0000-0000-0000000000aa")
	remote.refuse = domain.Refuse(domain.CodeNotAllowed, nil, nil)

	if _, err := u.Leave(ctx, id); err == nil {
		t.Fatal("a refusal from the account was swallowed")
	}
	if _, err := store.Workspace(ctx, id); err != nil {
		t.Error("the workspace went from here although the person is still in it")
	}
}

// A place in a workspace the account no longer has is a place already left: the point of leaving is
// that the workspace is not the person's, and a refusal for want of it would keep a dead row.
func TestAWorkspaceTheAccountNoLongerHasIsLeftAllTheSame(t *testing.T) {
	u, store, remote := withRemote(t)
	ctx := context.Background()

	if _, err := u.Create(ctx, WorkspaceInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := store.lastInAccount("00000000-0000-0000-0000-0000000000aa")
	remote.refuse = domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)

	if _, err := u.Leave(ctx, id); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if _, err := store.Workspace(ctx, id); err == nil {
		t.Error("the row stayed although the account has no such workspace")
	}
}

// The workspace the app is born with is the device's own in the account: there is no one else's place
// in it to step out of, and it is refused as deleting it is.
func TestTheWorkspaceTheAppIsBornWithIsNotLeft(t *testing.T) {
	u, _, remote := withRemote(t)

	if _, err := u.Leave(context.Background(), domain.WorkspacePersonalID); err == nil {
		t.Fatal("leaving the permanent workspace was allowed")
	}
	if len(remote.left) != 0 {
		t.Errorf("left = %v, want the account asked nothing", remote.left)
	}
}

// A workspace this device holds alone has no place in an account to step out of: there is nobody to
// leave, and the call refuses it as a workspace that is not there rather than dropping it here.
func TestAWorkspaceThatIsOnlyHereIsNotLeft(t *testing.T) {
	u, store, remote := withRemote(t)
	ctx := context.Background()

	created, err := u.Create(ctx, WorkspaceInput{Name: "Payments"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := made(t, created.WorkspaceState).ID

	if _, err := u.Leave(ctx, id); err == nil {
		t.Fatal("leaving a workspace nobody else has was allowed")
	}
	if len(remote.left) != 0 {
		t.Errorf("left = %v, want the account asked nothing", remote.left)
	}
	if _, err := store.Workspace(ctx, id); err != nil {
		t.Error("the workspace went although nobody was left out of anything")
	}
}

func TestAWorkspaceTheAccountRefusesToEndStaysHere(t *testing.T) {
	u, store, remote := withRemote(t)
	ctx := context.Background()

	if _, err := u.Create(ctx, WorkspaceInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := store.lastInAccount("00000000-0000-0000-0000-0000000000aa")
	remote.refuse = domain.Refuse(domain.CodeNotAllowed, nil, nil)

	if _, err := u.Delete(ctx, id); err == nil {
		t.Fatal("a refusal from the account was swallowed")
	}
	if _, err := store.Workspace(ctx, id); err != nil {
		t.Error("the workspace went from here although it is still in the account")
	}
}

// A workspace the account no longer has is nothing to report: the point of the call is that it is gone,
// and a device that has just been told so and asks anyway is asking about a row that is not there.
func TestAWorkspaceTheAccountNoLongerHasIsStillEndedHere(t *testing.T) {
	u, store, remote := withRemote(t)
	ctx := context.Background()

	if _, err := u.Create(ctx, WorkspaceInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := store.lastInAccount("00000000-0000-0000-0000-0000000000aa")
	remote.refuse = domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)

	if _, err := u.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Workspace(ctx, id); err == nil {
		t.Error("a workspace the account does not have was left here")
	}
}

// A workspace that is only here has nothing said about it anywhere: this device is the only place it
// exists, and a call to an account nobody signed in to would be one made for nothing.
func TestAWorkspaceThatIsOnlyHereIsNotSaidAnywhere(t *testing.T) {
	u, _, remote := withRemote(t)
	ctx := context.Background()

	created, err := u.Create(ctx, WorkspaceInput{Name: "Payments"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := u.Delete(ctx, made(t, created.WorkspaceState).ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(remote.ended) != 0 {
		t.Errorf("ended = %v, want nothing said about a local workspace", remote.ended)
	}
}

// What each workspace holds travels with the list: the switcher draws the counts in the same rows as
// the names, and a state without them would be a second call at every open.
func TestSnapshotCarriesTheCounts(t *testing.T) {
	u, store, _ := newWorkspaces(t)
	ctx := context.Background()
	for _, name := range []string{"Local", "Staging"} {
		env := domain.Environment{ID: name, Name: name, Vars: []domain.Variable{}}
		if err := store.db.SaveEnvironment(ctx, domain.WorkspacePersonalID, env); err != nil {
			t.Fatal(err)
		}
	}

	state, err := u.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	got := state.Counts[domain.WorkspacePersonalID]
	if got.Environments != 2 {
		t.Errorf("counts = %+v, want the two environments", got)
	}
}

// What a workspace is for decides who may be in it, so a workspace that is in the account is changed there
// and not here: whether anybody else is in it is the server's answer, and it is the server that
// refuses the change that would make a shared workspace somebody's own.
func TestChangingWhatAWorkspaceIsForIsAskedOfTheAccount(t *testing.T) {
	u, store, remote := withRemote(t)
	ctx := context.Background()

	if _, err := u.Create(ctx, WorkspaceInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := store.lastInAccount("00000000-0000-0000-0000-0000000000aa")

	kind := string(domain.WorkspaceTeam)
	if _, err := u.Update(ctx, id, WorkspacePatch{Kind: &kind}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	want := "00000000-0000-0000-0000-0000000000aa=team"
	if len(remote.kinds) != 1 || remote.kinds[0] != want {
		t.Errorf("asked = %v, want %q", remote.kinds, want)
	}
	if held, err := store.Workspace(ctx, id); err != nil {
		t.Fatalf("Workspace: %v", err)
	} else if held.Kind != domain.WorkspaceTeam {
		t.Errorf("kind = %q, want the workspace made a team's here as well", held.Kind)
	}
}

// And a refusal from the account is left where it is: a workspace the server will not turn into
// somebody's own stays a team's on this device too, rather than being drawn as something the person
// will not find when they look from anywhere else.
func TestAWorkspaceTheAccountRefusesToChangeStaysAsItWas(t *testing.T) {
	u, store, remote := withRemote(t)
	ctx := context.Background()

	if _, err := u.Create(ctx, WorkspaceInput{Name: "Payments", Kind: "team"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := store.lastInAccount("00000000-0000-0000-0000-0000000000aa")
	remote.refuse = domain.Refuse(domain.CodeWorkspacePersonal, domain.ErrNotAllowed, nil)

	personal := string(domain.WorkspacePersonal)
	if _, err := u.Update(ctx, id, WorkspacePatch{Kind: &personal}); domain.CodeOf(err) !=
		domain.CodeWorkspacePersonal {
		t.Fatalf("Update = %q, want the account's refusal", domain.CodeOf(err))
	}

	held, err := store.Workspace(ctx, id)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	if held.Kind != domain.WorkspaceTeam {
		t.Errorf("kind = %q, want the workspace left as the account has it", held.Kind)
	}
}

// A name another workspace wears is refused when making one and when renaming one, and a workspace keeping
// its own name is not a collision with itself.
func TestAWorkspaceTakesNoNameAnotherOneWears(t *testing.T) {
	u, store, _ := newWorkspaces(t)
	ctx := context.Background()

	if _, err := u.Create(ctx, WorkspaceInput{Name: "Alpha"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := u.Create(ctx, WorkspaceInput{Name: "Alpha"})
	if !errors.Is(err, domain.ErrConflict) || domain.CodeOf(err) != domain.CodeNameTaken {
		t.Errorf("a second %q = %v, want the refusal %q", "Alpha", err, domain.CodeNameTaken)
	}
	if len(store.list()) != 2 {
		t.Errorf("the store holds %d workspace(s), want the default and Alpha", len(store.list()))
	}

	state, err := u.Create(ctx, WorkspaceInput{Name: "Beta"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	beta := made(t, state.WorkspaceState).ID
	taken := "Alpha"
	_, err = u.Update(ctx, beta, WorkspacePatch{Name: &taken})
	if domain.CodeOf(err) != domain.CodeNameTaken {
		t.Errorf("renaming to %q = %v, want the refusal %q", taken, err, domain.CodeNameTaken)
	}
	own := "Beta"
	if _, err := u.Update(ctx, beta, WorkspacePatch{Name: &own}); err != nil {
		t.Errorf("renaming to its own name = %v, want it accepted", err)
	}
}

// The strip goes down whole and comes back as it was, and the drafts of tabs that are gone are
// deleted when it is read: a run that ended before the window discarded them leaves nothing behind.
func TestTheTabsComeBackAndOrphanDraftsGo(t *testing.T) {
	u, store, _ := newWorkspaces(t)
	ctx := t.Context()
	for _, id := range []domain.DraftID{"tab:kept", "tab:orphan", "node-1"} {
		if err := store.db.SaveDraft(ctx, domain.WorkspacePersonalID, domain.Draft{ID: id, Method: "GET"}); err != nil {
			t.Fatal(err)
		}
	}
	strip := domain.Strip{Workspace: domain.WorkspacePersonalID, Active: "b", Tabs: []domain.Tab{
		{ID: "a", Kind: domain.TabKindRequest, DraftID: "tab:kept", Pinned: true},
		{ID: "b", Kind: domain.TabKindHistory},
	}}
	if err := u.SaveTabs(ctx, strip); err != nil {
		t.Fatalf("SaveTabs: %v", err)
	}

	got, err := u.Tabs(ctx)
	if err != nil {
		t.Fatalf("Tabs: %v", err)
	}
	if got.Active != "b" || len(got.Tabs) != 2 || !got.Tabs[0].Pinned || got.Tabs[1].Kind != domain.TabKindHistory {
		t.Errorf("tabs = %+v, want the strip as it was saved", got)
	}
	left, err := store.db.DraftIDs(ctx, domain.WorkspacePersonalID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 2 || slices.Contains(left, "tab:orphan") {
		t.Errorf("drafts = %v, want the kept tab's and the node's", left)
	}
}

// A strip names its workspace, and a tab of a kind this build cannot draw is refused.
func TestTabsAreSavedIntoTheWorkspaceTheyName(t *testing.T) {
	u, _, _ := newWorkspaces(t)
	ctx := t.Context()
	if err := u.SaveTabs(ctx, domain.Strip{Workspace: "elsewhere"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("SaveTabs(elsewhere) = %v, want not found", err)
	}
	bad := domain.Strip{Workspace: domain.WorkspacePersonalID, Tabs: []domain.Tab{{ID: "x", Kind: "old"}}}
	if err := u.SaveTabs(ctx, bad); domain.CodeOf(err) != domain.CodeUnknownField {
		t.Errorf("SaveTabs(old kind) = %v, want refused", err)
	}
	got, err := u.Tabs(ctx)
	if err != nil || len(got.Tabs) != 0 || got.Workspace != domain.WorkspacePersonalID {
		t.Errorf("Tabs = %+v, %v, want an empty strip of the personal workspace", got, err)
	}
}
