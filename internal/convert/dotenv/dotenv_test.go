package dotenv

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []Entry
	}{
		{
			name: "simple assignments",
			text: "BASE_URL=https://api.example.com\nPAGE_SIZE=10",
			want: []Entry{
				{Name: "BASE_URL", Value: "https://api.example.com"},
				{Name: "PAGE_SIZE", Value: "10"},
			},
		},
		{
			name: "comments, blanks and export",
			text: "# a comment\n\nexport API_TOKEN=abc\n   \n",
			want: []Entry{{Name: "API_TOKEN", Value: "abc", Secret: true}},
		},
		{
			name: "quotes are stripped, both styles",
			text: "A=\"double\"\nB='single'",
			want: []Entry{{Name: "A", Value: "double"}, {Name: "B", Value: "single"}},
		},
		{
			name: "a value may contain equals signs",
			text: "URL=https://example.com/?a=1&b=2",
			want: []Entry{{Name: "URL", Value: "https://example.com/?a=1&b=2"}},
		},
		{
			name: "lines without an assignment are skipped",
			text: "JUST_A_WORD\n=no-name\n",
			want: nil,
		},
		{
			name: "CRLF and spaces around the pair",
			text: "  ITEM  =  value with spaces  \r\nOTHER=1\r\n",
			want: []Entry{
				{Name: "ITEM", Value: "value with spaces"},
				{Name: "OTHER", Value: "1"},
			},
		},
		{
			name: "empty value is a value",
			text: "EMPTY=",
			want: []Entry{{Name: "EMPTY", Value: ""}},
		},
		{
			name: "secret names are guessed, values are not searched",
			text: "PASSWORD=p\nGITHUB_TOKEN=t\nAUTH_HEADER=a\nAPI_KEY=k\nSOMETHING=my-secret-thing",
			want: []Entry{
				{Name: "PASSWORD", Value: "p", Secret: true},
				{Name: "GITHUB_TOKEN", Value: "t", Secret: true},
				{Name: "AUTH_HEADER", Value: "a", Secret: true},
				{Name: "API_KEY", Value: "k", Secret: true},
				{Name: "SOMETHING", Value: "my-secret-thing"},
			},
		},
		{
			name: "an unmatched ending quote stays",
			text: `BROKEN="unclosed`,
			want: []Entry{{Name: "BROKEN", Value: `"unclosed`}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.text)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse(%q) = %#v, want %#v", tc.text, got, tc.want)
			}
		})
	}
}
