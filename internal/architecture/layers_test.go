// The layering: what a package may import. The rule table of docs/ARCHITECTURE.md.
package architecture

import (
	"strings"
	"testing"
)

// wailsPkg is the desktop framework. Only the transport layer may see it: the moment a use case
// imports it, the logic stops being testable without a window.
const wailsPkg = "github.com/wailsapp/wails/v3"

// TestLayerDependencies is the rule table from docs/ARCHITECTURE.md, expressed as code.
func TestLayerDependencies(t *testing.T) {
	root, module := repository(t)
	imports := packageImports(t, root)

	// forbid maps a package prefix to the internal packages it must not reach.
	forbid := []struct {
		owner   string
		against []string
		explain string
	}{
		{"internal/domain", []string{module},
			"domain is entities and rules, with no dependency on the rest of the app"},
		{"internal/platform", []string{module}, "platform is stdlib-only by design"},
		{"internal/infra", []string{module + "/internal/usecase", module + "/internal/transport"},
			"infrastructure is called by use cases, never the other way round"},
		{"internal/usecase", []string{module + "/internal/infra", module + "/internal/transport"},
			"use cases declare what they need; only the composition root wires implementations"},
	}

	for _, rule := range forbid {
		for dir, list := range imports {
			if dir != rule.owner && !strings.HasPrefix(dir, rule.owner+"/") {
				continue
			}
			for _, target := range list {
				for _, bad := range rule.against {
					if target == bad || strings.HasPrefix(target, bad+"/") {
						t.Errorf("%s imports %s — %s", dir, target, rule.explain)
					}
				}
			}
		}
	}
}

// TestOnlyTransportUsesApplicationFrameworks keeps application frameworks at the boundary. The root is
// exempt: main.go is the composition root, and holding the *application.App it builds is its whole
// job.
func TestOnlyTransportUsesApplicationFrameworks(t *testing.T) {
	root, _ := repository(t)

	for dir, list := range packageImports(t, root) {
		if dir == "" || strings.HasPrefix(dir, "internal/transport") {
			continue
		}
		for _, target := range list {
			for _, framework := range []string{wailsPkg, "go.uber.org/fx"} {
				if target == framework || strings.HasPrefix(target, framework+"/") {
					t.Errorf("%s imports %s — application frameworks belong in internal/transport", dir, target)
				}
			}
		}
	}
}
