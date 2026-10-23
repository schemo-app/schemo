package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"schemo/internal/db"
	"schemo/internal/platform"
)

// blockedFolder is a path the data directory cannot be made at: a file is already holding the name,
// which is the shape of the real thing — something else got there first — and it is the one way to
// make a mkdir fail in a test without touching permissions.
func blockedFolder(t *testing.T) platform.DataDir {
	t.Helper()

	path := filepath.Join(t.TempDir(), platform.DataFolder)
	// Windows may briefly retain SQLite file handles after the store closes.
	t.Cleanup(func() {
		deadline := time.Now().Add(time.Second)
		for {
			err := os.RemoveAll(path)
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("removing data directory: %v", err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	if err := os.WriteFile(path, []byte("in the way"), 0o600); err != nil {
		t.Fatalf("writing the file in the way: %v", err)
	}
	return platform.DataDir(path)
}

func storeOn(t *testing.T, dir platform.DataDir) *db.Store {
	t.Helper()

	store, err := db.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("closing store: %v", err)
		}
	})
	return store
}

// A folder that cannot be made is recorded as that and not as a database that would not open. The
// screen says less for this one — no sentence about data being safe, no path, nothing to open —
// because there is no file behind it, and that has to be the failure it is told about.
func TestAFolderThatCannotBeMadeIsRecordedAsThat(t *testing.T) {
	dir := blockedFolder(t)
	status := NewStatus()
	store := storeOn(t, dir)

	_ = OpenStorage(store, status, dir)(context.Background())

	if status.Ready() {
		t.Fatal("the app called itself ready with nowhere to keep anything")
	}
	failure := status.Failure()
	if failure == nil || failure.Kind != FailureDataDir {
		t.Fatalf("failure = %+v, want the folder's", failure)
	}
	if !strings.Contains(failure.Detail, string(dir)) {
		t.Errorf("detail = %q, want the path in it: it is what a person acts on", failure.Detail)
	}
}

// The button is the whole point of that screen: a folder held by something else and then let go is
// the usual reason for this refusal, and the retry has to carry on into the database rather than
// stopping at the step that failed.
func TestARetryAfterTheFolderIsFreedBringsTheAppUp(t *testing.T) {
	dir := blockedFolder(t)
	status := NewStatus()
	store := storeOn(t, dir)

	retry := OpenStorage(store, status, dir)
	if status.Ready() {
		t.Fatal("the app called itself ready with nowhere to keep anything")
	}

	if err := os.Remove(string(dir)); err != nil {
		t.Fatalf("freeing the path: %v", err)
	}
	if err := retry(context.Background()); err != nil {
		t.Fatalf("the retry: %v", err)
	}
	if !status.Ready() {
		t.Errorf("failure = %+v, want the app up once the folder was free", status.Failure())
	}
}

// The web view's profile goes in the app's folder, unless that folder is what failed: a profile that
// cannot be created ends the app, and the screen that explains the folder would never be drawn.
func TestTheWebviewProfileGoesWhereTheFolderIs(t *testing.T) {
	dir := platform.DataDir(filepath.Join(t.TempDir(), platform.DataFolder))

	status := NewStatus()
	status.Succeed()
	if got := webviewDataPath(status, dir); got != dir.WebviewPath() {
		t.Errorf("profile = %q, want %q", got, dir.WebviewPath())
	}

	status.Fail(FailureDatabase, "locked")
	if got := webviewDataPath(status, dir); got != dir.WebviewPath() {
		t.Errorf("profile after a database failure = %q, want the folder still used", got)
	}

	status.Fail(FailureDataDir, "denied")
	if got := webviewDataPath(status, dir); got != "" {
		t.Errorf("profile after a folder failure = %q, want the system's default", got)
	}
}
