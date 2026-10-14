package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"schemo/internal/domain"
)

func serve(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server.URL
}

// A document comes back as the bytes the server sent.
func TestFetchReadsTheDocument(t *testing.T) {
	address := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"openapi": "3.1.0"}`))
	})

	got, err := New(http.DefaultClient).Fetch(context.Background(), address)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got) != `{"openapi": "3.1.0"}` {
		t.Errorf("body = %q, want what the server sent", got)
	}
}

// Everything that goes wrong before a byte is read is one refusal with one code: an answer that is
// not a success is the same news as a host that does not answer.
func TestFetchRefusesWhatItCannotRead(t *testing.T) {
	missing := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := New(http.DefaultClient).Fetch(context.Background(), missing); domain.CodeOf(
		err) != domain.CodeURLUnreachable {
		t.Errorf("a 404 = %v, want urlUnreachable", err)
	}

	dead := serve(t, func(http.ResponseWriter, *http.Request) {})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(http.DefaultClient).Fetch(ctx, dead); domain.CodeOf(err) !=
		domain.CodeURLUnreachable {
		t.Errorf("a canceled fetch = %v, want urlUnreachable", err)
	}

	if _, err := New(http.DefaultClient).Fetch(context.Background(), "not a url"); !errors.Is(
		err, domain.ErrNotAllowed) {
		t.Errorf("a malformed address = %v, want ErrNotAllowed", err)
	}
}

// A document larger than the ceiling is refused rather than truncated: half a specification reads
// as a broken one, and the person cannot tell the difference.
func TestFetchRefusesADocumentTooLarge(t *testing.T) {
	address := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 64)))
	})
	fetcher := New(http.DefaultClient)
	fetcher.MaxBytes = 16

	if _, err := fetcher.Fetch(context.Background(), address); domain.CodeOf(err) !=
		domain.CodeFileTooLarge {
		t.Errorf("Fetch = %v, want fileTooLarge", err)
	}
}
