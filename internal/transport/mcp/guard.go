package mcp

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
)

// guard is the one door into the MCP server: nothing reaches the protocol without the token. The
// token is held behind a pointer rather than copied into the handler, because reissuing it must
// take effect on the next call and not on the next restart.
type guard struct {
	next  http.Handler
	token atomic.Pointer[string]
}

func newGuard(token string, next http.Handler) *guard {
	g := &guard{next: next}
	g.set(token)
	return g
}

func (g *guard) set(token string) { g.token.Store(&token) }

func (g *guard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	want := ""
	if held := g.token.Load(); held != nil {
		want = *held
	}
	got := tokenOf(r)
	// The comparison is constant-time, and a server without a token is a server nobody enters: a
	// request that arrives before one was minted is refused like any other.
	if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		unauthorized(w)
		return
	}
	g.next.ServeHTTP(w, r)
}

// tokenOf reads the token as a client was told to send it: an Authorization header, or the query
// string for the clients that cannot set one.
func tokenOf(r *http.Request) string {
	if header := r.Header.Get("Authorization"); header != "" {
		if after, ok := strings.CutPrefix(header, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
		// A scheme spelled otherwise is still not a reason to fall through to the query string.
		return ""
	}
	return r.URL.Query().Get("token")
}

// unauthorized answers in the shape the protocol speaks, with the one thing the person on the other
// side needs: where the token comes from. It never says whether a token was close.
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="schemo"`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      nil,
		"error": map[string]any{
			"code": -32001,
			"message": "no access token — copy the config from Schemo: Settings → " +
				"MCP server → the agent's configuration",
		},
	})
}
