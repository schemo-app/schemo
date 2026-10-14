// Package authflow turns what a level answered about its authorization into what goes on the wire.
// Every scheme is one entry in a table, keyed by its type.
package authflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"schemo/internal/domain"
)

// Materializer holds the credentials the schemes in the table hand out. A token is kept in memory
// and never written down: one that is not stored cannot be exported, logged or screenshotted.
type Materializer struct {
	// client is what the app sends every request with, so the token endpoint gets the same proxy and
	// the same timeout as anything else.
	client *http.Client
	// browser is where the two grants that need a person are sent. Nil where there is nothing to open.
	browser Browser
	// exchange is browser as the scheme table asks for it, and nil where browser is nil: the two
	// grants that need one then refuse instead of hanging.
	exchange codeExchange

	mu     sync.Mutex
	tokens map[string]issued
	// pages is what a sign-in puts in the browser, handed over by the window as it mounts. It is
	// read when a sign-in starts, not captured, so a language change reaches the page that ends it.
	pages domain.SignInPages
}

// issued is a token with its expiry. A provider that named none has said all it will, and the token
// is kept until the window closes.
type issued struct {
	token   string
	expires time.Time
}

// expiryDelta is how long before a token dies it is treated as dead. A token good for another
// half-second is not one to send a request with, and asking for another is cheaper than a refusal.
const expiryDelta = 10 * time.Second

// New builds the materializer. A nil browser leaves the two grants that need one unable to run —
// the honest answer where there is nothing to open.
func New(client *http.Client, browser Browser) *Materializer {
	m := &Materializer{client: client, browser: browser, tokens: map[string]issued{}}
	if browser != nil {
		m.exchange = m.signIn
	}
	return m
}

// SetPages is the window handing over the words a sign-in puts in the browser: the catalogue is the
// window's, so Go cannot word them. An empty value — an older bundle calls by number — is ignored.
func (m *Materializer) SetPages(pages domain.SignInPages) {
	if pages == (domain.SignInPages{}) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pages = pages
}

// signIn is the exchange the two browser grants go through. It is a method because the words arrive
// later, as the window mounts, so a sign-in uses the language in force when it starts.
func (m *Materializer) signIn(ctx context.Context, auth domain.Auth) (string, time.Time, error) {
	m.mu.Lock()
	pages := m.pages
	m.mu.Unlock()
	return signIn(ctx, m.browser, auth, pages)
}

// Materialize answers with what the scheme puts on the request. A scheme that is not in the table —
// «None», «Inherit» — puts nothing there, the same answer an unfilled field gets.
func (m *Materializer) Materialize(
	ctx context.Context,
	auth domain.Auth,
	req domain.AuthRequest,
) (domain.AuthOutput, error) {
	entry, ok := schemes[auth.Type]
	if !ok {
		return domain.AuthOutput{}, nil
	}
	if entry.fetch == nil {
		return entry.put(auth, req)
	}
	token, err := m.obtain(ctx, auth, entry)
	if err != nil {
		return domain.AuthOutput{}, err
	}
	if token == "" {
		return domain.AuthOutput{}, nil
	}
	return carry(auth, token), nil
}

// Project answers without asking anybody for anything: a window drawing rows while a person types
// is not the moment to fetch a token. A fetched scheme draws nothing until one is held.
func (m *Materializer) Project(
	auth domain.Auth,
	req domain.AuthRequest,
) (domain.AuthOutput, error) {
	entry, ok := schemes[auth.Type]
	if !ok {
		return domain.AuthOutput{}, nil
	}
	if entry.fetch == nil {
		return entry.put(auth, req)
	}
	if token, held := m.held(auth); held {
		return carry(auth, token), nil
	}
	return domain.AuthOutput{}, nil
}

// Obtain asks for the token a scheme carries. The window asked for this in so many words, so what
// went wrong is reported rather than swallowed: it is the answer the user is waiting for.
func (m *Materializer) Obtain(ctx context.Context, auth domain.Auth) error {
	entry, ok := schemes[auth.Type]
	if !ok || entry.fetch == nil {
		return nil
	}
	m.Forget(auth)
	_, err := m.obtain(ctx, auth, entry)
	return err
}

// Forget drops a token, so the next send asks for another one; a scheme that carries what it was
// given has nothing to drop.
func (m *Materializer) Forget(auth domain.Auth) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, tokenKey(auth))
}

// Held is whether a scheme has a token at this moment and until when, which is all the window is
// told: it never sees the value. A token that has died is not held, so it agrees with a send.
func (m *Materializer) Held(auth domain.Auth) domain.AuthToken {
	found, ok := m.live(auth)
	if !ok {
		return domain.AuthToken{}
	}
	token := domain.AuthToken{Held: true}
	// Zero is what the window reads as «no expiry was named»; taking it out of a zero time instead
	// would hand it a date in the year 1.
	if !found.expires.IsZero() {
		token.ExpiresAt = found.expires.UnixMilli()
	}
	return token
}

func (m *Materializer) obtain(ctx context.Context, auth domain.Auth, entry scheme) (string, error) {
	if token, held := m.held(auth); held {
		return token, nil
	}
	// The library reads its HTTP client out of the context, so no call site has to thread one through
	// everything that touches a token.
	value, expires, err := entry.fetch(context.WithValue(ctx, oauth2.HTTPClient, m.client), auth,
		m.exchange)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[tokenKey(auth)] = issued{token: value, expires: expires}
	return value, nil
}

func (m *Materializer) held(auth domain.Auth) (string, bool) {
	found, ok := m.live(auth)
	if !ok {
		return "", false
	}
	return found.token, true
}

// live is the token a scheme is holding at this moment. One that has died, or dies within the
// delta, is not held: asking for another is cheaper than a request the server refuses.
func (m *Materializer) live(auth domain.Auth) (issued, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, ok := m.tokens[tokenKey(auth)]
	if !ok || found.token == "" {
		return issued{}, false
	}
	if !found.expires.IsZero() && time.Now().Add(expiryDelta).After(found.expires) {
		return issued{}, false
	}
	return found, true
}

// tokenKey names a token by the answers that would fetch it, so two requests that share an
// authorization share the token. Marshalling makes it stable: the same answers are the same string.
func tokenKey(auth domain.Auth) string {
	encoded, err := json.Marshal(auth)
	if err != nil {
		return string(auth.Type)
	}
	return string(encoded)
}

// scheme is what a scheme does to a request: it puts a credential on it, or fetches one.
type scheme struct {
	put putter
	// fetch is a scheme whose credential is asked for, and the only kind that remembers anything
	// between calls.
	fetch fetcher
}

// putter is what a scheme writes onto a request.
type putter func(auth domain.Auth, req domain.AuthRequest) (domain.AuthOutput, error)

// schemes is the other half of the registry in domain.AuthSchemes: a scheme is offered to the
// window if it is in that table, and it does something if it is in this one.
var schemes = map[domain.AuthType]scheme{
	domain.AuthBearer: {put: bearer},
	domain.AuthBasic:  {put: basic},
	domain.AuthAPIKey: {put: apiKey},
	domain.AuthJWT:    {put: jwtBearer},
	domain.AuthOAuth2: {fetch: oauthToken},
	domain.AuthDigest: {put: digest},
	domain.AuthAWS:    {put: awsSignature},
}

// encodingBase64 is the answer to a field that says the secret is not written out as it stands.
const encodingBase64 = "base64"

// bearer carries a token somebody else issued. The prefix is a field rather than a constant because
// not every server says «Bearer», and the one that does is not this package's to decide.
func bearer(auth domain.Auth, _ domain.AuthRequest) (domain.AuthOutput, error) {
	token := auth.Answer("token")
	if strings.TrimSpace(token) == "" {
		return domain.AuthOutput{}, nil
	}
	return header("Authorization", withPrefix(auth.Answer("prefix"), token)), nil
}

// basic is the credential of RFC 7617: the two halves joined by a colon and the pair encoded. The
// colon is the split, so a password may hold one and a login may not.
func basic(auth domain.Auth, _ domain.AuthRequest) (domain.AuthOutput, error) {
	username, password := auth.Answer("username"), auth.Answer("password")
	if username == "" && password == "" {
		return domain.AuthOutput{}, nil
	}
	joined := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return header("Authorization", "Basic "+joined), nil
}

// apiKey is a credential the server named itself, so both halves are fields: nothing here knows it
// is called X-API-Key. Where it goes is a field too — a header here, a query parameter there.
func apiKey(auth domain.Auth, _ domain.AuthRequest) (domain.AuthOutput, error) {
	name := strings.TrimSpace(auth.Answer("key"))
	value := auth.Answer("value")
	if name == "" || value == "" {
		return domain.AuthOutput{}, nil
	}
	pair := domain.HeaderPair{Name: name, Value: value}
	if auth.Answer("place") == domain.PlaceQuery {
		return domain.AuthOutput{Query: []domain.HeaderPair{pair}}, nil
	}
	return domain.AuthOutput{Headers: []domain.HeaderPair{pair}}, nil
}

// digest hands the engine the credential and nothing else: the hash needs a nonce the server has
// not sent yet, so the engine answers the challenge and sends again.
func digest(auth domain.Auth, _ domain.AuthRequest) (domain.AuthOutput, error) {
	username, password := auth.Answer("username"), auth.Answer("password")
	if username == "" && password == "" {
		return domain.AuthOutput{}, nil
	}
	return domain.AuthOutput{
		Digest: &domain.DigestCredentials{Username: username, Password: password},
	}, nil
}

func header(name, value string) domain.AuthOutput {
	return domain.AuthOutput{Headers: []domain.HeaderPair{{Name: name, Value: value}}}
}

func withPrefix(prefix, value string) string {
	if prefix = strings.TrimSpace(prefix); prefix == "" {
		return value
	}
	return prefix + " " + value
}
