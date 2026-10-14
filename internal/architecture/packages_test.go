// A package is named for what it gives.
package architecture

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenNames are package names that say nothing about what they provide. A package that needs
// one of these names has more than one responsibility and wants splitting.
var forbiddenNames = map[string]bool{
	"util": true, "utils": true, "common": true, "helpers": true, "helper": true,
	"models": true, "types": true, "constants": true, "misc": true, "shared": true,
}

// TestPackageNames rejects the names that hide a package's job. Naming is checked because it is
// the cheapest signal that a package has grown a second responsibility.
func TestPackageNames(t *testing.T) {
	root, _ := repository(t)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if path != root && (isFixtures(name) || notOurCode[name]) {
			return fs.SkipDir
		}
		if forbiddenNames[name] {
			t.Errorf("%s: package name %q says nothing about what it provides", path, name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
}

// TestPkgIsStandalone holds the one thing pkg/ claims: what lives there is a tool we carry along,
// not part of the architecture. The claim is exactly one property, and this is it — the package
// imports nothing of ours. Anything that reaches for `schemo/...` knows about our entities
// or our scenarios, and so belongs under internal/ with the layers that describe them.
func TestPkgIsStandalone(t *testing.T) {
	root, module := repository(t)

	for dir, list := range packageImports(t, root) {
		if dir != "pkg" && !strings.HasPrefix(dir, "pkg/") {
			continue
		}
		for _, target := range list {
			if target == module || strings.HasPrefix(target, module+"/") {
				t.Errorf("%s imports %s — a package under pkg/ must not import this module "+
					"(docs/ARCHITECTURE.md, \"Layers\")", dir, target)
			}
		}
	}
}
