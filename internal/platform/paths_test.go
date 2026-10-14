package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCreateRepairsPermissionsOfExistingDataDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows directory privacy is controlled by NTFS ACLs")
	}
	dir := DataDir(filepath.Join(t.TempDir(), "data"))
	if err := os.MkdirAll(string(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(string(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := dir.Create(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(string(dir))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("existing data directory did not become private: %v", err)
	}
}

// Resolving the path is not making the folder: a directory that does not exist is still a place the
// application knows how to write to, which is what lets the startup screen say where it failed
// instead of the app dying before there is a screen.
func TestNewDataDirTouchesNothing(t *testing.T) {
	dir, err := NewDataDir()
	if err != nil {
		t.Fatalf("NewDataDir: %v", err)
	}
	if filepath.Base(string(dir)) != DataDirName {
		t.Errorf("dir = %q, want it to end in %s", dir, DataDirName)
	}
}

// Create is what a retry does, so making it twice has to be as good as making it once: the second
// press of the button is the normal case, not an error.
func TestCreateIsWhatARetryDoes(t *testing.T) {
	dir := DataDir(filepath.Join(t.TempDir(), DataDirName))

	if err := dir.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := dir.Create(); err != nil {
		t.Fatalf("Create again: %v", err)
	}
	if info, err := os.Stat(string(dir)); err != nil || !info.IsDir() {
		t.Errorf("stat %s = %v, %v; want a directory", dir, info, err)
	}
}

// A folder that cannot be made answers with the path and the system's own reason, because that
// text is what the failure screen shows under its sentence — the one thing a person can act on.
func TestCreateNamesThePathAndTheReason(t *testing.T) {
	// A file where the folder would go: no mkdir wins against that, on any platform, and it is the
	// shape of the real thing — something else is already holding the name.
	blocked := filepath.Join(t.TempDir(), DataDirName)
	if err := os.WriteFile(blocked, []byte("in the way"), 0o600); err != nil {
		t.Fatalf("writing the file in the way: %v", err)
	}

	err := DataDir(blocked).Create()
	if err == nil {
		t.Fatal("Create over a file succeeded, want the failure the screen shows")
	}
	// One sentence, and the path in it once: the screen draws this line whole, and MkdirAll's own
	// error would say the path and the verb a second time in the middle of it.
	if !strings.HasPrefix(err.Error(), "creating "+blocked+": ") {
		t.Errorf("err = %q, want it to read \"creating %s: <reason>\"", err, blocked)
	}
	if strings.Count(err.Error(), blocked) != 1 {
		t.Errorf("err = %q, want the path named once", err)
	}
}

// And when the thing in the way lets go, the same call brings the application up. That is the whole
// reason the screen keeps a button rather than explaining the problem and stopping.
func TestCreateSucceedsOnceThePathIsFree(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), DataDirName)
	if err := os.WriteFile(blocked, []byte("in the way"), 0o600); err != nil {
		t.Fatalf("writing the file in the way: %v", err)
	}
	dir := DataDir(blocked)
	if err := dir.Create(); err == nil {
		t.Fatal("Create over a file succeeded, want it refused")
	}

	if err := os.Remove(blocked); err != nil {
		t.Fatalf("freeing the path: %v", err)
	}
	if err := dir.Create(); err != nil {
		t.Fatalf("Create after the path was freed: %v", err)
	}
}
