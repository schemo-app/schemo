package sqlite

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/schemo-app/proto/go/sync/v1"

	"schemo/internal/domain"
)

// entry represents an uncommitted outbox action targeting a specific entity.
type entry struct {
	entityType domain.EntityType
	entityID   string
	action     domain.Action
}

// queuedEntry represents a persisted outbox queue row including its operation ID.
type queuedEntry struct {
	entry
	operationID string
}

// queued fetches pending sync outbox entries for a workspace, excluding initial default records.
func queued(t *testing.T, store *Store, workspaceID string) []queuedEntry {
	t.Helper()
	rows, err := store.db.QueryContext(context.Background(),
		`SELECT entity_type, entity_id, action, operation_id
		   FROM sync_queue
		  WHERE workspace_id = ? AND status = 'pending'
		    AND NOT (entity_type = 'workspace' AND entity_id = 'personal')
		  ORDER BY created_at, rowid`, workspaceID)
	if err != nil {
		t.Fatalf("reading the queue of %s: %v", workspaceID, err)
	}
	defer rows.Close()

	out := []queuedEntry{}
	for rows.Next() {
		var one queuedEntry
		if err := rows.Scan(&one.entityType, &one.entityID, &one.action, &one.operationID); err != nil {
			t.Fatalf("reading an entry of %s: %v", workspaceID, err)
		}
		if one.operationID == "" {
			t.Errorf("the entry for %s %s carries no operation id", one.entityType, one.entityID)
		}
		out = append(out, one)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the queue of %s: %v", workspaceID, err)
	}
	return out
}

// wantQueued asserts that the outbox queue for a workspace matches expected entries in order.
func wantQueued(t *testing.T, store *Store, workspaceID string, want ...entry) {
	t.Helper()
	got := queued(t, store, workspaceID)
	if len(got) != len(want) {
		t.Fatalf("the queue of %s = %+v, want %+v", workspaceID, got, want)
	}
	for i, one := range want {
		if got[i].entry != one {
			t.Errorf("entry %d = %+v, want %+v", i, got[i].entry, one)
		}
	}
}

// only retrieves a single queuedEntry for the given entity type and ID from the workspace queue.
func only(
	t *testing.T,
	store *Store,
	workspaceID string,
	entityType domain.EntityType,
	entityID string,
) queuedEntry {
	t.Helper()
	for _, one := range queued(t, store, workspaceID) {
		if one.entityType == entityType && one.entityID == entityID {
			return one
		}
	}
	t.Fatalf("nothing is waiting for %s %s", entityType, entityID)
	return queuedEntry{}
}

// TestTheTreeIsQueuedRowByRow verifies that creating, updating, and moving collection tree nodes
// enqueues appropriate outbox records per row.
func TestTheTreeIsQueuedRowByRow(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)

	wantQueued(t, store, ws,
		entry{entityCollection, "col-1", actionCreate},
		entry{entityCollection, "f-1", actionCreate},
		entry{entityRequest, "r-1", actionCreate},
		entry{entityRequest, "r-2", actionCreate},
	)

	rename := nested("f-1", "col-1", 0, "Администраторы")
	if err := store.SaveCollection(ctx, ws, rename); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	wantQueued(t, store, ws,
		entry{entityCollection, "col-1", actionCreate},
		entry{entityCollection, "f-1", actionCreate},
		entry{entityRequest, "r-1", actionCreate},
		entry{entityRequest, "r-2", actionCreate},
	)

	sent(t, store, ws, entityCollection, "f-1")
	sent(t, store, ws, entityRequest, "r-2")
	if err := store.MoveNode(ctx, ws, "r-2", "col-1", 0); err != nil {
		t.Fatalf("MoveNode: %v", err)
	}
	wantQueued(t, store, ws,
		entry{entityCollection, "col-1", actionCreate},
		entry{entityRequest, "r-1", actionCreate},
		entry{entityRequest, "r-2", actionUpdate},
		entry{entityCollection, "f-1", actionUpdate},
	)
}

// TestADeletedTreeIsQueuedWhole verifies that deleting a parent collection enqueues deletion
// outbox entries for every child node in its subtree.
func TestADeletedTreeIsQueuedWhole(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	seedTree(t, store)
	markSent(t, store, ws)

	if err := store.DeleteCollection(ctx, ws, "col-1"); err != nil {
		t.Fatalf("DeleteCollection: %v", err)
	}
	wantQueued(t, store, ws,
		entry{entityCollection, "col-1", actionDelete},
		entry{entityCollection, "f-1", actionDelete},
		entry{entityRequest, "r-1", actionDelete},
		entry{entityRequest, "r-2", actionDelete},
	)

	markSent(t, store, ws)
	third := sampleNode("r-3", "col-1", 0, "Третий", "GET", "https://e.test/")
	if err := store.SaveNode(ctx, third); err != nil {
		t.Fatalf("SaveNode: %v", err)
	}
	markSent(t, store, ws)
	if err := store.DeleteNode(ctx, "r-3"); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	wantQueued(t, store, ws, entry{entityRequest, "r-3", actionDelete})
}

// TestAnEnvironmentIsQueuedWithItsVariables tests that deleting an environment also enqueues
// deletion outbox entries for all associated variables.
func TestAnEnvironmentIsQueuedWithItsVariables(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	environment := domain.Environment{ID: "env-1", Name: "Stage", Position: 0}
	if err := store.SaveEnvironment(ctx, ws, environment); err != nil {
		t.Fatalf("SaveEnvironment: %v", err)
	}
	scope := domain.EnvScope{Environment: "env-1"}
	for _, v := range []domain.Variable{
		{ID: "v-1", Name: "host", Value: "example.test", Kind: domain.VariableText, Enabled: true},
		{ID: "v-2", Name: "token", Value: "s3cret", Kind: domain.VariableSecret, Enabled: true},
	} {
		if err := store.SaveVariable(ctx, ws, scope, v); err != nil {
			t.Fatalf("SaveVariable %s: %v", v.ID, err)
		}
	}
	wantQueued(t, store, ws,
		entry{entityEnvironment, "env-1", actionCreate},
		entry{entityVariable, "v-1", actionCreate},
		entry{entityVariable, "v-2", actionCreate},
	)

	markSent(t, store, ws)
	if err := store.DeleteVariable(ctx, ws, "v-1"); err != nil {
		t.Fatalf("DeleteVariable: %v", err)
	}
	wantQueued(t, store, ws, entry{entityVariable, "v-1", actionDelete})

	markSent(t, store, ws)
	if err := store.DeleteEnvironment(ctx, ws, "env-1"); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	wantQueued(t, store, ws,
		entry{entityEnvironment, "env-1", actionDelete},
		entry{entityVariable, "v-2", actionDelete},
	)
}

// TestTheJarIsQueuedCookieByCookie tests outbox queueing behavior for cookie operations and domain
// blocking settings within the cookie jar.
func TestTheJarIsQueuedCookieByCookie(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	saveJar(t, store, jarCookie("c1", "api.example.com", "session", "abc"))
	saveJar(t, store, jarCookie("c2", "api.example.com", "theme", "dark"))
	wantQueued(t, store, ws,
		entry{entityCookie, "c1", actionCreate},
		entry{entityCookie, "c2", actionCreate},
	)

	markSent(t, store, ws)
	if err := store.DeleteJarCookie(ctx, ws, "c1"); err != nil {
		t.Fatalf("DeleteJarCookie: %v", err)
	}
	if err := store.SaveJarDomain(ctx, ws, "api.example.com", true); err != nil {
		t.Fatalf("SaveJarDomain: %v", err)
	}
	blocked, err := store.JarDomain(ctx, ws, "api.example.com")
	if err != nil || !blocked {
		t.Fatalf("JarDomain = %v, %v, want the flag that was just written", blocked, err)
	}
	got := queued(t, store, ws)
	if len(got) != 2 || got[0].entry != (entry{entityCookie, "c1", actionDelete}) {
		t.Fatalf("the queue = %+v, want the deleted cookie and the flag", got)
	}
	flag := only(t, store, ws, entityJarDomain, got[1].entityID)
	if flag.action != actionCreate {
		t.Errorf("the flag was queued as %+v, want a new row of its own", flag.entry)
	}

	markSent(t, store, ws)
	if err := store.DeleteJarDomain(ctx, ws, "api.example.com"); err != nil {
		t.Fatalf("DeleteJarDomain: %v", err)
	}
	wantQueued(t, store, ws,
		entry{entityJarDomain, got[1].entityID, actionDelete},
		entry{entityCookie, "c2", actionDelete},
	)
}

// TestADomainIsQueuedRowByRow verifies that deleting a domain queues a deletion for its flag and
// for each of its cookies, and none for a domain it did not touch.
func TestADomainIsQueuedRowByRow(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	saveJar(t, store, jarCookie("c1", "api.example.com", "session", "abc"))
	saveJar(t, store, jarCookie("c2", "api.example.com", "theme", "dark"))
	saveJar(t, store, jarCookie("c3", "other.example.org", "session", "def"))
	if err := store.SaveJarDomain(ctx, ws, "api.example.com", true); err != nil {
		t.Fatalf("SaveJarDomain: %v", err)
	}
	var flagID string
	for _, one := range queued(t, store, ws) {
		if one.entityType == entityJarDomain {
			flagID = one.entityID
		}
	}

	markSent(t, store, ws)
	if err := store.DeleteJarDomain(ctx, ws, "api.example.com"); err != nil {
		t.Fatalf("DeleteJarDomain: %v", err)
	}
	wantQueued(t, store, ws,
		entry{entityJarDomain, flagID, actionDelete},
		entry{entityCookie, "c1", actionDelete},
		entry{entityCookie, "c2", actionDelete},
	)

	markSent(t, store, ws)
	if err := store.DeleteJarDomain(ctx, ws, "api.example.com"); err != nil {
		t.Fatalf("DeleteJarDomain: %v", err)
	}
	wantQueued(t, store, ws)
}

// TestASendIsQueuedWhenItIsOffered verifies history record creation, modification, and explicit
// deletion queueing while ignoring local maintenance operations. A record is offered once the code
// around it has reported, and not as it is written.
func TestASendIsQueuedWhenItIsOffered(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	linked(t, store)
	markSent(t, store, ws)

	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-1", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	wantQueued(t, store, ws)
	if err := store.QueueRecord(ctx, ws, "rec-1"); err != nil {
		t.Fatalf("QueueRecord: %v", err)
	}
	wantQueued(t, store, ws, entry{entityRecord, "rec-1", actionCreate})

	if err := store.SaveCookieWrites(ctx, ws, "rec-1", nil); err != nil {
		t.Fatalf("SaveCookieWrites: %v", err)
	}
	wantQueued(t, store, ws, entry{entityRecord, "rec-1", actionCreate})

	markSent(t, store, ws)
	if err := store.SaveCookieWrites(ctx, ws, "rec-1", nil); err != nil {
		t.Fatalf("SaveCookieWrites: %v", err)
	}
	wantQueued(t, store, ws, entry{entityRecord, "rec-1", actionUpdate})

	markSent(t, store, ws)
	if err := store.DeleteRecords(ctx, ws, []string{"rec-1"}); err != nil {
		t.Fatalf("DeleteRecords: %v", err)
	}
	wantQueued(t, store, ws, entry{entityRecord, "rec-1", actionDelete})

	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-2", domain.SourceBrowser)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	markSent(t, store, ws)
	if _, err := store.DeleteAllRecords(ctx, ws); err != nil {
		t.Fatalf("DeleteAllRecords: %v", err)
	}
	wantQueued(t, store, ws)
}

// History travels from the moment a space is linked, and only into a space this person may write
// in: a record offered to any other is not queued, since nothing would ever drain the row.
func TestAHistoryRecordIsNotOfferedToASpaceItCannotGoTo(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveRecord(ctx, ws, sampleRecord("rec-1", domain.SourceManual)); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	if err := store.QueueRecord(ctx, ws, "rec-1"); err != nil {
		t.Fatalf("QueueRecord: %v", err)
	}
	wantQueued(t, store, ws)

	space := linked(t, store)
	markSent(t, store, ws)
	if _, err := store.Adopt(ctx, space.ID, domain.RemoteSpace{
		ID: space.RemoteID, Kind: domain.WorkspaceTeam,
		Access: domain.WorkspaceAccess{RoleName: "viewer"},
	}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if err := store.QueueRecord(ctx, ws, "rec-1"); err != nil {
		t.Fatalf("QueueRecord: %v", err)
	}
	wantQueued(t, store, ws)
}

// TestASpaceQueuesItsOwnRow tests outbox queueing behavior for workspace mutations.
func TestASpaceQueuesItsOwnRow(t *testing.T) {
	store := newMigratedStore(t)

	seedTeam(t, store)
	wantQueued(t, store, team, entry{entityWorkspace, team, actionCreate})

	markSent(t, store, team)
	made := domain.NewWorkspace(team, "Команда", domain.WorkspaceTeam, "purple", 1)
	made.Name = "Команда API"
	if err := store.SaveWorkspace(context.Background(), made); err != nil {
		t.Fatalf("SaveWorkspace: %v", err)
	}
	made.Name = "Команда API v2"
	if err := store.SaveWorkspace(context.Background(), made); err != nil {
		t.Fatalf("SaveWorkspace: %v", err)
	}
	wantQueued(t, store, team, entry{entityWorkspace, team, actionUpdate})
}

// sent updates a single sync outbox item status to 'sending'.
func sent(
	t *testing.T,
	store *Store,
	workspaceID string,
	entityType domain.EntityType,
	entityID string,
) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(),
		`UPDATE sync_queue SET status = 'sending'
		  WHERE workspace_id = ? AND entity_type = ? AND entity_id = ?`,
		workspaceID, entityType, entityID); err != nil {
		t.Fatalf("marking %s %s as sent: %v", entityType, entityID, err)
	}
}

// markSent updates all sync outbox items for a workspace to 'sending' status.
func markSent(t *testing.T, store *Store, workspaceID string) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(),
		`UPDATE sync_queue SET status = 'sending' WHERE workspace_id = ?`, workspaceID); err != nil {
		t.Fatalf("marking the queue of %s as sent: %v", workspaceID, err)
	}
}

// TestALandedWriteWakesTheEngine verifies that committing database writes triggers the wake
// callback.
func TestALandedWriteWakesTheEngine(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	var woken []string
	store.SetWake(func(space string) { woken = append(woken, space) })

	if err := store.SaveCollection(ctx, ws, nested("col-1", "", 0, "Пользователи")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	if len(woken) != 1 || woken[0] != ws {
		t.Fatalf("a write woke the engine for %v, want the space it was made in", woken)
	}

	if err := store.SaveNode(ctx, domain.CollectionNode{
		ID: "r-1", CollectionID: "nowhere", Name: "Список", Method: "GET", URL: "https://x.example",
	}); err == nil {
		t.Fatal("a node in a collection that is not there was saved")
	}
	if len(woken) != 1 {
		t.Fatalf("a write that failed woke the engine: %v, want the one wake", woken)
	}
}

// A move queues the rows it renumbers, and wakes the engine for them like any other write: without
// it the move waits for the next timed round, which is ten minutes.
func TestAMoveWakesTheEngine(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	for _, one := range []domain.Collection{nested("a", "", 0, "A"), nested("b", "", 1, "B")} {
		if err := store.SaveCollection(ctx, ws, one); err != nil {
			t.Fatalf("SaveCollection: %v", err)
		}
	}

	var woken []string
	store.SetWake(func(space string) { woken = append(woken, space) })
	if err := store.MoveCollection(ctx, ws, "b", "a", 0); err != nil {
		t.Fatalf("MoveCollection: %v", err)
	}

	if len(woken) != 1 || woken[0] != ws {
		t.Errorf("a move woke the engine for %v, want the space it was made in", woken)
	}
}

// TestAWriteThatOwesNothingWakesNobody verifies that non-queued writes do not invoke wake callback.
func TestAWriteThatOwesNothingWakesNobody(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	woken := 0
	store.SetWake(func(string) { woken++ })

	if err := store.inTx(ctx, "a write that queues nothing", func(tx *writeTx) error {
		_, err := tx.ExecContext(ctx, `UPDATE workspaces SET name = name WHERE id = ?`, ws)
		return err
	}); err != nil {
		t.Fatalf("inTx: %v", err)
	}
	if woken != 0 {
		t.Errorf("a write that owed nothing woke the engine %d times", woken)
	}
}

// TestAnEditAfterQueueingTravelsInItsNewShape verifies that multiple edits before sync read the
// latest state.
func TestAnEditAfterQueueingTravelsInItsNewShape(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	seedTree(t, store)

	space := linked(t, store)

	if err := store.SaveCollection(ctx, ws, domain.Collection{
		ID: "col-1", Name: "Пользователи API", Position: 0,
	}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}

	var payload []byte
	for _, change := range owed(t, store, space) {
		if change.Entity.Kind == domain.EntityCollection && change.Entity.ID == "col-1" {
			payload = change.Entity.Payload
		}
	}
	if payload == nil {
		t.Fatal("the collection is not in what the space owes")
	}

	var collection sync.CollectionData
	if err := proto.Unmarshal(payload, &collection); err != nil {
		t.Fatalf("reading the payload: %v", err)
	}
	if collection.GetName() != "Пользователи API" {
		t.Errorf("what travels is called %q, want the row as it stands now",
			collection.GetName())
	}
}

// TestAStoreWithoutAWakeIsStillAStore verifies that Store operates correctly when wake callback is
// nil.
func TestAStoreWithoutAWakeIsStillAStore(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	if err := store.SaveCollection(ctx, ws, nested("col-1", "", 0, "Пользователи")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
}

// ---- a space this person may only read ---------------------------------------------------------

// readerIn leaves the space linked and answered by the account as a reader's: a role without the
// right to write, rename or delete.
func readerIn(t *testing.T, store *Store) domain.SyncSpace {
	t.Helper()
	space := linked(t, store)
	if _, err := store.Adopt(context.Background(), space.ID, domain.RemoteSpace{
		ID: space.RemoteID, Kind: domain.WorkspaceTeam,
		Access: domain.WorkspaceAccess{RoleName: "viewer"},
	}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	return space
}

// Every write of shared content passes the queue, so the queue is where a space that cannot be
// written refuses it, and the row it was written to goes back with the transaction.
func TestAReaderCannotWriteSharedContent(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.SaveCollection(ctx, ws, aCollection("Users")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	readerIn(t, store)

	writes := map[string]func() error{
		"a collection": func() error {
			return store.SaveCollection(ctx, ws, domain.Collection{ID: "col-2", Name: "New"})
		},
		"a rename": func() error { return store.SaveCollection(ctx, ws, aCollection("Renamed")) },
		"a request": func() error {
			return store.SaveNode(ctx, sampleNode("r-1", "col-1", 0, "One", "GET", "/a"))
		},
		"a move":     func() error { return store.MoveCollection(ctx, ws, "col-1", "", 1) },
		"a deletion": func() error { return store.DeleteCollection(ctx, ws, "col-1") },
		"an environment": func() error {
			return store.SaveEnvironment(ctx, ws, domain.Environment{ID: "env-1", Name: "Dev"})
		},
		"a variable": func() error {
			return store.SaveVariable(ctx, ws, domain.EnvScope{},
				domain.Variable{ID: "v-1", Name: "host", Value: "x", Kind: domain.VariableText, Enabled: true})
		},
		"a cookie": func() error {
			return store.SaveJarCookie(ctx, ws, jarCookie("c-1", "api.test", "sid", "abc"))
		},
		"a domain flag": func() error { return store.SaveJarDomain(ctx, ws, "api.test", true) },
		"a rename of the space": func() error {
			renamed, err := store.Workspace(ctx, ws)
			if err != nil {
				return err
			}
			renamed.Name = "Renamed here"
			return store.SaveWorkspace(ctx, renamed)
		},
	}
	for name, write := range writes {
		if got := domain.CodeOf(write()); got != domain.CodeWorkspaceReadOnly {
			t.Errorf("%s: code = %q, want %q", name, got, domain.CodeWorkspaceReadOnly)
		}
	}

	collection, err := oneCollection(t, store, "col-1")
	if err != nil || collection.Name != "Users" {
		t.Errorf("collection = %+v, %v, want every refused write rolled back", collection, err)
	}
	if _, err := oneCollection(t, store, "col-2"); err == nil {
		t.Error("a collection written in a space that cannot be written is there")
	}
	wantQueued(t, store, ws)
}

// The right to rename the space is its own: a role that may change the space and not write in it
// can rename it, and still writes no content.
func TestTheRightToRenameTheSpaceIsNotTheRightToWriteInIt(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	space := linked(t, store)
	if _, err := store.Adopt(ctx, space.ID, domain.RemoteSpace{
		ID: space.RemoteID, Kind: domain.WorkspaceTeam,
		Access: domain.WorkspaceAccess{RoleName: "manager", Update: true},
	}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	renamed, err := store.Workspace(ctx, ws)
	if err != nil {
		t.Fatalf("Workspace: %v", err)
	}
	renamed.Name = "Renamed here"
	if err := store.SaveWorkspace(ctx, renamed); err != nil {
		t.Errorf("a rename by a role that may change the space was refused: %v", err)
	}
	err = store.SaveCollection(ctx, ws, aCollection("Users"))
	if domain.CodeOf(err) != domain.CodeWorkspaceReadOnly {
		t.Errorf("code = %q, want content refused to a role that may not write", domain.CodeOf(err))
	}
}

// A space the person writes in, or one nobody described, is untouched by any of it.
func TestAWriterAndASpaceNobodyDescribedStillWrite(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.SaveCollection(ctx, ws, aCollection("Alone")); err != nil {
		t.Errorf("a space nobody described refused a write: %v", err)
	}

	space := linked(t, store)
	if _, err := store.Adopt(ctx, space.ID, domain.RemoteSpace{
		ID: space.RemoteID, Kind: domain.WorkspaceTeam,
		Access: domain.WorkspaceAccess{RoleName: "editor", Write: true},
	}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if err := store.SaveCollection(ctx, ws, aCollection("Edited")); err != nil {
		t.Errorf("a writer's write was refused: %v", err)
	}
}

// A role taken away empties the queue of the space at once: nothing is pushed to it any more, and
// the status would go on saying that something waits.
func TestTakingTheRoleThatWritesAwayEmptiesTheQueue(t *testing.T) {
	store, ctx := newMigratedStore(t), context.Background()
	if err := store.SaveCollection(ctx, ws, aCollection("Users")); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	space := linked(t, store)
	if len(queuedFor(t, store, "col-1")) == 0 {
		t.Fatal("nothing was queued before the role changed")
	}

	if _, err := store.Adopt(ctx, space.ID, domain.RemoteSpace{
		ID: space.RemoteID, Kind: domain.WorkspaceTeam,
		Access: domain.WorkspaceAccess{RoleName: "viewer"},
	}); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	var left int
	if err := store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM sync_queue WHERE workspace_id = ?`, ws).Scan(&left); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if left != 0 {
		t.Errorf("%d entries are queued for a space nothing is pushed to", left)
	}
}
