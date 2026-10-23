package request

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"schemo/internal/domain"
)

func materialize(t *testing.T, auth domain.Auth) domain.AuthOutput {
	t.Helper()
	out, err := NewAuthorizer(nil, nil).Materialize(context.Background(), auth,
		domain.AuthRequest{Method: "GET", URL: "https://api.example.com/a"})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	return out
}

func headerOf(out domain.AuthOutput, name string) (string, bool) {
	for _, pair := range out.Headers {
		if pair.Name == name {
			return pair.Value, true
		}
	}
	return "", false
}

// The prefix is a field rather than the word "Bearer": not every server says Bearer.
func TestBearer(t *testing.T) {
	out := materialize(t,
		domain.NewAuth(domain.AuthBearer).With("prefix", "Bearer").With("token", "abc123"))
	if value, ok := headerOf(out, "Authorization"); !ok || value != "Bearer abc123" {
		t.Errorf("authorization = %q, want the prefix and the token", value)
	}

	out = materialize(t,
		domain.NewAuth(domain.AuthBearer).With("prefix", "Token").With("token", "abc123"))
	if value, _ := headerOf(out, "Authorization"); value != "Token abc123" {
		t.Errorf("authorization = %q, want the prefix the user named", value)
	}

	// A token that resolves to nothing is not a token: `Bearer ` with nothing after it is a header
	// the server reads as a mistake.
	out = materialize(t, domain.NewAuth(domain.AuthBearer).With("prefix", "Bearer"))
	if !out.Empty() {
		t.Errorf("output = %+v, want nothing for an empty token", out)
	}
}

// Basic is RFC 7617: the two halves joined by a colon and the pair encoded. The credential is
// decoded back rather than compared against a second encoding, so the test says what it must hold.
func TestBasic(t *testing.T) {
	cases := []struct{ name, username, password, want string }{
		{"login and password", "user", "pass", "user:pass"},
		// The colon is the join, so a password may hold one and a login may not: cutting at the
		// first colon is the server's business, and the credential has to survive it.
		{"a colon in the password", "user", "a:b", "user:a:b"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := materialize(t,
				domain.NewAuth(domain.AuthBasic).Normalized().
					With("username", c.username).With("password", c.password))
			value, ok := headerOf(out, "Authorization")
			if !ok {
				t.Fatalf("headers = %+v, want an authorization", out.Headers)
			}
			encoded, found := strings.CutPrefix(value, "Basic ")
			if !found {
				t.Fatalf("authorization = %q, want the Basic scheme", value)
			}
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatalf("authorization = %q, want a base64 credential: %v", value, err)
			}
			if string(decoded) != c.want {
				t.Errorf("credential = %q, want %q", decoded, c.want)
			}
		})
	}

	// Both halves empty is a field nobody filled in, not a credential of a colon.
	out := materialize(t, domain.NewAuth(domain.AuthBasic))
	if !out.Empty() {
		t.Errorf("output = %+v, want nothing for a basic with neither half", out)
	}
}

// A key the server named itself: both halves are fields, and so is where it goes.
func TestAPIKey(t *testing.T) {
	key := domain.NewAuth(domain.AuthAPIKey).With("key", "X-API-Key").With("value", "s3cret")

	out := materialize(t, key.With("place", domain.PlaceHeader))
	if value, ok := headerOf(out, "X-API-Key"); !ok || value != "s3cret" {
		t.Errorf("headers = %+v, want the key as a header", out.Headers)
	}

	out = materialize(t, key.With("place", domain.PlaceQuery))
	if len(out.Headers) != 0 {
		t.Errorf("headers = %+v, want none for a key that travels in the query", out.Headers)
	}
	if len(out.Query) != 1 || out.Query[0].Name != "X-API-Key" || out.Query[0].Value != "s3cret" {
		t.Errorf("query = %+v, want the key as a parameter", out.Query)
	}

	// A key with no name is a header called nothing, which is what an unfilled field means.
	out = materialize(t, domain.NewAuth(domain.AuthAPIKey).With("value", "s3cret"))
	if !out.Empty() {
		t.Errorf("output = %+v, want nothing for a key with no name", out)
	}
}

// The two answers that are not credentials put nothing on the request, and a scheme this app cannot
// work out yet puts nothing there either rather than something wrong.
func TestSchemesThatCarryNothing(t *testing.T) {
	for _, kind := range []domain.AuthType{
		domain.AuthNone, domain.AuthInherit, domain.AuthType("nobody-has-this"),
	} {
		if out := materialize(t, domain.NewAuth(kind).Normalized()); !out.Empty() {
			t.Errorf("%s: output = %+v, want nothing", kind, out)
		}
	}
}

// The answers a scheme does not ask for are carried and never read, which is what makes keeping
// them safe: a user who looked at Basic and came back would otherwise paste the token again.
func TestASchemeReadsOnlyItsOwnFields(t *testing.T) {
	auth := domain.NewAuth(domain.AuthBasic).Normalized().
		With("username", "user").With("password", "pass").
		With("token", "left-over")

	out := materialize(t, auth)
	if len(out.Headers) != 1 {
		t.Fatalf("headers = %+v, want the one credential", out.Headers)
	}
	value := out.Headers[0].Value
	if !strings.HasPrefix(value, "Basic ") {
		t.Errorf("authorization = %q, want the Basic credential the scheme is", value)
	}
	if strings.Contains(value, "left-over") {
		t.Errorf("authorization = %q, want nothing of the scheme that is not in use", value)
	}
}

// Digest puts nothing on the request: the credential goes to the engine, the side that is there
// when the server says how. Its output is empty and meaningful at once.
func TestDigestHandsTheCredentialToTheEngine(t *testing.T) {
	auth := domain.NewAuth(domain.AuthDigest).Normalized().
		With("username", "user").With("password", "pass")
	out := materialize(t, auth)

	if !out.Empty() {
		t.Errorf("output = %+v, want no headers: nothing can be computed yet", out)
	}
	if out.Digest == nil || out.Digest.Username != "user" || out.Digest.Password != "pass" {
		t.Fatalf("digest = %+v, want the credential for the engine", out.Digest)
	}
	// And with neither half filled in there is no conversation to have.
	if half := materialize(t, domain.NewAuth(domain.AuthDigest)); half.Digest != nil {
		t.Errorf("digest = %+v, want nothing for a scheme nobody filled in", half.Digest)
	}
}
