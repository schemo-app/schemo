package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTheDefaultFolderIsInTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(DataDirEnv, "")

	dir, err := NewDataDir()
	if want := filepath.Join(home, ".schemo"); err != nil || string(dir) != want {
		t.Fatalf("NewDataDir = %q, %v; want %q", dir, err, want)
	}
	if _, err := os.Stat(string(dir)); !os.IsNotExist(err) {
		t.Errorf("NewDataDir made the folder; only Create should")
	}
}

// The folder named by the environment is made absolute, so every process reads the same path.
func TestTheEnvironmentNamesAnotherFolder(t *testing.T) {
	chosen := filepath.Join(t.TempDir(), "profile")
	t.Setenv(DataDirEnv, chosen)
	if dir, err := NewDataDir(); err != nil || string(dir) != chosen {
		t.Errorf("NewDataDir = %q, %v; want %q", dir, err, chosen)
	}

	t.Setenv(DataDirEnv, filepath.Join("relative", "profile"))
	if dir, err := NewDataDir(); err != nil || !filepath.IsAbs(string(dir)) {
		t.Errorf("NewDataDir = %q, %v; want an absolute path", dir, err)
	}
}

func TestCreateMakesAPrivateFolderAndCanRunAgain(t *testing.T) {
	dir := DataDir(filepath.Join(t.TempDir(), "data"))
	if err := os.MkdirAll(string(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := dir.Create(); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	info, err := os.Stat(string(dir))
	if err != nil || !info.IsDir() {
		t.Fatalf("stat = %v, %v; want a directory", info, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, want 0700", info.Mode().Perm())
	}
}

// The startup screen shows this error and offers a retry, so it names the path once with the
// system's reason, and the retry works once the path is free.
func TestCreateOverAFileFailsUntilThePathIsFree(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), DataFolder)
	if err := os.WriteFile(blocked, []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := DataDir(blocked)

	err := dir.Create()
	if err == nil || !strings.HasPrefix(err.Error(), "creating "+blocked+": ") || strings.Count(err.Error(), blocked) != 1 {
		t.Fatalf("Create = %v, want \"creating %s: <reason>\"", err, blocked)
	}

	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := dir.Create(); err != nil {
		t.Fatalf("Create after the path was freed: %v", err)
	}
}
