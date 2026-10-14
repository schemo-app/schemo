package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This is the gate the whole arrangement exists for. Every identity value the packagers read is
// declared in internal/platform and written into the files by the table above; nothing but this
// test notices when the two disagree. Without it a rename leaves five files describing the
// previous name, which is exactly the state the reference project is in — its Windows metadata
// says 1.1.0 and its Go constant says 1.1.1, in the same commit.
//
// The version is deliberately not part of it: a committed tree carries the last release's version
// until the next one stamps over it, so checking it would fail every build between two releases.
func TestManifestsCarryTheIdentity(t *testing.T) {
	found, err := differences()
	if err != nil {
		t.Fatalf("differences: %v", err)
	}
	if len(found) > 0 {
		t.Errorf("the manifests disagree with internal/platform:\n%s\n\n%s",
			strings.Join(found, "\n"),
			"Run: go run ./internal/tools/manifest -version <the released version>")
	}
}

// A marker that matches nothing means a file was regenerated from the framework's template, and
// one that matches twice means the file grew a second place for the same value. Both are news
// rather than somewhere to guess, so both are errors.
func TestLocateRefusesAMarkerThatIsNotUnique(t *testing.T) {
	lines := []string{"  name: \"a\"", "  name: \"b\""}

	for _, tc := range []struct {
		what  string
		field Field
	}{
		{"nothing matches", Field{Find: "  absent: "}},
		{"two matches", Field{Find: `  name: "`}},
	} {
		if _, err := locate(lines, tc.field); err == nil {
			t.Errorf("%s: locate returned no error", tc.what)
		}
	}
}

func TestLocateFollowsTheOffset(t *testing.T) {
	lines := []string{"<key>CFBundleName</key>", "<string>Schemo</string>", "<key>Other</key>"}

	i, err := locate(lines, Field{Find: "<key>CFBundleName</key>", Offset: 1})
	if err != nil {
		t.Fatalf("locate: %v", err)
	}
	if i != 1 {
		t.Errorf("locate = %d, want the line below the marker", i)
	}

	// The scheme sits two lines under its key; an offset past the end of the file is a mistake in the
	// table, not a value that is missing.
	if _, err := locate(lines, Field{Find: "<key>Other</key>", Offset: 2}); err == nil {
		t.Error("locate ran past the end of the file without complaining")
	}
}

// Patching edits the marked lines and leaves every other byte alone — line endings included, since
// rewriting a Unix file with CRLF would show up as a change to all of it.
func TestPatchFileTouchesOnlyTheMarkedLines(t *testing.T) {
	for _, sep := range []string{"\n", "\r\n"} {
		path := filepath.Join(t.TempDir(), "sample.yml")
		original := strings.Join([]string{"info:", "  name: \"old\"", "  keep: me", ""}, sep)
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
			t.Fatalf("writing the sample: %v", err)
		}

		fields := []Field{{Value: "new", Find: `  name: "`, Format: `  name: "%s"`}}
		if err := patchFile(path, fields); err != nil {
			t.Fatalf("patchFile: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading back: %v", err)
		}
		want := strings.Join([]string{"info:", "  name: \"new\"", "  keep: me", ""}, sep)
		if string(data) != want {
			t.Errorf("with %q endings the file is %q, want %q", sep, data, want)
		}
	}
}

func TestPatchFileReportsAFileItCannotRead(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.yml")
	if err := patchFile(missing, []Field{{Find: "x", Format: "%s"}}); err == nil {
		t.Error("patching a file that is not there returned no error")
	}
}

// The version the command is given is the one it writes, and the guards exist because the two easy
// mistakes are passing nothing and passing the tag as it comes from git.
func TestCheckVersionRefusesWhatWouldShipWrong(t *testing.T) {
	for _, version := range []string{"", "dev", "v0.1.3", "0.1", "01.2.3", "1.2.3-", "1.2.3+build", "main", "1.2.65536"} {
		if err := checkVersion(version); err == nil {
			t.Errorf("checkVersion(%q) returned no error", version)
		}
	}
	if err := checkVersion("0.1.3"); err != nil {
		t.Errorf("checkVersion(%q) = %v, want it accepted", "0.1.3", err)
	}
	if err := checkVersion("1.2.3-beta.1"); err != nil {
		t.Errorf("checkVersion(prerelease) = %v", err)
	}
}

func TestPrereleaseUsesNumericNativeVersions(t *testing.T) {
	for _, file := range files(".", "1.2.3-beta.1") {
		for _, field := range file.Fields {
			if !field.Version {
				continue
			}
			want := "1.2.3-beta.1"
			switch {
			case strings.Contains(file.Path, "darwin"), strings.Contains(field.Find, "file_version"):
				want = "1.2.3"
			case strings.Contains(file.Path, "wails.exe.manifest"), strings.Contains(field.Find, "INFO_FILEVERSION"):
				want = "1.2.3.0"
			}
			if got := field.render(); !strings.Contains(got, `"`+want+`"`) && !strings.Contains(got, ">"+want+"<") {
				t.Errorf("version in %s (%s) = %q; want %s", file.Path, field.Find, got, want)
			}
		}
	}
}

func TestIdentityValuePrintsWhatTheTaskfileAsksFor(t *testing.T) {
	for _, field := range []string{"name", "slug", "description"} {
		value, err := identityValue(field)
		if err != nil {
			t.Errorf("identityValue(%q): %v", field, err)
		}
		if value == "" {
			t.Errorf("identityValue(%q) is empty", field)
		}
	}
	if _, err := identityValue("copyright"); err == nil {
		t.Error("identityValue accepted a field no build asks for")
	}
}
