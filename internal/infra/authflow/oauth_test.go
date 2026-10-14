package authflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"schemo/internal/domain"
)

// spyTokenEndpoint is a provider's token endpoint that remembers what it was asked: half of what
// these tests are about is what the request carried, and only the endpoint can say.
type spyTokenEndpoint struct {
	got    []map[string]string
	auth   []string
	token  string
	expiry int
}

func (e *spyTokenEndpoint) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form := map[string]string{}
		for key := range r.Form {
			form[key] = r.Form.Get(key)
		}
		e.got = append(e.got, form)
		e.auth = append(e.auth, r.Header.Get("Authorization"))

		if e.token == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": e.token,
			"token_type":   "Bearer",
			"expires_in":   e.expiry,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func oauthAuth(fields map[string]string) domain.Auth {
	auth := domain.NewAuth(domain.AuthOAuth2).Normalized()
	for key, value := range fields {
		auth = auth.With(key, value)
	}
	return auth
}

// A client credentials grant is the client asking for a token as itself: the answers the user gave
// go out in the form the provider expects, and what comes back is what the request carries.
func TestClientCredentialsGrant(t *testing.T) {
	endpoint := &spyTokenEndpoint{token: "issued-by-client", expiry: 3600}
	srv := endpoint.server(t)

	auth := oauthAuth(map[string]string{
		"grant": "client_credentials", "tokenUrl": srv.URL, "clientId": "id", "clientSecret": "secret",
		"scope": "read write", "audience": "https://api.example.com",
	})
	out, err := New(srv.Client(), nil).Materialize(context.Background(), auth, domain.AuthRequest{})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if value, ok := headerOf(out, "Authorization"); !ok || value != "Bearer issued-by-client" {
		t.Errorf("authorization = %q, want the fetched token behind the scheme's prefix", value)
	}

	if len(endpoint.got) != 1 {
		t.Fatalf("the endpoint was asked %d times, want once", len(endpoint.got))
	}
	form := endpoint.got[0]
	if form["grant_type"] != "client_credentials" {
		t.Errorf("grant_type = %q, want the grant the user chose", form["grant_type"])
	}
	// Several scopes are one space-separated field, which is how OAuth writes them.
	if form["scope"] != "read write" {
		t.Errorf("scope = %q, want what the user typed", form["scope"])
	}
	if form["audience"] != "https://api.example.com" {
		t.Errorf("audience = %q, want the one the user named", form["audience"])
	}
	// The default for how the client authenticates is the header, and the client's own credential is
	// not in the form as well: two credentials is a request some providers refuse.
	if !strings.HasPrefix(endpoint.auth[0], "Basic ") {
		t.Errorf("client authentication = %q, want the basic header the default asks for",
			endpoint.auth[0])
	}
	if _, present := form["client_secret"]; present {
		t.Errorf("form = %+v, want the secret in the header and not twice", form)
	}
}

// The other answer to «Client Authentication» puts the client's credential in the form instead,
// which some providers want and no provider can be guessed about.
func TestClientCredentialsInTheBody(t *testing.T) {
	endpoint := &spyTokenEndpoint{token: "issued-in-body", expiry: 3600}
	srv := endpoint.server(t)

	auth := oauthAuth(map[string]string{
		"grant": "client_credentials", "tokenUrl": srv.URL, "clientId": "id",
		"clientSecret": "secret", "clientAuth": "body",
	})
	if _, err := New(srv.Client(), nil).Materialize(context.Background(), auth,
		domain.AuthRequest{}); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if endpoint.auth[0] != "" {
		t.Errorf("client authentication = %q, want nothing in the header", endpoint.auth[0])
	}
	if endpoint.got[0]["client_id"] != "id" || endpoint.got[0]["client_secret"] != "secret" {
		t.Errorf("form = %+v, want the client's own credential in it", endpoint.got[0])
	}
}

// The password grant is the person asking for a token as themselves, and the login they gave goes
// out beside the client's own.
func TestPasswordGrant(t *testing.T) {
	endpoint := &spyTokenEndpoint{token: "issued-by-user", expiry: 3600}
	srv := endpoint.server(t)

	auth := oauthAuth(map[string]string{
		"grant": "password", "tokenUrl": srv.URL, "clientId": "id", "clientSecret": "secret",
		"owner": "user", "ownerPassword": "pass",
	})
	out, err := New(srv.Client(), nil).Materialize(context.Background(), auth, domain.AuthRequest{})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if value, _ := headerOf(out, "Authorization"); value != "Bearer issued-by-user" {
		t.Errorf("authorization = %q, want the fetched token", value)
	}

	form := endpoint.got[0]
	if form["grant_type"] != "password" {
		t.Errorf("grant_type = %q, want the password grant", form["grant_type"])
	}
	if form["username"] != "user" || form["password"] != "pass" {
		t.Errorf("form = %+v, want the credential the user typed", form)
	}
}

// A token is asked for once and used until it dies: two sends sharing an authorization share what
// was issued for it, which is the whole reason to keep one.
func TestATokenIsAskedForOnceAndKept(t *testing.T) {
	endpoint := &spyTokenEndpoint{token: "issued-once", expiry: 3600}
	srv := endpoint.server(t)
	materializer := New(srv.Client(), nil)

	auth := oauthAuth(map[string]string{
		"grant": "client_credentials", "tokenUrl": srv.URL, "clientId": "id", "clientSecret": "secret",
	})
	for i := 0; i < 3; i++ {
		if _, err := materializer.Materialize(context.Background(), auth,
			domain.AuthRequest{}); err != nil {
			t.Fatalf("Materialize: %v", err)
		}
	}
	if len(endpoint.got) != 1 {
		t.Errorf("the endpoint was asked %d times, want once", len(endpoint.got))
	}

	// And the window is told there is one, without being told what it is.
	held := materializer.Held(auth)
	if !held.Held || held.ExpiresAt == 0 {
		t.Errorf("held = %+v, want a token with an expiry", held)
	}
	if held.ExpiresAt <= time.Now().UnixMilli() {
		t.Error("a token good for an hour is reported with a time already past")
	}

	// Dropping it is what «Clear» does: the next send asks for another one.
	materializer.Forget(auth)
	if materializer.Held(auth).Held {
		t.Error("the token survived being dropped")
	}
	if _, err := materializer.Materialize(context.Background(), auth,
		domain.AuthRequest{}); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(endpoint.got) != 2 {
		t.Errorf("the endpoint was asked %d times, want another one after the drop", len(endpoint.got))
	}
}

// A token whose remaining life is inside the margin before it dies is asked for again rather than
// sent once more: asking is cheaper than the refusal a token that expired in flight would get.
func TestATokenAboutToDieIsAskedForAgain(t *testing.T) {
	// Five seconds is inside the ten the materializer treats as "already dead".
	endpoint := &spyTokenEndpoint{token: "short-lived", expiry: 5}
	srv := endpoint.server(t)
	materializer := New(srv.Client(), nil)

	auth := oauthAuth(map[string]string{
		"grant": "client_credentials", "tokenUrl": srv.URL, "clientId": "id", "clientSecret": "secret",
	})
	for i := 0; i < 2; i++ {
		if _, err := materializer.Materialize(context.Background(), auth,
			domain.AuthRequest{}); err != nil {
			t.Fatalf("Materialize: %v", err)
		}
	}
	if len(endpoint.got) != 2 {
		t.Errorf("the endpoint was asked %d times, want another one", len(endpoint.got))
	}
}

// A provider that names no expiry issues a token kept for as long as the window is open, and the
// window is told exactly that: «nothing was said about when it ends» is zero, not a moment taken
// out of a zero time, which is a date in the year 1.
func TestATokenWithNoExpiryIsHeldWithoutOne(t *testing.T) {
	// expires_in: 0 is how the endpoint spells "the provider named nothing".
	endpoint := &spyTokenEndpoint{token: "long-lived", expiry: 0}
	srv := endpoint.server(t)
	materializer := New(srv.Client(), nil)

	auth := oauthAuth(map[string]string{
		"grant": "client_credentials", "tokenUrl": srv.URL, "clientId": "id", "clientSecret": "secret",
	})
	if _, err := materializer.Materialize(context.Background(), auth,
		domain.AuthRequest{}); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	held := materializer.Held(auth)
	if !held.Held {
		t.Fatalf("held = %+v, want the token the provider issued", held)
	}
	if held.ExpiresAt != 0 {
		t.Errorf("expiresAt = %d, want zero for a provider that named no expiry", held.ExpiresAt)
	}
}

// A token that has died is not held, and the window hears what a send acts on. Two answers to one
// question would be two answers about one fact: «there is a token» drawn over a request that is
// fetching another one.
func TestADeadTokenIsNotHeld(t *testing.T) {
	// Five seconds is inside the ten the materializer treats as "already dead".
	endpoint := &spyTokenEndpoint{token: "short-lived", expiry: 5}
	srv := endpoint.server(t)
	materializer := New(srv.Client(), nil)

	auth := oauthAuth(map[string]string{
		"grant": "client_credentials", "tokenUrl": srv.URL, "clientId": "id", "clientSecret": "secret",
	})
	if _, err := materializer.Materialize(context.Background(), auth,
		domain.AuthRequest{}); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if held := materializer.Held(auth); held.Held {
		t.Errorf("held = %+v, want nothing for a token inside the margin", held)
	}
}

// Drawing is not asking. A window painting rows while a person types must not send anything
// anywhere, and «No token» is what it says until there is one.
func TestDrawingDoesNotAskForAToken(t *testing.T) {
	endpoint := &spyTokenEndpoint{token: "issued", expiry: 3600}
	srv := endpoint.server(t)
	materializer := New(srv.Client(), nil)

	auth := oauthAuth(map[string]string{
		"grant": "client_credentials", "tokenUrl": srv.URL, "clientId": "id", "clientSecret": "secret",
	})
	out, err := materializer.Project(auth, domain.AuthRequest{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !out.Empty() {
		t.Errorf("output = %+v, want nothing before a token has been asked for", out)
	}
	if len(endpoint.got) != 0 {
		t.Fatalf("the endpoint was asked %d times, want none", len(endpoint.got))
	}

	if _, err := materializer.Materialize(context.Background(), auth,
		domain.AuthRequest{}); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	out, err = materializer.Project(auth, domain.AuthRequest{})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if value, _ := headerOf(out, "Authorization"); value != "Bearer issued" {
		t.Errorf("authorization = %q, want the token that is already there", value)
	}
	if len(endpoint.got) != 1 {
		t.Errorf("the endpoint was asked %d times, want once", len(endpoint.got))
	}
}

// Changing an answer asks for another token: the key a token is kept under is the answers that
// would fetch it, and a token issued for one client is not another client's.
func TestTheTokenFollowsTheAnswers(t *testing.T) {
	endpoint := &spyTokenEndpoint{token: "issued", expiry: 3600}
	srv := endpoint.server(t)
	materializer := New(srv.Client(), nil)

	auth := oauthAuth(map[string]string{
		"grant": "client_credentials", "tokenUrl": srv.URL, "clientId": "one", "clientSecret": "secret",
	})
	if _, err := materializer.Materialize(context.Background(), auth,
		domain.AuthRequest{}); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if materializer.Held(auth.With("clientId", "two")).Held {
		t.Error("a token issued for one client was handed to another")
	}
}

// A provider that refuses is a refusal the user asked for in so many words, so it travels back
// rather than being swallowed into a request that goes out with nothing.
func TestARefusalTravelsBack(t *testing.T) {
	endpoint := &spyTokenEndpoint{} // no token: the endpoint answers with an error
	srv := endpoint.server(t)

	auth := oauthAuth(map[string]string{
		"grant": "client_credentials", "tokenUrl": srv.URL, "clientId": "id", "clientSecret": "bad",
	})
	if _, err := New(srv.Client(), nil).Materialize(context.Background(), auth,
		domain.AuthRequest{}); err == nil {
		t.Error("a refused token was not reported")
	}
}

// The two grants that need a person need a browser. Without one they say so rather than waiting
// five minutes for somebody who was never sent anywhere.
func TestTheBrowserGrantsNeedABrowser(t *testing.T) {
	for _, grant := range []string{"authorization_code", "implicit"} {
		auth := oauthAuth(map[string]string{
			"grant": grant, "authUrl": "https://idp.example.com/authorize",
			"tokenUrl": "https://idp.example.com/token", "clientId": "id",
		})
		if _, err := New(nil, nil).Materialize(context.Background(), auth,
			domain.AuthRequest{}); err == nil {
			t.Errorf("%s: a grant that needs a browser ran without one", grant)
		}
	}
}

// A grant nobody implements is not one to fall back from.
func TestAnUnknownGrantIsRefused(t *testing.T) {
	auth := oauthAuth(map[string]string{
		"grant": "device_code", "tokenUrl": "https://idp.example.com/token",
	})
	if _, err := New(nil, nil).Materialize(context.Background(), auth,
		domain.AuthRequest{}); err == nil {
		t.Error("an unknown grant was attempted anyway")
	}
}

// A token the user asked to carry in the query travels there, the same as any other credential.
func TestAFetchedTokenCanTravelInTheQuery(t *testing.T) {
	endpoint := &spyTokenEndpoint{token: "issued", expiry: 3600}
	srv := endpoint.server(t)

	auth := oauthAuth(map[string]string{
		"grant": "client_credentials", "tokenUrl": srv.URL, "clientId": "id", "clientSecret": "secret",
		"place": domain.PlaceQuery,
	})
	out, err := New(srv.Client(), nil).Materialize(context.Background(), auth, domain.AuthRequest{})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(out.Headers) != 0 {
		t.Errorf("headers = %+v, want none for a token that travels in the query", out.Headers)
	}
	if len(out.Query) != 1 || out.Query[0].Value != "issued" {
		t.Errorf("query = %+v, want the fetched token", out.Query)
	}
}

// Obtain is what «Get a token» does, and it reaches the provider even though nothing has been
// sent: the user asked for it in so many words.
func TestObtainAsksWithoutSendingAnything(t *testing.T) {
	endpoint := &spyTokenEndpoint{token: "issued-by-hand", expiry: 3600}
	srv := endpoint.server(t)
	materializer := New(srv.Client(), nil)

	auth := oauthAuth(map[string]string{
		"grant": "client_credentials", "tokenUrl": srv.URL, "clientId": "id", "clientSecret": "secret",
	})
	if err := materializer.Obtain(context.Background(), auth); err != nil {
		t.Fatalf("Obtain: %v", err)
	}
	if !materializer.Held(auth).Held {
		t.Error("the token asked for is not held")
	}
	if len(endpoint.got) != 1 {
		t.Errorf("the endpoint was asked %d times, want once", len(endpoint.got))
	}

	// An answer that changed makes the old token stale, so the next Obtain reaches the provider.
	if err := materializer.Obtain(context.Background(), auth.With("scope", "more")); err != nil {
		t.Fatalf("Obtain: %v", err)
	}
	if len(endpoint.got) != 2 {
		t.Errorf("the endpoint was asked %d times, want a second one for the new answers",
			len(endpoint.got))
	}
}
