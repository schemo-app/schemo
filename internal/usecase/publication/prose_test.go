package publication

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	snapshotv1 "github.com/schemo-app/proto/snapshot/v1"
)

func TestDescriptionSections(t *testing.T) {
	tests := []struct {
		name, markdown string
		want           snapshotv1.Prose
	}{
		{
			name:     "sections retain nested Markdown and leave other sections in the lead",
			markdown: "Intro.\n\n## **Authorization**\nSend a token.\n\n### Headers\n`Authorization`\n\n## Environments\nLocal and production.\n\n## Errors\n- 400: invalid\n\n## Rate limits\n60 requests.",
			want: snapshotv1.Prose{
				Lead: "Intro.\n\n## Environments\nLocal and production.",
				Auth: "Send a token.\n\n### Headers\n`Authorization`", Errors: "- 400: invalid", Limits: "60 requests.",
			},
		},
		{
			name:     "fenced and quoted headings do not create sections",
			markdown: "```md\n## Errors\nexample\n```\n\n> ## Limits\n> example\n\n### Authorization\nNested.",
			want:     snapshotv1.Prose{Lead: "```md\n## Errors\nexample\n```\n\n> ## Limits\n> example\n\n### Authorization\nNested."},
		},
		{
			name:     "formatted links code and multiline headings retain their visible titles",
			markdown: "## [**Auth**](https://example.com)\nSend a token.\n\n## `Errors`\nBad request.\n\nRate\nlimits\n------\n60 requests.",
			want:     snapshotv1.Prose{Auth: "Send a token.", Errors: "Bad request.", Limits: "60 requests."},
		},
		{
			name:     "setext headings and CRLF are parsed as Markdown",
			markdown: "Intro.\r\n\r\nErrors\r\n------\r\nBad request.\r\n\r\n## Limits\r\n60 requests.\r\n",
			want:     snapshotv1.Prose{Lead: "Intro.", Errors: "Bad request.", Limits: "60 requests."},
		},
		{
			name:     "duplicate sections retain their content and H1 closes a section",
			markdown: "## Errors\nFirst.\n\n## Errors\nSecond.\n\n# More\nOther text.",
			want:     snapshotv1.Prose{Lead: "# More\nOther text.", Errors: "First.\n\nSecond."},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := descriptionProse(test.markdown); got != test.want {
				t.Errorf("prose = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestLocalizedDescriptionHeadings(t *testing.T) {
	for _, locale := range []string{"en", "ru"} {
		t.Run(locale, func(t *testing.T) {
			raw, err := os.ReadFile("../../../frontend/src/locales/" + locale + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var catalog struct {
				Collections map[string]string `json:"collections"`
			}
			if err := json.Unmarshal(raw, &catalog); err != nil {
				t.Fatal(err)
			}
			var document strings.Builder
			for _, key := range []string{"docsAuthorization", "docsErrors", "docsLimits"} {
				document.WriteString("## " + catalog.Collections[key] + "\n" + key + "\n\n")
			}
			want := snapshotv1.Prose{Auth: "docsAuthorization", Errors: "docsErrors", Limits: "docsLimits"}
			if got := descriptionProse(document.String()); got != want {
				t.Errorf("prose = %+v, want %+v", got, want)
			}
		})
	}
}
