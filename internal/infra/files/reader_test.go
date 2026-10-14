package files

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"schemo/internal/domain"
)

func TestReadAnswersWithTheBytes(t *testing.T) {
	path := write(t, "hello")

	data, err := NewReader().Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("data = %q, want the file's contents", data)
	}
}

// A file that is not there is a send that does not happen, and the error has to say which.
func TestAMissingFileSaysNotFound(t *testing.T) {
	_, err := NewReader().Read(filepath.Join(t.TempDir(), "gone.bin"))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// A file exactly at the limit fits; one byte past it is an error and not a truncation — a cut-off
// upload is a corrupt file at the other end, which is worse than a send that did not happen.
func TestAFileOverTheLimitIsAnError(t *testing.T) {
	reader := &Reader{MaxBytes: 4}

	fits := write(t, "1234")
	if data, err := reader.Read(fits); err != nil || string(data) != "1234" {
		t.Errorf("a file at the limit = (%q, %v), want it read whole", data, err)
	}

	over := write(t, "12345")
	data, err := reader.Read(over)
	if err == nil {
		t.Fatal("a file over the limit was read")
	}
	if data != nil {
		t.Errorf("data = %q, want nothing back with the error", data)
	}
	if !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("err = %v, want ErrNotAllowed", err)
	}
}

func write(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	return path
}

// The default is the ceiling the engine reads a response under, so a body and its answer are the
// same size of problem.
func TestTheDefaultLimitMatchesTheEngines(t *testing.T) {
	if DefaultMaxBytes != 8<<20 {
		t.Errorf("DefaultMaxBytes = %d, want 8 MiB", DefaultMaxBytes)
	}
	if NewReader().MaxBytes != DefaultMaxBytes {
		t.Error("a reader with no limit of its own does not fall back to the default")
	}
}
