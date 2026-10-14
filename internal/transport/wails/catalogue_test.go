package wails

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Where the window words a value, the sentence is in its catalogue and the value is here. This
// file holds the two together: every code, every startup failure, every reason a command was
// refused, and every catalogue key this module writes out whole has a sentence waiting for it.
//
// It is a test rather than a type on the window's side because the value is declared here, and
// because a copy of the fact is what goes stale: the window's own catalogue cannot know what this
// side sends, so a list of it over there would be a second copy kept honest by a third mechanism.
//
// The values are read out of their sources rather than listed, for the same reason — with the one
// exception of the keys this module names outright, which are also read rather than listed.

// The frontend checks every translation against this reference catalogue.
const panelCatalogue = "../../../frontend/src/locales/en.json"

// panelOwn are the sentences under a checked prefix that the window wrote for itself rather than
// this module, each with the reason. Everything else under a prefix answers a value, so a code
// deleted here cannot leave its sentence behind.
var panelOwn = map[string]string{}

func catalogueKeys(source string) (map[string]bool, error) {
	var entries map[string]any
	if err := json.Unmarshal([]byte(source), &entries); err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	var walk func(map[string]any, string) error
	walk = func(group map[string]any, prefix string) error {
		for name, value := range group {
			if strings.Contains(name, ".") {
				return fmt.Errorf("catalogue key %q contains a dot", name)
			}
			path := prefix + name
			switch value := value.(type) {
			case string:
				keys[path] = true
			case map[string]any:
				if err := walk(value, path+"."); err != nil {
					return err
				}
			default:
				return fmt.Errorf("catalogue value %q is not a message or group", path)
			}
		}
		return nil
	}
	return keys, walk(entries, "")
}

// catalogue is the window's catalogue as a test needs it: read once, and refused when the reader
// has lost its way, because a reader that finds nothing agrees with everything.
func catalogue(t *testing.T) map[string]bool {
	t.Helper()
	source, err := os.ReadFile(panelCatalogue)
	if err != nil {
		t.Fatalf("reading the window's catalogue: %v", err)
	}
	sentences, err := catalogueKeys(string(source))
	if err != nil {
		t.Fatalf("reading the window's catalogue: %v", err)
	}
	if len(sentences) < 300 {
		t.Fatalf("read %d sentences out of %s: too few to be the catalogue", len(sentences),
			panelCatalogue)
	}
	return sentences
}

// worded is the demand every vocabulary makes: each value has its sentence, there are enough values
// for the reading to mean something, and nothing else sits under the prefix but what `panelOwn`
// allows.
func worded(t *testing.T, what, prefix string, values []string, key func(string) string) {
	t.Helper()
	sentences := catalogue(t)
	if len(values) < 2 {
		t.Fatalf("read %d values for %s: the reader has lost its way", len(values), what)
	}
	wanted := make(map[string]bool, len(values))
	for _, value := range values {
		wanted[key(value)] = true
		if !sentences[key(value)] {
			t.Errorf("this side sends %q as %s, and the window has no sentence for it: %s is not "+
				"in the catalogue", value, what, key(value))
		}
	}
	for path := range sentences {
		if !strings.HasPrefix(path, prefix+".") {
			continue
		}
		if wanted[path] {
			continue
		}
		if _, own := panelOwn[path]; own {
			continue
		}
		t.Errorf("the catalogue has %s, and this side sends no value for it: a sentence outlives "+
			"the value it words", path)
	}
	// An exception that names a sentence the catalogue does not have is one nobody needs.
	for path := range panelOwn {
		if strings.HasPrefix(path, prefix+".") && !sentences[path] {
			t.Errorf("panelOwn excuses %s, and the catalogue has no such sentence", path)
		}
	}
}

// named is every string constant a file declares under names beginning with a prefix, in the order
// it declares them. The name is how the file groups them — `Code…`, `Reason…`, `Failure…` — and the
// value is what crosses: the name is this side's word for it and the catalogue never sees it.
func named(t *testing.T, path, prefix string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var values []string
	for _, decl := range file.Decls {
		general, ok := decl.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			var matched bool
			for _, name := range value.Names {
				matched = matched || strings.HasPrefix(name.Name, prefix)
			}
			if !matched {
				continue
			}
			for _, one := range value.Values {
				literal, ok := one.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				values = append(values, strings.Trim(literal.Value, `"`))
			}
		}
	}
	if len(values) < 2 {
		t.Fatalf("read %d %s constants out of %s: too few to be its list", len(values), prefix, path)
	}
	return values
}

// A refusal crosses as a code and no sentence: the window words it, and a code with no sentence is
// a screen drawing the domain's own word — `nameTooLong`.
func TestEveryRefusalHasASentence(t *testing.T) {
	codes := named(t, "../../domain/failure.go", "Code")
	worded(t, "a refusal", "errors.codes", codes, func(code string) string {
		return "errors.codes." + code
	})
}

// A startup that did not get as far as a window has a kind, and the kind is what the first screen
// says its sentence by. The kinds are the desktop's — see desktop/failure.go — so they come
// from there.
func TestEveryStartupFailureHasASentence(t *testing.T) {
	kinds := named(t, "../desktop/failure.go", "Failure")
	worded(t, "a startup failure", "startup.failed", kinds, func(kind string) string {
		return "startup.failed." + kind
	})
}

// A command pasted into the request bar that could not be read is refused with a reason, and the
// reason is a contract with the catalogue: the values are written for the sentence, not for this
// side. The reader is `internal/command`'s, so its reasons are read out of it.
func TestEveryCommandReasonHasASentence(t *testing.T) {
	reasons := named(t, "../../command/command.go", "Reason")
	worded(t, "a reason a command was refused", "request.parseError", reasons,
		func(reason string) string { return "request.parseError." + reason })
}

// The auth schemes are the one place this side writes a catalogue key out whole: a field's label
// and placeholder, a menu entry, a note. Nothing checked them, so a key with a letter wrong drew
// itself in the popover.
//
// They are read as the only dotted literals the domain's auth file has — `request.auth.none` and
// its neighbours — which is what that file holds: no path, no filename, nothing else with a dot.
func TestEveryKeyGoNamesIsInTheCatalogue(t *testing.T) {
	source, err := os.ReadFile("../../domain/auth.go")
	if err != nil {
		t.Fatalf("reading the domain's auth schemes: %v", err)
	}
	pattern := regexp.MustCompile(`"(request\.[a-zA-Z0-9_.]+)"`)
	seen := map[string]bool{}
	var keys []string
	for _, found := range pattern.FindAllStringSubmatch(string(source), -1) {
		if seen[found[1]] {
			continue
		}
		seen[found[1]] = true
		keys = append(keys, found[1])
	}
	if len(keys) < 50 {
		t.Fatalf("read %d keys out of the domain's auth file: too few to be its list", len(keys))
	}
	sentences := catalogue(t)
	for _, key := range keys {
		if !sentences[key] {
			t.Errorf("this side names the key %s and the catalogue has no such sentence: the "+
				"popover would draw the key itself", key)
		}
	}
}
