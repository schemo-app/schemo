package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPlatformManifestsIncludeOnlySharedAndTargetFiles(t *testing.T) {
	root := t.TempDir()
	for _, target := range []struct {
		name  string
		paths []string
	}{
		{"darwin", []string{"build/config.yml", "build/darwin/Info.plist", "build/darwin/Info.dev.plist"}},
		{"linux", []string{"build/config.yml", "build/linux/nfpm/nfpm.yaml"}},
		{"windows", []string{"build/config.yml", "build/windows/info.json", "build/windows/nsis/project.nsi", "build/windows/nsis/wails_tools.nsh", "build/windows/wails.exe.manifest"}},
	} {
		t.Run(target.name, func(t *testing.T) {
			manifests, err := platformFiles(root, "0.0.10", target.name)
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, file := range manifests {
				relative, err := filepath.Rel(root, file.Path)
				if err != nil {
					t.Fatal(err)
				}
				paths = append(paths, filepath.ToSlash(relative))
			}
			if !slices.Equal(paths, target.paths) {
				t.Errorf("platformFiles(%q) paths = %v, want %v", target.name, paths, target.paths)
			}
		})
	}
	all, err := platformFiles(root, "0.0.10", "")
	if err != nil || len(all) != len(files(root, "0.0.10")) {
		t.Fatalf("platformFiles with no target = %d files, %v", len(all), err)
	}
	if _, err := platformFiles(root, "0.0.10", "freebsd"); err == nil {
		t.Fatal("platformFiles accepted an unsupported platform")
	}
}

// This is the gate the whole arrangement exists for. Every identity value the packagers read is
// declared in internal/platform and written into the files by the table above; nothing but this
// test notices when the two disagree, and a rename would otherwise leave five files describing the
// previous name.
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
			case strings.Contains(file.Path, "darwin"), strings.Contains(field.Find, "file_version"),
				strings.Contains(field.Find, "product_version"):
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

func TestStampWindowsVersionResource(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "info.json")
	original, err := os.ReadFile(filepath.Join(repoRoot, "build/windows/info.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, file := range files(repoRoot, "0.1.0-beta.1") {
		if filepath.Base(file.Path) == "info.json" {
			if err := patchFile(path, file.Fields); err != nil {
				t.Fatal(err)
			}
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var resource struct {
		Fixed map[string]string            `json:"fixed"`
		Info  map[string]map[string]string `json:"info"`
	}
	if err := json.Unmarshal(data, &resource); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"file_version", "product_version"} {
		if got := resource.Fixed[field]; got != "0.1.0" {
			t.Errorf("fixed %s = %q, want 0.1.0", field, got)
		}
	}
	// Windows FileVersionInfo needs FileVersion to select the string table, even for ProductVersion.
	for _, field := range []string{"FileVersion", "ProductVersion"} {
		if got := resource.Info["0409"][field]; got != "0.1.0-beta.1" {
			t.Errorf("string %s = %q, want 0.1.0-beta.1", field, got)
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
