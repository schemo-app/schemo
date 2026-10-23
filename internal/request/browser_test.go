package request

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"schemo/internal/domain"
)

// browserThatFollows is a browser as far as this package can tell: it is handed the provider's page
// and does what a person would do there — say yes and be sent back. What it is sent back with is
// the test's to choose, which is the whole point: the provider is the one party a test cannot be.
type browserThatFollows struct {
	t      *testing.T
	answer func(authorize url.URL) (redirect string, fragment string)
	opened string
	// page is what the last answer said, which is where the words on it are read from: a browser is
	// the only party that ever sees these pages.
	page string
}

func (b *browserThatFollows) OpenURL(raw string) error {
	b.opened = raw
	authorize, err := url.Parse(raw)
	if err != nil {
		b.t.Fatalf("the page to open is not an address: %v", err)
	}
	redirect, fragment := b.answer(*authorize)
	b.follow(redirect, fragment)
	return nil
}

func (b *browserThatFollows) follow(redirect, fragment string) {
	if redirect == "" {
		// A browser that opens nothing, which is what a test of the waiting needs.
		return
	}
	b.read(redirect)
	if fragment == "" {
		return
	}
	// The fragment never reaches a server, so the page serves a script that posts it back. A browser
	// without a script engine is one that does the script's work itself.
	target := strings.TrimSuffix(redirect, "/callback") + "/token"
	if _, err := http.PostForm(target, formOf(fragment)); err != nil {
		b.t.Errorf("posting the answer back: %v", err)
	}
}

// read loads a page and keeps it: a person looking at the browser sees this body and nothing else.
func (b *browserThatFollows) read(redirect string) {
	answer, err := http.Get(redirect)
	if err != nil {
		b.t.Errorf("loading the page: %v", err)
		return
	}
	defer answer.Body.Close()

	body, err := io.ReadAll(answer.Body)
	if err != nil {
		b.t.Errorf("reading the page: %v", err)
		return
	}
	b.page = string(body)
}

func formOf(fragment string) url.Values {
	values, err := url.ParseQuery(fragment)
	if err != nil {
		return url.Values{}
	}
	return values
}

// The code grant is the whole trip: the person is sent to the provider, comes back with a code, and
// the app exchanges it — with a PKCE verifier, so that a code read off the redirect is useless to
// anybody else.
func TestTheCodeGrantSignsInAndExchanges(t *testing.T) {
	endpoint := &spyTokenEndpoint{token: "issued-for-a-code", expiry: 3600}
	srv := endpoint.server(t)

	browser := &browserThatFollows{t: t, answer: func(authorize url.URL) (string, string) {
		query := authorize.Query()
		if query.Get("response_type") != "code" {
			t.Errorf("response_type = %q, want the code grant", query.Get("response_type"))
		}
		if query.Get("code_challenge") == "" {
			t.Error("no code challenge: a code read off the redirect would be anyone's to exchange")
		}
		return query.Get("redirect_uri") + "?code=the-code&state=" + query.Get("state"), ""
	}}

	auth := oauthAuth(map[string]string{
		"grant": "authorization_code", "authUrl": "https://idp.example.com/authorize",
		"tokenUrl": srv.URL, "clientId": "id", "clientSecret": "secret",
	})
	// The sign-in talks to the provider with the plain client: it is a browser address, not the app's
	// own engine, and the token endpoint is the test's.
	client := srv.Client()
	materializer := NewAuthorizer(client, browser)

	out, err := materializer.Materialize(context.Background(), auth, domain.AuthRequest{})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if value, _ := headerOf(out, "Authorization"); value != "Bearer issued-for-a-code" {
		t.Errorf("authorization = %q, want the token the code was exchanged for", value)
	}

	form := endpoint.got[0]
	if form["grant_type"] != "authorization_code" || form["code"] != "the-code" {
		t.Errorf("form = %+v, want the code exchanged for a token", form)
	}
	if form["code_verifier"] == "" {
		t.Error("the exchange carried no verifier: the challenge was for nothing")
	}
	// The address the provider is told to come back to is this machine, and the same one the app waits
	// on: two different ports would be a sign-in that never lands.
	if !strings.HasPrefix(browser.opened, "https://idp.example.com/authorize?") {
		t.Errorf("opened %q, want the provider's authorization endpoint", browser.opened)
	}
}

// The implicit grant is the same trip without the exchange: the provider hands the token over
// directly, in the part of a URL no server ever sees.
func TestTheImplicitGrantTakesTheTokenFromTheFragment(t *testing.T) {
	browser := &browserThatFollows{t: t, answer: func(authorize url.URL) (string, string) {
		query := authorize.Query()
		if query.Get("response_type") != "token" {
			t.Errorf("response_type = %q, want the grant that gets the token itself",
				query.Get("response_type"))
		}
		return query.Get("redirect_uri"),
			"access_token=from-the-fragment&expires_in=60&state=" + query.Get("state")
	}}

	auth := oauthAuth(map[string]string{
		"grant": "implicit", "authUrl": "https://idp.example.com/authorize", "clientId": "id",
	})
	out, err := NewAuthorizer(nil, browser).Materialize(context.Background(), auth, domain.AuthRequest{})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if value, _ := headerOf(out, "Authorization"); value != "Bearer from-the-fragment" {
		t.Errorf("authorization = %q, want the token the provider handed over", value)
	}
}

// An answer naming another sign-in is not exchanged, and the sign-in goes on waiting: a request
// that landed on the port by accident is not the answer that is still coming, and giving up over
// one would end a legitimate sign-in because something port-scanned the machine.
func TestAnAnswerFromAnotherSignInIsNotTaken(t *testing.T) {
	browser := &browserThatFollows{t: t, answer: func(authorize url.URL) (string, string) {
		return authorize.Query().Get("redirect_uri") + "?code=theirs&state=not-our-state", ""
	}}
	auth := oauthAuth(map[string]string{
		"grant": "authorization_code", "authUrl": "https://idp.example.com/authorize",
		"tokenUrl": "https://idp.example.com/token", "clientId": "id",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := NewAuthorizer(nil, browser).Materialize(ctx, auth, domain.AuthRequest{})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("an answer from another sign-in was accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the sign-in neither took the answer nor gave up")
	}
}

// A person who says no is not a failure of the app, and the provider's own word for it travels
// back.
func TestARefusalAtTheProviderTravelsBack(t *testing.T) {
	browser := &browserThatFollows{t: t, answer: func(authorize url.URL) (string, string) {
		return authorize.Query().Get("redirect_uri") + "?error=access_denied&state=" +
			authorize.Query().Get("state"), ""
	}}
	auth := oauthAuth(map[string]string{
		"grant": "authorization_code", "authUrl": "https://idp.example.com/authorize",
		"tokenUrl": "https://idp.example.com/token", "clientId": "id",
	})

	_, err := NewAuthorizer(nil, browser).Materialize(context.Background(), auth, domain.AuthRequest{})
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("err = %v, want the provider's own word for what happened", err)
	}
}

// The wait ends with the context: a sign-in nobody came back to must not hold a send open.
func TestTheWaitEndsWithTheContext(t *testing.T) {
	// A browser that opens nothing, so nothing ever comes back.
	browser := &browserThatFollows{t: t, answer: func(url.URL) (string, string) { return "", "" }}
	auth := oauthAuth(map[string]string{
		"grant": "authorization_code", "authUrl": "https://idp.example.com/authorize",
		"tokenUrl": "https://idp.example.com/token", "clientId": "id",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := NewAuthorizer(nil, browser).Materialize(ctx, auth, domain.AuthRequest{})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("a sign-in that was given up on reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("giving up on a sign-in did not end the wait")
	}
}

// windowPages are the words a window hands over. None of them reads as any other, so a page showing
// the wrong one of three is a page a test can tell apart from the right one.
func windowPages() domain.SignInPages {
	return domain.SignInPages{
		Waiting: domain.SignInPage{Title: "waiting title", Text: "waiting text"},
		Done:    domain.SignInPage{Title: "done title", Text: "done text"},
		Refused: domain.SignInPage{Title: "refused title", Text: "refused text"},
		Failed:  "failed text",
	}
}

// A window that hands over nothing does not take the words away. The bindings are called by number,
// so this is the shape of a window running the bundle from before this method's second argument: it
// sends one and leaves the rest empty, and a blank page is worse than a page in the wrong language.
func TestAWindowThatSaysNothingDoesNotBlankThePage(t *testing.T) {
	endpoint := &spyTokenEndpoint{token: "issued-for-a-code", expiry: 3600}
	srv := endpoint.server(t)

	browser := &browserThatFollows{t: t, answer: func(authorize url.URL) (string, string) {
		return authorize.Query().Get("redirect_uri") + "?code=the-code&state=" +
			authorize.Query().Get("state"), ""
	}}
	auth := oauthAuth(map[string]string{
		"grant": "authorization_code", "authUrl": "https://idp.example.com/authorize",
		"tokenUrl": srv.URL, "clientId": "id",
	})

	pages := windowPages()
	materializer := NewAuthorizer(srv.Client(), browser)
	materializer.SetPages(pages)
	// What an older window's call comes to: the first argument reaches the menu, and nothing else.
	materializer.SetPages(domain.SignInPages{})
	if _, err := materializer.Materialize(context.Background(), auth,
		domain.AuthRequest{}); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	assertPage(t, browser.page, pages.Done)
}

// assertPage is that a page says the words it was handed, in its title bar and in its body.
func assertPage(t *testing.T, page string, want domain.SignInPage) {
	t.Helper()
	if !strings.Contains(page, "<title>"+want.Title+"</title>") {
		t.Errorf("the page has no title %q:\n%s", want.Title, page)
	}
	if !strings.Contains(page, want.Text) {
		t.Errorf("the page does not say %q:\n%s", want.Text, page)
	}
}

// The page that ends a sign-in is worded by the window. Go serves it and has no catalogue to word
// it from, so the alternative to these words arriving is a page in no language at all.
func TestThePageThatEndsASignInCarriesTheWindowsWords(t *testing.T) {
	endpoint := &spyTokenEndpoint{token: "issued-for-a-code", expiry: 3600}
	srv := endpoint.server(t)

	browser := &browserThatFollows{t: t, answer: func(authorize url.URL) (string, string) {
		return authorize.Query().Get("redirect_uri") + "?code=the-code&state=" +
			authorize.Query().Get("state"), ""
	}}
	auth := oauthAuth(map[string]string{
		"grant": "authorization_code", "authUrl": "https://idp.example.com/authorize",
		"tokenUrl": srv.URL, "clientId": "id",
	})

	pages := windowPages()
	materializer := NewAuthorizer(srv.Client(), browser)
	materializer.SetPages(pages)
	_, err := materializer.Materialize(context.Background(), auth, domain.AuthRequest{})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	assertPage(t, browser.page, pages.Done)
}

// The page for a provider that said no is worded by the window too, and it is not the page for a
// sign-in that worked: they say different things to a person.
func TestThePageForARefusalCarriesTheWindowsWords(t *testing.T) {
	browser := &browserThatFollows{t: t, answer: func(authorize url.URL) (string, string) {
		return authorize.Query().Get("redirect_uri") + "?error=access_denied&state=" +
			authorize.Query().Get("state"), ""
	}}
	auth := oauthAuth(map[string]string{
		"grant": "authorization_code", "authUrl": "https://idp.example.com/authorize",
		"tokenUrl": "https://idp.example.com/token", "clientId": "id",
	})

	pages := windowPages()
	materializer := NewAuthorizer(nil, browser)
	materializer.SetPages(pages)
	_, err := materializer.Materialize(context.Background(), auth, domain.AuthRequest{})
	if err == nil {
		t.Fatal("a refusal at the provider was reported as a sign-in")
	}

	assertPage(t, browser.page, pages.Refused)
}

// The waiting page is the one page whose words are read by a script rather than drawn: one of them
// is the body until the answer comes back, and the other two are what replaces it either way.
func TestTheWaitingPageCarriesTheWindowsWords(t *testing.T) {
	browser := &browserThatFollows{t: t, answer: func(authorize url.URL) (string, string) {
		return authorize.Query().Get("redirect_uri"),
			"access_token=from-the-fragment&expires_in=60&state=" + authorize.Query().Get("state")
	}}
	auth := oauthAuth(map[string]string{
		"grant": "implicit", "authUrl": "https://idp.example.com/authorize", "clientId": "id",
	})

	pages := windowPages()
	materializer := NewAuthorizer(nil, browser)
	materializer.SetPages(pages)
	_, err := materializer.Materialize(context.Background(), auth, domain.AuthRequest{})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	assertPage(t, browser.page, pages.Waiting)
	for _, word := range []string{pages.Done.Text, pages.Failed} {
		if !strings.Contains(browser.page, word) {
			t.Errorf("the waiting page's script does not carry %q:\n%s", word, browser.page)
		}
	}
}

// A word is a sentence the window sent, not markup, and it stands in three contexts: a title, a
// body, and a string inside the waiting page's script. A translation with an ampersand or an angle
// bracket in it — which one has every right to contain — must arrive as text in all three, or the
// page is broken by its own words.
func TestAWordIsTextAndNotMarkup(t *testing.T) {
	word := `A & B </title><script>alert(1)</script> 'quoted'`

	browser := &browserThatFollows{t: t, answer: func(authorize url.URL) (string, string) {
		return authorize.Query().Get("redirect_uri"),
			"access_token=from-the-fragment&expires_in=60&state=" + authorize.Query().Get("state")
	}}
	auth := oauthAuth(map[string]string{
		"grant": "implicit", "authUrl": "https://idp.example.com/authorize", "clientId": "id",
	})

	pages := domain.SignInPages{
		Waiting: domain.SignInPage{Title: word, Text: word},
		Done:    domain.SignInPage{Title: word, Text: word},
		Refused: domain.SignInPage{Title: word, Text: word},
		Failed:  word,
	}
	materializer := NewAuthorizer(nil, browser)
	materializer.SetPages(pages)
	_, err := materializer.Materialize(context.Background(), auth, domain.AuthRequest{})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	// The page's own script opens and closes exactly once. Every `<script>` beyond that one is a
	// word that arrived as markup — walked out of the title, out of the body, or out of the string
	// it stands in inside the script — and a page with one of those is a page its own words broke.
	if opened, closed := strings.Count(browser.page, "<script>"),
		strings.Count(browser.page, "</script>"); opened != 1 || closed != 1 {
		t.Errorf("the page has %d <script> and %d </script>, want one of each:\n%s",
			opened, closed, browser.page)
	}
	// And the word is there: escaped, but the sentence a person reads is the one that was sent.
	if !strings.Contains(browser.page, "A &amp; B") {
		t.Errorf("the word did not reach the page as text:\n%s", browser.page)
	}
	if !strings.Contains(browser.page, `A \u0026 B`) {
		t.Errorf("the word did not reach the script as a string:\n%s", browser.page)
	}
}

// expires_in is a number of seconds (RFC 6749 §5.1): text after the digits, a sign, or nothing at
// all says nothing about when the token ends.
func TestExpiryComesFromTheSecondsTheProviderGave(t *testing.T) {
	before := time.Now()
	if got := expiryFrom(" 3600 "); got.Before(before.Add(3599*time.Second)) || got.After(time.Now().Add(3601*time.Second)) {
		t.Errorf("expiryFrom(3600) = %v, want an hour from now", got)
	}
	for _, raw := range []string{"", "0", "-5", "abc", "3600abc", "1.5", "99999999999"} {
		if got := expiryFrom(raw); !got.IsZero() {
			t.Errorf("expiryFrom(%q) = %v, want no expiry", raw, got)
		}
	}
}
