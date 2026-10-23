package request

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"schemo/internal/domain"
)

func TestPostMethodAndBodyAcrossRedirects(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusMovedPermanently, http.StatusFound,
		http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var methods, bodies []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				methods = append(methods, r.Method)
				bodies = append(bodies, string(body))
				if status != http.StatusOK && r.URL.Path == "/start" {
					http.Redirect(w, r, "/end", status)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			response := send(newTestEngine(t, EngineConfig{}), http.MethodPost, server.URL+"/start", nil, "payload")
			if response.Error != "" {
				t.Fatal(response.Error)
			}
			if methods[0] != http.MethodPost || bodies[0] != "payload" {
				t.Fatalf("initial request = %s %q, want POST with payload", methods[0], bodies[0])
			}
			wantMethod, wantBody, wantCount := http.MethodPost, "payload", 2
			switch status {
			case http.StatusOK:
				wantCount = 1
			case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther:
				wantMethod, wantBody = http.MethodGet, ""
			}
			last := len(methods) - 1
			if len(methods) != wantCount || methods[last] != wantMethod || bodies[last] != wantBody {
				t.Errorf("request chain = %v %q, want %d requests ending with %s %q",
					methods, bodies, wantCount, wantMethod, wantBody)
			}
		})
	}
}

func TestDisabledRedirectsLeavePostResponseVisible(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		http.Redirect(w, r, "/end", http.StatusMovedPermanently)
	}))
	defer server.Close()
	settings := domain.DefaultRequests()
	settings.FollowRedirects = false
	response := send(newTestEngine(t, EngineConfig{Requests: settings}), http.MethodPost, server.URL, nil, "payload")
	if response.Error != "" || response.Status != http.StatusMovedPermanently {
		t.Fatalf("disabled redirect response = %d %q, want 301 without error", response.Status, response.Error)
	}
	if len(methods) != 1 || methods[0] != http.MethodPost {
		t.Errorf("request chain = %v, want one POST", methods)
	}
}
