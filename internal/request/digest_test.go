package request

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/icholy/digest"

	"schemo/internal/domain"
)

// challenge is what a Digest server says to a request that arrived without a credential. It is
// written out rather than minted so that a test can name the realm and the nonce it expects back.
const challenge = `Digest realm="example", qop="auth", nonce="abc123", algorithm=MD5`

// What a Digest server does: refuse a request without a credential, accept one with it. The
// refusal is the point of the first request, not a failure of it — it carries realm and nonce.
func digestServer(
	t *testing.T,
	check func(*testing.T, *http.Request) bool,
) (*httptest.Server, *int32) {
	t.Helper()
	var requests int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)

		header := r.Header.Get("Authorization")
		if !digest.IsDigest(header) {
			w.Header().Set("WWW-Authenticate", challenge)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if check != nil && !check(t, r) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

// What the window gets is the second answer, not the refusal that asked for it.
func TestDigestAnswersTheChallenge(t *testing.T) {
	srv, requests := digestServer(t, func(t *testing.T, r *http.Request) bool {
		credentials, err := digest.ParseCredentials(r.Header.Get("Authorization"))
		if err != nil {
			t.Errorf("the answer does not parse: %v", err)
			return false
		}
		// The answer has to be computed over the address that was actually sent, and from what the
		// challenge asked for rather than from what the scheme guessed.
		if credentials.URI != "/thing" {
			t.Errorf("uri = %q, want the path that was requested", credentials.URI)
		}
		if credentials.Realm != "example" || credentials.Nonce != "abc123" {
			t.Errorf("realm/nonce = %q/%q, want the ones from the challenge", credentials.Realm,
				credentials.Nonce)
		}
		if credentials.Username != "user" {
			t.Errorf("username = %q, want the one the user typed", credentials.Username)
		}
		if credentials.Response == "" {
			t.Error("the answer carries no response: it would be refused")
		}
		return true
	})
	engine := newTestEngine(t, EngineConfig{})

	res := engine.Do(context.Background(), Spec{
		Method: "GET",
		URL:    srv.URL + "/thing",
		Digest: &domain.DigestCredentials{Username: "user", Password: "pass"},
	})

	if res.Status != http.StatusOK {
		t.Fatalf("status = %d, want the answer to the challenge: %+v", res.Status, res)
	}
	if got := atomic.LoadInt32(requests); got != 2 {
		t.Errorf("the server was asked %d times, want the refusal and then the answer", got)
	}
	// The header was computed by the engine and is on no list above it, so the response is where the
	// record learns it from. What it hands over is the mask: that the answer itself arrived is what
	// the callback above proved, and the value is a credential to replay rather than one to keep.
	if len(res.SentHeaders) != 1 || res.SentHeaders[0].Name != "Authorization" {
		t.Fatalf("sent headers = %+v, want the answer that was computed", res.SentHeaders)
	}
	if want := digest.Prefix + domain.SecretMask; res.SentHeaders[0].Value != want {
		t.Errorf("sent header = %q, want %q", res.SentHeaders[0].Value, want)
	}
}

// The body travels again: the second request is the same request with one more header, and a retry
// that dropped the body would be a different request asking to be authorized.
func TestDigestSendsTheBodyAgain(t *testing.T) {
	var bodies []string
	srv, _ := digestServer(t, nil)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		bodies = append(bodies, string(body))

		if !digest.IsDigest(r.Header.Get("Authorization")) {
			w.Header().Set("WWW-Authenticate", challenge)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	engine := newTestEngine(t, EngineConfig{})

	res := engine.Do(context.Background(), Spec{
		Method: "POST",
		URL:    srv.URL + "/thing",
		Body:   `{"a":1}`,
		Digest: &domain.DigestCredentials{Username: "user", Password: "pass"},
	})

	if res.Status != http.StatusOK {
		t.Fatalf("status = %d, want the answer to the challenge", res.Status)
	}
	if len(bodies) != 2 || bodies[0] != `{"a":1}` || bodies[1] != `{"a":1}` {
		t.Errorf("bodies = %q, want the same body on both halves", bodies)
	}
}

// Without a credential the refusal is the answer: the engine does not invent one, and it does not
// ask twice.
func TestDigestWithoutACredentialIsJustARefusal(t *testing.T) {
	srv, requests := digestServer(t, nil)
	engine := newTestEngine(t, EngineConfig{})

	res := send(engine, "GET", srv.URL+"/thing", nil, "")
	if res.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want the refusal", res.Status)
	}
	if got := atomic.LoadInt32(requests); got != 1 {
		t.Errorf("the server was asked %d times, want once", got)
	}
	if len(res.SentHeaders) != 0 {
		t.Errorf("sent headers = %+v, want none: the engine added nothing", res.SentHeaders)
	}
}

// A server that refuses without saying how is a server the credential cannot be answered to. The
// refusal stands — it is the answer — and no second request is sent into the dark.
func TestDigestWithoutAChallengeIsNotAnswered(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	engine := newTestEngine(t, EngineConfig{})
	res := engine.Do(context.Background(), Spec{
		Method: "GET",
		URL:    srv.URL + "/thing",
		Digest: &domain.DigestCredentials{Username: "user", Password: "pass"},
	})

	if res.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want the refusal left standing", res.Status)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Errorf("the server was asked %d times, want once", got)
	}
}

// A challenge in an algorithm this app has no answer for is not one it guesses at.
func TestDigestRefusesAnUnknownAlgorithm(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("WWW-Authenticate", `Digest realm="example", nonce="abc", algorithm=SHA-3`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	engine := newTestEngine(t, EngineConfig{})
	res := engine.Do(context.Background(), Spec{
		Method: "GET",
		URL:    srv.URL + "/thing",
		Digest: &domain.DigestCredentials{Username: "user", Password: "pass"},
	})

	if res.Status != http.StatusUnauthorized {
		t.Errorf("status = %d, want the refusal left standing", res.Status)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Errorf("the server was asked %d times, want once", got)
	}
}

// The answer is computed over the address the engine actually sends. A bare host is completed into
// a scheme and a path, and a response computed over what the user typed is one the server refuses.
func TestDigestAnswersOverTheAddressThatIsSent(t *testing.T) {
	srv, _ := digestServer(t, func(t *testing.T, r *http.Request) bool {
		credentials, err := digest.ParseCredentials(r.Header.Get("Authorization"))
		if err != nil {
			t.Errorf("the answer does not parse: %v", err)
			return false
		}
		// The engine also completes a bare host into a path, which is what the server sees.
		if credentials.URI == "" || !strings.HasPrefix(credentials.URI, "/") {
			t.Errorf("uri = %q, want the path that was requested", credentials.URI)
		}
		return true
	})
	engine := newTestEngine(t, EngineConfig{})

	res := engine.Do(context.Background(), Spec{
		Method: "GET",
		URL:    srv.URL + "/thing?a=1",
		Digest: &domain.DigestCredentials{Username: "user", Password: "pass"},
	})
	if res.Status != http.StatusOK {
		t.Fatalf("status = %d, want the answer to the challenge", res.Status)
	}
}
