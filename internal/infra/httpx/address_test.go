package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// What the address field's example promises: a bare host is an address, a port is not a scheme,
// and a local server is the one place where the guess goes the other way.
func TestCompleteReadsAnAddressTheWayABrowserWould(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"a host alone", "api.example.com/articles?include=author",
			"https://api.example.com/articles?include=author"},
		{"a port", "example.com:8080/users", "https://example.com:8080/users"},
		{"a scheme is left as it is", "http://example.com/x", "http://example.com/x"},
		{"so is one this app cannot speak", "ftp://example.com/x", "ftp://example.com/x"},
		{"a protocol-relative address", "//example.com/x", "https://example.com/x"},
		{"a local name", "localhost:3000/health", "http://localhost:3000/health"},
		{"a local name with a path", "localhost/api", "http://localhost/api"},
		{"a loopback address", "127.0.0.1:8123/ok", "http://127.0.0.1:8123/ok"},
		{"a loopback in brackets", "[::1]:8123", "http://[::1]:8123"},
		{"a name under .localhost", "api.localhost/x", "http://api.localhost/x"},
		{"nothing at all", "", ""},
		{"spaces around it", "  example.com/x  ", "https://example.com/x"},
		// A hole where a variable goes is not a missing scheme, and a bare path is not a host: both
		// are left to fail as what they are.
		{"an unresolved variable", "{{base}}/articles", "{{base}}/articles"},
		{"a path with no host", "/articles", "/articles"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := complete(c.raw); got != c.want {
				t.Errorf("complete(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func TestAHostWithoutASchemeIsSentToThatHost(t *testing.T) {
	var host string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// The test server's own address, with the scheme taken off: exactly what a person types.
	res := send(newTestEngine(t, Config{}), http.MethodGet, srv.URL[len("http://"):], nil, "")
	if res.Error != "" {
		t.Fatalf("a host without a scheme failed: %s", res.Error)
	}
	if res.Status != http.StatusOK {
		t.Errorf("status = %d, want the answer of the server it named", res.Status)
	}
	if want := srv.URL[len("http://"):]; host != want {
		t.Errorf("host = %q, want %q", host, want)
	}
}
