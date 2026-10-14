// Package account manages local user session, OIDC authentication, and server sync.
package account

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// TopicChanged is published whenever the account state changes (sign-in, sign-out, etc.).
const TopicChanged = "account:changed"

type UseCase struct {
	store       Store
	credentials Credentials
	server      Server
	browser     Browser
	data        Data
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

func NewUseCase(
	store Store,
	credentials Credentials,
	server Server,
	browser Browser,
	data Data,
	notifier domain.Notifier,
) *UseCase {
	u := &UseCase{store: store, credentials: credentials, browser: browser, data: data, notifier: notifier, now: time.Now}
	u.server = watched{Server: server, uc: u}
	return u
}

func (u *UseCase) noteCall(err error) {
	u.reach.note(u.now(), err)
}

// Status reads the stored row, with the sign-in in flight and the server's reach beside it.
func (u *UseCase) Status(ctx context.Context) (Status, error) {
	u.session.Lock()
	defer u.session.Unlock()
	return u.status(ctx)
}

func (u *UseCase) status(ctx context.Context) (Status, error) {
	stored, err := u.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return Status{}, err
	}

	u.mu.Lock()
	waiting, challenge := u.waiting, u.challenge
	u.mu.Unlock()

	signedIn := stored.SignedIn()
	if signedIn && !u.closed {
		_, err := u.refreshFor(stored)
		signedIn = err == nil
	} else if u.closed {
		signedIn = false
	}
	return Status{
		SignedIn:     signedIn,
		SessionOnly:  u.sessionOnly,
		Account:      stored,
		Address:      serverAddress(stored.Server),
		Waiting:      waiting,
		Challenge:    challenge,
		Reachability: u.reach.now(),
	}, nil
}

// Check pings the server via Me() request to refresh cached profile data and reachability state.
func (u *UseCase) Check(ctx context.Context) (Status, error) {
	stored, access, err := u.signedIn(ctx)
	if err != nil && domain.AsFailure(err) == nil {
		return Status{}, err
	}

	if err == nil {
		if fetched, err := u.server.Me(ctx, serverAddress(stored.Server), access); err == nil {
			fetched.Server, fetched.SignedInAt = stored.Server, stored.SignedInAt
			u.session.Lock()
			current, readErr := u.store.Account(ctx)
			if readErr != nil || sessionKey(current) != sessionKey(stored) {
				u.session.Unlock()
				return u.announce(ctx)
			}
			err := u.store.Save(ctx, fetched)
			u.session.Unlock()
			if err != nil {
				return Status{}, err
			}
		}
	}
	return u.announce(ctx)
}

// SetServer updates the target server address for future sign-in attempts.
func (u *UseCase) SetServer(ctx context.Context, address string) (Status, error) {
	u.session.Lock()
	stored, err := u.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		u.session.Unlock()
		return Status{}, err
	}
	chosen := chosenAddress(address)
	if serverAddress(chosen) != serverAddress(stored.Server) {
		if err := u.forgetAccountLocked(ctx, chosen); err != nil {
			u.session.Unlock()
			return Status{}, err
		}
	}
	if !stored.SignedIn() {
		stored.Server = chosen
		if err := u.store.Save(ctx, stored); err != nil {
			u.session.Unlock()
			return Status{}, err
		}
	}
	u.session.Unlock()
	return u.announce(ctx)
}

func (u *UseCase) revisionNow() uint64 {
	u.session.Lock()
	defer u.session.Unlock()
	return u.revision
}

func (u *UseCase) current(ctx context.Context, stored domain.Account, revision uint64) bool {
	row, err := u.store.Account(ctx)
	return err == nil && !u.closed && u.revision == revision && sessionKey(row) == sessionKey(stored)
}

func sessionKey(stored domain.Account) string {
	return serverAddress(stored.Server) + "\x00" + stored.UserID + "\x00" + stored.SessionID
}

func (u *UseCase) refreshFor(stored domain.Account) (string, error) {
	key := sessionKey(stored)
	if u.credentialKey == key {
		if u.refreshToken == "" {
			return "", domain.ErrNotFound
		}
		return u.refreshToken, nil
	}
	token, err := u.credentials.Get(stored)
	if err != nil || token == "" {
		return "", domain.ErrNotFound
	}
	u.credentialKey, u.refreshToken = key, token
	return token, nil
}

func (u *UseCase) saveCredential(stored domain.Account, refresh string) {
	u.credentialKey, u.refreshToken = sessionKey(stored), refresh
	u.sessionOnly = false
	if refresh == "" || u.credentials.Set(stored, refresh) != nil {
		u.sessionOnly = true
		_ = u.credentials.Delete(stored)
		log.Print("[account] secure storage unavailable; sign-in will not survive restart")
	}
}

func (u *UseCase) saveAccount(ctx context.Context, stored domain.Account, tokens domain.Tokens) error {
	if err := u.store.Save(ctx, stored); err != nil {
		return err
	}
	u.saveCredential(stored, tokens.Refresh)
	u.mu.Lock()
	u.access, u.expiresAt, u.accessKey = tokens.Access, tokens.ExpiresAt, sessionKey(stored)
	u.mu.Unlock()
	return nil
}

// CheckServer asks whether the address is a server this app can sign in to, before anybody signs
// in. Nothing is written and nobody is signed in: "look at it" and "keep it" are different clicks,
// and the one that keeps is Begin.
func (u *UseCase) CheckServer(ctx context.Context, address string) (Status, error) {
	if err := u.server.Discover(ctx, serverAddress(address)); err != nil {
		return Status{}, err
	}
	return u.Status(ctx)
}

// Begin initiates the browser-based auth flow and starts background code polling.
func (u *UseCase) Begin(ctx context.Context, address string) (Status, error) {
	to := serverAddress(address)
	u.session.Lock()
	if u.closed {
		u.session.Unlock()
		return Status{}, context.Canceled
	}
	u.stopLocked()
	revision := u.revision
	u.session.Unlock()

	device := u.device()
	challenge, err := u.server.Authorize(ctx, to, device)
	if err != nil {
		return Status{}, err
	}

	if err := u.browser.OpenURL(challenge.URL); err != nil {
		return Status{}, err
	}

	stored, err := u.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return Status{}, err
	}
	u.session.Lock()
	if u.closed || u.revision != revision {
		u.session.Unlock()
		return Status{}, context.Canceled
	}
	if stored.SignedIn() {
		_ = u.credentials.Delete(stored)
	}
	u.credentialKey, u.refreshToken, u.sessionOnly = "", "", false
	u.forgetTokens()
	stored = domain.Account{Server: chosenAddress(address)}
	if err := u.store.Save(ctx, stored); err != nil {
		u.session.Unlock()
		return Status{}, err
	}

	watching, stop := context.WithCancel(context.WithoutCancel(ctx))
	u.mu.Lock()
	u.waiting, u.challenge, u.cancel = true, challenge, stop
	u.mu.Unlock()
	u.flows.Go(func() { u.watch(watching, to, challenge, revision) })
	u.session.Unlock()

	state, err := u.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	u.notifier.Publish(TopicChanged, state)
	return state, nil
}

// Cancel aborts the active sign-in polling process locally.
func (u *UseCase) Cancel(ctx context.Context) (Status, error) {
	u.stop()
	return u.announce(ctx)
}

// Close cancels pending sign-in flows and waits until they can no longer use storage.
func (u *UseCase) Close() {
	u.session.Lock()
	u.closed = true
	u.stopLocked()
	u.forgetTokens()
	u.credentialKey, u.refreshToken = "", ""
	u.session.Unlock()
	u.flows.Wait()
}

// SignOut revokes the local session and attempts to notify the remote server. With `wipe` it also
// deletes what this device holds of the account: the spaces that carry its key, with everything in
// them. Work that never belonged to an account is left alone — that is the other half of the
// choice, and the window words both.
func (u *UseCase) SignOut(ctx context.Context, wipe bool) (Status, error) {
	u.stop()
	revision := u.revisionNow()
	stored, err := u.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return Status{}, err
	}

	if stored.SignedIn() {
		if access := u.cachedToken(stored); access != "" {
			revoke, cancel := context.WithTimeout(ctx, 3*time.Second)
			_ = u.server.SignOut(revoke, serverAddress(stored.Server), access)
			cancel()
		}
	}

	u.session.Lock()
	if u.revision != revision || (stored.SignedIn() && !u.current(ctx, stored, revision)) {
		u.session.Unlock()
		return u.announce(ctx)
	}
	// The spaces go before the account row does: the key they are found by lives in that row.
	if wipe {
		if err := u.erase(ctx, stored); err != nil {
			u.session.Unlock()
			return Status{}, err
		}
	}

	if err := u.forgetAccountLocked(ctx, stored.Server); err != nil {
		u.session.Unlock()
		return Status{}, err
	}
	u.session.Unlock()
	return u.announce(ctx)
}

// Delete purges the account and forgets it here. The order is the careful one: the server is asked
// first, and its answer is the whole of whether anything happened — a refusal leaves this device
// exactly as it was, because the account it holds is still there and the person is still signed in
// to it. Only an account that is really gone takes this device's copy of it with it.
//
// It is the one act here that cannot be undone, and it takes the same as a signing out that wipes:
// the spaces tagged with the account's key, with everything inside them. Keeping them would be
// keeping somebody's work for an account that no longer exists.
func (u *UseCase) Delete(ctx context.Context) (Status, error) {
	stored, access, err := u.signedIn(ctx)
	if err != nil {
		return Status{}, err
	}
	revision := u.revisionNow()
	if err := u.server.DeleteAccount(ctx, serverAddress(stored.Server), access); err != nil {
		return Status{}, err
	}
	u.session.Lock()
	if !u.current(ctx, stored, revision) {
		u.session.Unlock()
		return u.announce(ctx)
	}

	// The spaces go before the account row does: the key they are found by lives in that row.
	if err := u.erase(ctx, stored); err != nil {
		u.session.Unlock()
		return Status{}, err
	}
	if err := u.forgetAccountLocked(ctx, stored.Server); err != nil {
		u.session.Unlock()
		return Status{}, err
	}
	u.session.Unlock()
	return u.announce(ctx)
}

// erase removes the account's own spaces. A device that was never signed in has none, and an
// account without an address is not one to find them by.
func (u *UseCase) erase(ctx context.Context, stored domain.Account) error {
	if stored.Email == "" {
		return nil
	}
	_, err := u.data.Wipe(ctx, domain.AccountKey(serverAddress(stored.Server), stored.Email))
	return err
}

// Sessions returns all active sessions associated with the current account.
func (u *UseCase) Sessions(ctx context.Context) ([]domain.Session, error) {
	stored, access, err := u.signedIn(ctx)
	if err != nil {
		return nil, err
	}
	sessions, err := u.server.Sessions(ctx, serverAddress(stored.Server), access)
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

// EndSession terminates a specific session ID on the server and signs out if it matches local.
func (u *UseCase) EndSession(ctx context.Context, sessionID string) ([]domain.Session, error) {
	stored, access, err := u.signedIn(ctx)
	if err != nil {
		return nil, err
	}
	revision := u.revisionNow()

	left, err := u.server.EndSession(ctx, serverAddress(stored.Server), access, sessionID)
	if err != nil {
		return nil, err
	}
	if sessionID == stored.SessionID {
		u.session.Lock()
		if !u.current(ctx, stored, revision) {
			u.session.Unlock()
			return left, nil
		}
		if err := u.forgetAccountLocked(ctx, stored.Server); err != nil {
			u.session.Unlock()
			return nil, err
		}
		u.session.Unlock()
		if _, err := u.announce(ctx); err != nil {
			return nil, err
		}
	}
	return left, nil
}

// EndOthers revokes all active sessions except the current device's session.
func (u *UseCase) EndOthers(ctx context.Context) ([]domain.Session, error) {
	stored, access, err := u.signedIn(ctx)
	if err != nil {
		return nil, err
	}
	return u.server.EndOthers(ctx, serverAddress(stored.Server), access)
}

func (u *UseCase) watch(ctx context.Context, address string, challenge domain.Challenge, revision uint64) {
	tokens, err := u.server.Tokens(ctx, address, challenge)
	if err == nil {
		err = u.remember(ctx, address, tokens, revision)
	}

	var failure *domain.Failure
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		failure = domain.FailureOf(err, domain.CodeSignInFailed)
	}
	u.finishAt(revision, failure)
}

func (u *UseCase) remember(ctx context.Context, address string, tokens domain.Tokens, revision uint64) error {
	stored, err := u.server.Me(ctx, address, tokens.Access)
	if err != nil {
		return err
	}

	stored.Server = ""
	if address != platform.DefaultServer {
		stored.Server = address
	}
	stored.SignedInAt = u.now()

	u.session.Lock()
	defer u.session.Unlock()
	if ctx.Err() != nil || u.revision != revision {
		return context.Canceled
	}
	return u.saveAccount(ctx, stored, tokens)
}

func (u *UseCase) finishAt(revision uint64, failure *domain.Failure) {
	u.session.Lock()
	defer u.session.Unlock()
	if u.revision != revision {
		return
	}
	u.mu.Lock()
	u.waiting, u.challenge, u.cancel = false, domain.Challenge{}, nil
	u.mu.Unlock()

	state, err := u.status(context.Background())
	if err != nil {
		return
	}
	state.Failure = failure
	u.notifier.Publish(TopicChanged, state)
}

// Caller provides access tokens and metadata required for authorized server requests.
func (u *UseCase) Caller(ctx context.Context) (domain.Caller, error) {
	stored, access, err := u.signedIn(ctx)
	if err != nil {
		return domain.Caller{}, err
	}
	return domain.Caller{
		Address:   serverAddress(stored.Server),
		Access:    access,
		SessionID: stored.SessionID,
	}, nil
}

// Refused handles server-side token rejection, forcing a refresh or ending the session.
func (u *UseCase) Refused(ctx context.Context, sessionID string) {
	u.session.Lock()
	stored, err := u.store.Account(ctx)
	if err != nil || sessionID == "" || stored.SessionID != sessionID {
		u.session.Unlock()
		return
	}
	revision := u.revision

	u.forgetTokens()
	u.session.Unlock()
	if _, err := u.token(ctx, stored); err != nil {
		if domain.CodeOf(err) == domain.CodeNotSignedIn {
			// A rejected refresh clears the session and advances its revision once.
			u.finishAt(revision+1, &domain.Failure{Code: domain.CodeSessionEnded})
		}
	}
}

// Me returns local account details without triggering a remote API call.
func (u *UseCase) Me(ctx context.Context) (domain.Account, error) {
	stored, err := u.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.Account{}, err
	}
	return stored, nil
}

func (u *UseCase) signedIn(ctx context.Context) (domain.Account, string, error) {
	stored, err := u.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.Account{}, "", err
	}
	if !stored.SignedIn() {
		return domain.Account{}, "", domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
	}

	access, err := u.token(ctx, stored)
	if err != nil {
		return domain.Account{}, "", err
	}
	return stored, access, nil
}

func (u *UseCase) token(ctx context.Context, stored domain.Account) (string, error) {
	if access := u.cachedToken(stored); access != "" {
		return access, nil
	}
	key := serverAddress(stored.Server) + ":" + stored.SessionID
	result := u.refresh.DoChan(key, func() (any, error) {
		return u.renewToken(ctx, stored)
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return "", result.Err
		}
		return result.Val.(string), nil
	}
}

func (u *UseCase) renewToken(ctx context.Context, stored domain.Account) (string, error) {
	if access := u.cachedToken(stored); access != "" {
		return access, nil
	}

	u.session.Lock()
	revision := u.revision
	if !u.current(ctx, stored, revision) {
		u.session.Unlock()
		return "", domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
	}
	refresh, err := u.refreshFor(stored)
	u.session.Unlock()
	if err != nil {
		return "", domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
	}

	tokens, err := u.server.Refresh(ctx, serverAddress(stored.Server), refresh)
	u.session.Lock()
	if !u.current(ctx, stored, revision) {
		u.session.Unlock()
		return "", domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
	}
	if err != nil {
		switch domain.CodeOf(err) {
		case domain.CodeServerRefused, domain.CodeNotSignedIn:
			_ = u.forgetAccountLocked(ctx, stored.Server)
			u.session.Unlock()
			return "", domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)
		}
		u.session.Unlock()
		return "", err
	}

	if tokens.Refresh != "" && tokens.Refresh != refresh {
		u.saveCredential(stored, tokens.Refresh)
	}
	u.mu.Lock()
	u.access, u.expiresAt, u.accessKey = tokens.Access, tokens.ExpiresAt, sessionKey(stored)
	u.mu.Unlock()
	u.session.Unlock()
	return tokens.Access, nil
}

func (u *UseCase) cachedToken(stored domain.Account) string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.accessKey == sessionKey(stored) && u.access != "" && u.now().Add(time.Minute).Before(u.expiresAt) {
		return u.access
	}
	return ""
}

func (u *UseCase) forgetAccountLocked(ctx context.Context, server string) error {
	u.stopLocked()
	stored, err := u.store.Account(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	u.forgetTokens()
	u.credentialKey, u.refreshToken, u.sessionOnly = "", "", false
	if stored.SignedIn() {
		if err := u.credentials.Delete(stored); err != nil {
			log.Print("[account] could not remove the system credential; local session is being cleared")
		}
	}
	if err := u.store.Forget(ctx); err != nil {
		return err
	}
	if server == "" {
		return nil
	}
	return u.store.Save(ctx, domain.Account{Server: server})
}

func (u *UseCase) announce(ctx context.Context) (Status, error) {
	state, err := u.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	u.notifier.Publish(TopicChanged, state)
	return state, nil
}

func (u *UseCase) forgetTokens() {
	u.mu.Lock()
	u.access, u.expiresAt, u.accessKey = "", time.Time{}, ""
	u.mu.Unlock()
}

func (u *UseCase) stop() {
	u.session.Lock()
	defer u.session.Unlock()
	u.stopLocked()
}

func (u *UseCase) stopLocked() {
	u.revision++
	u.mu.Lock()
	cancel := u.cancel
	u.cancel = nil
	u.waiting, u.challenge = false, domain.Challenge{}
	u.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (u *UseCase) device() domain.Device {
	return domain.Device{
		Name:       platform.DeviceName(),
		App:        platform.Name,
		Platform:   platform.OSName(),
		AppVersion: platform.NewBuildInfo().Version,
	}
}

func serverAddress(chosen string) string {
	if named := chosenAddress(chosen); named != "" {
		return named
	}
	return platform.DefaultServer
}

func chosenAddress(address string) string {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	return strings.TrimSuffix(trimmed, "/")
}
