package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The window's copy of these numbers is only worth having while it is the same copy. A change to a
// limit in `domain` that nobody regenerates leaves the window drawing one range while Go clamps
// another — which is what happened to the inspector, and what this test is for.
func TestTheGeneratedFileIsWhatThisSideWouldWrite(t *testing.T) {
	path := filepath.Join("..", "..", "..", filepath.FromSlash(output))
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v (run `go run ./internal/tools/layout`)", path, err)
	}

	if want := rendered(); string(onDisk) != string(want) {
		t.Errorf("%s is not what this side would write.\nRun `go run ./internal/tools/layout`.\n"+
			"on disk:\n%s\nwant:\n%s", path, onDisk, want)
	}
}
