package search

import (
	"encoding/json"
	"strings"
	"testing"

	"schemo/internal/domain"
)

// The tiers are the design's, and this is where they are pinned: a whole name beats a name, a name
// beats the trail it sits in, and the trail beats a value that merely holds the word.
func TestMatchPicksTheTier(t *testing.T) {
	cases := []struct {
		name  string
		query string
		hit   domain.SearchHit
		want  domain.SearchMatch
		ok    bool
	}{
		{
			name:  "the whole name",
			query: "Пользователи",
			hit:   domain.SearchHit{Title: "Пользователи", Path: []string{"Пользователи"}},
			want:  domain.MatchExact,
			ok:    true,
		},
		{
			name:  "part of a name",
			query: "льзова",
			hit:   domain.SearchHit{Title: "Пользователи"},
			want:  domain.MatchName,
			ok:    true,
		},
		{
			name:  "somewhere in the trail",
			query: "каталог",
			hit:   domain.SearchHit{Title: "/users/{id}", Path: []string{"Каталог API", "Пользователи"}},
			want:  domain.MatchPath,
			ok:    true,
		},
		{
			name:  "only a value holds it",
			query: "admin",
			hit:   domain.SearchHit{Title: "role", MatchText: "admin"},
			want:  domain.MatchValue,
			ok:    true,
		},
		{
			name:  "nowhere",
			query: "ничего",
			hit: domain.SearchHit{Title: "Пользователи", Path: []string{"Каталог API"},
				MatchText: "admin"},
			want: 0,
			ok:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := matchFolded(strings.ToLower(tc.query), tc.hit)
			if ok != tc.ok {
				t.Fatalf("found = %v, want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("tier = %v, want %v", got, tc.want)
			}
		})
	}
}

// The reason matching is not a LIKE: SQLite folds case for ASCII alone, and every name in this app
// is Russian. A query typed in the wrong case has to find the row anyway.
func TestMatchIgnoresCaseInRussian(t *testing.T) {
	hit := domain.SearchHit{Title: "Пользователи", Path: []string{"Каталог API"}}
	for _, query := range []string{"ПОЛЬЗОВАТЕЛИ", "пользователи", "ПоЛьЗоВаТеЛи", "КАТАЛОГ"} {
		if _, ok := matchFolded(strings.ToLower(query), hit); !ok {
			t.Fatalf("%q did not find %q", query, hit.Title)
		}
	}
}

// The window draws the name of a variable and nothing else, so a value that answered the query must
// not travel with the answer: it has nowhere to be drawn, and when the variable is a secret it is
// the one thing in this database that may not leave.
func TestHitDoesNotCarryWhatMatched(t *testing.T) {
	encoded, err := json.Marshal(domain.SearchHit{
		Kind:      domain.SearchEnvironment,
		Title:     "token",
		MatchText: "s3cr3t-value",
	})
	if err != nil {
		t.Fatalf("encoding the hit: %v", err)
	}
	if strings.Contains(string(encoded), "s3cr3t-value") {
		t.Fatalf("the value that matched travelled: %s", encoded)
	}
}
