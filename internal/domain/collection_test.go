package domain

import "testing"

// A variable's value is text however it was stored: `2` and `"2"` both answer `{{page}}` with 2.
func TestValueTextSpellsAnyValueAsText(t *testing.T) {
	for _, c := range []struct {
		value any
		want  string
	}{
		{float64(2), "2"},
		{"2", "2"},
		{map[string]any{"a": true}, `{"a":true}`},
		{nil, ""},
	} {
		if got := ValueText(c.value); got != c.want {
			t.Errorf("ValueText(%v) = %q, want %q", c.value, got, c.want)
		}
	}
}
