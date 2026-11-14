package lens

import "testing"

func TestEveryNotationQuotesAKeyThatIsNotAnIdentifier(t *testing.T) {
	steps := []step{{key: "data"}, {index: 2, isIndex: true}, {key: `it's \ odd`}}
	cases := map[string]string{
		"JSONPath":   `$.data[2]['it\'s \\ odd']`,
		"jq":         `.data[2].["it's \\ odd"]`,
		"JavaScript": `body.data[2]["it's \\ odd"]`,
		"Python":     `body["data"][2]["it's \\ odd"]`,
	}
	for _, form := range jsonForms(steps) {
		want, ok := cases[form.Syntax]
		if ok && form.Text != want {
			t.Errorf("%s = %s, want %s", form.Syntax, form.Text, want)
		}
	}
}
