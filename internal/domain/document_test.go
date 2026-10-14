package domain

import "testing"

// `2` and `"2"` answer `{{page}}` the same way, which is why a number is not dropped for not being
// text.
func TestValueTextSpellsANumberAsItsDigits(t *testing.T) {
	if got := ValueText(float64(2)); got != "2" {
		t.Errorf("ValueText(2) = %q, want \"2\"", got)
	}
	if got := ValueText("2"); got != "2" {
		t.Errorf("ValueText(\"2\") = %q, want \"2\"", got)
	}
}

func TestValueTextSpellsAnObjectAsItsJSON(t *testing.T) {
	if got := ValueText(map[string]any{"a": true}); got != `{"a":true}` {
		t.Errorf("ValueText(object) = %q, want its JSON", got)
	}
}

func TestValueTextOfNothingIsEmpty(t *testing.T) {
	if got := ValueText(nil); got != "" {
		t.Errorf("ValueText(nil) = %q, want empty", got)
	}
}
