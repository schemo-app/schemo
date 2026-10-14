package lens

import (
	"strings"
	"testing"

	"schemo/internal/domain"
)

func diagnose(t *testing.T, status int, body string) (domain.Diagnosis, bool) {
	t.Helper()
	return mustOpen(t, "application/json", body).Diagnose(status)
}

// have is the problem that is about a member of the request.
func have(d domain.Diagnosis, where, field string) *domain.Problem {
	for i := range d.Problems {
		if d.Problems[i].Where == where && d.Problems[i].Field == field {
			return &d.Problems[i]
		}
	}
	return nil
}

// join writes a fixture over several lines without changing what it says.
func join(lines ...string) string { return strings.Join(lines, "") }

// Every shape below is one a server really writes. A reading is right when it names the format and
// says which member of the request a complaint is about.
func TestAnErrorIsReadInTheShapeTheServerWroteIt(t *testing.T) {
	cases := []struct {
		name, format string
		status       int
		body         string
		field        [2]string // where, field of a problem that must be found
	}{
		{"laravel", "validation", 422, join(
			`{"message": "The given data was invalid.", `,
			`"errors": {"email": ["Taken."], "items.0.name": ["Required."]}}`),
			[2]string{"body", "/items/0/name"}},
		{"rfc9457 with aspnet errors", "problem", 400, join(
			`{"type": "https://tools.ietf.org/html/rfc9110#section-15.5.1", `,
			`"title": "One or more validation errors occurred.", "status": 400, `,
			`"errors": {"Email": ["Invalid."], "$.address.street": ["Too long."]}}`),
			[2]string{"body", "/address/street"}},
		{"jsonapi", "jsonapi", 422, join(
			`{"errors": [{"status": "422", "title": "Invalid attribute", `,
			`"detail": "Name is blank", "source": {"pointer": "/data/attributes/name"}}]}`),
			[2]string{"body", "/data/attributes/name"}},
		{"jsonapi query parameter", "jsonapi", 400,
			`{"errors": [{"title": "Bad sort", "source": {"parameter": "sort"}}]}`,
			[2]string{"query", "sort"}},
		{"fastapi", "validation", 422, join(
			`{"detail": [{"loc": ["body", "user", "email"], "msg": "field required", `,
			`"type": "value_error.missing"}, {"loc": ["query", "page"], `,
			`"msg": "not a number", "type": "type_error"}]}`),
			[2]string{"query", "page"}},
		{"django rest", "validation", 400, join(
			`{"email": ["Enter a valid address."], "address": {"zip": ["Required."]}, `,
			`"non_field_errors": ["Bad."]}`),
			[2]string{"body", "/address/zip"}},
		{"spring", "spring", 400, join(
			`{"timestamp": "2024-01-01", "status": 400, "error": "Bad Request", `,
			`"message": "Validation failed", "path": "/users", "errors": `,
			`[{"field": "name", "defaultMessage": "must not be blank"}]}`),
			[2]string{"body", "/name"}},
		{"payment api", "error", 402, join(
			`{"error": {"type": "card_error", "code": "card_declined", `,
			`"message": "Declined.", "param": "card[number]"}}`),
			[2]string{"body", "/card/number"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := diagnose(t, c.status, c.body)
			if !ok || got.Format != c.format {
				t.Fatalf("diagnosis = %+v, %v, want the format %q", got, ok, c.format)
			}
			if have(got, c.field[0], c.field[1]) == nil {
				t.Errorf("problems = %+v, want one about %v", got.Problems, c.field)
			}
		})
	}
}

// A response is diagnosed only when it is an error that names a part of the request.
func TestAResponseThatNamesNothingOfTheRequestIsNotDiagnosed(t *testing.T) {
	for _, c := range []struct {
		status int
		body   string
	}{
		{200, `{"errors": {"email": ["Taken."]}}`},
		{404, `{"id": 1, "name": "Ann"}`},
		{500, `{"message": "Something broke"}`},
		{400, `{"error": "invalid_grant", "error_description": "The code has expired."}`},
		{200, `{"data": null, "errors": [{"message": "Not allowed", "path": ["user"]}]}`},
		{400, `[1, 2, 3]`},
	} {
		if got, ok := diagnose(t, c.status, c.body); ok {
			t.Errorf("%d %s was read as %+v", c.status, c.body, got)
		}
	}
}

func TestAProblemIsFoundInTheRequestWhereverTheTokensAre(t *testing.T) {
	body := `{"name": "{{name}}", "Email": {{email}}, ` +
		`"items": [{"qty": 1}, {"qty": "{{q}}"}]}`
	cases := map[string]string{
		"/name":          `"{{name}}"`,
		"/email":         `{{email}}`, // the server said `email`, the request wrote `Email`
		"/items/1/qty":   `"{{q}}"`,
		"/items/0":       `{"qty": 1}`,
		"/items/0/gone":  "",
		"/nothing/below": "",
	}
	for pointer, want := range cases {
		span, ok := Locate("application/json", body, pointer)
		if want == "" {
			if ok {
				t.Errorf("%s was found, want nothing", pointer)
			}
			continue
		}
		if !ok || body[span.Start:span.End] != want {
			t.Errorf("%s = %+v, %v, want %q", pointer, span, ok, want)
		}
	}
}
