package domain

import (
	"slices"
	"testing"
)

func TestPathParamsAreTheNamedSegmentsOfTheAddress(t *testing.T) {
	cases := []struct {
		address string
		want    []string
	}{
		{"{{baseUrl}}/products/:product", []string{"product"}},
		{"{{baseUrl}}/attributes/:attribute/enums/:enum", []string{"attribute", "enum"}},
		{"https://api.acme.com/v1/users/:id/posts/:id", []string{"id"}},
		{"/groups/:attribute_group/destroy", []string{"attribute_group"}},
		{"/tenants/:tenant-id/items", []string{"tenant-id"}},
		{"/files/:id.json", []string{"id"}},
	}
	for _, c := range cases {
		if got := PathParams(c.address); !slices.Equal(got, c.want) {
			t.Errorf("PathParams(%q) = %v, want %v", c.address, got, c.want)
		}
	}
}

func TestAPortAndASchemeAreNotPathParams(t *testing.T) {
	for _, address := range []string{
		"http://localhost:8080/api",
		"https://host:443",
		"{{host}}:8080/api",
		"/api/v1:beta/items",
		"/items/:1",
		"/items/:",
		"localhost:3000",
	} {
		if got := PathParams(address); len(got) != 0 {
			t.Errorf("PathParams(%q) = %v, want none", address, got)
		}
	}
}

func TestWhatLiesBehindTheQueryIsNotAPathParam(t *testing.T) {
	for _, address := range []string{
		"/items?next=/:cursor",
		"/items#/:anchor",
	} {
		if got := PathParams(address); len(got) != 0 {
			t.Errorf("PathParams(%q) = %v, want none", address, got)
		}
	}
}

func TestATypedVariableInFrontOfThePathDoesNotTakeItsParametersAway(t *testing.T) {
	for _, address := range []string{"{{/users/:id", "{{host}}/users/:id", "{{ho/users/:id"} {
		if got := PathParams(address); !slices.Equal(got, []string{"id"}) {
			t.Errorf("PathParams(%q) = %v, want the id: a half-typed variable is plain text", address, got)
		}
	}
}

func TestFillPathWritesTheValueInPlaceOfTheName(t *testing.T) {
	rows := []Row{{Name: "attribute", Value: "color"}, {Name: "enum", Value: "red"}}
	got := FillPath("{{baseUrl}}/attributes/:attribute/enums/:enum?lang=en", rows)
	want := "{{baseUrl}}/attributes/color/enums/red?lang=en"
	if got != want {
		t.Errorf("FillPath = %q, want %q", got, want)
	}
}

func TestFillPathKeepsWhatHasNoValue(t *testing.T) {
	rows := []Row{{Name: "attribute", Value: ""}, {Name: "other", Value: "x"}}
	address := "/attributes/:attribute/enums/:enum"
	if got := FillPath(address, rows); got != address {
		t.Errorf("FillPath = %q, want the address as written", got)
	}
}

func TestFillPathEscapesTheValueButNotAVariableInIt(t *testing.T) {
	rows := []Row{{Name: "product", Value: "a b/c"}}
	if got := FillPath("/p/:product", rows); got != "/p/a%20b%2Fc" {
		t.Errorf("FillPath = %q, want the value as one escaped segment", got)
	}

	rows = []Row{{Name: "product", Value: "{{sku}}/x y"}}
	if got := FillPath("/p/:product", rows); got != "/p/{{sku}}%2Fx%20y" {
		t.Errorf("FillPath = %q, want the variable left for the resolver", got)
	}
}

func TestFillPathFillsEveryOccurrenceOfAName(t *testing.T) {
	rows := []Row{{Name: "id", Value: "7"}}
	if got := FillPath("/a/:id/b/:id", rows); got != "/a/7/b/7" {
		t.Errorf("FillPath = %q, want both filled", got)
	}
}

func TestPathParamNameKeepsWhatAnAddressCanCarry(t *testing.T) {
	cases := map[string]string{
		"product":         "product",
		"attribute_group": "attribute_group",
		"tenant-id":       "tenant-id",
		"item.id":         "item_id",
		"a b  c":          "a_b_c",
		"1st":             "_1st",
		"":                "_",
		"{x}":             "_x_",
	}
	for raw, want := range cases {
		if got := PathParamName(raw); got != want {
			t.Errorf("PathParamName(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestReplacePathParamsLeavesTheRestOfTheAddress(t *testing.T) {
	got := ReplacePathParams("{{baseUrl}}/a/:x/b?next=/:y#/:z", func(name string) string {
		return "{" + name + "}"
	})
	if want := "{{baseUrl}}/a/{x}/b?next=/:y#/:z"; got != want {
		t.Errorf("ReplacePathParams = %q, want %q", got, want)
	}
}
