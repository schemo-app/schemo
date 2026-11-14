package request

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"schemo/internal/domain"
)

// Browser shows the provider's page for the interactive grants.
type Browser interface {
	OpenURL(url string) error
}

// signInTimeout is how long an interactive grant waits before giving up.
const signInTimeout = 5 * time.Minute

// signIn runs an interactive grant: a loopback listener, the browser at the provider, and a wait. The
// pages' words come from the window (domain.SignInPages).
func signIn(
	ctx context.Context,
	browser Browser,
	auth domain.Auth,
	pages domain.SignInPages,
) (string, time.Time, error) {
	implicit := auth.OrDefault("grant") == domain.GrantImplicit
	config := oauthConfig(auth)

	// A loopback port the OS picks rather than a custom scheme: a scheme has to be registered with the
	// system and only one app can hold it, while every provider already allows this machine's address.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", time.Time{}, fmt.Errorf("listening for the sign-in answer: %w", err)
	}
	defer listener.Close()
	config.RedirectURL = fmt.Sprintf("http://%s/callback", listener.Addr().String())

	state, err := nonce()
	if err != nil {
		return "", time.Time{}, err
	}
	// PKCE is what makes the code useless to anybody who reads it off the redirect: only the app that
	// started the sign-in can exchange it. It costs one field and closes the one hole in the grant.
	verifier := oauth2.GenerateVerifier()

	answers := make(chan signInAnswer, 1)
	server := &http.Server{
		Handler:           &callback{state: state, implicit: implicit, answers: answers, pages: pages},
		ReadHeaderTimeout: domain.LoopbackHeaderTimeout,
	}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	authorize := config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	if implicit {
		// The grant that skips the exchange asks the provider for the token itself, and gets it in the
		// part of a URL a server never sees.
		authorize = config.AuthCodeURL(state, oauth2.SetAuthURLParam("response_type", "token"))
	}
	if err := browser.OpenURL(authorize); err != nil {
		return "", time.Time{}, fmt.Errorf("opening a browser to sign in: %w", err)
	}

	select {
	case got := <-answers:
		if got.err != nil {
			return "", time.Time{}, got.err
		}
		if implicit {
			return got.token, got.expires, nil
		}
		token, err := config.Exchange(ctx, got.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return "", time.Time{}, fmt.Errorf("exchanging the code for a token: %w", err)
		}
		return token.AccessToken, token.Expiry, nil

	case <-ctx.Done():
		return "", time.Time{}, ctx.Err()

	case <-time.After(signInTimeout):
		return "", time.Time{}, fmt.Errorf("waiting for the sign-in: %w", domain.ErrNotAllowed)
	}
}

type signInAnswer struct {
	code    string
	token   string
	expires time.Time
	err     error
}

// callback is the listener the provider sends the person back to. It answers with a page, because
// the person is looking at a browser and not at this app.
type callback struct {
	state    string
	implicit bool
	answers  chan<- signInAnswer
	// pages is what those answers say, in the words the window handed over.
	pages domain.SignInPages
	// once guards the channel against reloads and parallel callbacks; handlers run concurrently.
	once sync.Once
}

func (c *callback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/callback":
		query := r.URL.Query()
		if !c.implicit {
			// The code grant comes back with everything in the query, and the state is checked before
			// anything else: an answer that belongs to another sign-in is not one to read.
			if c.isForeignAnswer(query.Get("state")) {
				http.Error(w, "This answer does not belong to this sign-in.", http.StatusBadRequest)
				return
			}
			if failure := query.Get("error"); failure != "" {
				c.deliver(signInAnswer{err: fmt.Errorf("signing in at the provider: %s", failure)})
				writePage(w, pageTemplate, c.pages.Refused)
				return
			}
			c.deliver(signInAnswer{code: query.Get("code")})
			writePage(w, pageTemplate, c.pages.Done)
			return
		}
		// The implicit grant's answer is in the fragment, which the server never sees: the page posts it
		// back, and the state is checked then.
		writePage(w, fragmentTemplate, c.pages)

	case "/token":
		if !c.implicit {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "unreadable answer", http.StatusBadRequest)
			return
		}
		form := r.PostForm
		if c.isForeignAnswer(form.Get("state")) {
			http.Error(w, "This answer does not belong to this sign-in.", http.StatusBadRequest)
			return
		}
		if failure := form.Get("error"); failure != "" {
			c.deliver(signInAnswer{err: fmt.Errorf("signing in at the provider: %s", failure)})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		token := form.Get("access_token")
		if token == "" {
			c.deliver(signInAnswer{err: errors.New("the provider sent no token")})
			w.WriteHeader(http.StatusNoContent)
			return
		}
		c.deliver(signInAnswer{token: token, expires: expiryFrom(form.Get("expires_in"))})
		w.WriteHeader(http.StatusNoContent)

	default:
		http.NotFound(w, r)
	}
}

func (c *callback) isForeignAnswer(state string) bool {
	return state != c.state
}

func (c *callback) deliver(got signInAnswer) {
	c.once.Do(func() { c.answers <- got })
}

// expiryFrom is how long the provider said the token is good for. A provider that said nothing, or
// said something that is not a number, has told us all it is going to.
func expiryFrom(raw string) time.Time {
	// Thirty-two bits is sixty-eight years of seconds, which a duration holds without wrapping.
	seconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil || seconds <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(seconds) * time.Second)
}

// nonce is a value that names this sign-in and nothing else. It goes out with the request and has
// to come back with the answer: one that came back with a different one belongs to somebody else's.
func nonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("naming this sign-in: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// Sign-in pages shown in the browser; fragmentTemplate posts the implicit grant's fragment back.
// html/template escapes translated text, including inside the script.
var (
	pageTemplate = template.Must(template.New("page").Parse(
		`<!doctype html><meta charset="utf-8"><title>{{.Title}}</title>
<body style="font: 14px system-ui; padding: 2rem">{{.Text}}</body>`))

	fragmentTemplate = template.Must(template.New("fragment").Parse(
		`<!doctype html><meta charset="utf-8"><title>{{.Waiting.Title}}</title>
<body style="font: 14px system-ui; padding: 2rem">{{.Waiting.Text}}
<script>
  fetch('/token', { method: 'POST', body: location.hash.slice(1),
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' } })
    .then(() => { document.body.textContent = '{{.Done.Text}}' })
    .catch(() => { document.body.textContent = '{{.Failed}}' })
</script>
</body>`))
)

func writePage(w http.ResponseWriter, page *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// A failure here is the client having gone away, not a mistake in the page: both templates are
	// parsed and checked at start-up, and their data is a struct of strings.
	_ = page.Execute(w, data)
}
