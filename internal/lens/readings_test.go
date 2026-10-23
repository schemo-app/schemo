package lens

import (
	"testing"

	"schemo/internal/domain"
)

func readOne(t *testing.T, kind domain.NodeKind, scalar string) []domain.Reading {
	t.Helper()
	return readingsOf(&node{kind: kind, scalar: scalar})
}

func find(list []domain.Reading, kind string) *domain.Reading {
	for i := range list {
		if list[i].Kind == kind {
			return &list[i]
		}
	}
	return nil
}

func rowOf(r *domain.Reading, key string) string {
	for _, one := range r.Rows {
		if one.Key == key {
			return one.Value
		}
	}
	return ""
}

func TestATokenIsReadWithoutItsKey(t *testing.T) {
	// {"alg":"HS256","typ":"JWT"} . {"sub":"1234","exp":1893456000,"iss":"acme"}
	const token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
		"eyJzdWIiOiIxMjM0IiwiZXhwIjoxODkzNDU2MDAwLCJpc3MiOiJhY21lIn0.c2ln"

	jwt := find(readOne(t, domain.NodeString, token), "jwt")
	if jwt == nil {
		t.Fatal("a token was not read as one")
	}
	if rowOf(jwt, "alg") != "HS256" || rowOf(jwt, "iss") != "acme" || rowOf(jwt, "sub") != "1234" {
		t.Errorf("rows = %+v, want the algorithm, the issuer and the subject", jwt.Rows)
	}
	if rowOf(jwt, "exp") != "2030-01-01T00:00:00.000Z" {
		t.Errorf("exp = %q, want the expiry as a moment", rowOf(jwt, "exp"))
	}
	if jwt.Nested == "" {
		t.Error("the claims were not offered as a document")
	}
}

func TestAnIdentifierGivesUpItsMoment(t *testing.T) {
	cases := map[string]struct{ kind, want string }{
		"018f3b7e-4a1c-7000-8000-000000000000": {"uuid", "2024-05-02T"},
		"01ARZ3NDEKTSV4RRFFQ69G5FAV":           {"ulid", "2016-07-30T"},
		"507f1f77bcf86cd799439011":             {"objectid", "2012-10-17T"},
	}
	for value, want := range cases {
		r := find(readOne(t, domain.NodeString, value), want.kind)
		if r == nil {
			t.Errorf("%s was not read as a %s", value, want.kind)
			continue
		}
		if got := rowOf(r, "time"); len(got) < len(want.want) || got[:len(want.want)] != want.want {
			t.Errorf("%s time = %q, want it to begin %q", value, got, want.want)
		}
	}
}

func TestANumberThatIsATimeIsOfferedAsOne(t *testing.T) {
	for _, text := range []string{"1700000000", "1700000000000"} {
		r := find(readOne(t, domain.NodeNumber, text), "timestamp")
		if r == nil || rowOf(r, "time") != "2023-11-14T22:13:20.000Z" {
			t.Errorf("%s = %+v, want 2023-11-14T22:13:20Z", text, r)
		}
	}
	if find(readOne(t, domain.NodeNumber, "42"), "timestamp") != nil {
		t.Error("42 was read as a time")
	}
}

func TestAnIntegerJavaScriptWouldRoundIsFlagged(t *testing.T) {
	r := find(readOne(t, domain.NodeNumber, "9007199254740993"), "bigint")
	if r == nil || rowOf(r, "exact") != "9007199254740993" ||
		rowOf(r, "asJavaScript") != "9007199254740992" {
		t.Errorf("reading = %+v, want the exact id and what a JavaScript client makes of it", r)
	}
	if find(readOne(t, domain.NodeNumber, "9007199254740991"), "bigint") != nil {
		t.Error("the largest safe integer was flagged")
	}
}

func TestATextThatIsAnEncodingIsDecoded(t *testing.T) {
	// base64 of {"a":1}
	encoded := readOne(t, domain.NodeString, "eyJhIjoxLCJiIjoyLCJjIjozfQ==")
	if r := find(encoded, "base64"); r == nil || r.Nested == "" {
		t.Errorf("reading = %+v, want the document inside", r)
	}
	if r := find(readOne(t, domain.NodeString, `{"a": 1}`), "json"); r == nil || r.Nested == "" {
		t.Errorf("reading = %+v, want the document written as text", r)
	}
	if find(readOne(t, domain.NodeString, "just-a-long-identifier-name"), "base64") != nil {
		t.Error("an ordinary identifier was read as an encoding")
	}
}

func TestAnAddressAndAColourAreRead(t *testing.T) {
	address := "https://api.example.com/v1/items?page=2&sort=name"
	url := find(readOne(t, domain.NodeString, address), "url")
	if url == nil || rowOf(url, "host") != "api.example.com" || rowOf(url, "param:page") != "2" {
		t.Errorf("url = %+v, want the host and the page parameter", url)
	}
	if find(readOne(t, domain.NodeString, "#3366ff"), "colour") == nil {
		t.Error("a hex colour was not offered a swatch")
	}
	written := readOne(t, domain.NodeString, "2026-09-28T10:00:00+05:00")
	if r := find(written, "datetime"); r == nil || rowOf(r, "time") != "2026-09-28T05:00:00.000Z" {
		t.Errorf("datetime = %+v, want the moment in UTC", r)
	}
}
