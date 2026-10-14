package oidc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"schemo/internal/domain"
	"schemo/internal/infra/channel"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func oauthClient(t *testing.T, token func(*http.Request) (int, string)) *Client {
	t.Helper()
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, ""
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			body = `{"device_authorization_endpoint":"https://identity.example/device","token_endpoint":"https://identity.example/token"}`
		case "/device":
			if r.Header.Get("X-Device-Name") != "Laptop" {
				t.Errorf("device name = %q", r.Header.Get("X-Device-Name"))
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("client_id") != clientID || r.Form.Get("scope") != "openid email offline_access" {
				t.Errorf("device form = %v", r.Form)
			}
			body = `{"device_code":"private-device-code","user_code":"CODE","verification_uri":"https://identity.example/verify","verification_uri_complete":"https://identity.example/verify?code=CODE","expires_in":300,"interval":1}`
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Header.Get("Authorization") != "" || r.Form.Get("client_id") != clientID {
				t.Errorf("public client authentication = %v", r.Form)
			}
			status, body = token(r)
		default:
			t.Fatalf("unexpected URL %s", r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return New(&http.Client{Transport: transport}, channel.New())
}

func TestDeviceFlowPendingSlowDownAndTokens(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var polls []time.Time
		client := oauthClient(t, func(r *http.Request) (int, string) {
			if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" || r.Form.Get("device_code") != "private-device-code" {
				t.Errorf("token form = %v", r.Form)
			}
			polls = append(polls, time.Now())
			switch len(polls) {
			case 1:
				return 400, `{"error":"authorization_pending"}`
			case 2:
				return 400, `{"error":"slow_down"}`
			default:
				return 200, `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`
			}
		})
		challenge, err := client.Authorize(context.Background(), "https://identity.example", domain.Device{Name: "Laptop"})
		if err != nil {
			t.Fatal(err)
		}
		if challenge.URL != "https://identity.example/verify?code=CODE" || challenge.UserCode != "CODE" {
			t.Fatalf("challenge = %+v", challenge)
		}
		tokens, err := client.Tokens(context.Background(), "https://identity.example", challenge)
		if err != nil {
			t.Fatal(err)
		}
		if tokens.Access != "access" || tokens.Refresh != "refresh" || time.Until(tokens.ExpiresAt) != time.Hour {
			t.Fatalf("tokens = %+v", tokens)
		}
		if len(polls) != 3 || polls[1].Sub(polls[0]) != time.Second || polls[2].Sub(polls[1]) != 6*time.Second {
			t.Fatalf("poll times = %v", polls)
		}
	})
}

func TestDeviceFlowRefusalAndExpiry(t *testing.T) {
	for _, code := range []string{"access_denied", "expired_token"} {
		t.Run(code, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client := oauthClient(t, func(*http.Request) (int, string) { return 400, `{"error":"` + code + `"}` })
				challenge, err := client.Authorize(context.Background(), "https://identity.example", domain.Device{Name: "Laptop"})
				if err != nil {
					t.Fatal(err)
				}
				_, err = client.Tokens(context.Background(), "https://identity.example", challenge)
				if domain.CodeOf(err) != domain.CodeSignInFailed {
					t.Fatalf("Tokens refusal = %v", err)
				}
			})
		})
	}
}

func TestRefreshRefusalDoesNotConfuseNetworkFailureWithRevocation(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		code   domain.Code
	}{
		{"revoked", 400, `{"error":"invalid_grant"}`, domain.CodeNotSignedIn},
		{"unavailable", 503, `{"error":"temporarily_unavailable"}`, domain.CodeServerUnreachable},
		{"pending availability", 400, `{"error":"temporarily_unavailable"}`, domain.CodeServerUnreachable},
		{"client configuration", 400, `{"error":"invalid_client"}`, domain.CodeServerRefused},
		{"malformed", 200, `not JSON`, domain.CodeServerRefused},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := oauthClient(t, func(r *http.Request) (int, string) {
				if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" {
					t.Errorf("refresh form = %v", r.Form)
				}
				return test.status, test.body
			})
			_, err := client.Refresh(context.Background(), "https://identity.example", "old-refresh")
			if domain.CodeOf(err) != test.code {
				t.Fatalf("Refresh = %v, want %s", err, test.code)
			}
		})
	}
}

func TestDeviceChallengeBelongsToItsServerAndIsConsumedOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := oauthClient(t, func(*http.Request) (int, string) { return 200, `{"access_token":"access","token_type":"Bearer"}` })
		challenge, err := client.Authorize(context.Background(), "https://identity.example", domain.Device{Name: "Laptop"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Tokens(context.Background(), "https://other.example", challenge); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cross-server challenge = %v", err)
		}
		if _, err := client.Tokens(context.Background(), "https://identity.example", challenge); err != nil {
			t.Fatal(err)
		}
		if _, err := client.Tokens(context.Background(), "https://identity.example", challenge); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("reused challenge = %v", err)
		}
	})
}

func TestDevicePollingRespectsCallerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := oauthClient(t, func(*http.Request) (int, string) { t.Fatal("canceled polling reached token endpoint"); return 0, "" })
		challenge, err := client.Authorize(context.Background(), "https://identity.example", domain.Device{Name: "Laptop"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := client.Tokens(ctx, "https://identity.example", challenge); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Tokens = %v", err)
		}
	})
}

type trackedBody struct {
	io.ReadCloser
	closed *bool
}

func (b trackedBody) Close() error { *b.closed = true; return b.ReadCloser.Close() }

func TestDeviceAuthorizationClosesResponse(t *testing.T) {
	client := oauthClient(t, func(*http.Request) (int, string) { return 0, "" })
	base := client.http.Transport
	closed := false
	client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, err := base.RoundTrip(request)
		if err == nil && request.URL.Path == "/device" {
			response.Body = trackedBody{ReadCloser: response.Body, closed: &closed}
		}
		return response, err
	})
	if _, err := client.Authorize(context.Background(), "https://identity.example", domain.Device{Name: "Laptop"}); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("device authorization response was not closed")
	}
}

func TestDeviceAuthorizationRespectsDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := oauthClient(t, func(*http.Request) (int, string) { return 0, "" })
		base := client.http.Transport
		client.http.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/device" {
				<-request.Context().Done()
				return nil, request.Context().Err()
			}
			return base.RoundTrip(request)
		})
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := client.Authorize(ctx, "https://identity.example", domain.Device{Name: "Laptop"}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Authorize after deadline = %v", err)
		}
	})
}

func TestDevicePollingExpiresWithoutParentCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := oauthClient(t, func(*http.Request) (int, string) { return 400, `{"error":"authorization_pending"}` })
		challenge, err := client.Authorize(context.Background(), "https://identity.example", domain.Device{Name: "Laptop"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Tokens(context.Background(), "https://identity.example", challenge)
		if domain.CodeOf(err) != domain.CodeSignInFailed {
			t.Fatalf("expired challenge = %v", err)
		}
	})
}
