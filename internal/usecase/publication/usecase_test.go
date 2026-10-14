package publication

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"schemo/internal/domain"
)

// fakeStore is the pages this device knows of, in one list the way the table keeps them.
type fakeStore struct {
	held []domain.Publication
	// spaces are the spaces this device holds, by the id the window names them with: the account
	// knows each of them by another id, and a space written here with none is one held alone.
	spaces map[string]domain.Workspace
	// saved counts the writes, so a scenario can say that a refusal wrote nothing.
	saved   int
	refuses error
}

// Workspace answers a space the account knows unless a scenario says otherwise: publishing is into
// a space that reaches the server, and a test with nothing to say about linking means it does.
func (s *fakeStore) Workspace(_ context.Context, id string) (domain.Workspace, error) {
	if s.refuses != nil {
		return domain.Workspace{}, s.refuses
	}
	if held, written := s.spaces[id]; written {
		return held, nil
	}
	return domain.Workspace{ID: id, RemoteID: "remote-" + id}, nil
}

func (s *fakeStore) find(collectionID string) (domain.Publication, bool) {
	for _, held := range s.held {
		if held.CollectionID == collectionID {
			return held, true
		}
	}
	return domain.Publication{}, false
}

func (s *fakeStore) Publication(
	_ context.Context,
	collectionID string,
) (domain.Publication, bool, error) {
	if s.refuses != nil {
		return domain.Publication{}, false, s.refuses
	}
	held, known := s.find(collectionID)
	return held, known, nil
}

func (s *fakeStore) OfWorkspace(_ context.Context, workspace string) ([]domain.Publication, error) {
	if s.refuses != nil {
		return nil, s.refuses
	}
	out := []domain.Publication{}
	for _, held := range s.held {
		if held.WorkspaceID == workspace {
			out = append(out, held)
		}
	}
	return out, nil
}

func (s *fakeStore) SavePublication(_ context.Context, held domain.Publication) error {
	if s.refuses != nil {
		return s.refuses
	}
	s.saved++
	for index, kept := range s.held {
		if kept.CollectionID == held.CollectionID {
			s.held[index] = held
			return nil
		}
	}
	s.held = append(s.held, held)
	return nil
}

// SavePublished keeps what this device alone knows, the way the table does: it writes those two
// fields over the row and leaves every other one exactly as the server's answer left it.
func (s *fakeStore) SavePublished(
	_ context.Context,
	collectionID string,
	outline []domain.OutlineItem,
	changes []domain.PageRelease,
) error {
	if s.refuses != nil {
		return s.refuses
	}
	for index, held := range s.held {
		if held.CollectionID == collectionID {
			s.held[index].Outline, s.held[index].Changes = outline, changes
			return nil
		}
	}
	return nil
}

func (s *fakeStore) ForgetPublication(_ context.Context, collectionID string) error {
	kept := make([]domain.Publication, 0, len(s.held))
	for _, held := range s.held {
		if held.CollectionID != collectionID {
			kept = append(kept, held)
		}
	}
	s.held = kept
	return nil
}

func (s *fakeStore) AwaitingUnpublish(context.Context) ([]domain.Publication, error) {
	if s.refuses != nil {
		return nil, s.refuses
	}
	out := []domain.Publication{}
	for _, held := range s.held {
		if held.PendingUnpublish && held.ID != "" {
			out = append(out, held)
		}
	}
	return out, nil
}

// fakeServer is the server as this feature uses it: it answers what a scenario tells it to, and it
// keeps what it was sent, so a test can read the document a publish actually carried.
type fakeServer struct {
	answered domain.Publication
	pages    []domain.Publication
	err      error

	sent   []domain.PublicationDraft
	taken  []string
	asked  []string
	listed []string
}

func (s *fakeServer) Publish(
	_ context.Context,
	_ domain.Caller,
	draft domain.PublicationDraft,
) (domain.Publication, error) {
	if s.err != nil {
		return domain.Publication{}, s.err
	}
	s.sent = append(s.sent, draft)
	out := s.answered
	out.CollectionID, out.WorkspaceID = draft.Collection, draft.Workspace
	if out.ID == "" {
		out.ID = "publication-1"
	}
	if out.Status == "" {
		out.Status = domain.PublicationActive
	}
	if out.ContentHash == "" {
		out.ContentHash = draft.ContentHash
	}
	return out, nil
}

func (s *fakeServer) Unpublish(_ context.Context, _ domain.Caller, publicationID string) error {
	if s.err != nil {
		return s.err
	}
	s.taken = append(s.taken, publicationID)
	return nil
}

func (s *fakeServer) Pages(
	_ context.Context,
	_ domain.Caller,
	collectionIDs []string,
) ([]domain.Publication, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.asked = append(s.asked, collectionIDs...)
	return s.pages, nil
}

func (s *fakeServer) Published(
	_ context.Context,
	_ domain.Caller,
	workspace string,
) ([]domain.Publication, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.listed = append(s.listed, workspace)
	return s.pages, nil
}

// fakeCollections is the tree read whole, which is the whole of what this feature asks of it: a
// folder is found by walking down to it, and the levels above it are the walk back up. A test that
// says nothing about folders is answered with the one collection it holds.
type fakeCollections struct {
	tree domain.Collection
	err  error
}

func (c fakeCollections) FullOf(_ context.Context, _, id string) (domain.Collection, error) {
	if c.err != nil {
		return domain.Collection{}, c.err
	}
	if found, held := under(c.tree, id); held {
		return found, nil
	}
	return c.tree, nil
}

func (c fakeCollections) PathOf(_ context.Context, _, id string) ([]domain.Collection, error) {
	if c.err != nil {
		return nil, c.err
	}
	if chain := wayUp([]domain.Collection{c.tree}, id); chain != nil {
		return chain, nil
	}
	return []domain.Collection{{ID: id, Name: c.tree.Name}}, nil
}

// under finds a level of a tree by id and says whether it found one: the level carries the folders
// inside it and nothing of the levels above, which is what reading one folder answers with.
func under(level domain.Collection, id string) (domain.Collection, bool) {
	if level.ID == id {
		return level, true
	}
	for _, child := range level.Children {
		if found, held := under(child, id); held {
			return found, true
		}
	}
	return domain.Collection{}, false
}

// wayUp is the levels from a collection up to the root, the collection first: the walk back up as
// looking for it unwinds, which is the shape the tree answers with.
func wayUp(levels []domain.Collection, id string) []domain.Collection {
	for _, level := range levels {
		if level.ID == id {
			return []domain.Collection{{ID: level.ID, Name: level.Name}}
		}
		if below := wayUp(level.Children, id); below != nil {
			return append(below, domain.Collection{ID: level.ID, Name: level.Name})
		}
	}
	return nil
}

// fakeEnvironments is the environment list as the sheet answers it, already masked.
type fakeEnvironments struct {
	state domain.EnvState
	err   error
}

func (e fakeEnvironments) Snapshot(context.Context) (domain.EnvState, error) {
	return e.state, e.err
}

// fakeSession is the account: a caller, or the refusal of one when nobody is signed in.
type fakeSession struct{ err error }

func (s fakeSession) Caller(context.Context) (domain.Caller, error) {
	if s.err != nil {
		return domain.Caller{}, s.err
	}
	return domain.Caller{Address: "https://api.example.com"}, nil
}

// published is the collection every scenario here publishes unless it says otherwise: one request
// in the collection's own name, written against a base address.
func published() domain.Collection {
	return domain.Collection{
		ID:   "collection-1",
		Name: "Example API",
		Items: []domain.CollectionNode{{
			ID: "node-1", Name: "List products", Method: "GET",
			URL: "{{baseUrl}}/v1/products",
		}},
	}
}

// production is the environment list: one environment with the base address every page shows.
func production() domain.EnvState {
	return domain.EnvState{
		Environments: []domain.Environment{{
			ID:   "env-1",
			Name: "Production",
			Vars: []domain.Variable{{Name: domain.BaseURLVariable, Value: "https://api.example.com"}},
		}},
		ActiveID: "env-1",
	}
}

func newTestUseCase(store Store, server Server, session Session) *UseCase {
	uc := NewUseCase(store, fakeCollections{tree: published()}, fakeEnvironments{state: production()},
		server, session)
	uc.now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	return uc
}

// A page behind a password is published with the text that opens it, and the text is on its way and
// nowhere else: the device writes down a page's address and never the thing a stranger would need.
func TestAPageBehindAPasswordIsSentWithItAndKeptNowhere(t *testing.T) {
	store := &fakeStore{}
	server := &fakeServer{}
	uc := newTestUseCase(store, server, fakeSession{})

	if _, err := uc.Publish(context.Background(), "space-1", "collection-1", "env-1",
		domain.VisibilityPassword, "en", "open-sesame", true); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(server.sent) != 1 {
		t.Fatalf("sent %d drafts, want one", len(server.sent))
	}
	sent := server.sent[0]
	if sent.Visibility != domain.VisibilityPassword || sent.Password != "open-sesame" {
		t.Errorf("sent %+v, want the page with the text that opens it", sent)
	}
	if carried := expanded(t, sent.Document); strings.Contains(carried, "open-sesame") {
		t.Error("the document carries the password, want the bytes read by whoever has the address")
	}
	kept, _ := json.Marshal(store.held)
	if bytes.Contains(kept, []byte("open-sesame")) {
		t.Errorf("the row keeps the password: %s", kept)
	}
}

// A password too short to stand behind is refused before anything leaves: the bound is the server's
// own, and a person in front of the popover is told while they are still there.
func TestAPasswordTooShortIsRefusedBeforeAnythingLeaves(t *testing.T) {
	store := &fakeStore{}
	server := &fakeServer{}
	uc := newTestUseCase(store, server, fakeSession{})

	_, err := uc.Publish(context.Background(), "space-1", "collection-1", "env-1",
		domain.VisibilityPassword, "en", strings.Repeat("p", domain.PasswordLeast-1), true)

	if domain.CodeOf(err) != domain.CodePasswordInvalid {
		t.Fatalf("err = %v, want the password refused", err)
	}
	if len(server.sent) != 0 || store.saved != 0 {
		t.Errorf("sent %d drafts and wrote %d rows, want nothing to have left",
			len(server.sent), store.saved)
	}
}

// A publish leaves under the id the account knows the space by, and the row it keeps is keyed by
// the id this device holds: the two are never the same string, and a page saved under the account's
// is one the window's own list never finds.
func TestAPublishLeavesUnderTheAccountsSpaceIDAndIsKeptUnderItsOwn(t *testing.T) {
	store := &fakeStore{}
	server := &fakeServer{}
	uc := newTestUseCase(store, server, fakeSession{})

	if _, err := uc.Publish(context.Background(), "space-1", "collection-1", "env-1",
		domain.VisibilityUnlisted, "en", "", true); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(server.sent) != 1 {
		t.Fatalf("sent %d drafts, want one", len(server.sent))
	}
	if sent := server.sent[0].Workspace; sent != "remote-space-1" {
		t.Errorf("published into %q, want the space the account knows", sent)
	}
	held, _ := store.find("collection-1")
	if held.WorkspaceID != "space-1" {
		t.Errorf("kept under %q, want the space this device holds", held.WorkspaceID)
	}
	listed, err := uc.List(context.Background(), "space-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Errorf("listed %d pages, want the one that was just published", len(listed))
	}
}

// A space this device holds alone has nothing to publish into: the page is the one thing here the
// server keeps, and a space that was never linked has no id to keep it under.
func TestASpaceHeldAloneHasNothingToPublishInto(t *testing.T) {
	store := &fakeStore{spaces: map[string]domain.Workspace{"space-1": {ID: "space-1"}}}
	server := &fakeServer{}
	uc := newTestUseCase(store, server, fakeSession{})

	_, err := uc.Publish(context.Background(), "space-1", "collection-1", "env-1",
		domain.VisibilityUnlisted, "en", "", true)

	if domain.CodeOf(err) != domain.CodeWorkspaceMissing {
		t.Fatalf("err = %v, want the space refused", err)
	}
	if len(server.sent) != 0 || store.saved != 0 {
		t.Errorf("sent %d drafts and wrote %d rows, want nothing to have left",
			len(server.sent), store.saved)
	}
}

// What a publish carries is the whole page, and a secret's value is not in it — nor the name it was
// kept under, because the name of a secret is part of the secret.
func TestAPublishedPageCarriesNoSecretsValue(t *testing.T) {
	tree := published()
	tree.Variables = []domain.Variable{{
		Name: "API_TOKEN", Value: "s3cr3t-value", Kind: domain.VariableSecret, Enabled: true,
	}}
	tree.Items[0].URL = "{{baseUrl}}/v1/products?key={{API_TOKEN}}"

	store := &fakeStore{}
	server := &fakeServer{}
	uc := newTestUseCase(store, server, fakeSession{})
	uc.collections = fakeCollections{tree: tree}

	if _, err := uc.Publish(context.Background(), "space-1", "collection-1", "env-1",
		domain.VisibilityUnlisted, "en", "", true); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(server.sent) != 1 {
		t.Fatalf("sent %d drafts, want one", len(server.sent))
	}

	carried := expanded(t, server.sent[0].Document)
	for _, secret := range []string{"s3cr3t-value", "API_TOKEN"} {
		if strings.Contains(carried, secret) {
			t.Errorf("the document carries %q", secret)
		}
	}
}

// A takedown the network did not carry is not lost: a person who takes a page down means it, and
// the one thing it may not do is be forgotten because of the weather.
func TestATakedownTheServerNeverHeardIsKept(t *testing.T) {
	store := &fakeStore{held: []domain.Publication{{
		CollectionID: "collection-1", WorkspaceID: "space-1", ID: "publication-1",
		Status: domain.PublicationActive,
	}}}
	server := &fakeServer{err: domain.Refuse(domain.CodeServerUnreachable, nil, nil)}
	uc := newTestUseCase(store, server, fakeSession{})

	if err := uc.Unpublish(context.Background(), "collection-1"); err != nil {
		t.Fatalf("Unpublish: %v", err)
	}

	held, known := store.find("collection-1")
	if !known || !held.PendingUnpublish || held.Status != domain.PublicationRevoked {
		t.Errorf("held = %+v, want a takedown kept for later", held)
	}
	if store.saved != 1 {
		t.Errorf("wrote %d rows, want the takedown written down once", store.saved)
	}
}

// The takedown that was waiting goes out the next time the device has a session and a server, and
// what is owed is cleared only once it has.
func TestTheTakedownIsPlayedBackOnceTheServerAnswers(t *testing.T) {
	store := &fakeStore{held: []domain.Publication{{
		CollectionID: "collection-1", ID: "publication-1", Status: domain.PublicationRevoked,
		PendingUnpublish: true,
	}}}
	server := &fakeServer{}
	uc := newTestUseCase(store, server, fakeSession{})

	if err := uc.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(server.taken) != 1 || server.taken[0] != "publication-1" {
		t.Errorf("taken = %v, want the page that was waiting", server.taken)
	}
	held, _ := store.find("collection-1")
	if held.PendingUnpublish {
		t.Errorf("held = %+v, want nothing left owing", held)
	}
}

// A device that is signed out is not a device that changed its mind: the takedown waits for a
// session rather than being dropped, and nothing is sent under nobody's name.
func TestASignedOutDevicePlaysBackNothing(t *testing.T) {
	store := &fakeStore{held: []domain.Publication{{
		CollectionID: "collection-1", ID: "publication-1", PendingUnpublish: true,
	}}}
	server := &fakeServer{}
	refusal := domain.Refuse(domain.CodeNotSignedIn, nil, nil)
	uc := newTestUseCase(store, server, fakeSession{err: refusal})

	if err := uc.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(server.taken) != 0 {
		t.Errorf("taken = %v, want nothing sent without a session", server.taken)
	}
	held, _ := store.find("collection-1")
	if !held.PendingUnpublish {
		t.Error("the takedown was dropped by a device that is only signed out")
	}
}

// Refresh asks about the collections this device knows of and no others: a collection it has never
// published is not one it may ask about, and a page of another space is not this space's answer.
func TestRefreshAsksAboutWhatThisDeviceKnows(t *testing.T) {
	store := &fakeStore{held: []domain.Publication{
		{CollectionID: "collection-1", WorkspaceID: "space-1", ID: "publication-1"},
		{CollectionID: "collection-2", WorkspaceID: "space-1", ID: "publication-2"},
		{CollectionID: "collection-9", WorkspaceID: "space-2", ID: "publication-9"},
	}}
	server := &fakeServer{pages: []domain.Publication{{
		CollectionID: "collection-1", WorkspaceID: "remote-space-1", ID: "publication-1",
		Revision: 4,
	}}}
	uc := newTestUseCase(store, server, fakeSession{})

	held, err := uc.Refresh(context.Background(), "space-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if len(server.asked) != 2 {
		t.Errorf("asked about %v, want the two collections of this space", server.asked)
	}
	if len(held) != 2 {
		t.Fatalf("listed %d pages, want this space's two", len(held))
	}
	one, _ := store.find("collection-1")
	if one.Revision != 4 {
		t.Errorf("revision = %d, want the server's answer kept", one.Revision)
	}
	// The answer names the space by the account's id, and the row is this device's: kept as it came,
	// the page would be listed under a space the window never asks about.
	if one.WorkspaceID != "space-1" {
		t.Errorf("workspace = %q, want the one this device holds the space by", one.WorkspaceID)
	}
}

// A device that holds no page of a space asks the space itself: the pages of a space are the
// space's, and a second device — or this one after its own table was cleared — has the same ones.
func TestADeviceThatHoldsNoPageAsksTheSpace(t *testing.T) {
	store := &fakeStore{}
	server := &fakeServer{pages: []domain.Publication{{
		CollectionID: "collection-1", WorkspaceID: "remote-space-1", ID: "publication-1",
		Slug: "example-api-3f9a2c10bd", Status: domain.PublicationActive,
	}}}
	uc := newTestUseCase(store, server, fakeSession{})

	held, err := uc.Refresh(context.Background(), "space-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if len(server.listed) != 1 || server.listed[0] != "remote-space-1" {
		t.Errorf("listed %v, want the space the account knows", server.listed)
	}
	if len(server.asked) != 0 {
		t.Errorf("asked about %v, want no question about collections", server.asked)
	}
	if len(held) != 1 || held[0].WorkspaceID != "space-1" {
		t.Fatalf("held = %+v, want the page kept under this device's own space", held)
	}
	if held[0].Slug != "example-api-3f9a2c10bd" {
		t.Errorf("slug = %q, want the address the server issued", held[0].Slug)
	}
}

// A device with nobody signed in is not a device with nothing to say: a refresh answers what it
// holds, and asking the server under nobody's name is not a question it puts.
func TestARefreshWithNoSessionAnswersWhatThisDeviceHolds(t *testing.T) {
	store := &fakeStore{held: []domain.Publication{{
		CollectionID: "collection-1", WorkspaceID: "space-1", ID: "publication-1",
		Status: domain.PublicationActive,
	}}}
	server := &fakeServer{}
	signedOut := domain.Refuse(domain.CodeNotSignedIn, nil, nil)
	uc := newTestUseCase(store, server, fakeSession{err: signedOut})

	held, err := uc.Refresh(context.Background(), "space-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if len(held) != 1 {
		t.Errorf("held %d pages, want the one this device knows of", len(held))
	}
	if len(server.asked) != 0 || len(server.listed) != 0 {
		t.Errorf("asked %v and listed %v, want nothing sent without a session",
			server.asked, server.listed)
	}
}

// A collection that has been edited since it was published is behind, and the window can say so
// without asking anybody: the document is built again and its digest compared.
func TestAChangedCollectionIsSaidToBeBehind(t *testing.T) {
	tree := published()
	tree.Items[0].URL = "{{baseUrl}}/v2/products"

	store := &fakeStore{held: []domain.Publication{{
		CollectionID: "collection-1", ID: "publication-1", Status: domain.PublicationActive,
		ContentHash: "a-digest-of-what-was-published",
	}}}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tree}

	change, err := uc.Changes(context.Background(), "space-1", "collection-1")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if change.Page == nil || !change.Behind {
		t.Errorf("change = %+v, want a page that is up and behind", change)
	}
}

// A page published a moment ago is not behind, and the language it was read in is not a change: the
// digest is of the collection, not of the build that wrote it.
func TestAPageThatWasJustPublishedIsNotBehind(t *testing.T) {
	store := &fakeStore{}
	server := &fakeServer{}
	uc := newTestUseCase(store, server, fakeSession{})
	ctx := context.Background()

	if _, err := uc.Publish(ctx, "space-1", "collection-1", "env-1", domain.VisibilityUnlisted,
		"ru", "", true); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	change, err := uc.Changes(ctx, "space-1", "collection-1")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if change.Page == nil || change.Behind {
		t.Errorf("change = %+v, want a page that is up to date", change)
	}
}

// A page is compared against the environment it went out with and not the one the app works in now:
// a page published from Production is not behind because the app has since moved to Local, and the
// popover a person opens on it is not showing them changes they would make by pressing nothing.
func TestAPageIsComparedAgainstTheEnvironmentItWentOutWith(t *testing.T) {
	store := &fakeStore{}
	server := &fakeServer{answered: domain.Publication{EnvironmentName: "Production"}}
	uc := newTestUseCase(store, server, fakeSession{})
	ctx := context.Background()
	both := domain.EnvState{
		Environments: []domain.Environment{
			{ID: "env-1", Name: "Production", Vars: addressed("https://api.example.com")},
			{ID: "env-2", Name: "Local", Vars: addressed("http://localhost:8080")},
		},
		ActiveID: "env-1",
	}
	uc.environments = fakeEnvironments{state: both}

	if _, err := uc.Publish(ctx, "space-1", "collection-1", "env-1", domain.VisibilityUnlisted,
		"en", "", true); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	both.ActiveID = "env-2"
	uc.environments = fakeEnvironments{state: both}

	change, err := uc.Changes(ctx, "space-1", "collection-1")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if change.Page == nil || change.Behind {
		t.Errorf("change = %+v, want a page that is up to date", change)
	}
	if len(change.Changes) != 0 {
		t.Errorf("changes = %+v, want nothing said to have moved", change.Changes)
	}
}

// The popover opens saying nothing about the environment, and Go reads that as the one the page
// went out with: a person who has not touched the field is asking for the page they are looking
// at, not for one built against whatever this device happens to work in now.
func TestThePopoverOpensOnThePagesOwnEnvironment(t *testing.T) {
	store := &fakeStore{}
	server := &fakeServer{answered: domain.Publication{EnvironmentName: "Production"}}
	uc := newTestUseCase(store, server, fakeSession{})
	ctx := context.Background()
	both := domain.EnvState{
		Environments: []domain.Environment{
			{ID: "env-1", Name: "Production", Vars: addressed("https://api.example.com")},
			{ID: "env-2", Name: "Local", Vars: addressed("http://localhost:8080")},
		},
		ActiveID: "env-1",
	}
	uc.environments = fakeEnvironments{state: both}

	if _, err := uc.Publish(ctx, "space-1", "collection-1", "env-1", domain.VisibilityUnlisted,
		"en", "", true); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	both.ActiveID = "env-2"
	uc.environments = fakeEnvironments{state: both}

	preview, err := uc.Preview(ctx, "space-1", "collection-1", "")
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.Behind || len(preview.Changes) != 0 {
		t.Errorf("preview = %+v, want the page as it went out", preview)
	}
}

// The word a window draws on a page's button comes with the requests behind it: whoever opens the
// popover is told which ones moved, so that «Changes · 1» is one they can read before publishing.
func TestAPageSaysWhichRequestsMovedAtIt(t *testing.T) {
	store := &fakeStore{}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: catalogue()}
	ctx := context.Background()

	if _, err := uc.Publish(ctx, "space-1", "collection-1", "env-1", domain.VisibilityUnlisted,
		"en", "", true); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	moved := catalogue()
	moved.Items[0].Name = "List all products"
	uc.collections = fakeCollections{tree: moved}

	change, err := uc.Changes(ctx, "space-1", "collection-1")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	want := domain.PageItem{Sign: signChanged, Method: "GET", Name: "List all products"}
	if !change.Behind || len(change.Changes) != 1 || change.Changes[0] != want {
		t.Errorf("change = %+v, want the one request that moved", change)
	}
}

// The popover opens without a network: what it draws is the report, the environments and what this
// device already holds, and none of it is the server's to answer.
func TestThePopoverOpensWithoutAskingTheServer(t *testing.T) {
	store := &fakeStore{}
	server := &fakeServer{err: errors.New("the network is not here")}
	uc := newTestUseCase(store, server, fakeSession{})

	preview, err := uc.Preview(context.Background(), "space-1", "collection-1", "env-1")
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	if preview.Publication != nil || preview.Behind {
		t.Errorf("preview = %+v, want a collection nobody has published", preview)
	}
	if preview.Report.Requests != 1 || preview.Report.Folders != 1 {
		t.Errorf("report = %+v, want the one request counted", preview.Report)
	}
	if len(preview.Environments) != 1 ||
		preview.Environments[0].BaseURL != "https://api.example.com" {
		t.Errorf("environments = %+v, want the one the addresses resolve against",
			preview.Environments)
	}
}

// The popover opens on the same list the changelog entry is made of: what a person reads before
// pressing «Update» is what the page will say moved at it, worded from this device's own outline.
func TestThePopoverOpensOnWhatAPublishWouldRecord(t *testing.T) {
	store := &fakeStore{}
	uc := newTestUseCase(store, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: catalogue()}
	ctx := context.Background()

	if _, err := uc.Publish(ctx, "space-1", "collection-1", "env-1", domain.VisibilityUnlisted,
		"en", "", true); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	moved := catalogue()
	moved.Items[0].Name = "List all products"
	uc.collections = fakeCollections{tree: moved}

	preview, err := uc.Preview(ctx, "space-1", "collection-1", "env-1")
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	snapshot, _, err := uc.Build(ctx, "space-1", "collection-1", "env-1", "")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(snapshot.Changes) != 1 {
		t.Fatalf("changes = %+v, want the one release", snapshot.Changes)
	}
	recorded := snapshot.Changes[0].Items
	want := domain.PageItem{
		Sign: recorded[0].Sign, Method: recorded[0].Method, Name: recorded[0].Name,
	}
	if len(preview.Changes) != len(recorded) || preview.Changes[0] != want {
		t.Errorf("preview = %+v, want the entry a publish would record: %+v",
			preview.Changes, recorded)
	}
}

// A name is a handle a reader clicks, so one too long to draw is refused rather than cut in half.
func TestARequestTooLongToNameIsRefused(t *testing.T) {
	tree := published()
	tree.Items[0].Name = strings.Repeat("n", maxNameRunes+1)
	uc := newTestUseCase(&fakeStore{}, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tree}

	_, _, err := uc.Build(context.Background(), "space-1", "collection-1", "env-1", "en")

	if domain.CodeOf(err) != domain.CodeNameTooLong {
		t.Fatalf("err = %v, want the name refused", err)
	}
}

// The collection's own name is the heading a stranger reads first, and a page is named more tightly
// than a request is: one the server would refuse as a title is refused here, where the sentence is
// this side's and the person is still in front of the popover.
func TestACollectionNameTooLongForATitleIsRefused(t *testing.T) {
	tree := published()
	tree.Name = strings.Repeat("t", maxTitleRunes+1)
	uc := newTestUseCase(&fakeStore{}, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tree}

	_, _, err := uc.Build(context.Background(), "space-1", "collection-1", "env-1", "en")

	if domain.CodeOf(err) != domain.CodeNameTooLong {
		t.Fatalf("err = %v, want the title refused", err)
	}
}

// What a bound is held to is the name the document carries rather than the one the author wrote: a
// name with a token in it is drawn as the mask, which is longer than some tokens are, and a name
// that fits as written but not as drawn is one the server refuses with nothing to say about it.
func TestAMaskedNameIsHeldToWhatTheDocumentCarries(t *testing.T) {
	tree := published()
	tree.Variables = []domain.Variable{{Name: "token", Value: "a-secret"}}
	tree.Items[0].Name = strings.Repeat("n", maxNameRunes-len([]rune(domain.SecretMask))+1) +
		"{{token}}"
	uc := newTestUseCase(&fakeStore{}, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tree}

	_, _, err := uc.Build(context.Background(), "space-1", "collection-1", "env-1", "en")

	if domain.CodeOf(err) != domain.CodeNameTooLong {
		t.Fatalf("err = %v, want the name refused as it would be drawn", err)
	}
}

// A description too long to draw is cut and noted instead: losing the tail of one is not a reason
// to lose a collection, and the window says which request it was about.
func TestALongDescriptionIsCutAndNoted(t *testing.T) {
	tree := published()
	tree.Items[0].Description = strings.Repeat("d", maxTextRunes+100)
	uc := newTestUseCase(&fakeStore{}, &fakeServer{}, fakeSession{})
	uc.collections = fakeCollections{tree: tree}

	snapshot, report, err := uc.Build(context.Background(), "space-1", "collection-1", "env-1", "en")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	held := snapshot.Collection.Groups[0].Endpoints[0].Description
	if len([]rune(held)) != maxTextRunes {
		t.Errorf("description is %d runes, want it cut to %d", len([]rune(held)), maxTextRunes)
	}
	if !noted(report, domain.CodeTextTooLong) {
		t.Errorf("problems = %+v, want the cut noted", report.Problems)
	}
}
