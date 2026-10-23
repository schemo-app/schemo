package cloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"schemo/internal/domain"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func oauthClient(t *testing.T, token func(*http.Request) (int, string)) *OIDC {
	t.Helper()
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, ""
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			body = `{"issuer":"https://identity.example","device_authorization_endpoint":"https://identity.example/device","token_endpoint":"https://identity.example/token"}`
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
	return NewOIDC(&http.Client{Transport: transport}, NewDialer())
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

func TestOAuthRedirectsNeverForwardRefreshCredentials(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, target := range []string{"https://cloud.example/redirected", "https://other.example/token"} {
			t.Run(fmt.Sprintf("%d/%s", status, target), func(t *testing.T) {
				var redirected bool
				transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
					response := &http.Response{StatusCode: 200, Header: http.Header{}, Request: req}
					switch req.URL.String() {
					case "https://cloud.example/.well-known/openid-configuration":
						response.Body = io.NopCloser(strings.NewReader(`{"issuer":"https://cloud.example","device_authorization_endpoint":"https://cloud.example/device","token_endpoint":"https://cloud.example/token"}`))
					case "https://cloud.example/token":
						response.StatusCode = status
						response.Header.Set("Location", target)
						response.Body = io.NopCloser(strings.NewReader(""))
					default:
						redirected = true
						response.Body = io.NopCloser(strings.NewReader(`{"access_token":"access","token_type":"Bearer"}`))
					}
					return response, nil
				})
				client := NewOIDC(&http.Client{Transport: transport}, nil)
				if _, err := client.Refresh(t.Context(), "https://cloud.example", "private-refresh-token"); err == nil || redirected {
					t.Fatal("OAuth request followed a redirect")
				}
			})
		}
	}
}

func TestDiscoveryRedirectsRemainAvailableWithoutSendingCredentials(t *testing.T) {
	client := NewOIDC(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		response := &http.Response{StatusCode: 200, Header: http.Header{}, Request: req}
		if req.URL.Host == "cloud.example" {
			response.StatusCode = http.StatusFound
			response.Header.Set("Location", "https://identity.example/discovery")
			response.Body = io.NopCloser(strings.NewReader(""))
		} else {
			response.Body = io.NopCloser(strings.NewReader(`{"issuer":"https://identity.example","device_authorization_endpoint":"https://identity.example/device","token_endpoint":"https://identity.example/token"}`))
		}
		return response, nil
	})}, nil)
	if err := client.Discover(t.Context(), "https://cloud.example"); err != nil {
		t.Fatal(err)
	}
}

// A discovery document says whose it is (OpenID Connect Discovery §4.3). One that names another
// issuer, or none, is not the configuration of the server it was asked of.
func TestDiscoveryDocumentMustNameItsOwnIssuer(t *testing.T) {
	for name, c := range map[string]struct {
		issuer string
		want   bool
	}{
		"the address":                  {`"https://cloud.example"`, true},
		"the address and a slash":      {`"https://cloud.example/"`, true},
		"another issuer":               {`"https://evil.example"`, false},
		"another scheme":               {`"http://cloud.example"`, false},
		"nothing":                      {`""`, false},
		"a document without an issuer": {`null`, false},
	} {
		t.Run(name, func(t *testing.T) {
			client := NewOIDC(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := `{"issuer":` + c.issuer + `,"device_authorization_endpoint":"https://cloud.example/device","token_endpoint":"https://cloud.example/token"}`
				return &http.Response{StatusCode: 200, Header: http.Header{}, Request: req, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}, nil)
			err := client.Discover(t.Context(), "https://cloud.example")
			if (err == nil) != c.want {
				t.Errorf("Discover with issuer %s = %v, want accepted = %v", c.issuer, err, c.want)
			}
		})
	}
}

// When the address hands the request to the identity provider, the provider is the issuer.
func TestDiscoveryIssuerMayBeTheOriginTheDocumentWasReadFrom(t *testing.T) {
	read := func(issuer string) error {
		client := NewOIDC(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			response := &http.Response{StatusCode: 200, Header: http.Header{}, Request: req}
			if req.URL.Host == "cloud.example" {
				response.StatusCode = http.StatusFound
				response.Header.Set("Location", "https://identity.example/discovery")
				response.Body = io.NopCloser(strings.NewReader(""))
				return response, nil
			}
			response.Body = io.NopCloser(strings.NewReader(`{"issuer":"` + issuer +
				`","device_authorization_endpoint":"https://identity.example/device","token_endpoint":"https://identity.example/token"}`))
			return response, nil
		})}, nil)
		return client.Discover(t.Context(), "https://cloud.example")
	}
	if err := read("https://identity.example"); err != nil {
		t.Errorf("the provider's own issuer was refused: %v", err)
	}
	if err := read("https://evil.example"); err == nil {
		t.Error("an issuer that is neither the address nor the provider was accepted")
	}
}

func TestDiscoveryRejectsOversizedDocumentInsteadOfAcceptingItsPrefix(t *testing.T) {
	valid := `{"issuer":"https://cloud.example","device_authorization_endpoint":"https://cloud.example/device","token_endpoint":"https://cloud.example/token"}`
	body := valid + strings.Repeat(" ", maxDiscoveryBytes-len(valid)) + "trailing-data"
	client := NewOIDC(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Request: req, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}, nil)
	if err := client.Discover(t.Context(), "https://cloud.example"); err == nil {
		t.Fatal("accepted the prefix of an oversized discovery response")
	}
}

func TestAccountRejectsUntrustedCertificates(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	if err := NewOIDC(HTTPClient(), nil).Discover(t.Context(), server.URL); err == nil {
		t.Fatal("account accepted an untrusted certificate")
	}
}

func TestDiscoveryCannotSelectInsecureEndpoints(t *testing.T) {
	for _, endpoint := range []string{"http://cloud.example/token", "http://127.0.0.1/token", "file:///token"} {
		t.Run(endpoint, func(t *testing.T) {
			client := HTTPClient()
			client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(
					`{"issuer":"https://cloud.example","device_authorization_endpoint":"https://cloud.example/device","token_endpoint":"` + endpoint + `"}`)), Header: http.Header{}, Request: req}, nil
			})
			if err := NewOIDC(client, nil).Discover(t.Context(), "https://cloud.example"); err == nil {
				t.Fatal("accepted insecure discovery endpoint")
			}
		})
	}
}

func TestRedirectCannotSendRefreshTokenOverPlaintext(t *testing.T) {
	var leaked bool
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		leaked = true
		_, _ = io.WriteString(w, `{}`)
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, plain.URL, http.StatusTemporaryRedirect)
	}))
	defer secure.Close()
	client := NewOIDC(secure.Client(), nil)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, secure.URL, strings.NewReader("refresh_token=private"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.http.Do(req)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil || leaked {
		t.Fatalf("redirect error=%v, token sent to plaintext=%v", err, leaked)
	}
}

func TestVerificationURLMustBeSecure(t *testing.T) {
	client := HTTPClient()
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"issuer":"https://cloud.example","device_authorization_endpoint":"https://cloud.example/device","token_endpoint":"https://cloud.example/token"}`
		if req.URL.Path == "/device" {
			body = `{"device_code":"private","user_code":"CODE","verification_uri":"http://cloud.example/verify","expires_in":300,"interval":1}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: req}, nil
	})
	if _, err := NewOIDC(client, nil).Authorize(t.Context(), "https://cloud.example", domain.Device{}); err == nil {
		t.Fatal("opened an insecure verification URL")
	}
}
