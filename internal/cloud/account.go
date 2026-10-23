package cloud

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"schemo/internal/db"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// TopicAccountChanged is published whenever the account state changes (sign-in, sign-out, etc.).
const TopicAccountChanged = "account:changed"

// Account is the account as the window sees it: who the app is signed in as, the sign-in itself,
// and the devices it is signed in on. Every method returns the whole state, and every window is
// told as well (see TopicChanged).
type Account struct {
	store       *db.Store
	credentials Credentials
	server      AccountServer
	browser     Browser
	notifier    domain.Notifier
	now         func() time.Time

	mu sync.Mutex
	// session serializes credential commits and logout; network calls run outside it.
	session       sync.Mutex
	flows         sync.WaitGroup
	closed        bool
	revision      uint64
	credentialKey string
	refreshToken  string
	sessionOnly   bool
	refresh       singleflight.Group
	// access is an in-memory short-lived access token.
	access    string
	accessKey string
	expiresAt time.Time
	// waiting indicates an ongoing sign-in request.
	waiting   bool
	challenge domain.Challenge
	cancel    context.CancelFunc
	// reach tracks server connectivity status.
	reach reach
}

func NewAccount(
	store *db.Store,
	credentials Credentials,
	server AccountServer,
	browser Browser,
	notifier domain.Notifier,
) *Account {
	s := &Account{store: store, credentials: credentials, browser: browser, notifier: notifier, now: time.Now}
	s.server = watched{AccountServer: server, account: s}
	return s
}

func (s *Account) recordServerCall(err error) {
	s.reach.note(s.now(), err)
}

// Status reads the stored row, with the sign-in in flight and the server's reach beside it.
func (s *Account) Status(ctx context.Context) (AccountStatus, error) {
	s.session.Lock()
	defer s.session.Unlock()
	return s.readStatus(ctx)
}

func (s *Account) readStatus(ctx context.Context) (AccountStatus, error) {
	stored, err := s.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return AccountStatus{}, err
	}

	s.mu.Lock()
	waiting, challenge := s.waiting, s.challenge
	s.mu.Unlock()

	signedIn := stored.SignedIn()
	if signedIn && !s.closed {
		_, err := s.refreshTokenOf(stored)
		signedIn = err == nil
	} else if s.closed {
		signedIn = false
	}
	return AccountStatus{
		SignedIn:     signedIn,
		SessionOnly:  s.sessionOnly,
		Account:      stored,
		Address:      serverAddressOrDefault(stored.Server),
		Waiting:      waiting,
		Challenge:    challenge,
		Reachability: s.reach.now(),
	}, nil
}

// Check pings the server via Me() request to refresh cached profile data and reachability state.
func (s *Account) Check(ctx context.Context) (AccountStatus, error) {
	stored, access, err := s.requireSignedIn(ctx)
	if err != nil && domain.AsFailure(err) == nil {
		return AccountStatus{}, err
	}

	if err == nil {
		if fetched, err := s.server.Me(ctx, serverAddressOrDefault(stored.Server), access); err == nil {
			fetched.Server, fetched.SignedInAt = stored.Server, stored.SignedInAt
			s.session.Lock()
			current, readErr := s.store.Account(ctx)
			if readErr != nil || sessionKey(current) != sessionKey(stored) {
				s.session.Unlock()
				return s.publishStatus(ctx)
			}
			err := s.store.Save(ctx, fetched)
			s.session.Unlock()
			if err != nil {
				return AccountStatus{}, err
			}
		}
	}
	return s.publishStatus(ctx)
}

// SetServer updates the target server address for future sign-in attempts.
func (s *Account) SetServer(ctx context.Context, address string) (AccountStatus, error) {
	s.session.Lock()
	stored, err := s.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		s.session.Unlock()
		return AccountStatus{}, err
	}
	chosen := normalizedAddress(address)
	if serverAddressOrDefault(chosen) != serverAddressOrDefault(stored.Server) {
		if err := s.forgetAccountLocked(ctx, chosen); err != nil {
			s.session.Unlock()
			return AccountStatus{}, err
		}
	}
	if !stored.SignedIn() {
		stored.Server = chosen
		if err := s.store.Save(ctx, stored); err != nil {
			s.session.Unlock()
			return AccountStatus{}, err
		}
	}
	s.session.Unlock()
	return s.publishStatus(ctx)
}

func (s *Account) currentRevision() uint64 {
	s.session.Lock()
	defer s.session.Unlock()
	return s.revision
}

func (s *Account) isCurrentSession(ctx context.Context, stored domain.Account, revision uint64) bool {
	row, err := s.store.Account(ctx)
	return err == nil && !s.closed && s.revision == revision && sessionKey(row) == sessionKey(stored)
}

func sessionKey(stored domain.Account) string {
	return serverAddressOrDefault(stored.Server) + "\x00" + stored.UserID + "\x00" + stored.SessionID
}

func (s *Account) refreshTokenOf(stored domain.Account) (string, error) {
	key := sessionKey(stored)
	if s.credentialKey == key {
		if s.refreshToken == "" {
			return "", domain.ErrNotFound
		}
		return s.refreshToken, nil
	}
	token, err := s.credentials.Get(stored)
	if err != nil || token == "" {
		return "", domain.ErrNotFound
	}
	s.credentialKey, s.refreshToken = key, token
	return token, nil
}

func (s *Account) storeRefreshToken(stored domain.Account, refresh string) {
	s.credentialKey, s.refreshToken = sessionKey(stored), refresh
	s.sessionOnly = false
	if refresh == "" || s.credentials.Set(stored, refresh) != nil {
		s.sessionOnly = true
		_ = s.credentials.Delete(stored)
		log.Print("[account] secure storage unavailable; sign-in will not survive restart")
	}
}

func (s *Account) saveSignedInAccount(ctx context.Context, stored domain.Account, tokens domain.Tokens) error {
	if err := s.store.Save(ctx, stored); err != nil {
		return err
	}
	s.storeRefreshToken(stored, tokens.Refresh)
	s.mu.Lock()
	s.access, s.expiresAt, s.accessKey = tokens.Access, tokens.ExpiresAt, sessionKey(stored)
	s.mu.Unlock()
	return nil
}

// CheckServer asks whether the address is a server this app can sign in to, before anybody signs
// in. Nothing is written and nobody is signed in: "look at it" and "keep it" are different clicks,
// and the one that keeps is Begin.
func (s *Account) CheckServer(ctx context.Context, address string) (AccountStatus, error) {
	if err := s.server.Discover(ctx, serverAddressOrDefault(address)); err != nil {
		return AccountStatus{}, err
	}
	return s.Status(ctx)
}

// Close cancels pending sign-in flows and waits until they can no longer use storage.
//
//wails:ignore
func (s *Account) Close() {
	s.session.Lock()
	s.closed = true
	s.cancelPendingSignInLocked()
	s.forgetTokens()
	s.credentialKey, s.refreshToken = "", ""
	s.session.Unlock()
	s.flows.Wait()
}

func (s *Account) publishStatus(ctx context.Context) (AccountStatus, error) {
	state, err := s.Status(ctx)
	if err != nil {
		return AccountStatus{}, err
	}
	s.notifier.Publish(TopicAccountChanged, state)
	return state, nil
}

func (s *Account) cancelPendingSignIn() {
	s.session.Lock()
	defer s.session.Unlock()
	s.cancelPendingSignInLocked()
}

func (s *Account) cancelPendingSignInLocked() {
	s.revision++
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.waiting, s.challenge = false, domain.Challenge{}
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (s *Account) thisDevice() domain.Device {
	return domain.Device{
		Name:       platform.DeviceName(),
		App:        platform.Name,
		Platform:   platform.OSName(),
		AppVersion: platform.NewBuildInfo().Version,
	}
}

func serverAddressOrDefault(chosen string) string {
	if named := normalizedAddress(chosen); named != "" {
		return named
	}
	return platform.DefaultServer
}

func normalizedAddress(address string) string {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	return strings.TrimSuffix(trimmed, "/")
}

// AccountStatus is the account as the window draws it: the stored row, a sign-in in flight, and whether
// the server is there.
type AccountStatus struct {
	SignedIn    bool           `json:"signedIn"`
	SessionOnly bool           `json:"sessionOnly"`
	Account     domain.Account `json:"account"`
	// Address is where the account actually talks: the stored choice, or the app's own server when
	// nobody chose one. The stored row keeps "" for the default, while a panel link needs the
	// address itself.
	Address string `json:"address"`
	// Waiting indicates an active sign-in flow waiting for browser confirmation.
	Waiting   bool             `json:"waiting"`
	Challenge domain.Challenge `json:"challenge"`
	// Failure contains the last sign-in error domain code, if any.
	Failure *domain.Failure `json:"failure,omitempty"`
	// Reachability reflects the outcome of the latest server communication attempt.
	Reachability Reachability `json:"reachability"`
}

// Reachability is what the last call to the server came to: whether the other side answered, and
// when it last did. A failed attempt does not move that moment, since "last contact" is not "last
// attempt". It is not part of the account, which is a row this app keeps: a server that is down is
// weather, not a fact about the person.
//
// Checked tells "not asked yet" from "did not answer", which a window words differently.
type Reachability struct {
	Checked   bool      `json:"checked"`
	Reachable bool      `json:"reachable"`
	At        time.Time `json:"at"`
}

// Credentials stores refresh tokens outside the application database.
type Credentials interface {
	Get(account domain.Account) (string, error)
	Set(account domain.Account, token string) error
	Delete(account domain.Account) error
}

// AccountServer is the other side: the OIDC endpoints a device signs in through and the API of the account
// it signs in to. It is one interface for one server, because a caller can never have half of it.
//
// Every method takes the address it is about: the client underneath is stateless, and the same
// process may talk to another server tomorrow.
type AccountServer interface {
	// Discover reads the server's discovery document and says whether the address is one this app
	// can sign in to. A window checks an address with it before offering to sign in; it needs no
	// account and creates nothing on the server.
	Discover(ctx context.Context, address string) error
	// Authorize asks for a device code: what the person has to confirm, and where.
	Authorize(ctx context.Context, address string, device domain.Device) (domain.Challenge, error)
	// Tokens waits for the person to confirm the code and returns the pair of tokens. It returns
	// when the code is confirmed, refused or expired, or the context is cancelled.
	Tokens(ctx context.Context, address string, challenge domain.Challenge) (domain.Tokens, error)
	// Refresh trades a refresh token for a new pair, which is how a device stays signed in without
	// anybody typing anything.
	Refresh(ctx context.Context, address, refresh string) (domain.Tokens, error)

	Me(ctx context.Context, address, access string) (domain.Account, error)
	Sessions(ctx context.Context, address, access string) ([]domain.Session, error)
	EndSession(ctx context.Context, address, access, sessionID string) ([]domain.Session, error)
	EndOthers(ctx context.Context, address, access string) ([]domain.Session, error)
	SignOut(ctx context.Context, address, access string) error
	// DeleteAccount purges the account and every session onto it. It is asked with nothing: the
	// address the server would check the act against is one this app already holds, and making the
	// person type it back would only add friction.
	DeleteAccount(ctx context.Context, address, access string) error
}

// Browser is where a person confirms the code. The app never asks for a password, so as far as the
// window is concerned the sign-in is handing a URL to something that can show a page.
type Browser interface {
	// OpenURL is the same name the window's own port uses, so the window is what serves this one
	// without an adapter in between.
	OpenURL(url string) error
}
