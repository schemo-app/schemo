package lens

import (
	"encoding/json"
	"strings"
	"testing"

	"schemo/internal/domain"
)

const users = `{"users": [
  {"id": 1, "name": "Ann", "email": "ann@x.io", "born": "2001-02-03", "tags": ["a"],
   "manager": null,
   "created": "2024-01-02T03:04:05Z", "profile": {"bio": "hi", "site-url": "https://a.b"}},
  {"id": 2, "name": "Bob", "email": null, "tags": [], "manager": {"id": 1},
   "created": "2024-01-03T03:04:05Z", "profile": {"bio": "yo"}}
]}`

func typesOf1(t *testing.T, format domain.TypeFormat) string {
	t.Helper()
	out, err := mustOpen(t, "application/json", users).Types("$", format)
	if err != nil {
		t.Fatalf("Types(%s): %v", format, err)
	}
	return out
}

func mustContain(t *testing.T, out string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(out, part) {
			t.Errorf("output lacks %q:\n%s", part, out)
		}
	}
}

func TestTheTypeIsWhatAllTheElementsHaveInCommon(t *testing.T) {
	out := typesOf1(t, domain.TypeTypeScript)
	mustContain(t, out,
		"export interface Response {\n  users: User[];",
		"export interface User {",
		"  id: number;",
		"  email: string | null;",
		"  born?: string; // date",
		"  created: string; // date-time",
		"  manager: Manager | null;",
		"  tags: string[];",
		"export interface Profile {\n  bio: string;\n  \"site-url\"?: string; // uri",
	)
}

func TestTheSameDocumentIsTypedForGo(t *testing.T) {
	out := typesOf1(t, domain.TypeGo)
	mustContain(t, out,
		"import \"time\"",
		"type User struct {",
		"ID int64 `json:\"id\"`",
		"Email *string `json:\"email\"`",
		"Born *string `json:\"born,omitempty\"`",
		"Created time.Time `json:\"created\"`",
		"Manager *Manager `json:\"manager\"`",
		"Tags []string `json:\"tags\"`",
		"SiteURL *string `json:\"site-url,omitempty\"`",
	)
}

func TestTheSameDocumentIsTypedForPython(t *testing.T) {
	out := typesOf1(t, domain.TypePython)
	mustContain(t, out,
		"from typing import Any, NotRequired, TypedDict",
		"class User(TypedDict):",
		"    email: str | None",
		"    born: NotRequired[str]",
		"    manager: Manager | None",
		`Profile = TypedDict("Profile", {`,
		`"site-url": NotRequired[str],`,
	)
	profile, user := strings.Index(out, `Profile = TypedDict`), strings.Index(out, "class User")
	if profile < 0 || profile > user {
		t.Error("Profile is used by User before it is defined")
	}
}

func TestTheSameDocumentIsDescribedAsAJSONSchema(t *testing.T) {
	out := typesOf1(t, domain.TypeJSONSchema)
	var schema map[string]any
	if err := json.Unmarshal([]byte(out), &schema); err != nil {
		t.Fatalf("the schema is not JSON: %v\n%s", err, out)
	}
	mustContain(t, out, `"$schema": "https://json-schema.org/draft/2020-12/schema"`,
		`"type": [`, `"format": "date-time"`, `"required": [`)
}

func TestAMapFromIdsIsARecordAndNotAStructPerKey(t *testing.T) {
	doc := mustOpen(t, "application/json", `{"1": {"n": "a"}, "2": {"n": "b"}, "3": {"n": "c"}}`)
	out, err := doc.Types("$", domain.TypeTypeScript)
	if err != nil {
		t.Fatalf("Types: %v", err)
	}
	mustContain(t, out, "export type Response = Record<string, Response2>",
		"export interface Response2 {")
}

func TestNamesFollowTheKeys(t *testing.T) {
	singulars := map[string]string{"authors": "author", "boxes": "box", "data": "data", "is": "is"}
	for key, want := range singulars {
		if got := singular(key); got != want {
			t.Errorf("singular(%q) = %q, want %q", key, got, want)
		}
	}
	names := map[string]string{"user_id": "UserID", "apiUrl": "APIURL", "created-at": "CreatedAt"}
	for key, want := range names {
		if got := goName(key); got != want {
			t.Errorf("goName(%q) = %q, want %q", key, got, want)
		}
	}
}
