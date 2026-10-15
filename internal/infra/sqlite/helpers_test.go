package sqlite

import (
	"context"
	"os"
	"testing"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
	"schemo/migrations"
)

// Shared fixtures live together: a test that built its own would be a test about the fake rather
// than about SQL.

// tempDataDir is t.TempDir for a store: SQLite lets go of app.db, its -wal and its -shm a moment
// after the pool holding them is closed, and on Windows a removal that meets one of them reports a
// directory that is not empty — failing a test that had nothing to do with it. So the removal is
// tried again for a second before it is believed.
func tempDataDir(t *testing.T) platform.DataDir {
	t.Helper()
	dir, err := os.MkdirTemp("", "schemo-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		for attempt := 0; ; attempt++ {
			err := os.RemoveAll(dir)
			if err == nil {
				return
			}
			if attempt == 20 {
				t.Errorf("removing %s: %v", dir, err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	return platform.DataDir(dir)
}

// nested builds a row the way a use case does, so the fixtures read like a real tree rather than a
// list of struct literals.
func nested(id, parentID string, position int64, name string) domain.Collection {
	return domain.Collection{
		ID: id, ParentID: parentID, Name: name, Position: position,
		Items: []domain.CollectionNode{}, Children: []domain.Collection{},
	}
}

func sampleNode(
	id, collectionID string,
	position int64,
	name,
	method, url string,
) domain.CollectionNode {
	return domain.CollectionNode{
		ID: id, CollectionID: collectionID, Name: name, Position: position, Method: method, URL: url,
		Params: []domain.Row{{ID: id + "-p", Name: "page", Value: "2", Enabled: true}},
		Headers: []domain.Row{
			{ID: id + "-h", Name: "Accept", Value: "application/vnd.api+json", Enabled: true},
		},
		PathParams: []domain.Row{
			{ID: id + "-pp", Name: "user", Value: "42", Enabled: true, Description: "which user"},
		},
		Body: `{"data": {"type": "users"}}`,
	}
}

// The shapes the tree has to keep straight: a nested collection and a top-level request share one
// number line, which is what makes them one list on screen.
func seedTree(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()

	bearer := bearerAuth("{{token}}")
	if err := store.SaveCollection(ctx, ws, domain.Collection{
		ID: "col-1", Name: "Users", Description: "test", Position: 0, Auth: &bearer,
	}); err != nil {
		t.Fatalf("SaveCollection: %v", err)
	}
	if err := store.SaveCollection(ctx, ws, nested("f-1", "col-1", 0, "Admin")); err != nil {
		t.Fatalf("SaveCollection nested: %v", err)
	}
	for _, node := range []domain.CollectionNode{
		sampleNode("r-1", "f-1", 0, "List", "GET", "https://api.example.com/users"),
		sampleNode("r-2", "col-1", 1, "One", "PATCH", "https://api.example.com/users/1"),
	} {
		if err := store.SaveNode(ctx, node); err != nil {
			t.Fatalf("SaveNode %s: %v", node.ID, err)
		}
	}
}

func sampleDraft() domain.Draft {
	return domain.Draft{
		ID:       domain.DraftCommandLine,
		Revision: 3,
		Method:   "POST",
		URL:      "https://api.example.com/a?x=1&y={{token}}",
		Params: []domain.Row{
			{ID: "p1", Name: "x", Value: "1", Enabled: true},
			{ID: "p2", Name: "y", Value: "{{token}}", Enabled: false},
		},
		Headers: []domain.Row{
			{ID: "h1", Name: "Authorization", Value: "Bearer {{token}}", Enabled: true},
		},
		PathParams: []domain.Row{
			{ID: "pp1", Name: "user", Value: "{{id}}", Enabled: true, Description: "which user"},
		},
		Auth:          bearerAuth("{{token}}"),
		Body:          `{"a": 1}`,
		EnvironmentID: "env-1",
	}
}

// ws is the workspace these tests work in — the one the schema seeds, since none of them is about a
// second space.
const ws = domain.WorkspacePersonalID

// bearerAuth is an authorization of the scheme these tests round-trip through the database. What a
// scheme asks for is the scheme's business; what these tests are about is that the answers survive
// being written down and read back.
func bearerAuth(token string) domain.Auth {
	return domain.NewAuth(domain.AuthBearer).With("token", token)
}

// newMigratedStore is a store on a migrated database in a temporary directory: the environment
// tables only exist after the migrations, so anything reading them needs this.
func newMigratedStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(tempDataDir(t))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	if err := store.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Migrate(ctx, migrations.FS); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func sampleRecord(id string, source domain.RecordSource) domain.Record {
	return domain.Record{
		RecordSummary: domain.RecordSummary{
			ID:         id,
			Source:     source,
			Method:     "GET",
			URL:        "https://api.example.com/articles",
			Status:     200,
			StatusText: "200 OK",
			DurationUs: 42000,
			StartedAt:  time.Now().UnixMilli(),
			TabID:      7,
			TabTitle:   "Example",
		},
		DNSUs:      micros(1000),
		ConnectUs:  micros(2000),
		TLSUs:      micros(3000),
		WaitUs:     micros(4000),
		DownloadUs: micros(5000),
		RequestHeaders: []domain.HeaderPair{
			{Name: "Accept", Value: "application/vnd.api+json"},
		},
		ResponseHeaders: []domain.HeaderPair{
			{Name: "Content-Type", Value: "application/vnd.api+json"},
			{Name: "Set-Cookie", Value: "a=1"},
			{Name: "Set-Cookie", Value: "b=2"},
		},
		CookieWrites: []domain.CookieWrite{
			{Name: "session", Value: "abc", State: domain.CookieAdded},
		},
		RequestBody:  &domain.BodyRef{Inline: `{"a":1}`, Size: 7},
		ResponseBody: &domain.BodyRef{Inline: `{"data":[]}`, Size: 11},
	}
}

// micros is a phase length for the fixtures: the field is a pointer, because a phase that did not
// happen is not the same as one that took no time.
func micros(us int64) *int64 { return &us }

// code is a status for the fixtures, a pointer for the same reason: a request that never came back
// has no status, which is not the same as one that came back with a zero in it.
func code(status int) *int { return &status }

func ids(rows []domain.Record) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.ID
	}
	return out
}

// A row is found by id, so a test reads as the row it asks about, not as a position in a list.
func findHit(t *testing.T, hits []domain.SearchHit, id string) domain.SearchHit {
	t.Helper()
	for _, h := range hits {
		if h.ID == id {
			return h
		}
	}
	t.Fatalf("no row %s in %+v", id, hits)
	return domain.SearchHit{}
}

// team is a second space to put things in. The schema seeds only the personal one, so every test
// about isolation has to make the other itself.
const team = "team-1"

func seedTeam(t *testing.T, store *Store) {
	t.Helper()
	// A second space beside the one the schema writes: the name, the kind and the colour are all this
	// has to get right.
	made := domain.NewWorkspace(team, "Team", domain.WorkspaceTeam, "purple", 1)
	if err := store.SaveWorkspace(context.Background(), made); err != nil {
		t.Fatalf("SaveWorkspace(%s): %v", team, err)
	}
}

// countRows answers how many rows of one table a space holds, which is the only question a test
// about a deleted workspace can ask: the store reads by id, and an id that is gone reads as nothing
// whether or not the row behind it is still there.
func countRows(t *testing.T, store *Store, table, workspace string) int {
	t.Helper()
	var n int
	if err := store.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM `+table+` WHERE workspace_id = ?`, workspace).Scan(&n); err != nil {
		t.Fatalf("counting the %s of %s: %v", table, workspace, err)
	}
	return n
}
