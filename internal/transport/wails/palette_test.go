package wails

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// A colour is a word that Go stores and the window draws. The words are listed once, in the
// palette's own table, and the two places that must answer for each — the token the stylesheet
// defines for it and the catalogue's name — are held to it here, so a colour added to the table
// cannot go unpainted or unnamed.

const (
	workspacePalette   = "../../../frontend/src/components/workspaces/palette.ts"
	environmentPalette = "../../../frontend/src/components/environments/palette.ts"
	stylesheet         = "../../../frontend/src/style.css"
)

// paletteWords are the keys of the table a palette file opens with `<name> = {`.
func paletteWords(t *testing.T, path, table string) []string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	opened := regexp.MustCompile(`(?s)` + table + ` = \{(.*?)\n\} as const`)
	block := opened.FindStringSubmatch(string(source))
	if block == nil {
		t.Fatalf("%s has no %s table", path, table)
	}
	words := regexp.MustCompile(`(?m)^  (\w+):`).FindAllStringSubmatch(block[1], -1)
	if len(words) < 3 {
		t.Fatalf("read %d colours out of %s: too few to be its palette", len(words), path)
	}
	out := make([]string, 0, len(words))
	for _, word := range words {
		out = append(out, word[1])
	}
	return out
}

// paletteTokens are the custom properties the table points its words at: `blue: 'var(--accent)'`
// answers with `--accent`.
func paletteTokens(t *testing.T, path, table string) map[string]string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	opened := regexp.MustCompile(`(?s)` + table + ` = \{(.*?)\n\} as const`)
	block := opened.FindStringSubmatch(string(source))
	if block == nil {
		t.Fatalf("%s has no %s table", path, table)
	}
	pattern := `(?m)^  (\w+): 'var\((--[\w-]+)\)',`
	rows := regexp.MustCompile(pattern).FindAllStringSubmatch(block[1], -1)
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row[1]] = row[2]
	}
	return out
}

// themeBlock is what a theme's own `:root` rule declares, so a token is looked for where it would
// have to be written for the theme to see it.
func themeBlock(t *testing.T, css, selector string) string {
	t.Helper()
	opened := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(selector) + ` \{`).FindStringIndex(css)
	if opened == nil {
		t.Fatalf("the stylesheet has no %s rule", selector)
	}
	rest := css[opened[1]:]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatalf("the %s rule is never closed", selector)
	}
	return rest[:end]
}

func TestEveryWorkspaceColourIsPaintedAndNamed(t *testing.T) {
	css, err := os.ReadFile(stylesheet)
	if err != nil {
		t.Fatalf("reading the stylesheet: %v", err)
	}
	light := themeBlock(t, string(css), ":root")
	dark := themeBlock(t, string(css), ":root.dark")
	tokens := paletteTokens(t, workspacePalette, "WORKSPACE_TINTS")
	sentences := catalogue(t)
	for _, word := range paletteWords(t, workspacePalette, "WORKSPACE_TINTS") {
		token := tokens[word]
		if token == "" {
			t.Errorf("the workspace colour %q names no token in its own palette", word)
			continue
		}
		if !strings.Contains(light, token+":") {
			t.Errorf("the workspace colour %q stands on %s, which :root does not define", word, token)
		}
		if !strings.Contains(dark, token+":") {
			t.Errorf("the workspace colour %q stands on %s, which :root.dark does not define", word, token)
		}
		if key := "workspaces.color" + capital(word); !sentences[key] {
			t.Errorf("the workspace colour %q has no name: %s is not in the catalogue", word, key)
		}
	}
}

func TestEveryEnvironmentColourIsNamed(t *testing.T) {
	sentences := catalogue(t)
	for _, word := range paletteWords(t, environmentPalette, "PALETTE") {
		if key := "environments.color" + capital(word); !sentences[key] {
			t.Errorf("the environment colour %q has no name: %s is not in the catalogue", word, key)
		}
	}
}

func capital(word string) string { return strings.ToUpper(word[:1]) + word[1:] }
