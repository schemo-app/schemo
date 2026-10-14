package authflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"schemo/internal/domain"
)

// Browser is what the two grants that send the person somewhere need and cannot do themselves: put
// a page in front of them. It is declared here rather than imported — the window this app draws
// through is the transport's business, and a package that computes credentials has no business
// knowing about it.
type Browser interface {
	OpenURL(url string) error
}

// signInTimeout is how long the app waits for the person to finish saying yes. A browser left open
// on a login screen for longer than this is a browser nobody is coming back to, and the alternative
// to giving up is a send that never returns.
const signInTimeout = 5 * time.Minute

// signIn is the two grants that need a person: a loopback listener for the answer to arrive at, a
// browser sent to the provider, and a wait. What the browser is shown while it waits comes from the
// window — see domain.SignInPages — because Go has no catalogue to word it from.
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
		ReadHeaderTimeout: 10 * time.Second,
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
	// once guards the channel: a person who reloads the page, or a provider that sends two requests
	// at once, must not block on a channel nobody is reading any more. Handlers run on their own
	// goroutines, so this is a lock and not a flag.
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
		// The implicit grant's answer is after the `#`, and a server never receives a fragment: the
		// page reads it in the browser and hands it back in a request of its own. So the state is not
		// checked here — there is nothing here to check it against — but in that request.
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
	seconds, err := parseSeconds(raw)
	if err != nil {
		return time.Time{}
	}
	return time.Now().Add(seconds)
}

func parseSeconds(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, errors.New("no expiry")
	}
	var seconds int
	if _, err := fmt.Sscanf(raw, "%d", &seconds); err != nil || seconds <= 0 {
		return 0, errors.New("no expiry")
	}
	return time.Duration(seconds) * time.Second, nil
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

// pageTemplate is one page a sign-in puts in the browser, and fragmentTemplate is the implicit
// grant's redirect page: it hands the browser's own fragment back in a request of its own,
// because a fragment never reaches a server.
//
// The words go in through html/template rather than a format string, and that is not decoration: a
// word is a sentence the window sent, and one with an ampersand or a bracket in it — which a
// translation has every right to contain — must reach the page as text instead of becoming one.
// Escaping for a script is the same statement in the other context, and html/template reads which
// one it is writing into.
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
