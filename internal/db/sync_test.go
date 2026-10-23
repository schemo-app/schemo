package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	historyv1 "github.com/schemo-app/proto/go/history/v1"

	"schemo/internal/domain"
)

// What these tests are about is the part of the engine that only a database can be wrong about: the
// entry the outbox holds, the row read at the moment of a push, and what an answer leaves behind.
// The decisions themselves are the service's, and they are tested there without a database.

// What this package owes the engine is written down in the composition layer, where the store is
// bound to the port: this package must not know that the engine exists.

// account is the account these tests link workspaces under: the two halves the engine composes when
// it writes one, and what a test about another account changes.
const account = "https://server.test|user-me"

// linked is a workspace of this device's own that the server already knows.
func linked(t *testing.T, store *Store) domain.SyncWorkspace {
	t.Helper()

	if err := store.Link(context.Background(), ws, domain.RemoteWorkspace{
		ID: "00000000-0000-0000-0000-0000000000ff", Name: "Personal", Version: 2,
	}, account); err != nil {
		t.Fatalf("Link: %v", err)
	}
	workspaces, err := store.SyncWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	for _, workspace := range workspaces {
		if workspace.ID == ws {
			return workspace
		}
	}
	t.Fatalf("the workspace %s is not among the ones read", ws)
	return domain.SyncWorkspace{}
}

// owed reads what a workspace owes, the way the engine asks for it.
func owed(t *testing.T, store *Store, workspace domain.SyncWorkspace) []domain.PendingChange {
	t.Helper()
	changes, err := store.Owed(context.Background(), workspace, 1<<20)
	if err != nil {
		t.Fatalf("Owed: %v", err)
	}
	return changes
}

// A workspace nobody has linked has nowhere to go: what it owes waits rather than being sent into a
// workspace that does not exist.
func TestAWorkspaceIsLocalUntilItIsLinked(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)

	workspaces, err := store.SyncWorkspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("workspaces = %+v, want the one the schema writes", workspaces)
	}
	if workspaces[0].RemoteID != "" || workspaces[0].State != domain.SyncLocal {
		t.Errorf("workspace = %+v, want a local one with nowhere to go", workspaces[0])
	}

	// And nothing is pushed: an unlinked workspace has no id on the server to be pushed into.
	changes, err := store.Owed(ctx, workspaces[0], 1<<20)
	if err != nil {
		t.Fatalf("Owed: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("owed = %d changes, want none before the workspace is linked", len(changes))
	}
}

// A row this device has just made has agreed with nobody, and the version it carries is what says
// so: it is the base of the first push, and the server's answer is what replaces it.
func TestARowMadeHereCarriesNoAgreedVersion(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	err := store.SaveEnvironment(ctx, ws, domain.Environment{ID: "env-1", Name: "Local"})
	if err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	cookie := jarCookie("c1", "api.example.com", "session", "abc")
	if err := store.SaveJarCookie(ctx, ws, cookie); err != nil {
		t.Fatalf("SaveJarCookie: %v", err)
	}
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{}, domain.Variable{
		ID: "g1", Name: "page_size", Value: "10", Kind: domain.VariableText, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}

	// Every table that carries a version, asked the way each of them is keyed: the workspace's own row is
	// the one thing in the schema that is not reached through the workspace it belongs to.
	versions := []struct {
		table string
		query string
		args  []any
	}{
		{"workspaces", `SELECT version FROM workspaces WHERE id = ?`, []any{ws}},
		{"entities", `SELECT max(version) FROM entities WHERE workspace_id = ?`, []any{ws}},
	}
	for _, one := range versions {
		var version int32
		if err := store.db.QueryRowContext(ctx, one.query, one.args...).Scan(&version); err != nil {
			t.Fatalf("reading the version of %s: %v", one.table, err)
		}
		if version != 0 {
			t.Errorf("%s carries version %d, want a row nobody has agreed on", one.table, version)
		}
	}

	var version int32
	if err := store.db.QueryRowContext(ctx,
		`SELECT version FROM entities WHERE id = 'c1'`).Scan(&version); err != nil {
		t.Fatalf("reading the version of a cookie: %v", err)
	}
	if version != 0 {
		t.Errorf("a cookie carries version %d, want 0", version)
	}

	if err := store.SaveJarDomain(ctx, ws, "auth.example.com", true); err != nil {
		t.Fatalf("SaveJarDomain: %v", err)
	}
	if err := store.db.QueryRowContext(ctx,
		`SELECT version FROM entities WHERE kind = 'jar_domain' AND workspace_id = ?`, ws).Scan(&version); err != nil {
		t.Fatalf("reading the version of a domain flag: %v", err)
	}
	if version != 0 {
		t.Errorf("a domain flag carries version %d, want 0", version)
	}
}

// Linking is where a workspace that a person has been using is offered whole: nothing in it has ever
// been agreed with a server, so the server is owed all of it.
func TestLinkingOffersEverythingTheWorkspaceHolds(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	err := store.SaveEnvironment(ctx, ws, domain.Environment{ID: "env-1", Name: "Local"})
	if err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{Environment: "env-1"}, domain.Variable{
		ID: "v1", Name: "base", Value: "https://api.example.com", Kind: domain.VariableText,
		Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}
	if err := store.SaveJarDomain(ctx, ws, "auth.example.com", true); err != nil {
		t.Fatalf("SaveJarDomain: %v", err)
	}

	workspace := linked(t, store)
	if workspace.RemoteID == "" || workspace.Cursor != 0 {
		t.Errorf("workspace = %+v, want it linked and read from the beginning", workspace)
	}

	changes := owed(t, store, workspace)
	seen := map[domain.EntityType]int{}
	for _, change := range changes {
		seen[change.Entity.Kind]++
	}
	for _, want := range []struct {
		kind  domain.EntityType
		count int
	}{
		{domain.EntityWorkspace, 1},
		{domain.EntityCollection, 2},
		{domain.EntityRequest, 2},
		{domain.EntityEnvironment, 1},
		{domain.EntityVariable, 1},
		{domain.EntityJarDomain, 1},
	} {
		if seen[want.kind] != want.count {
			t.Errorf("offered %d of %s, want %d: %+v", seen[want.kind], want.kind, want.count, seen)
		}
	}

	// The workspace's own row travels under the id the server knows it by, and never under the id it has
	// here: a device that pushed its own local id would be telling the server about a workspace that does
	// not exist there.
	for _, change := range changes {
		if change.Entity.Kind == domain.EntityWorkspace && change.Entity.ID != workspace.RemoteID {
			t.Errorf("the workspace travels as %q, want the server's id %q",
				change.Entity.ID, workspace.RemoteID)
		}
	}
}

// What "a workspace somebody uses" means, which is the other half of the question about the workspace the
// app is born with: a collection or an environment. History is deliberately not counted — a workspace
// in which a page was opened is not a workspace anything was built in.
func TestAWorkspaceHoldsWhatWasBuiltInIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()

	held, err := store.Holds(ctx, ws)
	if err != nil {
		t.Fatalf("Holds: %v", err)
	}
	if held {
		t.Error("a workspace the schema has just written is holding something")
	}

	if err := store.SaveRecord(ctx, ws, domain.Record{RecordSummary: domain.RecordSummary{
		ID: "rec-1", Source: domain.SourceBrowser, Method: "GET",
		URL: "https://api.example.com/users",
	}}); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	held, err = store.Holds(ctx, ws)
	if err != nil {
		t.Fatalf("Holds: %v", err)
	}
	if held {
		t.Error("a workspace somebody browsed in counts as one they built something in")
	}

	seedTree(t, store)
	held, err = store.Holds(ctx, ws)
	if err != nil {
		t.Fatalf("Holds: %v", err)
	}
	if !held {
		t.Error("a workspace with a collection in it is holding nothing")
	}
}

// A workspace of the account this device has never had: a row of its own here, and nothing owed.
// Offering it back would be telling the server what it already knows, and the two sides would hand
// one workspace to each other forever.
func TestAMirroredWorkspaceIsOneOfTheAccountsAndOwesNothing(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	server := domain.RemoteWorkspace{
		ID: "00000000-0000-0000-0000-0000000000aa", Name: "Payments", Colour: "blue", Version: 3,
		Access: domain.WorkspaceAccess{RoleName: "viewer"},
	}
	if err := store.Mirror(ctx, server, account); err != nil {
		t.Fatalf("Mirror: %v", err)
	}

	var (
		id, kind, colour string
		version          int32
	)
	err := store.db.QueryRowContext(ctx,
		`SELECT id, kind, color, version FROM workspaces WHERE remote_workspace_id = ?`,
		server.ID).Scan(&id, &kind, &colour, &version)
	if err != nil {
		t.Fatalf("reading the mirrored row: %v", err)
	}
	if id == ws || id == "" {
		t.Errorf("id = %q, want one of this device's own", id)
	}
	// The account's list answers a name and a colour and nothing about what a workspace is for: that
	// travels in the workspace's own row, and the pull that reads it is a moment away. Until then the
	// workspace wears the kind this app is born with, which is not a claim about who is in it.
	if kind != string(domain.WorkspacePersonal) || colour != "blue" {
		t.Errorf("kind = %q, colour = %q, want the account's own name and the default kind",
			kind, colour)
	}
	if version != server.Version {
		t.Errorf("version = %d, want the server's %d: it is the base of the next edit here",
			version, server.Version)
	}

	var queued int
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue WHERE workspace_id = ?`, id).Scan(&queued); err != nil {
		t.Fatalf("counting the outbox: %v", err)
	}
	if queued != 0 {
		t.Errorf("queued = %d, want nothing: a mirrored workspace is not this device's change", queued)
	}

	workspaces, err := store.SyncWorkspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	for _, workspace := range workspaces {
		if workspace.ID != id {
			continue
		}
		if workspace.State != domain.SyncLocal || workspace.Cursor != 0 {
			t.Errorf("workspace = %+v, want it read from the beginning", workspace)
		}
		if len(owed(t, store, workspace)) != 0 {
			t.Error("a workspace that has just arrived already owes something")
		}
		// Taken as it is, which includes what the account said this person may do in it: a workspace
		// written without its role would be drawn as one they may edit until the next round says
		// otherwise, and the edit would come back refused.
		if !workspace.ReadOnly {
			t.Errorf("workspace = %+v, want the viewer's role the account gave with the row", workspace)
		}
	}
}

// A workspace that has just arrived waits for its first pull, and stops waiting when the cursor moves:
// until then it looks empty, and nothing is to be made in it.
func TestAMirroredWorkspaceAwaitsItsFirstPull(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	server := domain.RemoteWorkspace{
		ID: "00000000-0000-0000-0000-0000000000ab", Name: "Payments", Version: 1,
	}
	if err := store.Mirror(ctx, server, account); err != nil {
		t.Fatalf("Mirror: %v", err)
	}
	var id string
	if err := store.db.QueryRowContext(ctx,
		`SELECT id FROM workspaces WHERE remote_workspace_id = ?`, server.ID).Scan(&id); err != nil {
		t.Fatalf("reading the mirrored row: %v", err)
	}

	if awaiting, err := store.AwaitsFirstPull(ctx, id); err != nil || !awaiting {
		t.Fatalf("awaiting = %v (%v), want a workspace nobody has pulled to be waiting", awaiting, err)
	}
	if awaiting, err := store.AwaitsFirstPull(ctx, ws); err != nil || awaiting {
		t.Errorf("awaiting = %v (%v), want a workspace of this device's own not to wait", awaiting, err)
	}

	if _, err := store.ApplyPage(ctx, id, nil, 5); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}
	if awaiting, err := store.AwaitsFirstPull(ctx, id); err != nil || awaiting {
		t.Errorf("awaiting = %v (%v), want it read once the cursor has moved", awaiting, err)
	}
}

// One workspace of the account is one row here, however many rounds ask for it.
func TestMirroringTheSameWorkspaceTwiceMakesOneRow(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	server := domain.RemoteWorkspace{
		ID: "00000000-0000-0000-0000-0000000000aa", Name: "Payments", Version: 3,
	}

	for turn := 0; turn < 2; turn++ {
		if err := store.Mirror(ctx, server, account); err != nil {
			t.Fatalf("Mirror: %v", err)
		}
	}

	var rows int
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM workspaces WHERE remote_workspace_id = ?`, server.ID).
		Scan(&rows); err != nil {
		t.Fatalf("counting the rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("rows = %d, want one", rows)
	}
}

// The workspace's own row arriving from another device: the name and the colour it was given there.
func TestAWorkspaceFromElsewhereTakesItsNameAndColour(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)

	changes := []domain.SyncChange{{
		Seq: 61,
		Entity: domain.SyncEntity{
			Kind: domain.EntityWorkspace, ID: workspace.RemoteID, Version: 4,
			Name: "Payments", Data: jsonOf(t, map[string]string{"color": "purple"}),
		},
	}}
	if _, err := store.ApplyPage(ctx, ws, changes, 61); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	held, err := store.Workspace(ctx, ws)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	if held.Name != "Payments" || held.Color != "purple" {
		t.Errorf("workspace = %+v, want what another device called it", held)
	}
}

// What a workspace is for does not travel with its row. It decides who may be in the workspace, so it is
// the server's word and not a device's: the row a push carries holds a name and a colour, and a
// device that read a kind out of one would be taking a second opinion about who may be let in.
func TestWhatAWorkspaceIsForDoesNotTravelWithItsRow(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	if _, err := store.ApplyPage(ctx, ws, []domain.SyncChange{{
		Seq: 62,
		Entity: domain.SyncEntity{
			Kind: domain.EntityWorkspace, ID: workspace.RemoteID, Version: 4,
			Name: "Tests",
		},
	}}, 62); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	if err := store.SaveWorkspace(ctx, domain.Workspace{
		ID: ws, Name: "Tests", Kind: domain.WorkspaceTeam, UpdatedAt: now(),
	}); err != nil {
		t.Fatalf("SaveWorkspace: %v", err)
	}

	if name := owedEntity(t, store, workspace, domain.EntityWorkspace, "").Name; name != "Tests" {
		t.Errorf("name = %q, want the name the workspace was given here", name)
	}
}

// The word the account gives is the one this row wears, and it is written by the round that read it
// rather than by a pull: what a workspace is for is answered in the account's list and not in the log.
func TestAWorkspaceWearsTheKindTheAccountGivesIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)

	if held, err := store.Workspace(ctx, ws); err != nil {
		t.Fatalf("Workspace: %v", err)
	} else if held.Kind != domain.WorkspacePersonal {
		t.Fatalf("kind = %q, want a linked workspace to start somebody's own", held.Kind)
	}

	if _, err := store.Adopt(ctx, workspace.ID, domain.RemoteWorkspace{
		ID: workspace.RemoteID, Kind: domain.WorkspaceTeam,
	}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	held, err := store.Workspace(ctx, ws)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	if held.Kind != domain.WorkspaceTeam {
		t.Errorf("kind = %q, want the word the account gave", held.Kind)
	}
}

// The account's say about a place in a workspace is kept with the workspace and read back with it: the
// role and the three acts the window draws a control for. It is kept rather than asked for on every
// draw — the sheet showing a workspace is opened from a list the round already read — and the answer
// of Adopt says whether it moved, which is what the windows redraw on.
func TestWhatTheAccountSaysAboutAWorkspaceIsKeptWithIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)

	said := domain.WorkspaceAccess{RoleName: "editor", Write: true}
	remote := domain.RemoteWorkspace{ID: workspace.RemoteID, Kind: domain.WorkspaceTeam, Access: said}
	moved, err := store.Adopt(ctx, workspace.ID, remote)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if !moved {
		t.Error("a new reading did not move the row")
	}

	held, err := store.Workspace(ctx, ws)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	if held.Access != said {
		t.Errorf("access = %+v, want what the account said %+v, refusals included", held.Access, said)
	}

	workspaces, err := store.SyncWorkspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if workspaces[0].ReadOnly {
		t.Errorf("workspace = %+v, want a role that writes to leave the workspace writable", workspaces[0])
	}

	if moved, err := store.Adopt(ctx, workspace.ID, remote); err != nil || moved {
		t.Errorf("Adopt of the same reading = %v, %v, want nothing moved", moved, err)
	}
}

// A workspace nobody answered about keeps saying nothing: a column with no reading in it is a workspace
// this device holds alone, and every act in it is the person's. It is written as nothing rather
// than as an answer of "may do none of it", which a window would draw as a workspace they cannot use.
func TestAWorkspaceNobodySaidAnythingAboutKeepsSayingNothing(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)

	held, err := store.Workspace(ctx, ws)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	if held.Access != domain.OwnAccess() {
		t.Errorf("access = %+v, want a workspace nobody described to be the person's own", held.Access)
	}

	var raw string
	if err := store.db.QueryRowContext(ctx,
		`SELECT access_json FROM workspaces WHERE id = ?`, workspace.ID).Scan(&raw); err != nil {
		t.Fatalf("reading the column: %v", err)
	}
	if raw != "" {
		t.Errorf("access_json = %q, want the nothing the account said", raw)
	}
}

// A reader's workspace is not started over with everything offered: the round does not push there, so
// what was queued would wait for ever. The cursor still goes back to the start, since the log is
// read from it either way.
func TestAReadersWorkspaceIsStartedOverWithNothingOffered(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)
	if _, err := store.Adopt(ctx, workspace.ID, domain.RemoteWorkspace{
		ID: workspace.RemoteID, Kind: domain.WorkspaceTeam,
		Access: domain.WorkspaceAccess{RoleName: "viewer"},
	}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if _, err := store.ApplyPage(ctx, ws, nil, 40); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	if err := store.Reset(ctx, ws); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	var queued int
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue WHERE workspace_id = ?`, ws).Scan(&queued); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if queued != 0 {
		t.Errorf("%d entries were queued for a workspace the person may not write in", queued)
	}
	workspaces, err := store.SyncWorkspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if workspaces[0].Cursor != 0 {
		t.Errorf("cursor = %d, want the log read from the beginning", workspaces[0].Cursor)
	}
}

// A workspace let go of by its account is nobody else's any more, and what the account last said about
// it goes with the link: a stale reader's role would draw a workspace this person made as one they may
// not write in.
func TestADetachedWorkspaceForgetsWhatTheAccountSaidOfIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)
	if _, err := store.Adopt(ctx, workspace.ID, domain.RemoteWorkspace{
		ID: workspace.RemoteID, Kind: domain.WorkspaceTeam,
		Access: domain.WorkspaceAccess{RoleName: "viewer"},
	}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	if err := store.Rebind(ctx, account, "someone-else"); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	held, err := store.Workspace(ctx, ws)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	if held.Access != domain.OwnAccess() {
		t.Errorf("access = %+v, want a workspace nobody answers for to be the person's own", held.Access)
	}
}

// A word this build does not have is the personal one: the column holds two, and a row from a build
// that knows a third would otherwise be refused by the schema and take the whole answer with it.
func TestAWorkspaceGivenAWordThisBuildDoesNotHaveIsSomebodysOwn(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)

	if _, err := store.Adopt(ctx, workspace.ID, domain.RemoteWorkspace{
		ID: workspace.RemoteID, Kind: domain.WorkspaceKind("shared"),
	}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	held, err := store.Workspace(ctx, ws)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	if held.Kind != domain.WorkspacePersonal {
		t.Errorf("kind = %q, want the one this build knows", held.Kind)
	}
}

// A workspace the server made itself carries no payload, and what it is called comes from the account's
// list rather than from the log: writing the emptiness over the name would leave a row nobody can
// tell apart from the workspace the app is born with.
func TestAWorkspaceTheServerMadeKeepsItsNameHere(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)

	changes := []domain.SyncChange{{
		Seq: 63,
		Entity: domain.SyncEntity{
			Kind: domain.EntityWorkspace, ID: workspace.RemoteID, Version: 4,
		},
	}}
	if _, err := store.ApplyPage(ctx, ws, changes, 63); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	held, err := store.Workspace(ctx, ws)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	if held.Name != workspace.Name {
		t.Errorf("name = %q, want %q: the panel's workspace travels without a payload",
			held.Name, workspace.Name)
	}
}

// A workspace the server says is gone is written nowhere here. What is left of it is somebody's work,
// and a tombstone is not a licence to take it away: the device says the workspace is no longer in the
// account and leaves the person to decide about what is in it.
func TestAWorkspaceTheServerSaysIsGoneIsLeftAlone(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	changes := []domain.SyncChange{{
		Seq: 62,
		Entity: domain.SyncEntity{
			Kind: domain.EntityWorkspace, ID: workspace.RemoteID, Version: 5, Deleted: true,
		},
	}}
	if _, err := store.ApplyPage(ctx, ws, changes, 62); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	held, err := store.Workspace(ctx, ws)
	if err != nil {
		t.Fatalf("the workspace went from here on a tombstone: %v", err)
	}
	if held.Name != workspace.Name || held.Color != workspace.Color {
		t.Errorf("workspace = %+v, want it as it stands: %+v", held, workspace)
	}
	if _, err := oneCollection(t, store, "col-1"); err != nil {
		t.Errorf("the collections went with it: %v", err)
	}
}

// A workspace this device has named an id for and whose making the account has not confirmed: the id
// and the mark are written together, and the answer is what ends it. The id is the half that has to
// outlive the process — a round that came after a crash asks under it again, and that is the whole
// of how a lost answer is recovered rather than doubled.
func TestTheIdANamedWorkspaceIsMadeUnderOutlivesTheProcess(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.MarkLinkPending(ctx, ws, "00000000-0000-0000-0000-0000000000ab",
		account); err != nil {
		t.Fatalf("MarkLinkPending: %v", err)
	}

	workspaces, err := store.SyncWorkspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if !workspaces[0].LinkPending || workspaces[0].RemoteID != "00000000-0000-0000-0000-0000000000ab" {
		t.Errorf("workspace = %+v, want the id named and the making unconfirmed", workspaces[0])
	}

	// And the account's own answer is what ends it: the flag goes, and the id that stands there
	// afterwards is the server's rather than the one this device guessed at.
	workspace := linked(t, store)
	if workspace.LinkPending {
		t.Error("a linked workspace is still waiting for the answer to a call that has come back")
	}
	if workspace.RemoteID == "00000000-0000-0000-0000-0000000000ab" {
		t.Error("the id this device named outlived the account's own answer")
	}
}

// A collection travels with its parent and name in the envelope and its content as data.
func TestACollectionTravelsWithItsEnvelope(t *testing.T) {
	store := newMigratedStore(t)
	seedTree(t, store)

	workspace := linked(t, store)
	entity := owedEntity(t, store, workspace, domain.EntityCollection, "f-1")
	if entity.Name != "Admin" || entity.Parent != "col-1" || len(entity.Data) == 0 {
		t.Errorf("entity = %+v, want the row it was read from", entity)
	}
}

// A secret's value travels — the server is what decides who may be given it back — and the payload
// says which keys are secrets, because the schemes are this app's table and nobody else's.
func TestASecretTravelsWithTheKeysThatHaveToBeWithheld(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()

	secret := bearerAuth("s3cret")
	if err := store.SaveCollection(ctx, ws, domain.Collection{
		ID: "col-1", Name: "Users", Position: 0, Auth: &secret,
	}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	workspace := linked(t, store)
	changes := owed(t, store, workspace)
	if len(changes) == 0 {
		t.Fatal("nothing is owed")
	}

	entity := entityIn(t, changes, entityCollection, "col-1")
	if at(t, entity.Data, "/auth/fields/token") != "s3cret" {
		t.Errorf("data = %s, want the value to travel", entity.Data)
	}
	if len(entity.Secrets) != 1 || entity.Secrets[0] != "/auth/fields/token" {
		t.Errorf("secrets = %v, want the one field that is a credential", entity.Secrets)
	}
}

// The same for a variable of an environment, which is a row of its own rather than part of a
// level — and under a different id, which is the harder half: the row that arrives takes the name
// away from the row this device holds, and a value written into a fresh row would leave nothing.
func TestAWithheldEnvironmentVariableKeepsItsValue(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	scope := domain.EnvScope{}
	if err := store.SaveVariable(ctx, ws, scope, domain.Variable{
		ID: "var-1", Name: "token", Value: "s3cret", Kind: domain.VariableSecret, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}
	linked(t, store)

	changes := []domain.SyncChange{{
		Seq: 72,
		Entity: domain.SyncEntity{
			Kind: domain.EntityVariable, ID: "var-2", Version: 2, Name: "token",
			Data:    jsonOf(t, domain.Variable{Kind: domain.VariableSecret, Enabled: true}),
			Secrets: []string{"/value"}, Withheld: []string{"/value"},
		},
	}}
	if _, err := store.ApplyPage(ctx, ws, changes, 72); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	value, err := store.VariableValue(ctx, ws, "var-2")
	if err != nil {
		t.Fatalf("VariableValue: %v", err)
	}
	if value != "s3cret" {
		t.Errorf("token = %q, want the value this device held", value)
	}
}

// A secret renamed on the device that keeps its value at home arrives under a name this device does
// not know, with the id it does: the value is the one the row has, and not one looked up by a name
// that no longer finds it.
func TestAWithheldVariableRenamedElsewhereKeepsItsValue(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{}, domain.Variable{
		ID: "var-1", Name: "token", Value: "s3cret", Kind: domain.VariableSecret, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}
	linked(t, store)

	changes := []domain.SyncChange{{
		Seq: 73,
		Entity: domain.SyncEntity{
			Kind: domain.EntityVariable, ID: "var-1", Version: 2, Name: "api_token",
			Data:    jsonOf(t, domain.Variable{Kind: domain.VariableSecret, Enabled: true}),
			Secrets: []string{"/value"}, Withheld: []string{"/value"},
		},
	}}
	if _, err := store.ApplyPage(ctx, ws, changes, 73); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	value, err := store.VariableValue(ctx, ws, "var-1")
	if err != nil {
		t.Fatalf("VariableValue: %v", err)
	}
	if value != "s3cret" {
		t.Errorf("token = %q, want the value the row had under its old name", value)
	}
}

// A cookie's value is withheld the same way, and the jar is the one place that must not end up
// with a session it cannot send. The cookie arrives under an id of its own, as one made on another
// device does — the name is what the two have in common.
func TestAWithheldCookieKeepsTheValueThisDeviceHolds(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	saveJar(t, store, jarCookie("c1", "api.example.com", "session", "mine"))
	linked(t, store)

	changes := []domain.SyncChange{{
		Seq: 74,
		Entity: domain.SyncEntity{
			Kind: domain.EntityCookie, ID: "c2", Version: 2, Parent: "api.example.com", Name: "session",
			Data:    jsonOf(t, domain.JarCookie{Path: "/", Origin: domain.CookieFromResponse}),
			Secrets: []string{"/value"}, Withheld: []string{"/value"},
		},
	}}
	if _, err := store.ApplyPage(ctx, ws, changes, 74); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	cookies, err := store.Jar(ctx, ws)
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	for _, entry := range cookies {
		for _, cookie := range entry.Cookies {
			if cookie.Name != "session" {
				continue
			}
			if cookie.Value != "mine" {
				t.Errorf("session = %q, want the value this device held", cookie.Value)
			}
			return
		}
	}
	t.Error("the withheld cookie is gone rather than blank")
}

// What a push sends is the row as it stands, and an entry whose row is gone is dropped rather than
// sent: it names a thing the server has never heard of, and a delete for a row nobody has is a
// sentence about nothing.
func TestAnEntryWhoseRowIsGoneIsDroppedRatherThanSent(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	// Clearing the history really drops rows, and the queue is not touched by it — which is exactly
	// how a name with nothing behind it is left in the outbox.
	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-1", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if _, err := store.DeleteAllRecords(ctx, ws); err != nil {
		t.Fatalf("DeleteAllRecords: %v", err)
	}

	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == domain.EntityRecord {
			t.Errorf("a record whose row is gone was offered: %+v", change.Entity)
		}
	}

	var rows int
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue WHERE workspace_id = ? AND entity_type = 'record'`,
		ws).Scan(&rows); err != nil {
		t.Fatalf("counting the record entries: %v", err)
	}
	if rows != 0 {
		t.Errorf("the queue still holds %d record entries", rows)
	}
}

// A record travels with everything that hangs off it: its bodies and the reports of the code around
// it. One document, because a report whose record is missing is a report of nothing.
//
// The run that produced it is a document of its own; the record names it, and the run names the
// record back.
func TestARecordTravelsWithItsBodiesAndTheRunThatProducedIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()

	// After the link, and not before: history travels from the moment a workspace is linked, because a
	// record is what this device saw and a link that dragged a year of them along would be a button
	// that uploads gigabytes.
	workspace := linked(t, store)
	seedTree(t, store)

	rec := sampleRecord("rec-1", domain.SourceManual)
	rec.RunID = "run-1"
	if err := store.SaveRecord(ctx, ws, rec); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.QueueRecord(ctx, ws, rec.ID); err != nil {
		t.Fatalf("QueueRecord: %v", err)
	}
	if err := store.SaveScriptRun(ctx, ws, domain.ScriptRun{
		ID: "script-1", RecordID: "rec-1", Scope: domain.ScriptPost, OK: true,
		Logs:  []domain.ScriptLog{{Level: "log", Message: "hello"}},
		Tests: []domain.TestResult{{Name: "status is 200", Passed: true}},
	}); err != nil {
		t.Fatalf("SaveScriptRun: %v", err)
	}
	if err := store.SaveRun(ctx, domain.CollectionRun{
		ID: "run-1", CollectionID: "col-1", Environment: "local", StartedAt: now(),
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := store.AppendRunResult(ctx, "run-1", domain.CollectionRunResult{
		NodeID: "r-1", RecordID: "rec-1", OK: true, Status: code(200),
	}); err != nil {
		t.Fatalf("AppendRunResult: %v", err)
	}

	var recordPayload, runPayload []byte
	for _, change := range owed(t, store, workspace) {
		switch change.Entity.Kind {
		case domain.EntityRecord:
			recordPayload = change.Entity.Data
		case domain.EntityRun:
			runPayload = change.Entity.Data
		}
	}
	if recordPayload == nil {
		t.Fatal("the record is not in what the workspace owes")
	}
	if runPayload == nil {
		t.Fatal("the run is not in what the workspace owes")
	}

	var entry historyv1.RecordEntry
	if err := proto.Unmarshal(recordPayload, &entry); err != nil {
		t.Fatalf("reading the payload: %v", err)
	}
	if wireRecord(t, entry.GetRecord()).URL != "https://api.example.com/articles" {
		t.Errorf("record = %+v, want the row it was read from", entry.GetRecord())
	}
	if len(entry.GetBodies()) != 2 {
		t.Errorf("bodies = %d, want both sides", len(entry.GetBodies()))
	}
	var reports []domain.ScriptRun
	if err := json.Unmarshal(entry.GetScripts(), &reports); err != nil || len(reports) != 1 ||
		len(reports[0].Tests) != 1 {
		t.Errorf("scripts = %s, want the report with its assertion", entry.GetScripts())
	}
	if entry.GetRecord().GetRunId() != "run-1" {
		t.Errorf("run = %q, want the run the record was sent by", entry.GetRecord().GetRunId())
	}

	var run historyv1.Run
	if err := proto.Unmarshal(runPayload, &run); err != nil {
		t.Fatalf("reading the run: %v", err)
	}
	if run.GetId() != "run-1" || run.GetCollectionId() != "col-1" {
		t.Errorf("run = %+v, want the run it was read from", &run)
	}
	var rows domain.CollectionRun
	if err := json.Unmarshal(run.GetData(), &rows); err != nil || len(rows.Results) != 1 ||
		rows.Results[0].RecordID != "rec-1" {
		t.Errorf("run = %s, want the row that names the record back", run.GetData())
	}
}

// A person who keeps their secrets at home does not send them: the variable travels as the name and
// the kind it is, and the value it carries is empty — the far side keeps what it had, and a device
// that never had the secret is not handed one.
func TestASecretStaysHomeWhenThePersonSaysSo(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)
	if err := store.SaveSetting(ctx, domain.SettingPreferences,
		`{"syncScopes":{"runs":true,"secrets":false}}`); err != nil {
		t.Fatalf("SaveSetting: %v", err)
	}

	if err := store.SaveVariable(ctx, ws, domain.EnvScope{}, domain.Variable{
		ID: "v2", Name: "token", Value: "s3cret", Kind: domain.VariableSecret, Enabled: true,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}
	if err := store.SaveVariable(ctx, ws, domain.EnvScope{}, domain.Variable{
		ID: "v1", Name: "base_url", Value: "https://api.example.com", Enabled: true,
		Kind: domain.VariableText,
	}); err != nil {
		t.Fatalf("SaveVariable: %v", err)
	}

	changes := owed(t, store, workspace)
	token := entityIn(t, changes, domain.EntityVariable, "v2")
	if at(t, token.Data, "/value") != "" || len(token.Withheld) != 1 {
		t.Errorf("token = %s withheld %v, want the value left at home and said so",
			token.Data, token.Withheld)
	}
	if at(t, token.Data, "/kind") != string(domain.VariableSecret) {
		t.Errorf("token = %s, want the kind kept so the far side knows what it was not given",
			token.Data)
	}
	plain := entityIn(t, changes, domain.EntityVariable, "v1")
	if at(t, plain.Data, "/value") != "https://api.example.com" || len(plain.Withheld) != 0 {
		t.Errorf("base_url = %s, want a plain variable sent whole", plain.Data)
	}
}

// And a person who keeps their runs at home sends the request without them: the record and its
// bodies travel — what was sent and what came back — and the run with the script reports stays.
func TestARunStaysHomeWhenThePersonSaysSo(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)
	seedTree(t, store)
	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-1", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.QueueRecord(ctx, ws, "rec-1"); err != nil {
		t.Fatalf("QueueRecord: %v", err)
	}
	if err := store.SaveScriptRun(ctx, ws, domain.ScriptRun{
		ID: "script-1", RecordID: "rec-1", Scope: domain.ScriptPost, OK: true,
		Tests: []domain.TestResult{{Name: "status is 200", Passed: true}},
	}); err != nil {
		t.Fatalf("SaveScriptRun: %v", err)
	}
	if err := store.SaveRun(ctx, domain.CollectionRun{
		ID: "run-1", CollectionID: "col-1", StartedAt: now(),
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := store.SaveSetting(ctx, domain.SettingPreferences,
		`{"syncScopes":{"runs":false,"secrets":true}}`); err != nil {
		t.Fatalf("SaveSetting: %v", err)
	}

	var payload []byte
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == domain.EntityRun {
			t.Error("a run is offered to a device that keeps its runs at home")
		}
		if change.Entity.Kind == domain.EntityRecord {
			payload = change.Entity.Data
		}
	}
	if payload == nil {
		t.Fatal("the record is not in what the workspace owes")
	}

	var entry historyv1.RecordEntry
	if err := proto.Unmarshal(payload, &entry); err != nil {
		t.Fatalf("reading the payload: %v", err)
	}
	if wireRecord(t, entry.GetRecord()).URL != "https://api.example.com/articles" ||
		len(entry.GetBodies()) != 2 {
		t.Errorf("entry = %+v, want the request and its bodies either way", &entry)
	}
	if len(entry.GetScripts()) != 0 {
		t.Errorf("scripts = %v, want none: the reports of a run that stays at home stay with it",
			entry.GetScripts())
	}
}

// An answer that landed stores the version the server answered, which is the base of this device's
// next edit of that row.
func TestAStoredRowKeepsTheVersionTheServerAnswered(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	var target domain.PendingChange
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == domain.EntityCollection && change.Entity.ID == "col-1" {
			target = change
		}
	}
	if target.QueueID == "" {
		t.Fatal("the collection is not in what the workspace owes")
	}

	if err := store.Accepted(ctx, target, 7); err != nil {
		t.Fatalf("Settled: %v", err)
	}

	var version int32
	if err := store.db.QueryRowContext(ctx,
		`SELECT version FROM entities WHERE id = 'col-1'`).Scan(&version); err != nil {
		t.Fatalf("reading the version: %v", err)
	}
	if version != 7 {
		t.Errorf("version = %d, want where the row stands on the server", version)
	}

	var entries int
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue WHERE id = ?`, target.QueueID).Scan(&entries); err != nil {
		t.Fatalf("counting the entry: %v", err)
	}
	if entries != 0 {
		t.Error("an entry the server took is still in the queue")
	}
}

// An edit that lost is replaced by what the server holds, whole: the content and the version,
// because the device was holding a version that no longer exists anywhere.
func TestAnEditThatLostIsReplacedByWhatTheServerHolds(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	var target domain.PendingChange
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == domain.EntityCollection && change.Entity.ID == "col-1" {
			target = change
		}
	}

	winner := domain.SyncEntity{
		Kind: domain.EntityCollection, ID: "col-1", Version: 9, Position: 3,
		Name: "theirs", Data: jsonOf(t, map[string]string{"description": "what the server holds"}),
	}
	if err := store.Superseded(ctx, target, winner); err != nil {
		t.Fatalf("Superseded: %v", err)
	}

	collection, err := oneCollection(t, store, "col-1")
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if collection.Name != "theirs" || collection.Description != "what the server holds" {
		t.Errorf("collection = %+v, want the server's row", collection)
	}

	var version int32
	if err := store.db.QueryRowContext(ctx,
		`SELECT version FROM entities WHERE id = 'col-1'`).Scan(&version); err != nil {
		t.Fatalf("reading the version: %v", err)
	}
	if version != 9 {
		t.Errorf("version = %d, want the one the winner carries", version)
	}
}

// A page of the server's log is written and the cursor moves with it: a batch half applied with the
// cursor already past it would lose whatever came between.
func TestAPageIsAppliedAndTheCursorMovesWithIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	changes := []domain.SyncChange{
		{
			Seq: 51,
			Entity: domain.SyncEntity{
				Kind: domain.EntityCollection, ID: "col-2", Version: 1, Position: 4,
				Name: "From another device",
				Data: jsonOf(t, domain.Collection{Variables: []domain.Variable{{
					ID: "v-host", Name: "host", Value: "https://api.example.com",
					Kind: domain.VariableText, Enabled: true,
				}}}),
			},
		},
		{
			Seq:    52,
			Entity: domain.SyncEntity{Kind: domain.EntityCollection, ID: "f-1", Version: 4, Deleted: true},
		},
	}

	if _, err := store.ApplyPage(ctx, ws, changes, 52); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	collection, err := oneCollection(t, store, "col-2")
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if collection.Name != "From another device" || len(collection.Variables) != 1 {
		t.Errorf("collection = %+v, want the row and its variables", collection)
	}
	// A variable of a level keeps the id it was given where it was made: the same variable is the
	// same row on every device, and a pull does not rename it.
	if collection.Variables[0].ID != "v-host" {
		t.Errorf("variable id = %q, want the one it travelled with", collection.Variables[0].ID)
	}

	// And a deletion is a tombstone: the row stops being drawn, and nothing reads it any more.
	listed, err := store.Collections(ctx, ws)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	for _, row := range listed {
		if row.ID == "f-1" {
			t.Error("a collection the server deleted is still in the list")
		}
	}

	workspaces, err := store.SyncWorkspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if workspaces[0].Cursor != 52 {
		t.Errorf("cursor = %d, want where the page ended", workspaces[0].Cursor)
	}
}

// A cookie is one row of the jar by its domain, path and name, and the row that arrived from the
// server is the truth for a workspace: whatever held that name under another id goes.
func TestACookieThatArrivedReplacesTheOneThatHeldItsName(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	saveJar(t, store, jarCookie("c1", "api.example.com", "session", "mine"))
	linked(t, store)

	changes := []domain.SyncChange{{
		Seq: 61,
		Entity: domain.SyncEntity{
			Kind: domain.EntityCookie, ID: "c2", Version: 1, Parent: "api.example.com", Name: "session",
			Data: jsonOf(t, domain.JarCookie{Value: "theirs", Path: "/", Origin: domain.CookieFromResponse}),
		},
	}}
	if _, err := store.ApplyPage(ctx, ws, changes, 61); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	cookies, err := store.Jar(ctx, ws)
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	held := 0
	for _, entry := range cookies {
		for _, cookie := range entry.Cookies {
			if cookie.Name == "session" {
				held++
				if cookie.Value != "theirs" {
					t.Errorf("the cookie is %q, want the one the server sent", cookie.Value)
				}
			}
		}
	}
	if held != 1 {
		t.Errorf("the jar holds %d cookies called session, want one", held)
	}
}

// A round that died left entries in flight, and a start is the moment that is known: an entry
// nobody is sending goes back to being owed.
func TestWhatACrashLeftInFlightComesBack(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	changes := owed(t, store, workspace)
	if len(changes) == 0 {
		t.Fatal("nothing is owed")
	}

	var sending int
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue WHERE status = 'sending'`).Scan(&sending); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if sending != len(changes) {
		t.Errorf("%d entries are in flight of %d read", sending, len(changes))
	}

	if err := store.ReleaseInFlight(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue WHERE status = 'sending'`).Scan(&sending); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if sending != 0 {
		t.Errorf("%d entries are still in flight", sending)
	}
}

// An entry that will not go is kept where it can be seen, with what refused it: a change that
// quietly disappeared is the one failure nobody would ever notice.
func TestAParkedEntrySaysWhatRefusedIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()

	if err := store.SaveCollection(ctx, ws, domain.Collection{
		ID: "col-1", Name: "Users", Position: 0,
	}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	workspace := linked(t, store)

	changes := owed(t, store, workspace)
	if len(changes) == 0 {
		t.Fatal("nothing is owed")
	}
	if err := store.Parked(ctx, changes[0], domain.ReasonTooLarge); err != nil {
		t.Fatalf("Parked: %v", err)
	}

	waiting, err := store.Waiting(ctx, ws)
	if err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	if len(waiting) == 0 {
		t.Fatal("nothing is waiting")
	}
	for _, entry := range waiting {
		if entry.Status == domain.QueueParked {
			if entry.Reason != string(domain.ReasonTooLarge) {
				t.Errorf("reason = %q, want the word the server refused it with", entry.Reason)
			}
			return
		}
	}
	t.Errorf("no parked entry in %+v", waiting)
}

// An entry the server refuses in a way that may pass comes back later every time it is refused: a
// parent that has not arrived is a matter of minutes, and a device that asks every thirty seconds
// for an hour has asked a hundred times too often to be told the same thing. The test reads the
// moment the entry was given, and not merely that it was given one.
func TestARefusedEntryWaitsLongerEveryTime(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()

	if err := store.SaveCollection(ctx, ws, domain.Collection{
		ID: "col-1", Name: "Users", Position: 0,
	}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	workspace := linked(t, store)

	changes := owed(t, store, workspace)
	if len(changes) == 0 {
		t.Fatal("nothing is owed")
	}
	change := changes[0]

	waiting := func() int64 {
		t.Helper()
		var at int64
		if err := store.db.QueryRowContext(ctx,
			`SELECT next_retry_at FROM sync_queue WHERE id = ?`, change.QueueID).Scan(&at); err != nil {
			t.Fatalf("reading when the entry comes back: %v", err)
		}
		return at
	}

	const wait = 30 * time.Second
	first := time.Now().UnixMilli()
	if err := store.Failed(ctx, change, domain.ReasonParentMissing, wait); err != nil {
		t.Fatalf("Failed: %v", err)
	}

	// Three refusals behind it: the fourth wait is the reason's own, doubled three times.
	change.Attempts = 3
	before := time.Now().UnixMilli()
	if err := store.Failed(ctx, change, domain.ReasonParentMissing, wait); err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if at := waiting(); at < before+int64(8*wait/time.Millisecond) {
		t.Errorf("the entry comes back at %d and the first refusal was asked for %d, want the wait "+
			"to grow with the refusals", at-first, wait.Milliseconds())
	}

	// And it stops growing: a change that could still go must not be left alone for a day.
	change.Attempts = 40
	if err := store.Failed(ctx, change, domain.ReasonParentMissing, wait); err != nil {
		t.Fatalf("Failed: %v", err)
	}
	ceiling := before + int64(maxRetryWait/time.Millisecond) + int64(time.Minute/time.Millisecond)
	if at := waiting(); at > ceiling {
		t.Errorf("the entry is left for %d ms, want no more than the ceiling", at-before)
	}
}

// Starting a workspace over clears the queue and reads the server's log from the beginning: what the
// device holds is offered again, and a cursor left behind would skip whatever sits above it.
func TestStartingOverOffersEverythingAgain(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	// One round that got somewhere, and then a server that cannot answer from where this device
	// stands.
	first := owed(t, store, workspace)
	if err := store.Accepted(ctx, first[0], 5); err != nil {
		t.Fatalf("Settled: %v", err)
	}
	if _, err := store.ApplyPage(ctx, ws, nil, 40); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	if err := store.Reset(ctx, ws); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	workspaces, err := store.SyncWorkspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if workspaces[0].Cursor != 0 {
		t.Errorf("cursor = %d, want the log read from the beginning", workspaces[0].Cursor)
	}

	var failed int
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue WHERE status = 'failed'`).Scan(&failed); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if failed != 0 {
		t.Errorf("%d entries are still waiting to be retried after a reset", failed)
	}

	offered := 0
	for _, change := range owed(t, store, workspaces[0]) {
		if change.Action == domain.ActionCreate {
			offered++
		}
	}
	if offered == 0 {
		t.Error("nothing was offered again")
	}
}

// Starting over is for the rows the server already knows: they are offered again so that whatever
// the two sides disagree about can be caught up. A record, and the run that produced it, are not
// that — they are what this device has not been told about yet, and a start-over leaves them be.
func TestAStartOverKeepsWhatTheServerHasNotBeenToldAbout(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	rec := sampleRecord("rec-1", domain.SourceManual)
	rec.RunID = "run-1"
	if err := store.SaveRecord(ctx, ws, rec); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.QueueRecord(ctx, ws, rec.ID); err != nil {
		t.Fatalf("QueueRecord: %v", err)
	}
	if err := store.SaveRun(ctx, domain.CollectionRun{
		ID: "run-1", CollectionID: "col-1", Environment: "local", StartedAt: now(),
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	if err := store.Reset(ctx, ws); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	kept := map[domain.EntityType]bool{}
	for _, change := range owed(t, store, workspace) {
		kept[change.Entity.Kind] = true
	}
	if !kept[domain.EntityRecord] {
		t.Error("the record the server has never been told about was thrown away")
	}
	if !kept[domain.EntityRun] {
		t.Error("the run the server has never been told about was thrown away")
	}
}

// A batch is bounded by bytes, and what does not fit in it is not in flight. Marking the whole
// queue as sending would leave the leftovers claimed by a round that never took them, and an entry
// marked sending is the one place an entry can go and never come back from.
func TestARowLeftOutOfABatchIsStillDue(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	// A limit no second row can fit under: the first goes out because a batch is at least one row,
	// and every row behind it waits for the round after this one.
	first, err := store.Owed(ctx, workspace, 1)
	if err != nil {
		t.Fatalf("Owed: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("batch = %d rows, want the one a batch always has", len(first))
	}

	var sending, waiting int
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue WHERE status = 'sending'`).Scan(&sending); err != nil {
		t.Fatalf("counting what is in flight: %v", err)
	}
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue WHERE status = 'pending'`).Scan(&waiting); err != nil {
		t.Fatalf("counting what is due: %v", err)
	}
	if sending != 1 {
		t.Errorf("%d rows are in flight, want only the one that went out", sending)
	}
	if waiting == 0 {
		t.Error("a row left out of the batch was claimed along with it")
	}

	next, err := store.Owed(ctx, workspace, 1<<20)
	if err != nil {
		t.Fatalf("Owed: %v", err)
	}
	if len(next) == 0 {
		t.Fatal("what the batch left behind is not owed again")
	}
	for _, change := range next {
		if change.QueueID == first[0].QueueID {
			t.Error("a row already in flight was handed out a second time")
		}
	}
}

// arriving is an entity a device sent, as another device receives it under an id and version.
func arriving(sent domain.SyncEntity, id string, version int32) domain.SyncEntity {
	sent.ID, sent.Version = id, version
	if sent.CreatedAt == 0 {
		sent.CreatedAt, sent.UpdatedAt = 1, 1
	}
	return sent
}

// The columns of a record that travel are the ones the contract has, and this is what the two have
// to agree on; a field added here without one there is a field another device never sees.
func TestTheContractAndTheRecordAgreeOnTheFieldsThatTravel(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)
	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-1", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.QueueRecord(ctx, ws, "rec-1"); err != nil {
		t.Fatalf("QueueRecord: %v", err)
	}

	var payload []byte
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == domain.EntityRecord {
			payload = change.Entity.Data
		}
	}

	var entry historyv1.RecordEntry
	if err := proto.Unmarshal(payload, &entry); err != nil {
		t.Fatalf("reading the payload: %v", err)
	}
	wire := entry.GetRecord()
	record := wireRecord(t, wire)
	if record.Method != "GET" || record.Status != 200 ||
		wire.GetSource() != historyv1.RecordSource_RECORD_SOURCE_MANUAL || record.TabID != 7 {
		t.Errorf("record = %+v, want the columns it was read from", record.RecordSummary)
	}
	if wire.GetStartedAt() == nil {
		t.Error("the moment it happened did not travel")
	}
	// A phase that did not happen is absent rather than zero, and a phase that did is a number.
	if record.DNSUs == nil || *record.DNSUs != 1000 {
		t.Errorf("dns = %v, want the phase it took", record.DNSUs)
	}
	// The headers and the writes are lists of the record, and they travel as lists: two Set-Cookie
	// lines are two cookies, not one.
	if len(record.ResponseHeaders) != 3 {
		t.Errorf("response headers = %d, want all three", len(record.ResponseHeaders))
	}
	if len(record.CookieWrites) != 1 {
		t.Errorf("cookie writes = %d, want the one the answer made", len(record.CookieWrites))
	}
	if len(entry.GetBodies()) != 2 {
		t.Errorf("bodies = %d, want both sides of it", len(entry.GetBodies()))
	}
}

// oneCollection is a collection as the list answers it, so that a test about a row can read the row
// without a tree around it.
func oneCollection(t *testing.T, store *Store, id string) (domain.Collection, error) {
	t.Helper()
	listed, err := store.Collections(context.Background(), ws)
	if err != nil {
		return domain.Collection{}, err
	}
	for _, row := range listed {
		if row.ID == id {
			return row, nil
		}
	}
	return domain.Collection{}, domain.ErrNotFound
}

// A page of the other log: a record another device made, written here under the workspace this device
// knows it by, and the history cursor moved in the same transaction. Bodies are not in a page.

// remoteRecord is a record on its way in, as the contract hands one over.
func remoteRecord(id, source string) domain.Record {
	return domain.Record{
		RecordSummary: domain.RecordSummary{
			ID: id, Source: domain.RecordSource(source), Method: "GET",
			URL: "https://api.test/users", Status: 200, StatusText: "OK",
			DurationUs: 1500, StartedAt: 1_700_000_000_000,
		},
	}
}

func TestAHistoryPageIsWrittenUnderTheWorkspaceOfTheDeviceReadingIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	page := []domain.RecordChange{
		{Seq: 5, RecordID: "rec-1", Record: remoteRecord("rec-1", "manual")},
	}
	if _, err := store.ApplyHistoryPage(ctx, ws, page, 5); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}

	// The record says which workspace it was made in, and that workspace is the sender's own name for it:
	// what it is filed under here is the workspace the page was read for.
	records, err := store.Records(ctx, ws, "", 10)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 1 || records[0].ID != "rec-1" {
		t.Fatalf("records = %+v, want the one that arrived", records)
	}
	if records[0].WorkspaceID != ws {
		t.Errorf("workspace = %q, want the workspace this page was read for", records[0].WorkspaceID)
	}
	if !records[0].IsRemote {
		t.Errorf("record = %+v, want it marked as one that came from another device", records[0])
	}

	workspaces, err := store.SyncWorkspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if workspaces[0].HistoryCursor != 5 {
		t.Errorf("history cursor = %d, want the number the page read to", workspaces[0].HistoryCursor)
	}
}

func TestARecordTakenBackIsDeletedAndKeepsItsPlace(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	if _, err := store.ApplyHistoryPage(ctx, ws, []domain.RecordChange{
		{Seq: 5, RecordID: "rec-1", Record: remoteRecord("rec-1", "manual")},
	}, 5); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}
	// A deletion is the entry alone: there is no record to write, and what it means is that the one
	// this device holds is gone.
	if _, err := store.ApplyHistoryPage(ctx, ws,
		[]domain.RecordChange{{Seq: 6, RecordID: "rec-1", Deleted: true}}, 6); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}

	live, err := store.Records(ctx, ws, "", 10)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("records = %+v, want the taken-back one gone from the list", live)
	}
}

// An amendment is applied only when its number is above the copy's, so pages out of order cannot
// undo a newer one.
func TestAnAmendmentIsAppliedOnlyWhenItIsNewer(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	amended := remoteRecord("rec-1", "manual")
	amended.CookieWrites = []domain.CookieWrite{{Name: "session", State: domain.CookieAdded}}
	if _, err := store.ApplyHistoryPage(ctx, ws,
		[]domain.RecordChange{{Seq: 9, RecordID: "rec-1", Record: amended}}, 9); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}

	older := remoteRecord("rec-1", "manual")
	older.CookieWrites = []domain.CookieWrite{{Name: "stale", State: domain.CookieNotSaved}}
	if _, err := store.ApplyHistoryPage(ctx, ws,
		[]domain.RecordChange{{Seq: 7, RecordID: "rec-1", Record: older}}, 9); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}

	records, err := store.Records(ctx, ws, "", 10)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	if len(records) != 1 || len(records[0].CookieWrites) != 1 ||
		records[0].CookieWrites[0].Name != "session" {
		t.Errorf("record = %+v, want the newer amendment kept", records[0].CookieWrites)
	}
}

// An undo of a cookie write is this device's act, and it travels: the echo of a change numbered
// above the copy this device holds must not put the write back while that change has not gone out.
func TestAnEchoDoesNotPutBackALocallyUndoneCookieWrite(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	writes := []domain.CookieWrite{{Name: "session", Value: "abc", State: domain.CookieAdded}}
	theirs := remoteRecord("rec-1", "manual")
	theirs.CookieWrites = writes
	if _, err := store.ApplyHistoryPage(ctx, ws,
		[]domain.RecordChange{{Seq: 5, RecordID: "rec-1", Record: theirs}}, 5); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}

	// The undo: the panel empties the list, and this device now owes the server that change.
	if err := store.SaveCookieWrites(ctx, ws, "rec-1", nil); err != nil {
		t.Fatalf("SaveCookieWrites: %v", err)
	}

	echo := remoteRecord("rec-1", "manual")
	echo.CookieWrites = writes
	if _, err := store.ApplyHistoryPage(ctx, ws,
		[]domain.RecordChange{{Seq: 9, RecordID: "rec-1", Record: echo}}, 9); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}
	if kept := keptWrites(t, store, "rec-1"); len(kept) != 0 {
		t.Errorf("writes = %+v, want the undo to have stood against the echo", kept)
	}

	// With the change gone out, the server's copy is the newer one again and is taken.
	if _, err := store.db.ExecContext(ctx,
		`DELETE FROM sync_queue WHERE entity_type = 'record' AND entity_id = 'rec-1'`); err != nil {
		t.Fatalf("clearing the queue: %v", err)
	}
	if _, err := store.ApplyHistoryPage(ctx, ws,
		[]domain.RecordChange{{Seq: 10, RecordID: "rec-1", Record: echo}}, 10); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}
	if kept := keptWrites(t, store, "rec-1"); len(kept) != 1 || kept[0].Name != "session" {
		t.Errorf("writes = %+v, want the server's copy once nothing is owed", kept)
	}
}

// A run the log brought is written with the rows it produced and is offered to nobody: the server
// is where it came from, and a device that queued it would be answering itself.
func TestARunTheLogBroughtIsWrittenAndNotOfferedBack(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	run := domain.CollectionRun{
		ID: "run-1", CollectionID: "col-1", NodeID: "req-1", Environment: "local",
		StartedAt: 2_000, FinishedAt: 2_500, Passed: 1, Failed: 1, DurationUs: 500,
		Results: []domain.CollectionRunResult{
			{NodeID: "req-1", Position: 0, Status: code(200), OK: true, RecordID: "rec-1"},
			{NodeID: "req-2", Position: 1, Status: code(500), Error: "boom"},
		},
	}
	if _, err := store.ApplyHistoryPage(ctx, ws,
		[]domain.RecordChange{{Seq: 7, Run: &run}}, 7); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}

	held, ok, err := store.LastRun(ctx, "col-1", "req-1")
	if err != nil || !ok {
		t.Fatalf("LastRun = %+v, %v, %v, want the run the log brought", held, ok, err)
	}
	if held.Passed != 1 || held.Failed != 1 || len(held.Results) != 2 {
		t.Errorf("run = %+v, want it with the two rows it produced", held)
	}
	// A row whose status was never read is not one that came back with a zero in it, and the two
	// draw differently: the run of another device has one of each.
	if held.Results[0].Status == nil || *held.Results[0].Status != 200 {
		t.Errorf("row = %+v, want the status it came back with", held.Results[0])
	}
	if held.Results[1].Status == nil || *held.Results[1].Status != 500 {
		t.Errorf("row = %+v, want the status it came back with", held.Results[1])
	}
	for _, one := range queued(t, store, ws) {
		if one.entityType == domain.EntityRun {
			t.Errorf("the run the server sent is owed back to it: %+v", one.entry)
		}
	}
}

// A record taken back elsewhere takes its bodies and its reports with it: they are the
// record's own, and a request that no longer exists is not one whose body should stay here.
func TestARecordTakenBackCarriesItsBodiesAndReportsAway(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-1", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.SaveScriptRun(ctx, ws, domain.ScriptRun{
		ID: "sr-1", RecordID: "rec-1", Scope: domain.ScriptPost, OK: true, CreatedAt: now(),
		Logs:  []domain.ScriptLog{{Level: "log", Message: "sent"}},
		Tests: []domain.TestResult{{Name: "200", Passed: true}},
	}); err != nil {
		t.Fatalf("SaveScriptRun: %v", err)
	}

	if _, err := store.ApplyHistoryPage(ctx, ws,
		[]domain.RecordChange{{Seq: 8, RecordID: "rec-1", Deleted: true}}, 8); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}

	if _, err := store.ReadBody(ctx, "rec-1", domain.SideResponse); !errors.Is(err,
		domain.ErrNotFound) {
		t.Errorf("body = %v, want it gone with the record", err)
	}
	reports, err := store.ScriptRuns(ctx, "rec-1")
	if err != nil {
		t.Fatalf("ScriptRuns: %v", err)
	}
	if len(reports) != 0 {
		t.Errorf("reports = %+v, want them gone with the record", reports)
	}
}

// A record fetched on demand is kept with everything the fetch brought beside its body: the reports
// of the code around it and the run it was sent by are its story, and none of it is news to the
// server it all came from.
func TestAFetchedRecordIsKeptWithItsReportsAndItsRun(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	if _, err := store.ApplyHistoryPage(ctx, ws,
		[]domain.RecordChange{{Seq: 3, RecordID: "rec-1", Record: remoteRecord("rec-1", "manual")}},
		3); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}

	run := domain.CollectionRun{ID: "run-1", CollectionID: "col-1", NodeID: "req-1", StartedAt: 900}
	if err := store.SaveFetched(ctx, ws, "rec-1", domain.RecordFetched{
		Bodies: map[domain.BodySide]domain.BodyRef{
			domain.SideResponse: {Inline: `{"data":[]}`, Size: 11},
		},
		Scripts: []domain.ScriptRun{{
			ID: "sr-1", RecordID: "rec-1", Scope: domain.ScriptPost, OK: true, CreatedAt: now(),
			Logs:  []domain.ScriptLog{{Level: "log", Message: "sent"}},
			Tests: []domain.TestResult{{Name: "200", Passed: true}},
		}},
		Run: &run,
	}); err != nil {
		t.Fatalf("SaveFetched: %v", err)
	}

	body, err := store.ReadBody(ctx, "rec-1", domain.SideResponse)
	if err != nil || body != `{"data":[]}` {
		t.Errorf("body = %q, %v, want what the fetch brought", body, err)
	}
	reports, err := store.ScriptRuns(ctx, "rec-1")
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports = %+v, %v, want the one the fetch brought", reports, err)
	}
	if len(reports[0].Logs) != 1 || len(reports[0].Tests) != 1 {
		t.Errorf("report = %+v, want its lines and its assertions", reports[0])
	}
	if _, ok, err := store.LastRun(ctx, "col-1", "req-1"); err != nil || !ok {
		t.Errorf("the run the record was sent by did not land: %v, %v", ok, err)
	}
	for _, one := range queued(t, store, ws) {
		held := one.entityType == domain.EntityRun && one.entityID == "run-1"
		held = held || (one.entityType == domain.EntityRecord && one.entityID == "rec-1")
		if held {
			t.Errorf("a fetch offered %s %s back to the server", one.entityType, one.entityID)
		}
	}
}

// A row read once on demand and later named by the server's own snapshot is history this device
// holds rather than a page read once: the list draws it from the snapshot on.
func TestASnapshotTakesTheMarkOffARowItNames(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	if err := store.SaveCached(ctx, ws, []domain.Record{remoteAt("old", 1_000)}); err != nil {
		t.Fatalf("SaveCached: %v", err)
	}
	if list, err := store.Records(ctx, ws, "", 10); err != nil || len(list) != 0 {
		t.Fatalf("records = %+v, %v, want the on-demand row out of the list", list, err)
	}

	if err := store.ApplyHistorySnapshot(ctx, ws, []domain.Record{remoteAt("old", 1_000)},
		0, 12); err != nil {
		t.Fatalf("ApplyHistorySnapshot: %v", err)
	}

	list, err := store.Records(ctx, ws, "", 10)
	if err != nil || len(list) != 1 || list[0].ID != "old" {
		t.Errorf("records = %+v, %v, want the row the server named in the list", list, err)
	}
}

// keptWrites is what one record claims it wrote into the jar, which is what an undo empties.
func keptWrites(t *testing.T, store *Store, id string) []domain.CookieWrite {
	t.Helper()
	rec, err := store.Record(context.Background(), id)
	if err != nil {
		t.Fatalf("Record(%s): %v", id, err)
	}
	return rec.CookieWrites
}

// startedAt is a record of another device's that started at a moment.
func remoteAt(id string, at int64) domain.Record {
	record := remoteRecord(id, "manual")
	record.StartedAt = at
	return record
}

// A snapshot replaces what the server no longer holds inside its span; older, own and on-request
// records stay.
func TestASnapshotReplacesWhatTheServerNoLongerHoldsInsideItsSpan(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	if _, err := store.ApplyHistoryPage(ctx, ws, []domain.RecordChange{
		{Seq: 1, RecordID: "old", Record: remoteAt("old", 1_000)},
		{Seq: 2, RecordID: "gone", Record: remoteAt("gone", 2_500)},
		{Seq: 3, RecordID: "kept", Record: remoteAt("kept", 2_000)},
	}, 3); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}
	own := sampleRecord("mine", domain.SourceManual)
	own.StartedAt = 2_200
	if err := store.SaveRecord(ctx, ws, own); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}

	if err := store.ApplyHistorySnapshot(ctx, ws,
		[]domain.Record{remoteAt("kept", 2_000), remoteAt("new", 3_000)}, 0, 40); err != nil {
		t.Fatalf("ApplyHistorySnapshot: %v", err)
	}

	list, err := store.Records(ctx, ws, "", 20)
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	var ids []string
	for _, one := range list {
		ids = append(ids, one.ID)
	}
	slices.Sort(ids)
	if want := []string{"kept", "mine", "new", "old"}; !slices.Equal(ids, want) {
		t.Errorf("records = %v, want %v: the one the server no longer holds is dropped, the older one "+
			"and this device's own stay", ids, want)
	}
	workspaces, err := store.SyncWorkspaces(ctx)
	if err != nil || workspaces[0].HistoryCursor != 40 {
		t.Errorf("history cursor = %v, %v, want where the log stood (40)", workspaces, err)
	}
}

// A snapshot of nothing from a server that holds nothing empties what this device kept of it.
func TestAnEmptySnapshotDropsWhatWasKeptOfAnEmptyServer(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)
	if _, err := store.ApplyHistoryPage(ctx, ws, []domain.RecordChange{
		{Seq: 1, RecordID: "a", Record: remoteAt("a", 2_000)},
	}, 1); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}

	if err := store.ApplyHistorySnapshot(ctx, ws, nil, 0, 0); err != nil {
		t.Fatalf("ApplyHistorySnapshot: %v", err)
	}
	if list, err := store.Records(ctx, ws, "", 20); err != nil || len(list) != 0 {
		t.Errorf("records = %v, %v, want none", list, err)
	}
}

// Workspaces of other accounts stay in the database but out of sight until their account is back.
func TestTheWorkspacesOfAnotherAccountAreOutOfSightUntilTheirAccountReturns(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)
	if err := store.Mirror(ctx, domain.RemoteWorkspace{ID: "theirs-1", Name: "Team"},
		"https://server.test|b@server.test"); err != nil {
		t.Fatalf("Mirror: %v", err)
	}
	names := func() []string {
		list, err := store.Workspaces(ctx)
		if err != nil {
			t.Fatalf("Workspaces: %v", err)
		}
		var out []string
		for _, w := range list {
			out = append(out, w.Name)
		}
		return out
	}
	if got := names(); len(got) != 2 {
		t.Fatalf("workspaces = %v, want both while no account is the one", got)
	}

	if err := store.SetContext(ctx, workspace.RemoteAccount); err != nil {
		t.Fatalf("SetContext: %v", err)
	}
	if got := names(); len(got) != 1 || got[0] == "Team" {
		t.Errorf("workspaces = %v, want the other account's workspace out of sight", got)
	}

	if err := store.SetActiveWorkspace(ctx, "no-such"); err != nil {
		t.Fatalf("SetActiveWorkspace: %v", err)
	}
	if active, err := store.ActiveWorkspace(ctx); err != nil || active != ws {
		t.Errorf("active = %q, %v, want the pointer on a workspace in sight", active, err)
	}

	if err := store.SetContext(ctx, "https://server.test|b@server.test"); err != nil {
		t.Fatalf("SetContext: %v", err)
	}
	if got := names(); len(got) != 1 || !slices.Contains(got, "Team") {
		t.Errorf("workspaces = %v, want the workspace back, and the first account's out of sight", got)
	}
}

// With nothing in sight the device gets a workspace of its own, under a new id.
func TestADeviceWithNothingInSightIsGivenAWorkspaceOfItsOwn(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	linked(t, store)
	if err := store.SetContext(ctx, "https://server.test|somebody@else.test"); err != nil {
		t.Fatalf("SetContext: %v", err)
	}

	if err := store.EnsureOne(ctx); err != nil {
		t.Fatalf("EnsureOne: %v", err)
	}
	list, err := store.Workspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	if len(list) != 1 || list[0].ID == ws || list[0].RemoteID != "" {
		t.Errorf("workspaces = %+v, want one new workspace that is only here", list)
	}
}

// Release drops copies with nothing to send and keeps the rest.
func TestLettingGoOfAnAccountKeepsWhatHasNotBeenSent(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	linked(t, store)
	if err := store.Mirror(ctx, domain.RemoteWorkspace{ID: "clean", Name: "Clean"}, account); err != nil {
		t.Fatalf("Mirror: %v", err)
	}
	if err := store.Mirror(ctx, domain.RemoteWorkspace{ID: "dirty", Name: "Dirty"}, account); err != nil {
		t.Fatalf("Mirror: %v", err)
	}
	var dirty string
	if err := store.db.QueryRowContext(ctx,
		`SELECT id FROM workspaces WHERE name = 'Dirty'`).Scan(&dirty); err != nil {
		t.Fatalf("finding the workspace: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		`DELETE FROM sync_queue
		  WHERE workspace_id IN (SELECT id FROM workspaces WHERE remote_account = ?)`,
		account); err != nil {
		t.Fatalf("emptying the queue: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO sync_queue (id, workspace_id, entity_type, entity_id, action, operation_id,
		                        created_at)
		VALUES ('q1', ?, 'collection', 'c1', 'create', 'op1', 0)`, dirty); err != nil {
		t.Fatalf("queueing a change: %v", err)
	}

	let, err := store.Release(ctx, account)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}

	if let != 2 {
		t.Errorf("let go of %d, want the two that owe nothing", let)
	}
	workspaces, err := store.SyncWorkspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	held := map[string]bool{}
	for _, one := range workspaces {
		held[one.Name] = true
	}
	if !held["Dirty"] {
		t.Error("the workspace with a change to send was let go of")
	}
	if held["Clean"] {
		t.Error("the workspace that owed nothing is still here")
	}
}

// Wiping an account takes every workspace that carries its key with all that stands in them, and leaves
// the work that never belonged to an account — the other half of the choice the window offers.
func TestWipingAnAccountTakesItsWorkspacesAndLeavesTheLocalOnes(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTeam(t, store)
	workspace := linked(t, store)
	if err := store.SaveCollection(ctx, workspace.ID, domain.Collection{
		ID: "col-1", Name: "Users", Position: 0,
	}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	if err := store.SetContext(ctx, account); err != nil {
		t.Fatalf("SetContext: %v", err)
	}

	let, err := store.Wipe(ctx, account)
	if err != nil {
		t.Fatalf("Wipe: %v", err)
	}
	if let != 1 {
		t.Errorf("wiped %d workspaces, want the one the account holds", let)
	}
	if rows := countRows(t, store, "collections", workspace.ID); rows != 0 {
		t.Errorf("%d collections are left in the wiped workspace", rows)
	}
	var left int
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM workspaces WHERE id = ?`, workspace.ID).Scan(&left); err != nil {
		t.Fatalf("counting the wiped workspace: %v", err)
	}
	if left != 0 {
		t.Errorf("%d rows of the account's workspace are left", left)
	}
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM workspaces WHERE id = ?`, team).Scan(&left); err != nil {
		t.Fatalf("counting the local workspace: %v", err)
	}
	if left != 1 {
		t.Errorf("the local workspace has %d rows, want it untouched", left)
	}
	held, err := store.Context(ctx)
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if held != "" {
		t.Errorf("context = %q, want nothing pointing at workspaces that are gone", held)
	}
}

// Rebind detaches the account's workspaces and stores the new server id in one transaction.
func TestRebindingLeavesTheWorkspacesOnlyHereAndRemembersTheNewRow(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if row, known, err := store.Seen(ctx, account); err != nil || known || row != "" {
		t.Fatalf("Seen = %q, %v, %v, want nothing seen of an address never signed in", row, known, err)
	}
	if err := store.Remember(ctx, account, "user-1"); err != nil {
		t.Fatalf("Remember: %v", err)
	}
	workspace := linked(t, store)
	if workspace.RemoteID == "" {
		t.Fatal("the workspace is not linked")
	}

	if err := store.Rebind(ctx, account, "user-2"); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	if row, _, _ := store.Seen(ctx, account); row != "user-2" {
		t.Errorf("Seen = %q, want the row it has now", row)
	}
	workspaces, err := store.SyncWorkspaces(ctx)
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	for _, one := range workspaces {
		if one.ID == ws && (one.RemoteID != "" || one.RemoteAccount != "") {
			t.Errorf("workspace = %+v, want it linked to nothing", one)
		}
	}
}

// The saved answers travel with the request: what an import brought is on every device, and an
// edit of them on one is the state of the row on the others.
func TestARequestTravelsWithItsSavedAnswers(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	node := sampleNode("r-6", "col-1", 2, "List", "GET", "https://api.example.com/products")
	node.Responses = []domain.ResponseExample{{
		Name: "The list", Status: 200, Body: `{"data":[]}`,
		Headers: []domain.HeaderPair{{Name: "Content-Type", Value: "application/json"}},
	}}
	if err := store.SaveNode(ctx, node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	var change domain.PendingChange
	for _, one := range owed(t, store, workspace) {
		if one.Entity.Kind == domain.EntityRequest && one.Entity.ID == "r-6" {
			change = one
		}
	}
	other := newMigratedStore(t)
	seedTree(t, other)
	arrived := linked(t, other)
	changes := []domain.SyncChange{{WorkspaceID: arrived.ID, Seq: 1, Entity: arriving(change.Entity, "r-6", 1)}}
	if _, err := other.ApplyPage(ctx, arrived.ID, changes, 1); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}
	back, err := other.Node(ctx, "r-6")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if len(back.Responses) != 1 || back.Responses[0].Name != "The list" ||
		back.Responses[0].Headers[0].Value != "application/json" {
		t.Errorf("responses = %+v, want what travelled", back.Responses)
	}
}

// A form row that sends a file says so, and keeps the path of the file apart from its text: the
// round trip must not turn one into the other.
func TestAFormRowKeepsWhetherItSendsAFile(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	node := sampleNode("r-9", "col-1", 3, "Upload", "POST", "https://api.example.com/files")
	node.BodyKind = domain.BodyForm
	node.Form = []domain.FormRow{
		{ID: "f1", Name: "logo", Value: "kept text", Src: "/tmp/logo.png", File: true, Enabled: true},
		{ID: "f2", Name: "note", Value: "hello", Src: "/tmp/old.txt", Enabled: true},
	}
	if err := store.SaveNode(ctx, node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	sent := owedEntity(t, store, workspace, domain.EntityRequest, "r-9")

	other := newMigratedStore(t)
	seedTree(t, other)
	arrived := linked(t, other)
	changes := []domain.SyncChange{{WorkspaceID: arrived.ID, Seq: 1, Entity: arriving(sent, "r-9", 1)}}
	if _, err := other.ApplyPage(ctx, arrived.ID, changes, 1); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}
	back, err := other.Node(ctx, "r-9")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if len(back.Form) != 2 || back.Form[0] != node.Form[0] || back.Form[1] != node.Form[1] {
		t.Errorf("form = %+v, want %+v", back.Form, node.Form)
	}
}

// A device whose owner keeps secrets at home sends a secret without its value and says that it did:
// the echo of its own push must not take the value from where it is kept. The variable of a level
// and the value of a cookie are written in places of their own, so each has a test.
func TestASecretKeptAtHomeIsSentWithheldAndComesBackWhole(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.SaveCollection(ctx, ws, domain.Collection{
		ID: "col-1", Name: "Users", Position: 0,
		Variables: []domain.Variable{
			{ID: "v1", Name: "token", Value: "s3cret", Kind: domain.VariableSecret, Enabled: true},
			{ID: "v2", Name: "host", Value: "acme.com", Kind: domain.VariableText, Enabled: true},
		},
	}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	workspace := linked(t, store)
	if err := store.SaveSetting(ctx, domain.SettingPreferences,
		`{"syncScopes":{"runs":true,"secrets":false}}`); err != nil {
		t.Fatalf("SaveSetting: %v", err)
	}

	sent := owedEntity(t, store, workspace, domain.EntityCollection, "col-1")
	if at(t, sent.Data, "/variables/@v1/value") != "" || len(sent.Withheld) != 1 ||
		sent.Withheld[0] != "/variables/@v1/value" {
		t.Errorf("sent %s withheld %v, want the secret without its value and said to be withheld",
			sent.Data, sent.Withheld)
	}
	if at(t, sent.Data, "/variables/@v2/value") != "acme.com" {
		t.Errorf("sent %s, want the text variable whole", sent.Data)
	}

	echo := []domain.SyncChange{{WorkspaceID: workspace.ID, Seq: 2, Entity: arriving(sent, "col-1", 2)}}
	if _, err := store.ApplyPage(ctx, workspace.ID, echo, 2); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}
	back, err := oneCollection(t, store, "col-1")
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if back.Variables[0].Value != "s3cret" || back.Variables[0].ID != "v1" {
		t.Errorf("secret = %+v, want the value kept and the id it had", back.Variables[0])
	}
}

// A request's path parameters and the descriptions of its rows are part of what it says: they go
// out with it, and a device they arrive on gets all of them back — a pull rewrites the lists, so a
// field the wire did not carry would be wiped by the echo of the device's own push.
func TestARequestTravelsWithItsPathParametersAndDescriptions(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	node := sampleNode("r-7", "f-1", 2, "One user", "GET", "/users/:user")
	node.Params[0].Description = "which page"
	if err := store.SaveNode(ctx, node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}

	var change domain.PendingChange
	for _, one := range owed(t, store, workspace) {
		if one.Entity.Kind == domain.EntityRequest && one.Entity.ID == "r-7" {
			change = one
		}
	}
	if change.Entity.ID == "" {
		t.Fatal("the request is not in what the workspace owes")
	}
	other := newMigratedStore(t)
	seedTree(t, other)
	arrived := linked(t, other)
	changes := []domain.SyncChange{{WorkspaceID: arrived.ID, Seq: 1, Entity: arriving(change.Entity, "r-8", 1)}}
	if _, err := other.ApplyPage(ctx, arrived.ID, changes, 1); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}
	back, err := other.Node(ctx, "r-8")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if len(back.PathParams) != 1 || back.PathParams[0].Value != "42" ||
		back.PathParams[0].Description != "which user" {
		t.Errorf("path params = %+v, want what travelled", back.PathParams)
	}
	if len(back.Params) != 1 || back.Params[0].Description != "which page" {
		t.Errorf("params = %+v, want the description kept", back.Params)
	}
}

// A request's authorization answers are credentials too, and they go under the switch a secret
// variable does: sent without the secret ones and with their names said to be withheld, and left
// where they are kept when the echo comes back.
func TestARequestsTokenKeptAtHomeIsSentWithheldAndComesBackWhole(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)

	node := sampleNode("r-7", "f-1", 2, "One user", "GET", "/users/:user")
	node.Auth = &domain.Auth{
		Type: domain.AuthBearer, Fields: map[string]string{"token": "s3cret", "prefix": "Bearer"},
	}
	if err := store.SaveNode(ctx, node); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	if err := store.SaveSetting(ctx, domain.SettingPreferences,
		`{"syncScopes":{"runs":true,"secrets":false}}`); err != nil {
		t.Fatalf("SaveSetting: %v", err)
	}

	sent := owedEntity(t, store, workspace, domain.EntityRequest, "r-7")
	if at(t, sent.Data, "/auth/fields/token") != "" || at(t, sent.Data, "/auth/fields/prefix") != "Bearer" {
		t.Errorf("sent %s, want the token kept at home and the rest sent", sent.Data)
	}
	if len(sent.Withheld) != 1 || sent.Withheld[0] != "/auth/fields/token" {
		t.Errorf("withheld = %v, want the token named", sent.Withheld)
	}

	echo := []domain.SyncChange{{WorkspaceID: workspace.ID, Seq: 2, Entity: arriving(sent, "r-7", 2)}}
	if _, err := store.ApplyPage(ctx, workspace.ID, echo, 2); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}
	back, err := store.Node(ctx, "r-7")
	if err != nil {
		t.Fatalf("Node: %v", err)
	}
	if back.Auth == nil || back.Auth.Fields["token"] != "s3cret" {
		t.Errorf("auth = %+v, want the token kept where it was", back.Auth)
	}
}

// A cookie's value is a credential, and it goes under the switch a secret variable does: sent
// without the value and said to be withheld, and left where it is kept when the echo comes back.
func TestACookieKeptAtHomeIsSentWithheldAndComesBackWhole(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	cookie := jarCookie("c1", "api.example.com", "session", "abc")
	if err := store.SaveJarCookie(ctx, ws, cookie); err != nil {
		t.Fatalf("SaveJarCookie: %v", err)
	}
	workspace := linked(t, store)
	if err := store.SaveSetting(ctx, domain.SettingPreferences,
		`{"syncScopes":{"runs":true,"secrets":false}}`); err != nil {
		t.Fatalf("SaveSetting: %v", err)
	}

	var change domain.PendingChange
	for _, one := range owed(t, store, workspace) {
		if one.Entity.Kind == domain.EntityCookie {
			change = one
		}
	}
	if at(t, change.Entity.Data, "/value") != "" || len(change.Entity.Withheld) != 1 ||
		change.Entity.Name != "session" {
		t.Errorf("cookie on the wire = %+v, want it without its value and said to be withheld", change.Entity)
	}

	sent := change.Entity
	sent.Version, sent.UpdatedAt = 2, change.Entity.UpdatedAt+1
	echo := []domain.SyncChange{{WorkspaceID: workspace.ID, Seq: 2, Entity: sent}}
	if _, err := store.ApplyPage(ctx, workspace.ID, echo, 2); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}
	var value string
	if err := store.db.QueryRowContext(ctx,
		`SELECT json_extract(data, '$.value') FROM entities WHERE id = 'c1'`).Scan(&value); err != nil {
		t.Fatalf("reading the cookie: %v", err)
	}
	if value != "abc" {
		t.Errorf("cookie value = %q, want the one this device held", value)
	}
}

// aRecordWithSecrets is a record carrying a credential of each kind a record carries, beside a
// header that is not one: what the switch is asked about, and what it must leave alone.
func aRecordWithSecrets(id string) domain.Record {
	rec := sampleRecord(id, domain.SourceManual)
	rec.RequestHeaders = []domain.HeaderPair{
		{Name: "Authorization", Value: "Bearer s3cret"},
		{Name: "Accept", Value: "application/vnd.api+json"},
	}
	rec.CookieWrites = []domain.CookieWrite{
		{Name: "session", Value: "abc", Domain: "api.example.com", Path: "/",
			State: domain.CookieAdded},
	}
	return rec
}

// pendingEntry is the record a workspace owes, read back as the payload the server will read it from.
func pendingEntry(
	t *testing.T,
	store *Store,
	workspace domain.SyncWorkspace,
	id string,
) *historyv1.RecordEntry {
	t.Helper()
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind != domain.EntityRecord || change.Entity.ID != id {
			continue
		}
		entry := &historyv1.RecordEntry{}
		if err := proto.Unmarshal(change.Entity.Data, entry); err != nil {
			t.Fatalf("reading the payload of %s: %v", id, err)
		}
		return entry
	}
	t.Fatalf("the workspace owes no record %s", id)
	return nil
}

// wireRecord reads the document of a record on the wire.
func wireRecord(t *testing.T, wire *historyv1.Record) domain.Record {
	t.Helper()
	record, err := domain.RecordFromDocument(wire.GetData(), wire.GetWithheld())
	if err != nil {
		t.Fatalf("reading the record on the wire: %v", err)
	}
	return record
}

// headerOnWire is the position and value of the one header of that name, failing the test when the
// payload has none: a header a mask dropped would otherwise pass every assertion about its value.
func headerOnWire(t *testing.T, headers []domain.HeaderPair, name string) (string, string) {
	t.Helper()
	for i, header := range headers {
		if header.Name == name {
			return strconv.Itoa(i), header.Value
		}
	}
	t.Fatalf("the record on the wire carries no %s header", name)
	return "", ""
}

// A record goes to the server under the same switch a secret variable does, and a record carries
// three kinds of value a person may keep: the headers a credential hides in, and what the answer
// did to the jar. What the request was is not one of them, and it travels either way.
func TestARecordIsSentWithoutTheValuesThePersonKeptAtHome(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)
	if err := store.SaveRecord(ctx, ws, aRecordWithSecrets("rec-1")); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.QueueRecord(ctx, ws, "rec-1"); err != nil {
		t.Fatalf("QueueRecord: %v", err)
	}
	if err := store.SaveSetting(ctx, domain.SettingPreferences,
		`{"syncScopes":{"runs":true,"secrets":false}}`); err != nil {
		t.Fatalf("SaveSetting: %v", err)
	}

	wire := pendingEntry(t, store, workspace, "rec-1").GetRecord()
	record := wireRecord(t, wire)
	at, auth := headerOnWire(t, record.RequestHeaders, "Authorization")
	if auth != "" || !slices.Contains(wire.GetWithheld(), "/requestHeaders/"+at+"/value") {
		t.Errorf("authorization on the wire = %q (withheld %v), want it kept at home",
			auth, wire.GetWithheld())
	}
	at, cookie := headerOnWire(t, record.ResponseHeaders, "Set-Cookie")
	if cookie != "" || !slices.Contains(wire.GetWithheld(), "/responseHeaders/"+at+"/value") {
		t.Errorf("set-cookie on the wire = %q, want an answer's credential kept too", cookie)
	}
	// What is not a credential is what the record is: a reader learns which request went out.
	at, accept := headerOnWire(t, record.RequestHeaders, "Accept")
	if accept != "application/vnd.api+json" ||
		slices.Contains(wire.GetSecrets(), "/requestHeaders/"+at+"/value") {
		t.Errorf("accept on the wire = %q, want it as it was", accept)
	}

	writes := record.CookieWrites
	if len(writes) != 1 {
		t.Fatalf("the wire holds %d cookie writes, want the one that was made", len(writes))
	}
	if writes[0].Name != "session" || writes[0].Value != "" || !writes[0].Withheld {
		t.Errorf("the cookie on the wire = %+v, want the mutation said and the value kept", writes[0])
	}
}

// The switch is a person's, and with it on the record travels whole: a device that has let secrets
// out has nothing left to keep back, and a flag on a value that is there would be a lie.
func TestARecordIsSentWholeWhenThePersonLetsSecretsOut(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	workspace := linked(t, store)
	if err := store.SaveRecord(ctx, ws, aRecordWithSecrets("rec-1")); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.QueueRecord(ctx, ws, "rec-1"); err != nil {
		t.Fatalf("QueueRecord: %v", err)
	}
	if err := store.SaveSetting(ctx, domain.SettingPreferences,
		`{"syncScopes":{"runs":true,"secrets":true}}`); err != nil {
		t.Fatalf("SaveSetting: %v", err)
	}

	wire := pendingEntry(t, store, workspace, "rec-1").GetRecord()
	record := wireRecord(t, wire)
	at, auth := headerOnWire(t, record.RequestHeaders, "Authorization")
	if auth != "Bearer s3cret" || len(wire.GetWithheld()) != 0 {
		t.Errorf("authorization on the wire = %q withheld %v, want the value it went out with",
			auth, wire.GetWithheld())
	}
	// The secret is still named, so the server keeps it from readers who may not see secrets.
	if !slices.Contains(wire.GetSecrets(), "/requestHeaders/"+at+"/value") {
		t.Errorf("secrets = %v, want the authorization named", wire.GetSecrets())
	}
	writes := record.CookieWrites
	if len(writes) != 1 || writes[0].Value != "abc" || writes[0].Withheld {
		t.Errorf("the cookie on the wire = %+v, want the value it was given", writes)
	}
}

// A value the server did not hand back is one it does not have or one this reader may not see, and
// either way the copy here is the better one: a record arriving without a cookie's value keeps the
// one this device wrote for it.
func TestAWithheldCookieValueLeavesTheLocalOneInPlace(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	theirs := remoteRecord("rec-1", "manual")
	theirs.CookieWrites = []domain.CookieWrite{
		{Name: "session", Value: "abc", Domain: "api.test", Path: "/", State: domain.CookieAdded},
	}
	if _, err := store.ApplyHistoryPage(ctx, ws,
		[]domain.RecordChange{{Seq: 5, RecordID: "rec-1", Record: theirs}}, 5); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}

	// This device's own act: the panel gives the cookie a value of its own, and owes the server it.
	if err := store.SaveCookieWrites(ctx, ws, "rec-1", []domain.CookieWrite{
		{Name: "session", Value: "mine", Domain: "api.test", Path: "/", State: domain.CookieAdded},
	}); err != nil {
		t.Fatalf("SaveCookieWrites: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		`DELETE FROM sync_queue WHERE entity_type = 'record' AND entity_id = 'rec-1'`); err != nil {
		t.Fatalf("clearing the queue: %v", err)
	}

	// The server answers with the mutation it holds, and does not hand the value over.
	redacted := remoteRecord("rec-1", "manual")
	redacted.CookieWrites = []domain.CookieWrite{{
		Name: "session", Domain: "api.test", Path: "/", State: domain.CookieAdded, Withheld: true,
	}}
	if _, err := store.ApplyHistoryPage(ctx, ws,
		[]domain.RecordChange{{Seq: 9, RecordID: "rec-1", Record: redacted}}, 9); err != nil {
		t.Fatalf("ApplyHistoryPage: %v", err)
	}

	kept := keptWrites(t, store, "rec-1")
	if len(kept) != 1 || kept[0].Value != "mine" || kept[0].Withheld {
		t.Errorf("writes = %+v, want the value this device holds left in place", kept)
	}
}

// A record that comes again, as it does once its scripts have reported, finds the copy a reader
// holds already withheld: the value is still missing and is not turned into an empty one, which the
// jar would take for a cookie with nothing in it.
func TestAWithheldCookieStaysMissingWhenTheRecordComesAgain(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	linked(t, store)

	withheld := func() domain.Record {
		theirs := remoteRecord("rec-1", "manual")
		theirs.CookieWrites = []domain.CookieWrite{{
			Name: "session", Domain: "api.test", Path: "/", State: domain.CookieReplaced,
			Withheld: true, PreviousWithheld: true,
			Previous: &domain.JarCookie{Name: "session", Domain: "api.test", Path: "/"},
		}}
		return theirs
	}
	for seq := int64(5); seq <= 6; seq++ {
		if _, err := store.ApplyHistoryPage(ctx, ws,
			[]domain.RecordChange{{Seq: seq, RecordID: "rec-1", Record: withheld()}}, seq); err != nil {
			t.Fatalf("ApplyHistoryPage: %v", err)
		}
	}

	kept := keptWrites(t, store, "rec-1")
	if len(kept) != 1 || !kept[0].Withheld || !kept[0].PreviousWithheld {
		t.Errorf("writes = %+v, want both values still said to be missing", kept)
	}
}

// aCollection is a collection the queue tests edit: the name is what the edit changes.
func aCollection(name string) domain.Collection {
	return domain.Collection{ID: "col-1", Name: name, Position: 0}
}

// waitingEntry is a queue entry of one collection as the queue tests look at it.
type waitingEntry struct {
	status      string
	operationID string
}

// queuedFor reads every queue entry of one collection, whatever it is doing, in the order made.
func queuedFor(t *testing.T, store *Store, id string) []waitingEntry {
	t.Helper()
	rows, err := store.db.QueryContext(context.Background(),
		`SELECT status, operation_id FROM sync_queue
		  WHERE workspace_id = ? AND entity_type = 'collection' AND entity_id = ?
		  ORDER BY rowid`, ws, id)
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}
	defer rows.Close()
	var out []waitingEntry
	for rows.Next() {
		var one waitingEntry
		if err := rows.Scan(&one.status, &one.operationID); err != nil {
			t.Fatalf("reading the queue: %v", err)
		}
		out = append(out, one)
	}
	return out
}

// An edit that lands after an entry was read gives it another operation id, and the payload built
// from the older row must not be claimed under it: the edit would go out under an id the server has
// seen, and be settled away with the entry.
func TestAnEntryEditedAfterItWasReadIsNotClaimed(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.SaveCollection(ctx, ws, aCollection("Users")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	workspace := linked(t, store)
	read, err := store.owed(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("owed: %v", err)
	}
	var entry queueRow
	for _, one := range read {
		if one.Kind == entityCollection {
			entry = one
		}
	}

	if err := store.SaveCollection(ctx, ws, aCollection("Users renamed")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	taken, err := store.claim(ctx, entry)

	if err != nil || taken {
		t.Fatalf("claim = %v, %v, want an entry that changed since it was read left alone", taken, err)
	}
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == entityCollection && change.OperationID == entry.OperationID {
			t.Error("the next round offered the old operation id for the edited row")
		}
	}
}

// A row edited while its entry is in flight gets an entry of its own beside it, and settling the
// one that went out must leave the other where it is: it carries the edit.
func TestSettlingWhatWentOutLeavesTheEditMadeMeanwhile(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.SaveCollection(ctx, ws, aCollection("Users")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	workspace := linked(t, store)
	var out domain.PendingChange
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == entityCollection {
			out = change
		}
	}
	if err := store.SaveCollection(ctx, ws, aCollection("Users renamed")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	if err := store.Accepted(ctx, out, 4); err != nil {
		t.Fatalf("Accepted: %v", err)
	}

	left := queuedFor(t, store, "col-1")
	if len(left) != 1 || left[0].status != "pending" || left[0].operationID == out.OperationID {
		t.Errorf("queue = %+v, want one pending entry carrying the edit under an id of its own", left)
	}
}

// An entry in flight that fails while a newer one waits beside it is dropped for the newer: two
// waiting entries for a row are what the index forbids, and the newer is built from the row.
func TestAnEntryThatFailsWithANewerOneBesideItIsDropped(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.SaveCollection(ctx, ws, aCollection("Users")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	workspace := linked(t, store)
	var out domain.PendingChange
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == entityCollection {
			out = change
		}
	}
	if err := store.SaveCollection(ctx, ws, aCollection("Users renamed")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	if err := store.Failed(ctx, out, domain.ReasonParentMissing, time.Minute); err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if err := store.Parked(ctx, out, domain.ReasonForbidden); err != nil {
		t.Fatalf("Parked: %v", err)
	}

	left := queuedFor(t, store, "col-1")
	if len(left) != 1 || left[0].status != "pending" {
		t.Errorf("queue = %+v, want only the newer entry, still due", left)
	}
}

// Putting back what a failed round left in flight must not trip over an entry made beside it, nor
// leave the other entries in flight where they were.
func TestReleasingWhatWasInFlightSurvivesEntriesMadeBesideIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)
	if err := store.SaveCollection(ctx, ws, aCollection("Users")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	workspace := linked(t, store)
	owed(t, store, workspace)
	if err := store.SaveCollection(ctx, ws, aCollection("Users renamed")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	if err := store.ReleaseInFlight(ctx); err != nil {
		t.Fatalf("ReleaseInFlight: %v", err)
	}

	var sending int
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue WHERE status = 'sending'`).Scan(&sending); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if sending != 0 {
		t.Errorf("%d entries are still in flight, want them all put back", sending)
	}
	if left := queuedFor(t, store, "col-1"); len(left) != 1 {
		t.Errorf("queue = %+v, want one entry for the edited row", left)
	}
}

// Two entries for one row in flight, which the index allows, come back as one.
func TestTwoEntriesInFlightForOneRowComeBackAsOne(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.SaveCollection(ctx, ws, aCollection("Users")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	workspace := linked(t, store)
	owed(t, store, workspace)
	if err := store.SaveCollection(ctx, ws, aCollection("Users renamed")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	owed(t, store, workspace)
	if got := queuedFor(t, store, "col-1"); len(got) != 2 {
		t.Fatalf("queue = %+v, want two entries in flight", got)
	}

	if err := store.ReleaseInFlight(ctx); err != nil {
		t.Fatalf("ReleaseInFlight: %v", err)
	}

	if left := queuedFor(t, store, "col-1"); len(left) != 1 || left[0].status != "pending" {
		t.Errorf("queue = %+v, want one pending entry, the newer", left)
	}
}

// The cursor only goes forward through a page: a slow page that reaches the store after a newer one
// must not take the device back to read what it already has. Starting over is the one way back.
func TestAStalePageDoesNotMoveTheCursorBack(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)

	if _, err := store.ApplyPage(ctx, workspace.ID, nil, 50); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}
	if _, err := store.ApplyPage(ctx, workspace.ID, nil, 20); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}
	if got := cursorOf(t, store); got != 50 {
		t.Errorf("cursor = %d, want 50", got)
	}

	if err := store.Reset(ctx, workspace.ID); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if got := cursorOf(t, store); got != 0 {
		t.Errorf("cursor = %d, want a start-over to take it back to 0", got)
	}
}

func cursorOf(t *testing.T, store *Store) int64 {
	t.Helper()
	workspaces, err := store.SyncWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("Workspaces: %v", err)
	}
	return workspaces[0].Cursor
}

// echoOf is a page of one collection as the server answers it: the content a push left there.
func echoOf(t *testing.T, workspaceID, name string, version int32) []domain.SyncChange {
	t.Helper()
	return []domain.SyncChange{{WorkspaceID: workspaceID, Seq: 9, Entity: domain.SyncEntity{
		Kind: domain.EntityCollection, ID: "col-1", Version: version, UpdatedAt: 5,
		Name: name, Data: []byte(`{}`),
	}}}
}

// inFlightWithAnEdit leaves a collection claimed for the wire, edited meanwhile, and settled: the
// state a round is in when it pulls the echo of what it just pushed.
func inFlightWithAnEdit(t *testing.T, store *Store) domain.SyncWorkspace {
	t.Helper()
	ctx := context.Background()
	if err := store.SaveCollection(ctx, ws, aCollection("Users")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	workspace := linked(t, store)
	var out domain.PendingChange
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == entityCollection {
			out = change
		}
	}
	if err := store.SaveCollection(ctx, ws, aCollection("Users renamed")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	if err := store.Accepted(ctx, out, 4); err != nil {
		t.Fatalf("Accepted: %v", err)
	}
	return workspace
}

// The echo of a push that was on the wire when the row was edited again carries the content the
// push had, and applying it would give the edit back to the older text, to be pushed from there.
func TestTheEchoOfAPushDoesNotOverwriteAnEditMadeMeanwhile(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := inFlightWithAnEdit(t, store)

	if _, err := store.ApplyPage(ctx, workspace.ID, echoOf(t, workspace.ID, "Users", 4), 9); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	got, err := oneCollection(t, store, "col-1")
	if err != nil || got.Name != "Users renamed" {
		t.Errorf("collection = %+v, %v, want the edit kept for the push to settle", got, err)
	}
}

// A row deleted while its last push was on the wire must stay deleted: the echo is of the live row.
func TestTheEchoOfAPushDoesNotBringBackARowDeletedMeanwhile(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.SaveCollection(ctx, ws, aCollection("Users")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	workspace := linked(t, store)
	var out domain.PendingChange
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == entityCollection {
			out = change
		}
	}
	if err := store.DeleteCollection(ctx, ws, "col-1"); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	if err := store.Accepted(ctx, out, 4); err != nil {
		t.Fatalf("Accepted: %v", err)
	}

	if _, err := store.ApplyPage(ctx, workspace.ID, echoOf(t, workspace.ID, "Users", 4), 9); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	if got, err := oneCollection(t, store, "col-1"); err == nil {
		t.Errorf("collection = %+v, want it to stay deleted until the push says so", got)
	}
}

// A workspace this person may not write in never drains its queue, so its rows would stay as they are
// for ever if a pull left them alone.
func TestAReadersRowsAreRewrittenByAPullAllTheSame(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := inFlightWithAnEdit(t, store)
	if _, err := store.Adopt(ctx, workspace.ID, domain.RemoteWorkspace{
		ID: workspace.RemoteID, Kind: domain.WorkspaceTeam,
		Access: domain.WorkspaceAccess{RoleName: "viewer"},
	}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	// An entry left from before the role was taken away, which nothing queues now.
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO sync_queue (id, workspace_id, entity_type, entity_id, action, operation_id,
		                         created_at)
		 VALUES ('legacy', ?, 'collection', 'col-1', 'update', 'op-legacy', 1)`, ws); err != nil {
		t.Fatalf("queueing an old entry: %v", err)
	}

	if _, err := store.ApplyPage(ctx, workspace.ID, echoOf(t, workspace.ID, "Users", 4), 9); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	if got, _ := oneCollection(t, store, "col-1"); got.Name != "Users" {
		t.Errorf("collection = %+v, want the server row in a workspace nothing can be pushed to", got)
	}
}

// An entry that is parked will not be tried again, and a row held for it would stay as it is.
func TestAParkedEntryDoesNotHoldItsRow(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := inFlightWithAnEdit(t, store)
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == entityCollection {
			if err := store.Parked(ctx, change, domain.ReasonForbidden); err != nil {
				t.Fatalf("Parked: %v", err)
			}
		}
	}

	if _, err := store.ApplyPage(ctx, workspace.ID, echoOf(t, workspace.ID, "Users", 4), 9); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	if got, _ := oneCollection(t, store, "col-1"); got.Name != "Users" {
		t.Errorf("collection = %+v, want a row nobody will push taken from the server", got)
	}
}

// Starting over offers everything, and reads the log from the beginning to be told what is the
// server's: the entries it made are not edits, and the rows they point at are overwritten.
func TestAReadFromTheBeginningRewritesWhatWasOfferedAgain(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := inFlightWithAnEdit(t, store)
	if err := store.Reset(ctx, workspace.ID); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	if _, err := store.ApplyPage(ctx, workspace.ID, echoOf(t, workspace.ID, "Users", 4), 9); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	if got, _ := oneCollection(t, store, "col-1"); got.Name != "Users" {
		t.Errorf("collection = %+v, want the server row after a start-over", got)
	}
}

// An edit made after the workspace was offered again is work again, and is held like any other.
func TestAnEditAfterTheWorkspaceWasOfferedAgainIsHeld(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := inFlightWithAnEdit(t, store)
	if err := store.Reset(ctx, workspace.ID); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if err := store.SaveCollection(ctx, ws, aCollection("Edited after")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	if _, err := store.ApplyPage(ctx, workspace.ID, echoOf(t, workspace.ID, "Users", 4), 9); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	if got, _ := oneCollection(t, store, "col-1"); got.Name != "Edited after" {
		t.Errorf("collection = %+v, want the edit made after the start-over kept", got)
	}
}

// The workspace's own row is queued under the id this device knows it by, and a page carries it under
// the account's: a rename made here is not overwritten by the name the server still holds.
func TestARenameOfTheWorkspaceIsNotOverwrittenByAPull(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := inFlightWithAnEdit(t, store)
	renamed, err := store.Workspace(ctx, ws)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	renamed.Name = "Renamed here"
	if err := store.SaveWorkspace(ctx, renamed); err != nil {
		t.Fatalf("SaveWorkspace: %v", err)
	}

	page := []domain.SyncChange{{WorkspaceID: workspace.ID, Seq: 9, Entity: domain.SyncEntity{
		Kind: domain.EntityWorkspace, ID: workspace.RemoteID, Version: 4, UpdatedAt: 5,
		Name: "The server name",
	}}}
	if _, err := store.ApplyPage(ctx, workspace.ID, page, 9); err != nil {
		t.Fatalf("ApplyPage: %v", err)
	}

	got, err := store.Workspace(ctx, ws)
	if err != nil || got.Name != "Renamed here" {
		t.Errorf("workspace = %+v, %v, want the rename kept for the push to settle", got, err)
	}
}

// collectionsOwed is the ids of the collections a workspace owes, in the order Owed hands them out.
func collectionsOwed(t *testing.T, store *Store, workspace domain.SyncWorkspace, maxBytes int64) []string {
	t.Helper()
	changes, err := store.Owed(context.Background(), workspace, maxBytes)
	if err != nil {
		t.Fatalf("Owed: %v", err)
	}
	var ids []string
	for _, change := range changes {
		if change.Entity.Kind == entityCollection {
			ids = append(ids, change.Entity.ID)
		}
	}
	return ids
}

// aThreeLevelTree makes three collections as roots, deepest id first, and then nests them: the
// queue holds them oldest first, so the deepest is the first in line and the root the last, which
// is exactly the order the server cannot take them in.
func aThreeLevelTree(t *testing.T, store *Store) domain.SyncWorkspace {
	t.Helper()
	ctx := context.Background()
	for _, one := range []domain.Collection{
		{ID: "a-grand", Name: "Grand"},
		{ID: "m-child", Name: "Child"},
		{ID: "z-root", Name: "Root"},
	} {
		if err := store.SaveCollection(ctx, ws, one); err != nil {
			t.Fatalf("SaveCollection %s: %v", one.ID, err)
		}
	}
	workspace := linked(t, store)
	if err := store.MoveCollection(ctx, ws, "m-child", "z-root", 0); err != nil {
		t.Fatalf("MoveCollection: %v", err)
	}
	if err := store.MoveCollection(ctx, ws, "a-grand", "m-child", 0); err != nil {
		t.Fatalf("MoveCollection: %v", err)
	}
	return workspace
}

// Three levels, queued deepest first: the server refuses a child it cannot find the parent of, and
// the entry waits out a backoff.
func TestAParentIsOwedBeforeItsChildren(t *testing.T) {
	store := newMigratedStore(t)
	workspace := aThreeLevelTree(t, store)

	got := collectionsOwed(t, store, workspace, 1<<20)

	if want := []string{"z-root", "m-child", "a-grand"}; !slices.Equal(got, want) {
		t.Errorf("collections owed = %v, want parents first: %v", got, want)
	}
}

// The cut by size falls after the order is made, so a batch that holds only one collection holds
// the root, and never a child whose parent is left for the round after.
func TestTheCutBySizeLeavesTheDeeperCollectionsForLater(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := aThreeLevelTree(t, store)

	// The workspace's own row goes first by kind and takes the one place a batch of one byte has.
	changes, err := store.Owed(ctx, workspace, 1)
	if err != nil {
		t.Fatalf("Owed: %v", err)
	}
	if len(changes) != 1 || changes[0].Entity.Kind != domain.EntityWorkspace {
		t.Fatalf("batch = %+v, want the workspace's own row first", changes)
	}
	if got := collectionsOwed(t, store, workspace, 1); !slices.Equal(got, []string{"z-root"}) {
		t.Errorf("next batch = %v, want the root before anything below it", got)
	}
}

// A collection moved under one made later is queued ahead of it by time, and must go after it.
func TestACollectionMovedUnderANewerOneGoesAfterIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	for _, one := range []domain.Collection{
		{ID: "child", Name: "Child"},
		{ID: "parent", Name: "Parent"},
	} {
		if err := store.SaveCollection(ctx, ws, one); err != nil {
			t.Fatalf("SaveCollection %s: %v", one.ID, err)
		}
	}
	workspace := linked(t, store)
	if err := store.MoveCollection(ctx, ws, "child", "parent", 0); err != nil {
		t.Fatalf("MoveCollection: %v", err)
	}

	got := collectionsOwed(t, store, workspace, 1<<20)

	if want := []string{"parent", "child"}; !slices.Equal(got, want) {
		t.Errorf("collections owed = %v, want %v", got, want)
	}
}

// Rows let go of by their account keep versions that were that server's, and pushed to another that
// holds nothing of them they would be changes of a version nobody has: they go back to unagreed.
func TestARowOfADetachedWorkspaceForgetsTheVersionTheServerGaveIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.SaveCollection(ctx, ws, aCollection("Users")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	workspace := linked(t, store)
	for _, change := range owed(t, store, workspace) {
		if change.Entity.Kind == entityCollection {
			if err := store.Accepted(ctx, change, 7); err != nil {
				t.Fatalf("Accepted: %v", err)
			}
		}
	}
	versionOf := func() int {
		var version int
		if err := store.db.QueryRowContext(ctx,
			`SELECT version FROM entities WHERE id = 'col-1'`).Scan(&version); err != nil {
			t.Fatalf("reading the version: %v", err)
		}
		return version
	}
	if versionOf() != 7 {
		t.Fatalf("version = %d, want the one the server agreed on", versionOf())
	}

	if err := store.Rebind(ctx, account, "someone-else"); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	if got := versionOf(); got != versionUnagreed {
		t.Errorf("version = %d, want it back to unagreed", got)
	}
}

// A page is in the order the server last wrote the rows, so a request edited early can come before
// a collection that was edited later, and the foreign key refuses it until its parent is there.
func TestAChildThatComesBeforeItsParentInAPageIsStillApplied(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)
	page := []domain.SyncChange{
		{WorkspaceID: workspace.ID, Seq: 6, Entity: domain.SyncEntity{
			Kind: domain.EntityRequest, ID: "r-9", Version: 1, UpdatedAt: 5,
			Parent: "col-9", Name: "Child", Data: []byte(`{"method":"GET","url":"/x"}`),
		}},
		{WorkspaceID: workspace.ID, Seq: 40, Entity: domain.SyncEntity{
			Kind: domain.EntityCollection, ID: "col-9", Version: 3, UpdatedAt: 6,
			Name: "Parent", Data: []byte(`{}`),
		}},
	}

	skipped, err := store.ApplyPage(ctx, workspace.ID, page, 40)

	if err != nil || skipped != 0 {
		t.Fatalf("ApplyPage = %d skipped, %v, want both rows written", skipped, err)
	}
	if _, err := store.Node(ctx, "r-9"); err != nil {
		t.Errorf("the request that came first is not here: %v", err)
	}
}

// A page written deepest first needs a pass for each level, and a row whose parent is nowhere in
// the page, or on the device, is still the one thing that is skipped.
func TestAPageWrittenDeepestFirstIsAppliedAndAnOrphanIsStillSkipped(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	workspace := linked(t, store)
	collection := func(id, parent string, seq int64) domain.SyncChange {
		return domain.SyncChange{WorkspaceID: workspace.ID, Seq: seq, Entity: domain.SyncEntity{
			Kind: domain.EntityCollection, ID: id, Version: 1, UpdatedAt: 5,
			Parent: parent, Name: id, Data: []byte(`{}`),
		}}
	}
	page := []domain.SyncChange{
		collection("grand", "child", 1),
		collection("orphan", "nowhere", 2),
		collection("child", "root", 3),
		collection("root", "", 4),
	}

	skipped, err := store.ApplyPage(ctx, workspace.ID, page, 4)

	if err != nil || skipped != 1 {
		t.Fatalf("ApplyPage = %d skipped, %v, want only the orphan skipped", skipped, err)
	}
	for _, id := range []string{"root", "child", "grand"} {
		var held int
		if err := store.db.QueryRowContext(ctx,
			`SELECT count(*) FROM entities WHERE id = ?`, id).Scan(&held); err != nil || held != 1 {
			t.Errorf("collection %s held %d times, %v, want it written", id, held, err)
		}
	}
}

// The window is told the oldest entries of a workspace by name and the number of all of them by count,
// so a first link that queues thousands of rows is not described by thousands of queries each time.
func TestTheQueueIsDescribedByTheOldestEntriesAndCountedWhole(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	for i := range waitingListed + 20 {
		one := domain.Collection{ID: fmt.Sprintf("col-%03d", i), Name: "C", Position: int64(i)}
		if err := store.SaveCollection(ctx, ws, one); err != nil {
			t.Fatalf("SaveCollection: %v", err)
		}
	}

	listed, err := store.Waiting(ctx, ws)
	if err != nil {
		t.Fatalf("Waiting: %v", err)
	}
	counts, err := store.QueueCounts(ctx)
	if err != nil {
		t.Fatalf("QueueCounts: %v", err)
	}

	if len(listed) != waitingListed {
		t.Errorf("%d entries described, want the oldest %d", len(listed), waitingListed)
	}
	if got := counts[ws].Pending; got < waitingListed+20 {
		t.Errorf("%d counted, want every entry the workspace owes", got)
	}
}
