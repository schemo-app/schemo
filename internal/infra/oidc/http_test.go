package oidc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"schemo/internal/domain"
)

func TestOAuthRedirectsNeverForwardRefreshCredentials(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, target := range []string{"https://cloud.example/redirected", "https://other.example/token"} {
			t.Run(fmt.Sprintf("%d/%s", status, target), func(t *testing.T) {
				var redirected bool
				transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
					response := &http.Response{StatusCode: 200, Header: http.Header{}, Request: req}
					switch req.URL.String() {
					case "https://cloud.example/.well-known/openid-configuration":
						response.Body = io.NopCloser(strings.NewReader(`{"device_authorization_endpoint":"https://cloud.example/device","token_endpoint":"https://cloud.example/token"}`))
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
				client := New(&http.Client{Transport: transport}, nil)
				if _, err := client.Refresh(t.Context(), "https://cloud.example", "private-refresh-token"); err == nil || redirected {
					t.Fatal("OAuth request followed a redirect")
				}
			})
		}
	}
}

func TestDiscoveryRedirectsRemainAvailableWithoutSendingCredentials(t *testing.T) {
	client := New(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		response := &http.Response{StatusCode: 200, Header: http.Header{}, Request: req}
		if req.URL.Host == "cloud.example" {
			response.StatusCode = http.StatusFound
			response.Header.Set("Location", "https://identity.example/discovery")
			response.Body = io.NopCloser(strings.NewReader(""))
		} else {
			response.Body = io.NopCloser(strings.NewReader(`{"device_authorization_endpoint":"https://identity.example/device","token_endpoint":"https://identity.example/token"}`))
		}
		return response, nil
	})}, nil)
	if err := client.Discover(t.Context(), "https://cloud.example"); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryRejectsOversizedDocumentInsteadOfAcceptingItsPrefix(t *testing.T) {
	valid := `{"device_authorization_endpoint":"https://cloud.example/device","token_endpoint":"https://cloud.example/token"}`
	body := valid + strings.Repeat(" ", maxDiscoveryBytes-len(valid)) + "trailing-data"
	client := New(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
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
	if err := New(HTTPClient(), nil).Discover(t.Context(), server.URL); err == nil {
		t.Fatal("account accepted an untrusted certificate")
	}
}

func TestDiscoveryCannotSelectInsecureEndpoints(t *testing.T) {
	for _, endpoint := range []string{"http://cloud.example/token", "http://127.0.0.1/token", "file:///token"} {
		t.Run(endpoint, func(t *testing.T) {
			client := HTTPClient()
			client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(
					`{"device_authorization_endpoint":"https://cloud.example/device","token_endpoint":"` + endpoint + `"}`)), Header: http.Header{}, Request: req}, nil
			})
			if err := New(client, nil).Discover(t.Context(), "https://cloud.example"); err == nil {
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
	client := New(secure.Client(), nil)
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
		body := `{"device_authorization_endpoint":"https://cloud.example/device","token_endpoint":"https://cloud.example/token"}`
		if req.URL.Path == "/device" {
			body = `{"device_code":"private","user_code":"CODE","verification_uri":"http://cloud.example/verify","expires_in":300,"interval":1}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: req}, nil
	})
	if _, err := New(client, nil).Authorize(t.Context(), "https://cloud.example", domain.Device{}); err == nil {
		t.Fatal("opened an insecure verification URL")
	}
}
