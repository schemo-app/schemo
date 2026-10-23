package cloud

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"schemo/internal/db"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"schemo/internal/platform"
)

// The account is one row and a conversation with a server, and both halves are faked here: what
// these tests are about is what the app decides — when it asks, what it keeps, and what it says
// when the other side is not there.

// fakeKeyring is the OS keyring, beside the real store that keeps the account row.
type fakeKeyring struct {
	db    *db.Store
	mu    sync.Mutex
	token string
}

func (f *fakeKeyring) Get(domain.Account) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token == "" {
		return "", domain.ErrNotFound
	}
	return f.token, nil
}

func (f *fakeKeyring) Set(_ domain.Account, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.token = token
	return nil
}

func (f *fakeKeyring) Delete(domain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.token = ""
	return nil
}

// saved is the account row as the store holds it, and the token as the keyring does.
func (f *fakeKeyring) saved() (domain.Account, string) {
	account, _ := f.db.Account(context.Background())
	f.mu.Lock()
	defer f.mu.Unlock()
	return account, f.token
}

// workspaceOf makes a workspace tagged with an account key, and answers whether it is still there.
func (f *fakeKeyring) workspaceOf(t *testing.T, key string) func() bool {
	t.Helper()
	ctx := context.Background()
	if err := f.db.SaveWorkspace(ctx, domain.NewWorkspace("acct-space", "Team", domain.WorkspaceTeam, "", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.DB().ExecContext(ctx,
		`UPDATE workspaces SET remote_account = ? WHERE id = 'acct-space'`, key); err != nil {
		t.Fatal(err)
	}
	return func() bool {
		_, err := f.db.Workspace(ctx, "acct-space")
		return err == nil
	}
}

// fakeServer is the other side: what it answers, and when.
type fakeServer struct {
	mu    sync.Mutex
	calls []string
	// authorizeTo is the address the sign-in was asked to go to. A method name alone would not catch
	// a sign-in that went to the wrong server, and the address is what the app then lives on.
	authorizeTo string
	// checked is the address a check asked about, a question that leaves the account where it was.
	checked string

	challenge    domain.Challenge
	authorizeErr error
	// discoverErr is what the discovery document says about an address: a server this app cannot
	// sign in to is refused there.
	discoverErr error
	// confirming is where a test says what the person did: the answer the token endpoint would give.
	confirming chan answer

	me       domain.Account
	meErr    error
	sessions []domain.Session
	refresh  domain.Tokens
	// refreshErr is what the token endpoint says when the refresh token is no good.
	refreshErr error
	// deleteErr is the account refusing to go: somebody who may not be the last one out of an
	// organisation is the case, and the device holds everything it did for them.
	deleteErr error
}

type answer struct {
	tokens domain.Tokens
	err    error
}

func newFakeServer() *fakeServer {
	return &fakeServer{
		challenge: domain.Challenge{
			UserCode:  "BCDF-GHJK",
			URL:       "https://api.example.com/activate?user_code=BCDF-GHJK",
			ExpiresAt: time.Now().Add(10 * time.Minute),
		},
		confirming: make(chan answer, 1),
		me: domain.Account{
			UserID:    "user-1",
			Email:     "sample.dev@example.com",
			SessionID: "session-1",
		},
		sessions: []domain.Session{{ID: "session-1", Device: domain.Device{Platform: "macOS"}}},
		refresh:  tokens("access-2", "refresh-2"),
	}
}

func (f *fakeServer) note(method string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
}

func (f *fakeServer) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.calls...)
}

func (f *fakeServer) Discover(_ context.Context, address string) error {
	f.note("discover")

	f.mu.Lock()
	f.checked = address
	f.mu.Unlock()

	return f.discoverErr
}

// checkedAt is the address the window asked about, which is the account's own only once somebody
// signs in there.
func (f *fakeServer) checkedAt() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checked
}

func (f *fakeServer) Authorize(_ context.Context, address string, _ domain.Device) (
	domain.Challenge, error) {
	f.note("authorize")

	f.mu.Lock()
	f.authorizeTo = address
	f.mu.Unlock()

	return f.challenge, f.authorizeErr
}

// askedTo is where the sign-in went, which is the address the app is then signed in to.
func (f *fakeServer) askedTo() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authorizeTo
}

// Tokens waits for the person, which is what the real one does: it returns when the test says what
// they answered, or when the app stops caring.
func (f *fakeServer) Tokens(ctx context.Context, _ string, _ domain.Challenge) (
	domain.Tokens, error) {
	f.note("tokens")
	select {
	case given := <-f.confirming:
		return given.tokens, given.err
	case <-ctx.Done():
		return domain.Tokens{}, ctx.Err()
	}
}

func (f *fakeServer) Refresh(context.Context, string, string) (domain.Tokens, error) {
	f.note("refresh")
	return f.refresh, f.refreshErr
}

func (f *fakeServer) Me(context.Context, string, string) (domain.Account, error) {
	f.note("me")
	return f.me, f.meErr
}

func (f *fakeServer) Sessions(context.Context, string, string) ([]domain.Session, error) {
	f.note("sessions")
	return f.sessions, nil
}

func (f *fakeServer) EndSession(_ context.Context, _, _, sessionID string) (
	[]domain.Session, error) {
	f.note("end " + sessionID)
	return f.sessions, nil
}

func (f *fakeServer) EndOthers(context.Context, string, string) ([]domain.Session, error) {
	f.note("end others")
	return f.sessions, nil
}

func (f *fakeServer) SignOut(context.Context, string, string) error {
	f.note("sign out")
	return nil
}

func (f *fakeServer) DeleteAccount(context.Context, string, string) error {
	f.note("delete account")
	return f.deleteErr
}

// fakeBrowser remembers what it was asked to open, and says nothing about whether it managed.
type fakeBrowser struct{ opened []string }

func (f *fakeBrowser) OpenURL(url string) error {
	f.opened = append(f.opened, url)
	return nil
}

// fakeNotifier hands every announcement to the test, which is what the window does.
type fakeNotifier struct{ events chan AccountStatus }

func newFakeNotifier() *fakeNotifier { return &fakeNotifier{events: make(chan AccountStatus, 8)} }

func (n *fakeNotifier) Publish(_ string, payload any) {
	state, ok := payload.(AccountStatus)
	if !ok {
		return
	}
	n.events <- state
}

// until waits for the announcement a test is about. The app says several things while a sign-in
// runs its course — the code is out, the code was confirmed — and a test that read the first one
// would be reading the wrong step.
func (n *fakeNotifier) until(t *testing.T, wanted func(AccountStatus) bool) AccountStatus {
	t.Helper()

	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		select {
		case state := <-n.events:
			if wanted(state) {
				return state
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	t.Fatal("the announcement a test was waiting for never came")
	return AccountStatus{}
}

func newAccount(t *testing.T) (*Account, *fakeKeyring, *fakeServer, *fakeBrowser, *fakeNotifier) {
	t.Helper()
	store, server := &fakeKeyring{db: dbtest.New(t)}, newFakeServer()
	browser, notifier := &fakeBrowser{}, newFakeNotifier()
	return NewAccount(store.db, store, server, browser, notifier), store, server, browser, notifier
}

// tokens is the pair a confirmed code produces, good for an hour: what the app keeps in memory and
// what it hands to the server.
func tokens(access, refresh string) domain.Tokens {
	return domain.Tokens{Access: access, Refresh: refresh, ExpiresAt: time.Now().Add(time.Hour)}
}

// signedIn is an app that already has an account, which is where the tests about anything but
// signing in start. It waits for the sign-in to finish rather than assuming it has: the work
// happens in the watch, and a test that raced it would be a test about timing.
func signedIn(t *testing.T, account *Account) {
	t.Helper()

	ctx := context.Background()
	notifier, ok := account.notifier.(*fakeNotifier)
	if !ok {
		t.Fatal("the service was built without the fake notifier")
	}

	if _, err := account.Begin(ctx, "https://api.example.com"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	theServer(t, account).confirming <- answer{
		tokens: tokens("access-1", "refresh-1"),
	}
	notifier.until(t, func(state AccountStatus) bool { return state.SignedIn })
}

// expired moves the app's clock past the token it holds, which is what makes it go and buy another.
func expired(account *Account) {
	account.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
}

// theServer is the fake under the wrapper the service talks to: every call to the port is recorded
// by the wrapper, so a test driving the fake has to reach through it.
func theServer(t *testing.T, account *Account) *fakeServer {
	t.Helper()

	watcher, ok := account.server.(watched)
	if !ok {
		t.Fatal("the service was built without the wrapper that records reachability")
	}
	server, ok := watcher.AccountServer.(*fakeServer)
	if !ok {
		t.Fatal("the port under the wrapper is not the fake")
	}
	return server
}

func TestASignInShowsACodeAndOpensTheBrowser(t *testing.T) {
	account, _, _, browser, _ := newAccount(t)

	state, err := account.Begin(context.Background(), "https://api.example.com")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	if state.Challenge.UserCode != "BCDF-GHJK" {
		t.Errorf("code = %q, want the one the server handed out", state.Challenge.UserCode)
	}
	if !state.Waiting {
		t.Error("the app is not waiting for anybody, and the person has not answered yet")
	}
	// The browser opens on the address the server gave, code and all: the person should not have to
	// type anything.
	if len(browser.opened) != 1 || browser.opened[0] != state.Challenge.URL {
		t.Errorf("opened %v, want the confirmation page", browser.opened)
	}
}

func TestASignInThatIsConfirmedEndsWithAnAccount(t *testing.T) {
	account, store, server, _, notifier := newAccount(t)

	if _, err := account.Begin(context.Background(), "api.example.com"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	server.confirming <- answer{
		tokens: tokens("access-1", "refresh-1"),
	}

	announced := notifier.until(t, func(state AccountStatus) bool { return state.SignedIn })
	if announced.Account.Email != "sample.dev@example.com" {
		t.Errorf("email = %q, want the address the server answered with", announced.Account.Email)
	}
	if announced.Account.SessionID != "session-1" {
		t.Errorf("session = %q, want the one the server named", announced.Account.SessionID)
	}
	// The address is kept with the scheme it was given: the app talks to this one again.
	if announced.Account.Server != "https://api.example.com" {
		t.Errorf("server = %q, want the normalised address", announced.Account.Server)
	}
	if announced.Waiting {
		t.Error("the app is still waiting after the code was confirmed")
	}

	saved, token := store.saved()
	if saved.UserID != "user-1" || token != "refresh-1" {
		t.Errorf("kept %+v with token %q, want the account and its credential", saved, token)
	}
}

func TestASignInThatCameToNothingSaysSo(t *testing.T) {
	account, _, server, _, notifier := newAccount(t)

	if _, err := account.Begin(context.Background(), "https://api.example.com"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// The code ran out while the person was looking for their phone.
	server.confirming <- answer{err: errors.New("expired")}

	announced := notifier.until(t, func(state AccountStatus) bool { return state.Failure != nil })
	if announced.Failure == nil {
		t.Fatal("the app said nothing about a sign-in that went nowhere")
	}
	if announced.SignedIn || announced.Waiting {
		t.Errorf("state = %+v, want nothing signed in and nothing in progress", announced)
	}
}

func TestCancellingASignInStopsWaiting(t *testing.T) {
	account, _, server, _, notifier := newAccount(t)

	if _, err := account.Begin(context.Background(), "https://api.example.com"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := account.Cancel(context.Background()); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	announced := notifier.until(t, func(state AccountStatus) bool { return !state.Waiting })
	if announced.Waiting {
		t.Errorf("state = %+v, want the wait to be over", announced)
	}
	// Nothing else is announced afterwards: a code confirmed after the person gave up must not sign
	// anybody in.
	server.confirming <- answer{
		tokens: tokens("access-1", "refresh-1"),
	}

	select {
	case late := <-notifier.events:
		if late.SignedIn {
			t.Fatalf("a cancelled sign-in signed the app in: %+v", late)
		}
	case <-time.After(200 * time.Millisecond):
	}
}

func TestTheAddressIsKeptWithASchemeAndWithoutASlash(t *testing.T) {
	account, store, _, _, _ := newAccount(t)

	if _, err := account.SetServer(context.Background(), "  api.example.com/  "); err != nil {
		t.Fatalf("SetServer: %v", err)
	}
	saved, _ := store.saved()
	if saved.Server != "https://api.example.com" {
		t.Errorf("server = %q, want a scheme and no trailing slash", saved.Server)
	}
}

// Checking an address before signing in asks the server at it — the named one, not the default —
// and keeps nothing: neither the address nor the fact of the check, because the only thing a check
// decides is what the window says about the address.
func TestCheckingAnAddressAsksTheServerAtIt(t *testing.T) {
	account, store, server, _, _ := newAccount(t)

	if _, err := account.CheckServer(context.Background(), "api.example.com"); err != nil {
		t.Fatalf("CheckServer: %v", err)
	}
	if went := server.checkedAt(); went != "https://api.example.com" {
		t.Errorf("the check went to %q, want the named server", went)
	}
	saved, _ := store.saved()
	if saved.Server != "" {
		t.Errorf("stored server = %q, want a check to keep nothing", saved.Server)
	}
}

// An address that is not a server this app can sign in to is refused, and the refusal is the
// answer: the window says so before anybody is sent to a browser with a code.
func TestAnAddressThatIsNotAServerIsRefused(t *testing.T) {
	account, _, server, _, _ := newAccount(t)
	server.discoverErr = domain.Refuse(domain.CodeServerRefused, nil, nil)

	_, err := account.CheckServer(context.Background(), "example.com")
	if domain.CodeOf(err) != domain.CodeServerRefused {
		t.Errorf("err = %v, want a refusal", err)
	}
}

// An installation nobody has told about a server is not one that cannot sign in: the program has
// one of its own, so a person who has never chosen one presses the button and is given a code.
func TestASignInWithNoAddressGoesToTheProgramsOwnServer(t *testing.T) {
	account, store, server, _, _ := newAccount(t)

	state, err := account.Begin(context.Background(), "")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if went := server.askedTo(); went != platform.DefaultServer {
		t.Errorf("the sign-in went to %q, want %q", went, platform.DefaultServer)
	}
	// Nothing was chosen, so nothing is drawn: the window shows an address only for a deployment
	// somebody named, and this installation never named one.
	if state.Account.Server != "" {
		t.Errorf("the window is shown %q, want no server of its own", state.Account.Server)
	}
	saved, _ := store.saved()
	if saved.Server != "" {
		t.Errorf("stored server = %q, want nothing kept", saved.Server)
	}
}

// And a sign-in that goes to a server somebody named keeps the name: the window says which server
// it is on.
func TestASignInToAChosenServerKeepsTheName(t *testing.T) {
	account, store, server, _, notifier := newAccount(t)

	if _, err := account.Begin(context.Background(), "api.example.com"); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	server.confirming <- answer{tokens: tokens("access-1", "refresh-1")}
	announced := notifier.until(t, func(state AccountStatus) bool { return state.SignedIn })

	if went := server.askedTo(); went != "https://api.example.com" {
		t.Errorf("the sign-in went to %q, want the named server", went)
	}
	if announced.Account.Server != "https://api.example.com" {
		t.Errorf("the window is shown %q, want the named server", announced.Account.Server)
	}
	saved, _ := store.saved()
	if saved.Server != "https://api.example.com" {
		t.Errorf("stored server = %q, want the named server", saved.Server)
	}
}

// Clearing the address is an answer and not a mistake — "the program's own server" — and what is
// kept is the clearing itself, so that a build pointing somewhere else later takes this
// installation with it. The window then has no server of its own to draw, which is the point.
func TestClearingTheAddressGoesBackToTheProgramsOwnServer(t *testing.T) {
	account, store, _, _, _ := newAccount(t)

	if _, err := account.SetServer(context.Background(), "api.example.com"); err != nil {
		t.Fatalf("SetServer: %v", err)
	}
	state, err := account.SetServer(context.Background(), "   ")
	if err != nil {
		t.Fatalf("SetServer: %v", err)
	}
	if state.Account.Server != "" {
		t.Errorf("the window is shown %q, want no server of its own", state.Account.Server)
	}
	saved, _ := store.saved()
	if saved.Server != "" {
		t.Errorf("stored server = %q, want nothing kept", saved.Server)
	}
}

func TestSigningOutForgetsTheAccountAndKeepsTheAddress(t *testing.T) {
	account, store, server, _, _ := newAccount(t)
	signedIn(t, account)

	state, err := account.SignOut(context.Background(), false)
	if err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	if state.SignedIn {
		t.Error("the app is still signed in after signing out")
	}

	saved, token := store.saved()
	if saved.Server != "https://api.example.com" {
		t.Errorf("server = %q, want the address kept for next time", saved.Server)
	}
	if saved.UserID != "" || token != "" {
		t.Errorf("kept %+v with token %q, want nothing of the account", saved, token)
	}
	if !contains(server.called(), "sign out") {
		t.Errorf("the server was not told: %v", server.called())
	}
}

// Signing out keeps the account's workspaces on this device unless the person asks to wipe them.
func TestSigningOutWipesTheAccountsWorkspacesOnlyWhenAsked(t *testing.T) {
	for _, wipe := range []bool{false, true} {
		account, store, _, _, _ := newAccount(t)
		signedIn(t, account)
		exists := store.workspaceOf(t, domain.AccountKey("https://api.example.com", "sample.dev@example.com"))

		if _, err := account.SignOut(context.Background(), wipe); err != nil {
			t.Fatalf("SignOut: %v", err)
		}
		if exists() == wipe {
			t.Errorf("wipe=%v: workspace kept = %v", wipe, exists())
		}
	}
}

// Deleting the account is the one act here that cannot be undone, and it is the account's act
// before it is this device's: the server is asked, and what it says is the whole of the answer.
// Only once the account is really gone do the workspaces it named go with it — they are somebody's
// work, and an account that still exists is still theirs.
func TestDeletingAnAccountPurgesItAndTakesItsWorkspacesWithIt(t *testing.T) {
	account, store, server, _, _ := newAccount(t)
	signedIn(t, account)
	exists := store.workspaceOf(t, domain.AccountKey("https://api.example.com", "sample.dev@example.com"))

	state, err := account.Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !contains(server.called(), "delete account") {
		t.Errorf("called: %v, want the account asked to go", server.called())
	}
	if exists() {
		t.Error("the account's workspace is still here")
	}
	saved, token := store.saved()
	if saved.SignedIn() || token != "" {
		t.Errorf("kept %+v with token %q, want the account forgotten", saved, token)
	}
	if state.SignedIn {
		t.Error("the window is still told somebody is signed in")
	}
}

// A refusal is the whole answer: the account is still there and the person is still signed in to
// it, so this device is left exactly as it was, workspaces and all.
func TestAnAccountTheServerWillNotDeleteIsLeftAlone(t *testing.T) {
	account, store, server, _, _ := newAccount(t)
	signedIn(t, account)
	exists := store.workspaceOf(t, domain.AccountKey("https://api.example.com", "sample.dev@example.com"))

	server.deleteErr = domain.Refuse(domain.CodeNotAllowed, nil, nil)

	if _, err := account.Delete(context.Background()); err == nil {
		t.Fatal("a refusal from the server was swallowed")
	}
	if !exists() {
		t.Error("the workspaces went although the account still exists")
	}
	if saved, _ := store.saved(); !saved.SignedIn() {
		t.Error("the account was forgotten although the server never let it go")
	}
}

func TestLeavingWorksEvenWhenTheServerIsAway(t *testing.T) {
	account, store, server, _, _ := newAccount(t)
	signedIn(t, account)

	server.refreshErr = domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	expired(account)

	if _, err := account.SignOut(context.Background(), false); err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	// The session stays on the server — nobody could tell it — and that is not a reason to keep
	// somebody signed in on a machine they asked to leave.
	if contains(server.called(), "sign out") {
		t.Errorf("called: %v, want no call to a server that is away", server.called())
	}
	saved, token := store.saved()
	if saved.SignedIn() || token != "" {
		t.Errorf("kept %+v with token %q, want the account gone", saved, token)
	}
}

func TestAServerThatCannotBeReachedIsNotALostAccount(t *testing.T) {
	account, store, server, _, _ := newAccount(t)
	signedIn(t, account)

	server.refreshErr = domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	expired(account)

	_, err := account.Sessions(context.Background())
	if domain.CodeOf(err) != domain.CodeServerUnreachable {
		t.Fatalf("Sessions = %v, want the server being out of reach", err)
	}
	// The account is still here: a server that is down is weather, not a reason to sign somebody out.
	saved, _ := store.saved()
	if saved.UserID != "user-1" {
		t.Errorf("kept %+v, want the account left alone", saved)
	}
}

func TestACheckOnAServerThatIsAwaySaysSo(t *testing.T) {
	account, store, server, _, _ := newAccount(t)
	signedIn(t, account)

	// Once while it is there, so that the moment it last answered is known and can be watched.
	answered, err := account.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	server.refreshErr = domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	expired(account)

	state, err := account.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// The verdict travels as state rather than as an error: a server that does not answer has
	// answered this question, and a call that failed would leave the window with nothing to draw.
	if !state.Reachability.Checked || state.Reachability.Reachable {
		t.Errorf("reachability = %+v, want checked and out of reach", state.Reachability)
	}
	// "Last contact" is when it last answered, and a call that never got there does not move it.
	if !state.Reachability.At.Equal(answered.Reachability.At) {
		t.Errorf("kept %v, want the moment the server last answered (%v)",
			state.Reachability.At, answered.Reachability.At)
	}
	saved, _ := store.saved()
	if saved.UserID != "user-1" {
		t.Errorf("kept %+v, want the account left alone", saved)
	}
}

func TestACheckOnAServerThatAnswersSaysSo(t *testing.T) {
	account, _, _, _, _ := newAccount(t)
	signedIn(t, account)

	state, err := account.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !state.Reachability.Checked || !state.Reachability.Reachable {
		t.Errorf("reachability = %+v, want checked and reached", state.Reachability)
	}
	if state.Reachability.At.IsZero() {
		t.Error("no moment was kept for a call that was made")
	}
}

func TestAServerThatRefusedStillCountsAsReached(t *testing.T) {
	account, _, server, _, _ := newAccount(t)
	signedIn(t, account)

	// The server answers and says no: it is there, and "there" is the whole of what this asks. The
	// two are told apart because the person's next move is different in each case.
	server.meErr = domain.Refuse(domain.CodeServerRefused, nil, nil)

	state, err := account.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !state.Reachability.Reachable {
		t.Errorf("reachability = %+v, want a server that answered", state.Reachability)
	}
}

func TestAnAccountNobodySignedIntoIsNotChecked(t *testing.T) {
	account, _, server, _, _ := newAccount(t)

	state, err := account.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if state.Reachability.Checked {
		t.Errorf("reachability = %+v, want nothing said about a server nobody asked", state.Reachability)
	}
	if called := server.called(); len(called) != 0 {
		t.Errorf("called the server: %v, want no call at all", called)
	}
}

func TestACheckIsAnnouncedToEveryWindow(t *testing.T) {
	account, _, server, _, notifier := newAccount(t)
	signedIn(t, account)

	server.refreshErr = domain.Refuse(domain.CodeServerUnreachable, nil, nil)
	expired(account)

	if _, err := account.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}

	// Every window draws the account, so the verdict is published rather than only handed back to
	// whoever asked: the window that asked is not the only one looking.
	arrived := notifier.until(t, func(state AccountStatus) bool {
		return state.Reachability.Checked && !state.Reachability.Reachable
	})
	if arrived.Reachability.Reachable {
		t.Errorf("announced %+v, want the server out of reach", arrived.Reachability)
	}
}

func TestARefusedRefreshEndsTheAccountHere(t *testing.T) {
	account, store, server, _, _ := newAccount(t)
	signedIn(t, account)

	// The server says this refresh token is no good: the account was ended somewhere else.
	server.refreshErr = domain.Refuse(domain.CodeServerRefused, nil, nil)
	expired(account)

	_, err := account.Sessions(context.Background())
	if domain.CodeOf(err) != domain.CodeNotSignedIn {
		t.Fatalf("Sessions = %v, want the app to say nobody is signed in", err)
	}
	saved, token := store.saved()
	if saved.UserID != "" || token != "" {
		t.Errorf("kept %+v with token %q, want the dead account dropped", saved, token)
	}
}

// A call the server refused for being signed out is not this app's own idea of a token that ran
// out; it is the server saying the session it names is over — ended from another device, most of
// all. Whoever got that answer hands it here, and what comes of it is decided once: a token that is
// refused is replaced, and a session whose refresh is refused as well is a session that has ended.
func TestACallRefusedForBeingSignedOutEndsTheAccount(t *testing.T) {
	account, store, server, _, notifier := newAccount(t)
	signedIn(t, account)

	// The device was ended in the panel, so the server wants nothing to do with either token it holds.
	server.refreshErr = domain.Refuse(domain.CodeNotSignedIn, domain.ErrNotAllowed, nil)

	account.Refused(context.Background(), "session-1")

	saved, token := store.saved()
	if saved.UserID != "" || token != "" {
		t.Errorf("kept %+v with token %q, want the dead account dropped", saved, token)
	}
	state := notifier.until(t, func(state AccountStatus) bool { return !state.SignedIn })
	if state.Account.Email != "" {
		t.Errorf("the window was told %+v, want nobody signed in", state.Account)
	}
	if state.Failure == nil || state.Failure.Code != domain.CodeSessionEnded {
		t.Errorf("the refusal is %+v, want the window told why the session ended", state.Failure)
	}
}

// And the same refusal about a token that merely ran out is not the end of anything: the token is
// bought again and the account stays where it was.
func TestARefusalThatOnlyNeededANewTokenKeepsTheAccount(t *testing.T) {
	account, store, server, _, _ := newAccount(t)
	signedIn(t, account)

	account.Refused(context.Background(), "session-1")

	if bought(server) != 1 {
		t.Errorf("the token was bought %d times, want the refused one replaced once", bought(server))
	}
	if saved, token := store.saved(); saved.UserID == "" || token == "" {
		t.Errorf("kept %+v with token %q, want this device still signed in", saved, token)
	}
}

// A refusal naming a session this device no longer holds is the answer to a call made before a
// sign-in, and it must not end the one that came after it.
func TestARefusalAboutSomebodyElsesSessionIsIgnored(t *testing.T) {
	account, store, _, _, _ := newAccount(t)
	signedIn(t, account)

	account.Refused(context.Background(), "session-of-a-device-that-was-ended")

	if saved, token := store.saved(); saved.UserID == "" || token == "" {
		t.Errorf("kept %+v with token %q, want this device left alone", saved, token)
	}
}

func TestTheTokenIsBoughtOnceAndKeptUntilItRunsOut(t *testing.T) {
	account, _, server, _, _ := newAccount(t)
	signedIn(t, account)

	// The token from the sign-in is good for an hour, and asking three times does not buy three of
	// them: what a call needs is a token, not a new one.
	for range 3 {
		if _, err := account.Sessions(context.Background()); err != nil {
			t.Fatalf("Sessions: %v", err)
		}
	}
	if bought(server) != 0 {
		t.Errorf("the token was bought %d times while it was still good", bought(server))
	}

	// And when it has run out, one call buys the next one — and the two after it do not.
	//
	// The renewed token's own lifetime is measured from the app's clock, which is the one the app
	// looks at: the fixture's hour is the hour it would really have been given.
	expired(account)
	server.refresh = domain.Tokens{
		Access: "access-2", Refresh: "refresh-2", ExpiresAt: account.now().Add(time.Hour),
	}
	for range 3 {
		if _, err := account.Sessions(context.Background()); err != nil {
			t.Fatalf("Sessions: %v", err)
		}
	}
	if bought(server) != 1 {
		t.Errorf("the token was bought %d times, want the one renewal it needed", bought(server))
	}
}

// bought is how many times the app asked the server for a token.
func bought(server *fakeServer) int {
	count := 0
	for _, call := range server.called() {
		if call == "refresh" {
			count++
		}
	}
	return count
}

func TestEndingThisDeviceSignsTheAppOut(t *testing.T) {
	account, store, _, _, _ := newAccount(t)
	signedIn(t, account)

	if _, err := account.EndSession(context.Background(), "session-1"); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	saved, token := store.saved()
	if saved.UserID != "" || token != "" {
		t.Errorf("kept %+v with token %q, want the app signed out of a session that is gone",
			saved, token)
	}
}

func TestEndingAnotherDeviceLeavesThisOneAlone(t *testing.T) {
	account, store, _, _, _ := newAccount(t)
	signedIn(t, account)

	if _, err := account.EndSession(context.Background(), "session-2"); err != nil {
		t.Fatalf("EndSession: %v", err)
	}

	saved, _ := store.saved()
	if saved.UserID != "user-1" {
		t.Errorf("kept %+v, want this device still signed in", saved)
	}
}

func contains(all []string, wanted string) bool {
	for _, one := range all {
		if strings.HasPrefix(one, wanted) {
			return true
		}
	}
	return false
}
