package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// fakeStore is the switcher's two tables without a database: the workspaces in their order, and the
// pointer that names the one on screen. It keeps the rules the SQL keeps — the space the schema
// writes exists from the first launch, and a pointer at a row that is gone answers with the oldest
// one left — so the tests below are about the use case.
type fakeStore struct {
	workspaces []domain.Workspace
	active     string
	// reopen is the preference the store answers for "reopen the last workspace". The store the app
	// runs on reads it from the settings table; the zero value here would be a fresh installation
	// that never stored it, which is on — so the fake is built with it on and a test turns it off.
	reopen bool
	// deleted is every id the use case asked to remove, so a test can say a refusal never got here.
	deleted []string
	// counts is what the store says each space holds, for the tests about the reading travelling.
	counts map[string]domain.WorkspaceCounts
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		reopen: true,
		workspaces: []domain.Workspace{
			domain.NewWorkspace(domain.WorkspacePersonalID, "", domain.WorkspacePersonal, "blue", 0),
		},
	}
}

func (f *fakeStore) has(id string) bool {
	for _, w := range f.workspaces {
		if w.ID == id {
			return true
		}
	}
	return false
}

func (f *fakeStore) Workspaces(context.Context) ([]domain.Workspace, error) {
	out := make([]domain.Workspace, len(f.workspaces))
	copy(out, f.workspaces)
	return out, nil
}

func (f *fakeStore) Workspace(_ context.Context, id string) (domain.Workspace, error) {
	for _, w := range f.workspaces {
		if w.ID == id {
			return w, nil
		}
	}
	return domain.Workspace{}, domain.ErrNotFound
}

func (f *fakeStore) SaveWorkspace(_ context.Context, w domain.Workspace) error {
	for i := range f.workspaces {
		if f.workspaces[i].ID == w.ID {
			f.workspaces[i] = w
			return nil
		}
	}
	f.workspaces = append(f.workspaces, w)
	return nil
}

func (f *fakeStore) DeleteWorkspace(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	kept := f.workspaces[:0]
	for _, w := range f.workspaces {
		if w.ID != id {
			kept = append(kept, w)
		}
	}
	f.workspaces = kept
	return nil
}

// ActiveWorkspace answers the way the store does: the stored pointer when it names a row that is
// still there, and the oldest row left otherwise.
func (f *fakeStore) ActiveWorkspace(context.Context) (string, error) {
	if f.active != "" && f.has(f.active) {
		return f.active, nil
	}
	if len(f.workspaces) == 0 {
		return "", domain.ErrNotFound
	}
	return f.workspaces[0].ID, nil
}

// Counts is the reading the window draws in the same rows as the names. What a workspace holds is
// not this feature's business — the SQL counts it — so a test says what it should answer.
func (f *fakeStore) Counts(context.Context) (map[string]domain.WorkspaceCounts, error) {
	return f.counts, nil
}

func (f *fakeStore) SetActiveWorkspace(_ context.Context, id string) error {
	f.active = id
	return nil
}

func (f *fakeStore) ReopenLast(context.Context) (bool, error) {
	return f.reopen, nil
}

// fakeNotifier keeps what was published, so a test can say the windows were told and what about.
type fakeNotifier struct {
	topics   []string
	payloads []any
}

func (f *fakeNotifier) Publish(topic string, payload any) {
	f.topics = append(f.topics, topic)
	f.payloads = append(f.payloads, payload)
}

func newUseCase() (*UseCase, *fakeStore, *fakeNotifier) {
	store := newFakeStore()
	notifier := &fakeNotifier{}
	return NewUseCase(store, platform.NewIDGen(), notifier, &fakeRemote{},
		&fakeLinker{}, &fakeInviter{}), store, notifier
}

// fakeLinker is the account's side of making a space there and then, which records what it was
// asked and answers what a test tells it to. A space of somebody's own never asks.
type fakeLinker struct {
	linked []string
	refuse error
}

func (l *fakeLinker) LinkNow(_ context.Context, workspaceID string) error {
	l.linked = append(l.linked, workspaceID)
	return l.refuse
}

// fakeInviter is the team of a space, as far as making one is concerned.
type fakeInviter struct {
	sent   []string
	refuse error
}

func (i *fakeInviter) Invite(_ context.Context, workspaceID, email, roleID string) error {
	i.sent = append(i.sent, workspaceID+" "+email+" "+roleID)
	return i.refuse
}

// fakeRemote is the account's side of a space, which records what it was asked and answers what a
// test tells it to. A space with no account behind it never asks.
type fakeRemote struct {
	ended  []string
	kinds  []string
	left   []string
	refuse error
}

// Delete is the account's deletion. It takes the id and nothing else — the account answers about
// the space from its own row, so a name this device happens to draw cannot be refused over.
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

// Leave is the account's other half of an exit: the place in the space, and not the space itself.
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

// withRemote is the engine with the account's side of a space in hand: the tests about a space that
// exists in both places need to see what was said there.
func withRemote() (*UseCase, *fakeStore, *fakeRemote) {
	store := newFakeStore()
	remote := &fakeRemote{}
	return NewUseCase(store, platform.NewIDGen(), &fakeNotifier{}, remote,
		&fakeLinker{}, &fakeInviter{}), store, remote
}

func TestCreateAppendsToList(t *testing.T) {
	u, store, _ := newUseCase()
	ctx := context.Background()

	state, err := u.Create(ctx, CreateInput{Name: "Internal catalog of API", Color: "purple"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(state.Workspaces) != 2 {
		t.Fatalf("workspaces = %d, want the default and the new one", len(state.Workspaces))
	}

	// The space the app is born with keeps the first row: the list is drawn in the order the spaces
	// were made.
	if state.Workspaces[0].ID != domain.WorkspacePersonalID {
		t.Errorf("the first workspace = %+v, want the one the schema wrote", state.Workspaces[0])
	}

	added := made(t, state.WorkspaceState)
	if added.Name != "Internal catalog of API" || added.Color != "purple" {
		t.Errorf("the new workspace = %+v, want the name and the colour it was given", added)
	}
	// The form has no kind: a space made here is a personal one, and the row says so.
	if added.Kind != domain.WorkspacePersonal {
		t.Errorf("kind = %q, want a personal space", added.Kind)
	}
	if added.ID == "" || added.ID == domain.WorkspacePersonalID {
		t.Errorf("id = %q, want one of its own", added.ID)
	}
	// A new space is empty, so the window is left where it was rather than moved into it.
	if state.ActiveID != domain.WorkspacePersonalID {
		t.Errorf("activeId = %q, want the window left where it was", state.ActiveID)
	}
	if len(store.workspaces) != 2 {
		t.Errorf("the store holds %d workspace(s), want the two", len(store.workspaces))
	}
}

// The space the app is born with may not be deleted, renamed or turned into a team's, and it says
// so before the store is asked anything. The rule used to be about the number of spaces — the last
// one could not go, whichever it was — and it is about what this space is now: a device's own space
// in the account is paired with it, and a rename or a deletion would take the pair away.
func TestTheWorkspaceTheAppIsBornWithIsPermanent(t *testing.T) {
	u, store, _ := newUseCase()
	ctx := context.Background()

	other, err := u.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// A second space, so that the old rule would have allowed this: what is refused is the deletion
	// itself and not the number that is left.
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
			_, err := u.Update(ctx, domain.WorkspacePersonalID, Patch{Name: &name})
			return err
		}},
		{"change kind", func() error {
			_, err := u.Update(ctx, domain.WorkspacePersonalID, Patch{Kind: &team})
			return err
		}},
	} {
		err := one.do()
		if !errors.Is(err, domain.ErrNotAllowed) || domain.CodeOf(err) != domain.CodeWorkspacePermanent {
			t.Errorf("%s = %v, want it refused as permanent", one.what, err)
		}
	}
	// The refusals are the use case's, and none of them reaches the store: the only deletion there is
	// the second space this test made and took away.
	for _, id := range store.deleted {
		if id == domain.WorkspacePersonalID {
			t.Errorf("the store was asked to delete the personal space, want it never touched")
		}
	}

	state, err := u.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Workspaces) != 1 || state.ActiveID != domain.WorkspacePersonalID {
		t.Errorf("after the refusals = %+v, want the one space and the pointer at it", state)
	}
	if state.Workspaces[0].Name != "" || state.Workspaces[0].Kind != domain.WorkspacePersonal {
		t.Errorf("the space = %+v, want its name and its kind untouched", state.Workspaces[0])
	}

	// Its colour is its own to change: nothing is found by a colour.
	colour := "purple"
	if _, err := u.Update(ctx, domain.WorkspacePersonalID, Patch{Color: &colour}); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

// Deleting the newest space leaves the window on the one that has been there longest — the same
// rule the born-with row obeys, said from the other side.
func TestDeleteFallsBackToTheOldestLeft(t *testing.T) {
	u, store, notifier := newUseCase()
	ctx := context.Background()

	created, err := u.Create(ctx, CreateInput{Name: "Team"})
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
	if store.active != domain.WorkspacePersonalID {
		t.Errorf("the stored pointer = %q, want the default written down", store.active)
	}

	// The window is told, and about the space it landed in rather than the one that went away.
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

// Where a launch starts: the space the app was left in, unless the user has turned that off, in
// which case the pointer is moved to the oldest one. It is written rather than answered, so
// nothing below this launch has to know the preference exists.
func TestStartPointsAtTheOldestWhenReopeningIsOff(t *testing.T) {
	u, store, _ := newUseCase()
	ctx := context.Background()

	created, err := u.Create(ctx, CreateInput{Name: "Team"})
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
	if store.active != second {
		t.Errorf("with reopening on the pointer = %q, want %q — the space last left in",
			store.active, second)
	}

	store.reopen = false
	if err := u.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if store.active != domain.WorkspacePersonalID {
		t.Errorf("with reopening off the pointer = %q, want the oldest space", store.active)
	}

	// Turning the preference back on does not undo the move: it says what the *next* launch does, and
	// until the user switches somewhere the app opens where this one left it.
	store.reopen = true
	if err := u.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if store.active != domain.WorkspacePersonalID {
		t.Errorf("the pointer = %q, want it left where the last launch put it", store.active)
	}
}

func TestSwitchStoresAndPublishes(t *testing.T) {
	u, store, notifier := newUseCase()
	ctx := context.Background()

	created, err := u.Create(ctx, CreateInput{Name: "Team"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	target := made(t, created.WorkspaceState).ID

	state, err := u.Switch(ctx, target)
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if store.active != target {
		t.Errorf("the stored pointer = %q, want %q", store.active, target)
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
		t.Errorf("switching to a space that is not there = %v, want ErrNotFound", err)
	}
	if store.active != target {
		t.Errorf("the pointer moved to %q on a failed switch, want it left at %q", store.active, target)
	}
}

// A space is made with the kind the form chose — the design's "Personal" or "Team" — and one
// nobody said anything about is somebody's own.
func TestASpaceIsMadeWithTheKindItWasChosenWith(t *testing.T) {
	u, _, _ := newUseCase()
	ctx := context.Background()

	state, err := u.Create(ctx, CreateInput{Name: "Development team", Kind: "team"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := made(t, state.WorkspaceState).Kind; got != domain.WorkspaceTeam {
		t.Errorf("kind = %q, want the one that was chosen", got)
	}

	state, err = u.Create(ctx, CreateInput{Name: "Mine"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := made(t, state.WorkspaceState).Kind; got != domain.WorkspacePersonal {
		t.Errorf("kind = %q, want a space nobody said anything about to be somebody's own", got)
	}
}

// And a build sending a word this one does not have is refused rather than given one of the two: a
// space made with the wrong kind is a space somebody has to notice and rename.
func TestASpaceIsNotMadeWithAWordThisBuildDoesNotHave(t *testing.T) {
	u, store, _ := newUseCase()

	_, err := u.Create(context.Background(), CreateInput{Name: "Tests", Kind: "shared"})
	if domain.CodeOf(err) != domain.CodeUnknownField {
		t.Errorf("refusal = %q, want the kind named as what is wrong", domain.CodeOf(err))
	}
	if len(store.workspaces) != 1 {
		t.Errorf("workspaces = %+v, want nothing made", store.workspaces)
	}
}

// What a space is for is not fixed at the moment it is made: the same patch that names it changes
// it, and the space keeps the word everywhere it is. The one exception is the space the app is born
// with, whose kind is as fixed as its name — see the test above.
func TestTheKindOfASpaceCanBeChanged(t *testing.T) {
	u, _, _ := newUseCase()
	ctx := context.Background()

	made, err := u.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	target := made.Workspaces[len(made.Workspaces)-1].ID

	team := string(domain.WorkspaceTeam)
	state, err := u.Update(ctx, target, Patch{Kind: &team})
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
	_, err = u.Update(ctx, target, Patch{Kind: &unknown})
	if domain.CodeOf(err) != domain.CodeUnknownField {
		t.Errorf("refusal = %q, want the kind refused", domain.CodeOf(err))
	}
}

// held is one space out of a state, which is what a test about one row asks for.
func held(state domain.WorkspaceState, id string) domain.Workspace {
	for _, one := range state.Workspaces {
		if one.ID == id {
			return one
		}
	}
	return domain.Workspace{}
}

func TestUpdateRenamesAndRecolors(t *testing.T) {
	u, _, notifier := newUseCase()
	ctx := context.Background()

	created, err := u.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	target := made(t, created.WorkspaceState).ID
	// The space being renamed is the one on screen, so this is also the case where the windows have
	// to hear about it.
	if _, err := u.Switch(ctx, target); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	notifier.topics = nil

	name := "Payments"
	state, err := u.Update(ctx, target, Patch{Name: &name})
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
	state, err = u.Update(ctx, target, Patch{Color: &color})
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
		t.Errorf("topics = %v, want one %q per change of the space on screen",
			notifier.topics, TopicChanged)
	}

	if _, err := u.Update(ctx, "not-found", Patch{Name: &name}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("updating a space that is not there = %v, want ErrNotFound", err)
	}
}

func TestNameValidation(t *testing.T) {
	u, store, _ := newUseCase()
	ctx := context.Background()

	for _, bad := range []string{"", "   ", strings.Repeat("x", maxNameLength+1)} {
		if _, err := u.Create(ctx, CreateInput{Name: bad}); !errors.Is(err, domain.ErrNotAllowed) {
			t.Errorf("Create(%q) = %v, want domain.ErrNotAllowed", bad, err)
		}
	}
	// The two refusals are told apart by their code, because the window words them differently.
	if _, err := u.Create(ctx, CreateInput{Name: ""}); domain.CodeOf(err) != domain.CodeNameEmpty {
		t.Errorf("an empty name = %q, want %q", domain.CodeOf(err), domain.CodeNameEmpty)
	}
	tooLong := strings.Repeat("x", maxNameLength+1)
	if _, err := u.Create(ctx,
		CreateInput{Name: tooLong}); domain.CodeOf(err) != domain.CodeNameTooLong {
		t.Errorf("a name past the limit = %q, want %q", domain.CodeOf(err), domain.CodeNameTooLong)
	}
	// Nothing was made by any of the refusals.
	if len(store.workspaces) != 1 {
		t.Errorf("the store holds %d workspace(s), want the default alone", len(store.workspaces))
	}

	// The limit counts characters rather than bytes, including non-ASCII letters.
	if _, err := u.Create(ctx, CreateInput{Name: strings.Repeat("é", maxNameLength)}); err != nil {
		t.Errorf("a name of exactly %d characters = %v, want it accepted", maxNameLength, err)
	}
	// A name is trimmed rather than refused for the space around it.
	state, err := u.Create(ctx, CreateInput{Name: "  Team  "})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := made(t, state.WorkspaceState).Name; got != "Team" {
		t.Errorf("name = %q, want it trimmed", got)
	}

	// A patch is held to the same rule as a create. The space being renamed is the one the last
	// create made, because the space the app is born with may not be renamed at all.
	target := made(t, state.WorkspaceState).ID
	empty := "   "
	if _, err := u.Update(ctx, target,
		Patch{Name: &empty}); domain.CodeOf(err) != domain.CodeNameEmpty {
		t.Errorf("renaming to an empty name = %q, want %q", domain.CodeOf(err), domain.CodeNameEmpty)
	}
}

// A space the app did not come with is one somebody made, and it goes like any other.
func TestDeleteTakesASpaceSomebodyMade(t *testing.T) {
	u, store, _ := newUseCase()
	ctx := context.Background()

	created, err := u.Create(ctx, CreateInput{Name: "Work"})
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
	if len(store.deleted) != 1 || store.deleted[0] != going {
		t.Errorf("the store was asked to delete %v, want the one that went", store.deleted)
	}
}

// A space that is also in the account is ended there first: a copy deleted only here would be
// handed back by the next round, and what was in it would be gone from the place it was deleted.
func TestASpaceTheAccountHasIsEndedThereToo(t *testing.T) {
	u, store, remote := withRemote()
	ctx := context.Background()

	if _, err := u.Create(ctx, CreateInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := len(store.workspaces) - 1
	id := store.workspaces[at].ID
	store.workspaces[at].RemoteID = "00000000-0000-0000-0000-0000000000aa"

	if _, err := u.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The account's own id travels, and nothing else: the space is named there, and a name sent from
	// here would be the one this device happens to draw.
	want := "00000000-0000-0000-0000-0000000000aa"
	if len(remote.ended) != 1 || remote.ended[0] != want {
		t.Errorf("ended = %v, want the account's own id %q", remote.ended, want)
	}
}

// Leaving is the exit of somebody who is only a member: the place in the account goes, and the
// space goes from here with it. A member who may not end a space for everybody would otherwise
// have no way out of one at all.
func TestLeaveStepsOutOfASpaceSomebodyElseHas(t *testing.T) {
	u, store, remote := withRemote()
	ctx := context.Background()

	if _, err := u.Create(ctx, CreateInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := len(store.workspaces) - 1
	id := store.workspaces[at].ID
	store.workspaces[at].RemoteID = "00000000-0000-0000-0000-0000000000aa"

	state, err := u.Leave(ctx, id)
	if err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if got := "00000000-0000-0000-0000-0000000000aa"; len(remote.left) != 1 || remote.left[0] != got {
		t.Errorf("left = %v, want the account's own id %q", remote.left, got)
	}
	if _, err := store.Workspace(ctx, id); err == nil {
		t.Error("the space stayed here although the person stepped out of it")
	}
	if len(remote.ended) != 0 {
		t.Errorf("ended = %v, want the space itself left standing", remote.ended)
	}
	if len(state.Workspaces) == 0 {
		t.Error("no state was handed back to the window")
	}
}

// The account is asked first, and its refusal is the whole answer: a space dropped here while the
// place in it still stood would be made again by the next round, which finds the person in it.
func TestASpaceTheAccountWillNotLetThisPersonOutOfStaysHere(t *testing.T) {
	u, store, remote := withRemote()
	ctx := context.Background()

	if _, err := u.Create(ctx, CreateInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := len(store.workspaces) - 1
	id := store.workspaces[at].ID
	store.workspaces[at].RemoteID = "00000000-0000-0000-0000-0000000000aa"
	remote.refuse = domain.Refuse(domain.CodeNotAllowed, nil, nil)

	if _, err := u.Leave(ctx, id); err == nil {
		t.Fatal("a refusal from the account was swallowed")
	}
	if _, err := store.Workspace(ctx, id); err != nil {
		t.Error("the space went from here although the person is still in it")
	}
}

// A place in a space the account no longer has is a place already left: the point of leaving is
// that the space is not the person's, and a refusal for want of it would keep a dead row.
func TestASpaceTheAccountNoLongerHasIsLeftAllTheSame(t *testing.T) {
	u, store, remote := withRemote()
	ctx := context.Background()

	if _, err := u.Create(ctx, CreateInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := len(store.workspaces) - 1
	id := store.workspaces[at].ID
	store.workspaces[at].RemoteID = "00000000-0000-0000-0000-0000000000aa"
	remote.refuse = domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)

	if _, err := u.Leave(ctx, id); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if _, err := store.Workspace(ctx, id); err == nil {
		t.Error("the row stayed although the account has no such space")
	}
}

// The space the app is born with is the device's own in the account: there is no one else's place
// in it to step out of, and it is refused as deleting it is.
func TestTheSpaceTheAppIsBornWithIsNotLeft(t *testing.T) {
	u, _, remote := withRemote()

	if _, err := u.Leave(context.Background(), domain.WorkspacePersonalID); err == nil {
		t.Fatal("leaving the permanent space was allowed")
	}
	if len(remote.left) != 0 {
		t.Errorf("left = %v, want the account asked nothing", remote.left)
	}
}

// A space this device holds alone has no place in an account to step out of: there is nobody to
// leave, and the call refuses it as a space that is not there rather than dropping it here.
func TestASpaceThatIsOnlyHereIsNotLeft(t *testing.T) {
	u, store, remote := withRemote()
	ctx := context.Background()

	created, err := u.Create(ctx, CreateInput{Name: "Payments"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := made(t, created.WorkspaceState).ID

	if _, err := u.Leave(ctx, id); err == nil {
		t.Fatal("leaving a space nobody else has was allowed")
	}
	if len(remote.left) != 0 {
		t.Errorf("left = %v, want the account asked nothing", remote.left)
	}
	if _, err := store.Workspace(ctx, id); err != nil {
		t.Error("the space went although nobody was left out of anything")
	}
}

func TestASpaceTheAccountRefusesToEndStaysHere(t *testing.T) {
	u, store, remote := withRemote()
	ctx := context.Background()

	if _, err := u.Create(ctx, CreateInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := len(store.workspaces) - 1
	id := store.workspaces[at].ID
	store.workspaces[at].RemoteID = "00000000-0000-0000-0000-0000000000aa"
	remote.refuse = domain.Refuse(domain.CodeNotAllowed, nil, nil)

	if _, err := u.Delete(ctx, id); err == nil {
		t.Fatal("a refusal from the account was swallowed")
	}
	if _, err := store.Workspace(ctx, id); err != nil {
		t.Error("the space went from here although it is still in the account")
	}
}

// A space the account no longer has is nothing to report: the point of the call is that it is gone,
// and a device that has just been told so and asks anyway is asking about a row that is not there.
func TestASpaceTheAccountNoLongerHasIsStillEndedHere(t *testing.T) {
	u, store, remote := withRemote()
	ctx := context.Background()

	if _, err := u.Create(ctx, CreateInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := len(store.workspaces) - 1
	id := store.workspaces[at].ID
	store.workspaces[at].RemoteID = "00000000-0000-0000-0000-0000000000aa"
	remote.refuse = domain.Refuse(domain.CodeWorkspaceMissing, domain.ErrNotFound, nil)

	if _, err := u.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Workspace(ctx, id); err == nil {
		t.Error("a space the account does not have was left here")
	}
}

// A space that is only here has nothing said about it anywhere: this device is the only place it
// exists, and a call to an account nobody signed in to would be one made for nothing.
func TestASpaceThatIsOnlyHereIsNotSaidAnywhere(t *testing.T) {
	u, _, remote := withRemote()
	ctx := context.Background()

	created, err := u.Create(ctx, CreateInput{Name: "Payments"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := u.Delete(ctx, made(t, created.WorkspaceState).ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(remote.ended) != 0 {
		t.Errorf("ended = %v, want nothing said about a local space", remote.ended)
	}
}

// The window never lands on a row that is gone: the space that went was the one on screen, so the
// pointer moves to the oldest that is left, and that is the space the windows are told about.
func TestDeleteFallsBackToTheOldestRemaining(t *testing.T) {
	u, store, notifier := newUseCase()
	ctx := context.Background()

	created, err := u.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	left := domain.WorkspacePersonalID
	going := made(t, created.WorkspaceState).ID
	if _, err := u.Switch(ctx, going); err != nil {
		t.Fatalf("Switch: %v", err)
	}

	state, err := u.Delete(ctx, going)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if state.ActiveID != left || store.active != left {
		t.Errorf("activeId = %q, stored = %q, want the space that is left %q", state.ActiveID,
			store.active, left)
	}
	last := notifier.payloads[len(notifier.payloads)-1]
	changed, ok := last.(Changed)
	if !ok {
		t.Fatalf("the last event carried %T, want Changed", last)
	}
	if changed.Workspace.ID != left {
		t.Errorf("the event named %q, want the space the window landed in %q", changed.Workspace.ID,
			left)
	}
}

// What each space holds travels with the list: the switcher draws the counts in the same rows as
// the names, and a state without them would be a second call at every open.
func TestSnapshotCarriesTheCounts(t *testing.T) {
	u, store, _ := newUseCase()
	ctx := context.Background()
	store.counts = map[string]domain.WorkspaceCounts{
		domain.WorkspacePersonalID: {Collections: 2, Environments: 3, Runs: 1},
	}

	state, err := u.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	got := state.Counts[domain.WorkspacePersonalID]
	if got.Collections != 2 || got.Environments != 3 || got.Runs != 1 {
		t.Errorf("counts = %+v, want what the store answered with", got)
	}
}

// What a space is for decides who may be in it, so a space that is in the account is changed there
// and not here: whether anybody else is in it is the server's answer, and it is the server that
// refuses the change that would make a shared space somebody's own.
func TestChangingWhatASpaceIsForIsAskedOfTheAccount(t *testing.T) {
	u, store, remote := withRemote()
	ctx := context.Background()

	if _, err := u.Create(ctx, CreateInput{Name: "Payments"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := len(store.workspaces) - 1
	id := store.workspaces[at].ID
	store.workspaces[at].RemoteID = "00000000-0000-0000-0000-0000000000aa"

	kind := string(domain.WorkspaceTeam)
	if _, err := u.Update(ctx, id, Patch{Kind: &kind}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	want := "00000000-0000-0000-0000-0000000000aa=team"
	if len(remote.kinds) != 1 || remote.kinds[0] != want {
		t.Errorf("asked = %v, want %q", remote.kinds, want)
	}
	if held, err := store.Workspace(ctx, id); err != nil {
		t.Fatalf("Workspace: %v", err)
	} else if held.Kind != domain.WorkspaceTeam {
		t.Errorf("kind = %q, want the space made a team's here as well", held.Kind)
	}
}

// And a refusal from the account is left where it is: a space the server will not turn into
// somebody's own stays a team's on this device too, rather than being drawn as something the person
// will not find when they look from anywhere else.
func TestASpaceTheAccountRefusesToChangeStaysAsItWas(t *testing.T) {
	u, store, remote := withRemote()
	ctx := context.Background()

	if _, err := u.Create(ctx, CreateInput{Name: "Payments", Kind: "team"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	at := len(store.workspaces) - 1
	id := store.workspaces[at].ID
	store.workspaces[at].RemoteID = "00000000-0000-0000-0000-0000000000aa"
	remote.refuse = domain.Refuse(domain.CodeWorkspacePersonal, domain.ErrNotAllowed, nil)

	personal := string(domain.WorkspacePersonal)
	if _, err := u.Update(ctx, id, Patch{Kind: &personal}); domain.CodeOf(err) !=
		domain.CodeWorkspacePersonal {
		t.Fatalf("Update = %q, want the account's refusal", domain.CodeOf(err))
	}

	held, err := store.Workspace(ctx, id)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	if held.Kind != domain.WorkspaceTeam {
		t.Errorf("kind = %q, want the space left as the account has it", held.Kind)
	}
}

// A name another space wears is refused when making one and when renaming one, and a space keeping
// its own name is not a collision with itself.
func TestASpaceTakesNoNameAnotherOneWears(t *testing.T) {
	u, store, _ := newUseCase()
	ctx := context.Background()

	if _, err := u.Create(ctx, CreateInput{Name: "Alpha"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := u.Create(ctx, CreateInput{Name: "Alpha"})
	if !errors.Is(err, domain.ErrConflict) || domain.CodeOf(err) != domain.CodeNameTaken {
		t.Errorf("a second %q = %v, want the refusal %q", "Alpha", err, domain.CodeNameTaken)
	}
	if len(store.workspaces) != 2 {
		t.Errorf("the store holds %d workspace(s), want the default and Alpha", len(store.workspaces))
	}

	state, err := u.Create(ctx, CreateInput{Name: "Beta"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	beta := made(t, state.WorkspaceState).ID
	taken := "Alpha"
	_, err = u.Update(ctx, beta, Patch{Name: &taken})
	if domain.CodeOf(err) != domain.CodeNameTaken {
		t.Errorf("renaming to %q = %v, want the refusal %q", taken, err, domain.CodeNameTaken)
	}
	own := "Beta"
	if _, err := u.Update(ctx, beta, Patch{Name: &own}); err != nil {
		t.Errorf("renaming to its own name = %v, want it accepted", err)
	}
}
