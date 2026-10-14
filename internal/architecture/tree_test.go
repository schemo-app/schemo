// Package architecture checks dependency boundaries independently of naming and formatting.
package architecture

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// repository is this module's root and its import path, found from this file rather than from
// the working directory: a test binary started anywhere else used to walk "../..", which in a
// workspace of two sibling repositories is the directory holding both of them, and every rule then
// reported on the wrong tree. The import path is read out of the same go.mod, so that it cannot
// disagree with the module after a rename.
func repository(t *testing.T) (root, module string) {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("this test cannot say which file it is")
	}
	dir := filepath.Dir(file)
	for {
		manifest := filepath.Join(dir, "go.mod")
		if held, err := os.ReadFile(manifest); err == nil {
			line, found := strings.CutPrefix(string(held), "module ")
			if !found {
				t.Fatalf("%s does not begin with a module line", manifest)
			}
			name, _, _ := strings.Cut(line, "\n")
			return dir, strings.TrimSpace(name)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s: this test cannot find the repository it belongs to",
				filepath.Dir(file))
		}
		dir = parent
	}
}

// isFixtures is a directory the rules do not look into: fixtures are not packages, and dot-folders
// are tooling.
func isFixtures(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, ".")
}

// notOurCode are trees that hold no Go package of ours: the window and its dependencies, the
// extension's build output, and the release artefacts the Taskfile writes. Walking them is slow and
// they are not what any of these rules is about.
var notOurCode = map[string]bool{
	"frontend": true, "node_modules": true, "extension": true,
	"build": true, "bin": true, "dist": true,
}

// packageImports maps a package directory (relative to the repository root, slash-separated, the
// module-relative form the rules talk about) to the module-internal packages it imports.
func packageImports(t *testing.T, root string) map[string][]string {
	t.Helper()

	fset := token.NewFileSet()
	imports := map[string][]string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (isFixtures(d.Name()) || notOurCode[d.Name()]) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		dir, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			return relErr
		}
		dir = filepath.ToSlash(dir)
		if dir == "." {
			// The root package has no directory of its own; the empty name is how the rules say it.
			dir = ""
		}

		for _, spec := range file.Imports {
			quoted, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				continue
			}
			imports[dir] = append(imports[dir], quoted)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	return imports
}
