package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"schemo/internal/db"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"strings"
	"testing"
	"time"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
)

// fixture is the pages this device knows of, in the real store, with the two workspaces the tests
// publish from linked to the account.
type fixture struct {
	t       *testing.T
	db      *db.Store
	initial []domain.Publication
}

// newStore is a store holding the given pages, in a workspace-1 and a workspace-2 the account knows as
// remote-workspace-1 and remote-workspace-2.
func newStore(t *testing.T, held ...domain.Publication) *fixture {
	t.Helper()
	ctx := context.Background()
	f := &fixture{t: t, db: dbtest.New(t)}
	for _, id := range []string{"space-1", "space-2"} {
		if err := f.db.SaveWorkspace(ctx, domain.NewWorkspace(id, id, domain.WorkspaceTeam, "", 1)); err != nil {
			t.Fatal(err)
		}
		f.link(id, "remote-"+id)
	}
	f.put(held...)
	return f
}

// link names the id the account knows a workspace by; an empty one is a workspace held alone.
func (f *fixture) link(workspace, remote string) {
	f.t.Helper()
	if _, err := f.db.DB().Exec(`UPDATE workspaces SET remote_workspace_id = ? WHERE id = ?`,
		remote, workspace); err != nil {
		f.t.Fatal(err)
	}
}

// put writes pages as a publish or a refresh left them, and takes them as the state a test starts
// from.
func (f *fixture) put(pages ...domain.Publication) {
	f.t.Helper()
	ctx := context.Background()
	for _, page := range pages {
		if err := f.db.SavePublication(ctx, page); err != nil {
			f.t.Fatalf("SavePublication(%s): %v", page.CollectionID, err)
		}
		if page.Outline != nil || page.Changes != nil || page.Options != nil {
			options := domain.PublicationOptions{}
			if page.Options != nil {
				options = *page.Options
			}
			if err := f.db.SavePublished(ctx, page.CollectionID, page.Outline, page.Changes,
				options); err != nil {
				f.t.Fatal(err)
			}
		}
	}
	f.initial = f.held()
}

// held is every page the store keeps, in the order they were first written.
func (f *fixture) held() []domain.Publication {
	f.t.Helper()
	rows, err := f.db.DB().Query(`SELECT collection_id FROM publications ORDER BY rowid`)
	if err != nil {
		f.t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			f.t.Fatal(err)
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	out := []domain.Publication{}
	for _, id := range ids {
		page, _ := f.find(id)
		out = append(out, page)
	}
	return out
}

func (f *fixture) find(collectionID string) (domain.Publication, bool) {
	f.t.Helper()
	page, known, err := f.db.Publication(context.Background(), collectionID)
	if err != nil {
		f.t.Fatal(err)
	}
	return page, known
}

// untouched says nothing was written since the test set the store up.
func (f *fixture) untouched() bool {
	return reflect.DeepEqual(f.initial, f.held())
}

// fakePublicationServer is the server as this feature uses it: it answers what a scenario tells it to, and it
// keeps what it was sent, so a test can read the document a publish actually carried.
type fakePublicationServer struct {
	answered         domain.Publication
	pages            []domain.Publication
	err              error
	restoredDocument []byte

	sent   []domain.PublicationDraft
	taken  []string
	asked  []string
	listed []string
}

func (s *fakePublicationServer) Details(context.Context, domain.Caller, string) (domain.PublicationDetails, error) {
	return domain.PublicationDetails{}, s.err
}

func (s *fakePublicationServer) Restore(context.Context, domain.Caller, string, int) (domain.Publication, []byte, error) {
	return s.answered, s.restoredDocument, s.err
}

func (s *fakePublicationServer) Publish(
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
	if out.Revision == 0 {
		out.Revision = len(s.sent)
	}
	if out.Status == "" {
		out.Status = domain.PublicationActive
	}
	if out.ContentHash == "" {
		out.ContentHash = draft.ContentHash
	}
	return out, nil
}

func (s *fakePublicationServer) Unpublish(_ context.Context, _ domain.Caller, publicationID string) error {
	if s.err != nil {
		return s.err
	}
	s.taken = append(s.taken, publicationID)
	return nil
}

func (s *fakePublicationServer) Pages(
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

func (s *fakePublicationServer) Published(
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

func (e fakeEnvironments) State(context.Context) (domain.EnvState, error) {
	return e.state, e.err
}

// publicationCaller is the account: a caller, or the refusal of one when nobody is signed in.
type publicationCaller struct{ err error }

func (s publicationCaller) Me(context.Context) (domain.Account, error) {
	return domain.Account{Server: "https://api.example.com", UserID: "user"}, s.err
}

func (s publicationCaller) Caller(context.Context) (domain.Caller, error) {
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

func newPublications(store *fixture, server PublicationServer, session publicationSession) *Publications {
	publications := NewPublications(store.db, fakeCollections{tree: published()}, fakeEnvironments{state: production()},
		server, session, nil)
	publications.now = func() time.Time { return time.UnixMilli(1_700_000_000_000) }
	return publications
}

// publishing is the request most tests make: an unlisted page of one collection, confirmed, out of
// the first workspace and environment. A test about anything else says what differs.
func publishing(collection string) PublishRequest {
	return PublishRequest{
		Workspace:     "space-1",
		Collection:    collection,
		EnvironmentID: "env-1",
		Visibility:    domain.VisibilityUnlisted,
		Locale:        "en",
		Confirmed:     true,
	}
}

// Passwords travel separately and never enter public documents or local publication rows.
func TestPublicationPasswordStaysOutOfDocumentsAndDatabaseRows(t *testing.T) {
	store := newStore(t)
	server := &fakePublicationServer{}
	publications := newPublications(store, server, publicationCaller{})

	if _, err := publications.publish(context.Background(), PublishRequest{
		Workspace:     "space-1",
		Collection:    "collection-1",
		EnvironmentID: "env-1",
		Visibility:    domain.VisibilityPassword,
		Locale:        "en",
		Password:      "open-sesame",
		Confirmed:     true,
	}); err != nil {
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
	kept, _ := json.Marshal(store.held())
	if bytes.Contains(kept, []byte("open-sesame")) {
		t.Errorf("the row keeps the password: %s", kept)
	}
}

// A password too short to stand behind is refused before anything leaves: the bound is the server's
// own, and a person in front of the popover is told while they are still there.
func TestAPasswordTooShortIsRefusedBeforeAnythingLeaves(t *testing.T) {
	store := newStore(t)
	server := &fakePublicationServer{}
	publications := newPublications(store, server, publicationCaller{})

	_, err := publications.publish(context.Background(), PublishRequest{
		Workspace:     "space-1",
		Collection:    "collection-1",
		EnvironmentID: "env-1",
		Visibility:    domain.VisibilityPassword,
		Locale:        "en",
		Password:      strings.Repeat("p", domain.PasswordLeast-1),
		Confirmed:     true,
	})

	if domain.CodeOf(err) != domain.CodePasswordInvalid {
		t.Fatalf("err = %v, want the password refused", err)
	}
	if len(server.sent) != 0 || !store.untouched() {
		t.Errorf("sent %d drafts or wrote rows, want nothing to have left", len(server.sent))
	}
}

// A publish leaves under the id the account knows the workspace by, and the row it keeps is keyed by
// the id this device holds: the two are never the same string, and a page saved under the account's
// is one the window's own list never finds.
func TestAPublishLeavesUnderTheAccountsWorkspaceIDAndIsKeptUnderItsOwn(t *testing.T) {
	store := newStore(t)
	server := &fakePublicationServer{}
	publications := newPublications(store, server, publicationCaller{})

	if _, err := publications.publish(context.Background(), publishing("collection-1")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(server.sent) != 1 {
		t.Fatalf("sent %d drafts, want one", len(server.sent))
	}
	if sent := server.sent[0].Workspace; sent != "remote-space-1" {
		t.Errorf("published into %q, want the workspace the account knows", sent)
	}
	held, _ := store.find("collection-1")
	if held.WorkspaceID != "space-1" {
		t.Errorf("kept under %q, want the workspace this device holds", held.WorkspaceID)
	}
	listed, err := publications.List(context.Background(), "space-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Errorf("listed %d pages, want the one that was just published", len(listed))
	}
}

// A workspace this device holds alone has nothing to publish into: the page is the one thing here the
// server keeps, and a workspace that was never linked has no id to keep it under.
func TestAWorkspaceHeldAloneHasNothingToPublishInto(t *testing.T) {
	store := newStore(t)
	store.link("space-1", "")
	server := &fakePublicationServer{}
	publications := newPublications(store, server, publicationCaller{})

	_, err := publications.publish(context.Background(), publishing("collection-1"))

	if domain.CodeOf(err) != domain.CodeWorkspaceMissing {
		t.Fatalf("err = %v, want the workspace refused", err)
	}
	if len(server.sent) != 0 || !store.untouched() {
		t.Errorf("sent %d drafts or wrote rows, want nothing to have left", len(server.sent))
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

	store := newStore(t)
	server := &fakePublicationServer{}
	publications := newPublications(store, server, publicationCaller{})
	publications.collections = fakeCollections{tree: tree}

	if _, err := publications.publish(context.Background(), publishing("collection-1")); err != nil {
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
	store := newStore(t, domain.Publication{
		CollectionID: "collection-1", WorkspaceID: "space-1", ID: "publication-1",
		Status: domain.PublicationActive,
	})
	server := &fakePublicationServer{err: domain.Refuse(domain.CodeServerUnreachable, nil, nil)}
	publications := newPublications(store, server, publicationCaller{})

	if err := publications.Unpublish(context.Background(), "collection-1"); err != nil {
		t.Fatalf("Unpublish: %v", err)
	}

	held, known := store.find("collection-1")
	if !known || !held.PendingUnpublish || held.Status != domain.PublicationRevoked {
		t.Errorf("held = %+v, want a takedown kept for later", held)
	}
}

// The takedown that was waiting goes out the next time the device has a session and a server, and
// what is owed is cleared only once it has.
func TestTheTakedownIsPlayedBackOnceTheServerAnswers(t *testing.T) {
	store := newStore(t, domain.Publication{
		CollectionID: "collection-1", ID: "publication-1", Status: domain.PublicationRevoked,
		PendingUnpublish: true,
	})
	server := &fakePublicationServer{}
	publications := newPublications(store, server, publicationCaller{})

	if err := publications.Replay(context.Background()); err != nil {
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
	store := newStore(t, domain.Publication{
		CollectionID: "collection-1", ID: "publication-1", PendingUnpublish: true,
	})
	server := &fakePublicationServer{}
	refusal := domain.Refuse(domain.CodeNotSignedIn, nil, nil)
	publications := newPublications(store, server, publicationCaller{err: refusal})

	if err := publications.Replay(context.Background()); err != nil {
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
// published is not one it may ask about, and a page of another workspace is not this workspace's answer.
func TestRefreshAsksAboutWhatThisDeviceKnows(t *testing.T) {
	store := newStore(t, domain.Publication{CollectionID: "collection-1", WorkspaceID: "space-1", ID: "publication-1"},
		domain.Publication{CollectionID: "collection-2", WorkspaceID: "space-1", ID: "publication-2"},
		domain.Publication{CollectionID: "collection-9", WorkspaceID: "space-2", ID: "publication-9"})
	server := &fakePublicationServer{pages: []domain.Publication{{
		CollectionID: "collection-1", WorkspaceID: "remote-space-1", ID: "publication-1",
		Revision: 4,
	}}}
	publications := newPublications(store, server, publicationCaller{})

	held, err := publications.Refresh(context.Background(), "space-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if len(server.asked) != 2 {
		t.Errorf("asked about %v, want the two collections of this workspace", server.asked)
	}
	if len(held) != 2 {
		t.Fatalf("listed %d pages, want this workspace's two", len(held))
	}
	one, _ := store.find("collection-1")
	if one.Revision != 4 {
		t.Errorf("revision = %d, want the server's answer kept", one.Revision)
	}
	// The answer names the workspace by the account's id, and the row is this device's: kept as it came,
	// the page would be listed under a workspace the window never asks about.
	if one.WorkspaceID != "space-1" {
		t.Errorf("workspace = %q, want the one this device holds the workspace by", one.WorkspaceID)
	}
}

// A device that holds no page of a workspace asks the workspace itself: the pages of a workspace are the
// workspace's, and a second device — or this one after its own table was cleared — has the same ones.
func TestADeviceThatHoldsNoPageAsksTheWorkspace(t *testing.T) {
	store := newStore(t)
	server := &fakePublicationServer{pages: []domain.Publication{{
		CollectionID: "collection-1", WorkspaceID: "remote-space-1", ID: "publication-1",
		Slug: "example-api-3f9a2c10bd", Status: domain.PublicationActive,
	}}}
	publications := newPublications(store, server, publicationCaller{})

	held, err := publications.Refresh(context.Background(), "space-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if len(server.listed) != 1 || server.listed[0] != "remote-space-1" {
		t.Errorf("listed %v, want the workspace the account knows", server.listed)
	}
	if len(server.asked) != 0 {
		t.Errorf("asked about %v, want no question about collections", server.asked)
	}
	if len(held) != 1 || held[0].WorkspaceID != "space-1" {
		t.Fatalf("held = %+v, want the page kept under this device's own workspace", held)
	}
	if held[0].Slug != "example-api-3f9a2c10bd" {
		t.Errorf("slug = %q, want the address the server issued", held[0].Slug)
	}
}

// A device with nobody signed in is not a device with nothing to say: a refresh answers what it
// holds, and asking the server under nobody's name is not a question it puts.
func TestARefreshWithNoSessionAnswersWhatThisDeviceHolds(t *testing.T) {
	store := newStore(t, domain.Publication{
		CollectionID: "collection-1", WorkspaceID: "space-1", ID: "publication-1",
		Status: domain.PublicationActive,
	})
	server := &fakePublicationServer{}
	signedOut := domain.Refuse(domain.CodeNotSignedIn, nil, nil)
	publications := newPublications(store, server, publicationCaller{err: signedOut})

	held, err := publications.Refresh(context.Background(), "space-1")
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

	store := newStore(t, domain.Publication{
		CollectionID: "collection-1", ID: "publication-1", Status: domain.PublicationActive,
		ContentHash: "a-digest-of-what-was-published",
	})
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tree}

	change, err := publications.Changes(context.Background(), "space-1", "collection-1")
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
	store := newStore(t)
	server := &fakePublicationServer{}
	publications := newPublications(store, server, publicationCaller{})
	ctx := context.Background()

	if _, err := publications.publish(ctx, PublishRequest{
		Workspace:     "space-1",
		Collection:    "collection-1",
		EnvironmentID: "env-1",
		Visibility:    domain.VisibilityUnlisted,
		Locale:        "ru",
		Confirmed:     true,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	change, err := publications.Changes(ctx, "space-1", "collection-1")
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
	store := newStore(t)
	server := &fakePublicationServer{answered: domain.Publication{EnvironmentName: "Production"}}
	publications := newPublications(store, server, publicationCaller{})
	ctx := context.Background()
	both := domain.EnvState{
		Environments: []domain.Environment{
			{ID: "env-1", Name: "Production", Vars: addressed("https://api.example.com")},
			{ID: "env-2", Name: "Local", Vars: addressed("http://localhost:8080")},
		},
		ActiveID: "env-1",
	}
	publications.environments = fakeEnvironments{state: both}

	if _, err := publications.publish(ctx, publishing("collection-1")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	both.ActiveID = "env-2"
	publications.environments = fakeEnvironments{state: both}

	change, err := publications.Changes(ctx, "space-1", "collection-1")
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
	store := newStore(t)
	server := &fakePublicationServer{answered: domain.Publication{EnvironmentName: "Production"}}
	publications := newPublications(store, server, publicationCaller{})
	ctx := context.Background()
	both := domain.EnvState{
		Environments: []domain.Environment{
			{ID: "env-1", Name: "Production", Vars: addressed("https://api.example.com")},
			{ID: "env-2", Name: "Local", Vars: addressed("http://localhost:8080")},
		},
		ActiveID: "env-1",
	}
	publications.environments = fakeEnvironments{state: both}

	if _, err := publications.publish(ctx, publishing("collection-1")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	both.ActiveID = "env-2"
	publications.environments = fakeEnvironments{state: both}

	preview, err := publications.Preview(ctx, "space-1", "collection-1", "", nil)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.Behind || len(preview.Changes) != 0 {
		t.Errorf("preview = %+v, want the page as it went out", preview)
	}
}

// The word a window draws on a page's button comes with the requests behind it: whoever opens the
// popover is told which ones moved, so that "Changes · 1" is one they can read before publishing.
func TestAPageSaysWhichRequestsMovedAtIt(t *testing.T) {
	store := newStore(t)
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: catalogue()}
	ctx := context.Background()

	if _, err := publications.publish(ctx, publishing("collection-1")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	moved := catalogue()
	moved.Items[0].Name = "List all products"
	publications.collections = fakeCollections{tree: moved}

	change, err := publications.Changes(ctx, "space-1", "collection-1")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	want := domain.PageItem{Sign: signChanged, Method: "GET", Name: "List all products", Fields: []string{"name"}}
	if !change.Behind || len(change.Changes) != 1 || !reflect.DeepEqual(change.Changes[0], want) {
		t.Errorf("change = %+v, want the one request that moved", change)
	}
}

// The popover opens without a network: what it draws is the report, the environments and what this
// device already holds, and none of it is the server's to answer.
func TestThePopoverOpensWithoutAskingTheServer(t *testing.T) {
	store := newStore(t)
	server := &fakePublicationServer{err: errors.New("the network is not here")}
	publications := newPublications(store, server, publicationCaller{})

	preview, err := publications.Preview(context.Background(), "space-1", "collection-1", "env-1", nil)
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
// pressing "Update" is what the page will say moved at it, worded from this device's own outline.
func TestThePopoverOpensOnWhatAPublishWouldRecord(t *testing.T) {
	store := newStore(t)
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: catalogue()}
	ctx := context.Background()

	if _, err := publications.publish(ctx, publishing("collection-1")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	moved := catalogue()
	moved.Items[0].Name = "List all products"
	publications.collections = fakeCollections{tree: moved}

	preview, err := publications.Preview(ctx, "space-1", "collection-1", "env-1", nil)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	snapshot, _, err := publications.Build(ctx, "space-1", "collection-1", "env-1", "")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(snapshot.Changes) != 2 {
		t.Fatalf("changes = %+v, want the one release", snapshot.Changes)
	}
	recorded := snapshot.Changes[0].Items
	want := domain.PageItem{
		Sign: recorded[0].Sign, Method: recorded[0].Method, Name: recorded[0].Name, Fields: recorded[0].Fields,
	}
	if len(preview.Changes) != len(recorded) || !reflect.DeepEqual(preview.Changes[0], want) {
		t.Errorf("preview = %+v, want the entry a publish would record: %+v",
			preview.Changes, recorded)
	}
}

// A name is a handle a reader clicks, so one too long to draw is refused rather than cut in half.
func TestARequestTooLongToNameIsRefused(t *testing.T) {
	tree := published()
	tree.Items[0].Name = strings.Repeat("n", maxNameRunes+1)
	publications := newPublications(newStore(t), &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tree}

	_, _, err := publications.Build(context.Background(), "space-1", "collection-1", "env-1", "en")

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
	publications := newPublications(newStore(t), &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tree}

	_, _, err := publications.Build(context.Background(), "space-1", "collection-1", "env-1", "en")

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
	publications := newPublications(newStore(t), &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tree}

	_, _, err := publications.Build(context.Background(), "space-1", "collection-1", "env-1", "en")

	if domain.CodeOf(err) != domain.CodeNameTooLong {
		t.Fatalf("err = %v, want the name refused as it would be drawn", err)
	}
}

// A description too long to draw is cut and noted instead: losing the tail of one is not a reason
// to lose a collection, and the window says which request it was about.
func TestALongDescriptionIsCutAndNoted(t *testing.T) {
	tree := published()
	tree.Items[0].Description = strings.Repeat("d", maxTextRunes+100)
	publications := newPublications(newStore(t), &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tree}

	snapshot, report, err := publications.Build(context.Background(), "space-1", "collection-1", "env-1", "en")
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

func TestChangeDetailsIdentifyDescriptionsParametersAndResponseStatuses(t *testing.T) {
	before := snapshotv1.Snapshot{Collection: snapshotv1.Collection{ID: "collection", Name: "Catalog",
		Groups: []snapshotv1.Group{{ID: "collection", Endpoints: []snapshotv1.Endpoint{{ID: "request", Method: "GET", Name: "List products",
			Parameters: []snapshotv1.Parameter{{In: "query", Name: "per_page", Example: "20"}},
			Responses:  []snapshotv1.Response{{Status: 200, Name: "OK", Body: "[]"}},
		}}}}}}
	old, err := outlineOf(before)
	if err != nil {
		t.Fatal(err)
	}
	after := before
	endpoint := &after.Collection.Groups[0].Endpoints[0]
	endpoint.Description = "Pagination details."
	endpoint.Parameters[0].Example = "100"
	endpoint.Responses = append(endpoint.Responses, snapshotv1.Response{Status: 422, Name: "Invalid input", Body: "{}"})
	next, err := outlineOf(after)
	if err != nil {
		t.Fatal(err)
	}
	changes, cut := moved(old, next)
	want := []string{"description", "parameter:query:per_page", "response:422:Invalid input"}
	if cut || len(changes) != 1 || !reflect.DeepEqual(changes[0].Fields, want) {
		t.Fatalf("changes = %+v, cut = %t", changes, cut)
	}
}

func TestRestorationKeepsTheEditorTreeAndUpdatesOnlyThePublishedBaseline(t *testing.T) {
	store, server := newStore(t), &fakePublicationServer{}
	u := newPublications(store, server, publicationCaller{})
	u.collections = fakeCollections{tree: catalogue()}
	put(t, u, "collection-1")
	snapshot, _, err := u.Build(t.Context(), "space-1", "collection-1", "env-1", "en")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Collection.Groups[0].Endpoints[0].Description = "Earlier published description."
	packed, hash, err := Encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	server.answered = domain.Publication{ID: "publication-1", CollectionID: "collection-1", ContentHash: hash, Revision: 3, Status: domain.PublicationActive}
	server.restoredDocument = packed
	if _, err := u.Restore(t.Context(), "space-1", "collection-1", 1); err != nil {
		t.Fatal(err)
	}
	change, err := u.Changes(t.Context(), "space-1", "collection-1")
	if err != nil || !change.Behind || change.Page.Revision != 3 {
		t.Fatalf("restored baseline = %+v, %v", change, err)
	}
	server.answered.ContentHash = "wrong-hash"
	if _, err := u.Restore(t.Context(), "space-1", "collection-1", 1); err == nil {
		t.Fatal("accepted a mismatched restored document")
	}
}

func TestPublicationOptionsFilterTheDocumentAndPersistAfterRefresh(t *testing.T) {
	store := newStore(t)
	server := &fakePublicationServer{}
	publications := newPublications(store, server, publicationCaller{})
	tree := published()
	tree.Description = "Overview.\n\n## Errors\nInvalid request."
	tree.Items[0].Description = "List every product."
	publications.collections = fakeCollections{tree: tree}
	options := domain.PublicationOptions{Docs: false, Examples: false}
	if _, err := publications.publish(context.Background(), PublishRequest{
		Workspace:     "space-1",
		Collection:    "collection-1",
		EnvironmentID: "env-1",
		Visibility:    domain.VisibilityUnlisted,
		Locale:        "en",
		Confirmed:     true,
		Options:       &options,
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := publications.Preview(context.Background(), "space-1", "collection-1", "env-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Options != options {
		t.Fatalf("options = %+v, want %+v", preview.Options, options)
	}
	snapshot, _, err := publications.Build(context.Background(), "space-1", "collection-1", "env-1", "en")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Collection.Description != "" || snapshot.Prose.Lead != "" {
		t.Fatalf("hidden page content survived: %+v", snapshot)
	}
	for _, group := range snapshot.Collection.Groups {
		for _, endpoint := range group.Endpoints {
			if endpoint.Description != "" || len(endpoint.Responses) != 0 {
				t.Errorf("hidden endpoint content survived: %+v", endpoint)
			}
		}
	}
}

type fakePasswords struct {
	value string
	err   error
}

func (f *fakePasswords) PublicationPassword(domain.Account, string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.value == "" {
		return "", domain.ErrNotFound
	}
	return f.value, nil
}

func (f *fakePasswords) SetPublicationPassword(_ domain.Account, _, password string) error {
	if f.err != nil {
		return f.err
	}
	f.value = password
	return nil
}

func (f *fakePasswords) DeletePublicationPassword(domain.Account, string) error {
	if f.err != nil {
		return f.err
	}
	f.value = ""
	return nil
}

func TestPublicationPasswordsAreExplicitLocalSecrets(t *testing.T) {
	page := domain.Publication{ID: "page", CollectionID: "collection", CanManage: true,
		Status: domain.PublicationActive, Visibility: domain.VisibilityPassword}
	store := newStore(t, page)
	secrets := &fakePasswords{}
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	publications.passwords = secrets
	if err := publications.rememberPassword(t.Context(), "collection", "saved-password", ""); err != nil {
		t.Fatal(err)
	}
	if password, err := publications.Password(t.Context(), "collection"); err != nil || password != "saved-password" {
		t.Fatalf("saved password unavailable: %v", err)
	}
	if !store.untouched() {
		t.Fatal("password was written to the publication database")
	}
	if err := publications.rememberPassword(t.Context(), "collection", "replacement-password", "other-session"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("account switch allowed credential saving: %v", err)
	}
	page.CanManage = false
	store.put(page)
	if _, err := publications.Password(t.Context(), "collection"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("reader could reveal the password: %v", err)
	}
	page.CanManage, page.Visibility = true, domain.VisibilityPublic
	store.put(page)
	if err := publications.rememberPassword(t.Context(), "collection", "", ""); err != nil {
		t.Fatal(err)
	}
	if secrets.value != "" {
		t.Fatal("removing password protection retained the credential")
	}
}

func TestUnavailablePasswordStoreHasNoDatabaseFallback(t *testing.T) {
	store := newStore(t, domain.Publication{ID: "page", CollectionID: "collection", CanManage: true,
		Status: domain.PublicationActive, Visibility: domain.VisibilityPassword})
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	publications.passwords = &fakePasswords{err: errors.New("keyring unavailable")}
	if err := publications.rememberPassword(t.Context(), "collection", "saved-password", ""); err == nil {
		t.Fatal("unavailable keyring reported a saved password")
	}
	if _, err := publications.Password(t.Context(), "collection"); err == nil {
		t.Fatal("unavailable keyring was ignored")
	}
	if !store.untouched() {
		t.Fatal("password fell back to the database")
	}
}

func TestDescriptionsTrackCollectionsAndNestedFolders(t *testing.T) {
	publications := newPublications(newStore(t), &fakePublicationServer{}, publicationCaller{})
	tree := tiers()
	tree.Description = "Original overview."
	tree.Children[0].Description = "Order documentation."
	tree.Children[0].Children[0].Description = "Archive documentation."
	tree.Children[0].Children[0].Items = nil
	publications.collections = fakeCollections{tree: tree}
	put(t, publications, tree.ID)

	tree.Description = "Updated overview."
	tree.Children[0].Description = "Updated order documentation."
	tree.Children[0].Children[0].Description = "Updated archive documentation."
	publications.collections = fakeCollections{tree: tree}
	for _, test := range []struct {
		id    string
		count int
	}{{"collection-1", 3}, {"folder-1", 2}, {"folder-2", 1}} {
		t.Run(test.id, func(t *testing.T) {
			change, err := publications.Changes(context.Background(), "space-1", test.id)
			if err != nil {
				t.Fatal(err)
			}
			if !change.Behind || len(change.Changes) != test.count {
				t.Fatalf("change = %+v, want %d description changes", change, test.count)
			}
			for _, item := range change.Changes {
				if item.Method != "" || item.Sign != signChanged {
					t.Errorf("description change = %+v, want an edited folder", item)
				}
			}
		})
	}

	snapshot, _, err := publications.Build(context.Background(), "space-1", tree.ID, "env-1", "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Collection.Groups) != 3 || snapshot.Collection.Groups[0].Description != "" ||
		snapshot.Collection.Groups[1].Description != tree.Children[0].Description ||
		snapshot.Collection.Groups[2].Description != tree.Children[0].Children[0].Description {
		t.Fatalf("groups = %+v, want descriptions including the folder without requests", snapshot.Collection.Groups)
	}
	packed, _, err := Encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	document, err := AsDocument(packed)
	if err != nil {
		t.Fatal(err)
	}
	if document.Collections[0].Children[1].Description != tree.Children[0].Children[0].Description {
		t.Fatal("import discarded the description of a folder without requests")
	}
}

func TestHiddenFolderDescriptionsDoNotChangeThePublication(t *testing.T) {
	publications := newPublications(newStore(t), &fakePublicationServer{}, publicationCaller{})
	tree := tiers()
	tree.Children[0].Children[0].Items = nil
	publications.collections = fakeCollections{tree: tree}
	options := domain.PublicationOptions{Docs: false, Examples: true}
	if _, err := publications.publish(context.Background(), PublishRequest{
		Workspace:     "space-1",
		Collection:    tree.ID,
		EnvironmentID: "env-1",
		Visibility:    domain.VisibilityUnlisted,
		Locale:        "en",
		Confirmed:     true,
		Options:       &options,
	}); err != nil {
		t.Fatal(err)
	}
	tree.Children[0].Children[0].Description = "Private draft documentation."
	publications.collections = fakeCollections{tree: tree}
	change, err := publications.Changes(context.Background(), "space-1", tree.ID)
	if err != nil {
		t.Fatal(err)
	}
	if change.Behind || len(change.Changes) != 0 {
		t.Fatalf("hidden description changed the publication: %+v", change)
	}
}

func TestImportKeepsAnEquallyNamedChildFolder(t *testing.T) {
	publications := newPublications(newStore(t), &fakePublicationServer{}, publicationCaller{})
	tree := tiers()
	tree.Items = nil
	tree.Children[0].Name = tree.Name
	tree.Children[0].Description = "Child folder documentation."
	publications.collections = fakeCollections{tree: tree}
	snapshot, _, err := publications.Build(context.Background(), "space-1", tree.ID, "env-1", "en")
	if err != nil {
		t.Fatal(err)
	}
	imported := readCollection(snapshot)
	if len(imported.Items) != 0 || len(imported.Children) != 2 ||
		imported.Children[0].Description != tree.Children[0].Description {
		t.Fatalf("imported = %+v, want the equally named child folder preserved", imported)
	}
}

// tiers is a collection of one request with a folder inside it, and a folder inside that: the least
// a page can hold beyond the collection's own requests, and so the least a folder of a page can be
// read against.
func tiers() domain.Collection {
	return domain.Collection{
		ID:   "collection-1",
		Name: "Example API",
		Items: []domain.CollectionNode{
			{ID: "node-1", Name: "List products", Method: "GET", URL: "{{baseUrl}}/v1/products"},
		},
		Children: []domain.Collection{{
			ID:   "folder-1",
			Name: "Orders",
			Items: []domain.CollectionNode{
				{ID: "node-2", Name: "List orders", Method: "GET", URL: "{{baseUrl}}/v1/orders"},
			},
			Children: []domain.Collection{{
				ID:   "folder-2",
				Name: "Archive",
				Items: []domain.CollectionNode{
					{ID: "node-3", Name: "List archived", Method: "GET",
						URL: "{{baseUrl}}/v1/orders/archive"},
				},
			}},
		}},
	}
}

// put sends the collection out, which is the start of every scenario here: a folder is only a
// question at all once something above it is up.
func put(t *testing.T, publications *Publications, collectionID string) {
	t.Helper()
	if _, err := publications.publish(context.Background(), publishing(collectionID)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

// A page is a whole tree, so a folder inside a published one is published by it: what the window
// asks about the folder is the page above it, said to be above it, and the folder has nothing of
// its own to publish into.
func TestAFolderInsideAPublishedOneAnswersAtThePageThatHoldsIt(t *testing.T) {
	store := newStore(t)
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tiers()}
	put(t, publications, "collection-1")

	change, err := publications.Changes(context.Background(), "space-1", "folder-2")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}

	if change.Page == nil || change.Page.CollectionID != "collection-1" {
		t.Fatalf("change = %+v, want the row of the page that holds the folder", change)
	}
	if change.InheritedFrom != "Example API" {
		t.Errorf("inheritedFrom = %q, want the folder whose page this is", change.InheritedFrom)
	}
	if change.Behind || len(change.Changes) != 0 {
		t.Errorf("change = %+v, want a page that just went up and nothing moved at it", change)
	}
}

// A folder's list is the requests a reader of that folder sees and not the ones beside it: a page
// holds the whole tree, and a folder that showed the page's whole history would be claiming its
// siblings' work as its own.
func TestAFolderIsToldOfItsOwnRequestsAlone(t *testing.T) {
	store := newStore(t)
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tiers()}
	ctx := context.Background()
	put(t, publications, "collection-1")

	moved := tiers()
	moved.Items[0].Name = "List every product"
	moved.Children[0].Items[0].Name = "List every order"
	publications.collections = fakeCollections{tree: moved}

	change, err := publications.Changes(ctx, "space-1", "folder-1")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}

	want := domain.PageItem{Sign: signChanged, Method: "GET", Name: "List every order", Fields: []string{"name"}}
	if len(change.Changes) != 1 || !reflect.DeepEqual(change.Changes[0], want) {
		t.Errorf("changes = %+v, want the folder's own request and not the page's", change.Changes)
	}
}

// Whether a page is behind is the page's question, not the folder's: the revision is the page's,
// so a folder above a request that moved is behind, while the list under it — the folder's own —
// stays empty rather than naming a change that is no business of this one.
func TestAFolderIsBehindForWhatMovedBesideItAndNamesNothing(t *testing.T) {
	store := newStore(t)
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tiers()}
	ctx := context.Background()
	put(t, publications, "collection-1")

	moved := tiers()
	moved.Items[0].Name = "List every product"
	publications.collections = fakeCollections{tree: moved}

	change, err := publications.Changes(ctx, "space-1", "folder-2")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}

	if !change.Behind {
		t.Error("behind = false, want the page said to be behind: the page is what the revision is")
	}
	if len(change.Changes) != 0 {
		t.Errorf("changes = %+v, want nothing of the folder's own to have moved", change.Changes)
	}
}

// A folder does not publish the levels above it: a page is a whole tree read downwards, so a folder
// nobody put up is its own collection still, and the folder that holds it is not one it can be said
// to be inside of.
func TestAParentIsNotPublishedByTheFolderUnderIt(t *testing.T) {
	store := newStore(t)
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tiers()}
	put(t, publications, "folder-2")

	change, err := publications.Changes(context.Background(), "space-1", "folder-1")
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}

	if change.Page != nil || change.InheritedFrom != "" {
		t.Errorf("change = %+v, want a collection past a page rather than inside one", change)
	}
}

// A publish from a folder carries the page that holds it, and carries the whole page: the names a
// reader has are the page's own, so a document built from the folder alone would be a page whose
// every request went missing, and a second page claiming the same collection.
func TestAPublishFromAFolderCarriesThePageThatHoldsIt(t *testing.T) {
	store := newStore(t)
	server := &fakePublicationServer{}
	publications := newPublications(store, server, publicationCaller{})
	publications.collections = fakeCollections{tree: tiers()}
	ctx := context.Background()
	put(t, publications, "collection-1")
	server.sent = nil

	held, err := publications.publish(ctx, publishing("folder-2"))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(server.sent) != 1 || server.sent[0].Collection != "collection-1" {
		t.Fatalf("sent %+v, want the page that holds the folder", server.sent)
	}
	if held.CollectionID != "collection-1" {
		t.Errorf("published %q, want the page the folder sits in", held.CollectionID)
	}
	if len(store.held()) != 1 || store.held()[0].CollectionID != "collection-1" {
		t.Errorf("kept %+v, want one row, of the page", store.held())
	}
	carried := expanded(t, server.sent[0].Document)
	for _, node := range []string{"node-1", "node-2", "node-3"} {
		if !strings.Contains(carried, PageID("collection-1", node)) {
			t.Errorf("the document leaves out %q, want the whole tree carried", node)
		}
	}
}

// The popover a folder opens is the page's own with the folder's changes under it: what an author
// reads there is the page they would be updating, and the folder it is of is named rather than left
// to be mistaken for a page of the folder. The collection the page is of says no such thing.
func TestAFoldersPopoverIsThePagesWithTheFoldersChanges(t *testing.T) {
	store := newStore(t)
	publications := newPublications(store, &fakePublicationServer{}, publicationCaller{})
	publications.collections = fakeCollections{tree: tiers()}
	ctx := context.Background()
	put(t, publications, "collection-1")

	moved := tiers()
	moved.Children[0].Items[0].Name = "List every order"
	publications.collections = fakeCollections{tree: moved}

	folder, err := publications.Preview(ctx, "space-1", "folder-1", "env-1", nil)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if folder.Collection != "collection-1" || folder.InheritedFrom != "Example API" {
		t.Errorf("preview = %+v, want the page that holds the folder, named", folder)
	}
	if folder.Publication == nil || !folder.Behind {
		t.Errorf("preview = %+v, want the page that went up and is behind", folder)
	}
	want := domain.PageItem{Sign: signChanged, Method: "GET", Name: "List every order", Fields: []string{"name"}}
	if len(folder.Changes) != 1 || !reflect.DeepEqual(folder.Changes[0], want) {
		t.Errorf("changes = %+v, want the folder's own request", folder.Changes)
	}

	page, err := publications.Preview(ctx, "space-1", "collection-1", "env-1", nil)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if page.InheritedFrom != "" {
		t.Errorf("inheritedFrom = %q, want a page that is of no folder above it", page.InheritedFrom)
	}
}
