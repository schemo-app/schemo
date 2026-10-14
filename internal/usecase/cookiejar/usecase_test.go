package cookiejar

import (
	"context"
	"errors"
	"testing"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// fakeStore is the jar in memory. The identity it keys by is the one HTTP gives a cookie — host,
// path and name — so a write that replaces is a write over the same key, exactly as the table's
// unique index makes it.
type fakeStore struct {
	cookies map[string]domain.JarCookie
	blocked map[string]bool
	saved   []domain.JarCookie
	deleted []string
	fail    error
}

func newFakeStore() *fakeStore {
	return &fakeStore{cookies: map[string]domain.JarCookie{}, blocked: map[string]bool{}}
}

func keyOf(host, path, name string) string { return host + "|" + path + "|" + name }

func (f *fakeStore) Jar(context.Context, string) ([]domain.JarDomain, error) {
	out := []domain.JarDomain{}
	at := map[string]int{}
	for _, cookie := range f.cookies {
		if _, ok := at[cookie.Domain]; !ok {
			at[cookie.Domain] = len(out)
			out = append(out, domain.JarDomain{
				Domain: cookie.Domain, Blocked: f.blocked[cookie.Domain],
				Cookies: []domain.JarCookie{},
			})
		}
		level := at[cookie.Domain]
		out[level].Cookies = append(out[level].Cookies, cookie)
	}
	return out, nil
}

func (f *fakeStore) JarCookie(
	_ context.Context,
	_, host, path, name string,
) (domain.JarCookie, error) {
	cookie, ok := f.cookies[keyOf(host, path, name)]
	if !ok {
		return domain.JarCookie{}, domain.ErrNotFound
	}
	return cookie, nil
}

func (f *fakeStore) SaveJarCookie(_ context.Context, _ string, cookie domain.JarCookie) error {
	if f.fail != nil {
		return f.fail
	}
	// A row keeps its identity, so a save that moves it — the manager rewriting a cookie's domain —
	// leaves the row it was rather than a second copy of it, which is what the table's primary key
	// makes of the same write.
	for key, held := range f.cookies {
		if held.ID != "" && held.ID == cookie.ID {
			delete(f.cookies, key)
		}
	}
	f.cookies[keyOf(cookie.Domain, cookie.Path, cookie.Name)] = cookie
	f.saved = append(f.saved, cookie)
	return nil
}

func (f *fakeStore) DeleteJarCookie(_ context.Context, _, id string) error {
	for key, cookie := range f.cookies {
		if cookie.ID == id {
			delete(f.cookies, key)
			f.deleted = append(f.deleted, id)
		}
	}
	return nil
}

func (f *fakeStore) DeleteJarDomain(_ context.Context, _, host string) error {
	for key, cookie := range f.cookies {
		if cookie.Domain == host {
			delete(f.cookies, key)
		}
	}
	delete(f.blocked, host)
	return nil
}

func (f *fakeStore) JarDomain(_ context.Context, _, host string) (bool, error) {
	return f.blocked[host], nil
}

func (f *fakeStore) SaveJarDomain(_ context.Context, _, host string, blocked bool) error {
	f.blocked[host] = blocked
	return nil
}

func newUseCase() (*UseCase, *fakeStore) {
	store := newFakeStore()
	return NewUseCase(store, scope{}, platform.NewIDGen()), store
}

type scope struct{}

func (scope) ActiveWorkspace(context.Context) (string, error) {
	return domain.WorkspacePersonalID, nil
}

func answer(pairs ...string) []domain.HeaderPair {
	out := make([]domain.HeaderPair, 0, len(pairs))
	for _, value := range pairs {
		out = append(out, domain.HeaderPair{Name: "Set-Cookie", Value: value})
	}
	return out
}

// A cookie the jar did not have is the answer adding one, and the record is told which of the two
// it was — the window says "Added" against it.
func TestAnAnswerAddsWhatTheJarDidNotHave(t *testing.T) {
	uc, store := newUseCase()

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/articles", answer("session=abc; Path=/; HttpOnly"))
	if len(writes) != 1 || writes[0].State != domain.CookieAdded {
		t.Fatalf("writes = %+v, want the one cookie added", writes)
	}
	if writes[0].Previous != nil {
		t.Errorf("previous = %+v, want none: nothing was there to replace", writes[0].Previous)
	}
	if writes[0].Domain != "api.example.com" {
		t.Errorf("domain = %q, want the host that answered", writes[0].Domain)
	}

	stored := store.cookies[keyOf("api.example.com", "/", "session")]
	if stored.Value != "abc" || !stored.HTTPOnly || stored.Origin != domain.CookieFromResponse {
		t.Errorf("jar = %+v, want the answer's cookie", stored)
	}
}

// The same name, path and domain is one cookie set twice: a replacement, and the record keeps what
// it replaced so the window can say what it was and take the write back.
func TestTheSameCookieSetAgainIsAReplacement(t *testing.T) {
	uc, store := newUseCase()
	store.cookies[keyOf("api.example.com", "/", "session")] = domain.JarCookie{
		ID: "old", Domain: "api.example.com", Name: "session", Value: "first", Path: "/",
	}

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("session=second; Path=/"))
	if len(writes) != 1 || writes[0].State != domain.CookieReplaced {
		t.Fatalf("writes = %+v, want one replacement", writes)
	}
	if writes[0].Previous == nil || writes[0].Previous.Value != "first" {
		t.Errorf("previous = %+v, want the value it replaced", writes[0].Previous)
	}
	if stored := store.cookies[keyOf("api.example.com", "/", "session")]; stored.Value != "second" {
		t.Errorf("jar = %+v, want the new value", stored)
	}
	// The row keeps the id it had: it is the same cookie set again, and the window addresses a row by
	// id — a new one would be a row that moved under the cursor.
	if store.cookies[keyOf("api.example.com", "/", "session")].ID != "old" {
		t.Errorf("jar = %+v, want the row it already had", store.cookies)
	}
}

// A cookie whose life has run out is the server asking for it to be removed, not for another value.
func TestACookieThatDiedIsADeletion(t *testing.T) {
	uc, store := newUseCase()
	store.cookies[keyOf("api.example.com", "/", "session")] = domain.JarCookie{
		ID: "old", Domain: "api.example.com", Name: "session", Value: "first", Path: "/",
	}

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("session=; Path=/; Max-Age=0"))
	if len(writes) != 1 || writes[0].State != domain.CookieDeleted {
		t.Fatalf("writes = %+v, want one deletion", writes)
	}
	if len(store.cookies) != 0 {
		t.Errorf("jar = %+v, want the cookie gone", store.cookies)
	}
	if writes[0].Previous == nil || writes[0].Previous.Value != "first" {
		t.Errorf("previous = %+v, want what the deletion took", writes[0].Previous)
	}
}

// A server takes a cookie away by sending it with no value and a date in the past, and that is how
// google.com clears cookies. Read as a live cookie — which is what a date nobody can parse turns it
// into — the jar would keep it empty and send `__Secure-STRP=` on every request to that host.
func TestACookieTakenAwayWithAnExpiresIsDeleted(t *testing.T) {
	uc, store := newUseCase()
	store.cookies[keyOf("www.google.com", "/", "__Secure-STRP")] = domain.JarCookie{
		ID: "old", Domain: "www.google.com", Name: "__Secure-STRP", Value: "ANmZwa0m", Path: "/",
	}

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.google.com/",
		answer("__Secure-STRP=; expires=Thu, 01-Jan-1970 00:00:00 GMT; path=/; Secure"))
	if len(writes) != 1 || writes[0].State != domain.CookieDeleted {
		t.Fatalf("writes = %+v, want the one deletion", writes)
	}
	if writes[0].Previous == nil || writes[0].Previous.Value != "ANmZwa0m" {
		t.Errorf("previous = %+v, want what the deletion took", writes[0].Previous)
	}
	if len(store.cookies) != 0 {
		t.Errorf("jar = %+v, want the cookie gone", store.cookies)
	}
}

// The same line for a cookie the jar never had is a deletion of nothing, and the answer is reported
// as having changed nothing: a row saying "deleted" over a cookie nobody held is a row about
// nothing, and a browser's own jar has no answer to give for it either.
func TestADeletionOfACookieTheJarNeverHadChangesNothing(t *testing.T) {
	uc, store := newUseCase()

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.google.com/",
		answer("__Secure-STRP=; expires=Thu, 01-Jan-1970 00:00:00 GMT; path=/; Secure"))
	if len(writes) != 0 {
		t.Errorf("writes = %+v, want none: nothing was there to take away", writes)
	}
	if len(store.cookies) != 0 {
		t.Errorf("jar = %+v, want nothing written", store.cookies)
	}
}

// A line may only name a domain its own host owns. Without this any response could write a cookie
// this app would later send to a host that never asked for it — and the one thing worth showing a
// person is that the answer tried.
func TestADomainTheAnswerDoesNotOwnIsKeptOut(t *testing.T) {
	uc, store := newUseCase()

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/articles",
		answer("session=abc; Domain=evil.example.net; Path=/"))
	if len(writes) != 1 || writes[0].State != domain.CookieNotSaved {
		t.Fatalf("writes = %+v, want the one cookie kept out", writes)
	}
	if writes[0].Domain != "evil.example.net" {
		t.Errorf("domain = %q, want the domain the line named", writes[0].Domain)
	}
	if len(store.cookies) != 0 {
		t.Errorf("jar = %+v, want nothing written", store.cookies)
	}
}

// The host that answered is a domain its answers own, and so is anything under it: this is the line
// that lets google.com's answer clear a cookie on www.google.com.
func TestAHostAndItsChildrenAreItsOwn(t *testing.T) {
	uc, store := newUseCase()

	uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.google.com/", answer("wide=1; Domain=google.com; Path=/"))
	if _, ok := store.cookies[keyOf("google.com", "/", "wide")]; !ok {
		t.Errorf("jar = %+v, want the cookie on the domain the answer owns", store.cookies)
	}
}

// The answer google.com gives a request that already carries its cookies: a fresh __Secure-STRP,
// then a deletion for every host variant of it, then NID. It is the answer that reads as this app
// breaking the jar — one cookie named five times, four of them deletions — and what it comes to is
// two changes: the cookie google.com set and cleared is gone, and NID is set.
//
// The four lines that name www.google.com clear a cookie of that host, which the jar never held:
// nothing came of them, so nothing is reported. Chrome, given these six lines through the same
// redirect, ends holding NID alone — which is what the jar is held to here.
func TestTheAnswerThatNamesOneCookieFiveTimes(t *testing.T) {
	uc, store := newUseCase()
	store.cookies[keyOf("google.com", "/", "__Secure-STRP")] = domain.JarCookie{
		ID: "old", Domain: "google.com", Name: "__Secure-STRP", Value: "4a91c2f8", Path: "/",
	}

	lines := answer(
		"__Secure-STRP=ANmZwa2P9Olw; expires=Sat, 19-Sep-2026 16:04:13 GMT; path=/; "+
			"domain=.google.com; Secure; SameSite=strict",
		"__Secure-STRP=; expires=Mon, 01-Jan-1990 00:00:00 GMT; path=/; domain=www.google.com; Secure",
		"__Secure-STRP=; expires=Mon, 01-Jan-1990 00:00:00 GMT; path=/; domain=.www.google.com; Secure",
		"__Secure-STRP=; expires=Mon, 01-Jan-1990 00:00:00 GMT; path=/; domain=google.com; Secure",
		"__Secure-STRP=; expires=Mon, 01-Jan-1990 00:00:00 GMT; path=/; domain=.google.com; Secure",
		"NID=CVIB; expires=Sun, 21-Mar-2027 15:58:58 GMT; path=/; domain=.google.com; HttpOnly")

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.google.com/", lines)
	if len(writes) != 2 {
		t.Fatalf("writes = %d, want one per cookie the answer changed: %+v", len(writes), writes)
	}
	// The cookie google.com set and then cleared: the last line stands for it, and it remembers the
	// row the first one found — the row the jar held before this answer.
	deleted := writes[0]
	if deleted.Domain != "google.com" || deleted.State != domain.CookieDeleted {
		t.Errorf("first write = %+v, want the cookie the answer set and took away", deleted)
	}
	if deleted.Previous == nil || deleted.Previous.Value != "4a91c2f8" {
		t.Errorf("previous = %+v, want the row the answer found", deleted.Previous)
	}
	if writes[1].Name != "NID" || writes[1].State != domain.CookieAdded {
		t.Errorf("second write = %+v, want NID added", writes[1])
	}

	// What the jar holds afterwards is NID alone, which is where Chrome ends too.
	jar, err := store.Jar(context.Background(), domain.WorkspacePersonalID)
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	if len(jar) != 1 || len(jar[0].Cookies) != 1 || jar[0].Cookies[0].Name != "NID" {
		t.Errorf("jar = %+v, want NID alone", jar)
	}
}

// Taking that answer back puts the cookie where it was. The write that stands for it is the last
// line's, so the row it restores has to be the one the first line found.
func TestTakingThatAnswerBackRestoresTheRowTheFirstLineFound(t *testing.T) {
	uc, store := newUseCase()
	store.cookies[keyOf("google.com", "/", "__Secure-STRP")] = domain.JarCookie{
		ID: "old", Domain: "google.com", Name: "__Secure-STRP", Value: "4a91c2f8", Path: "/",
	}

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.google.com/",
		answer(
			"__Secure-STRP=ANmZwa2P9Olw; expires=Sat, 19-Sep-2026 16:04:13 GMT; path=/; "+
				"domain=.google.com; Secure; SameSite=strict",
			"__Secure-STRP=; expires=Mon, 01-Jan-1990 00:00:00 GMT; path=/; domain=.google.com; Secure",
		))
	if err := uc.Undo(context.Background(), domain.WorkspacePersonalID, writes); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := store.cookies[keyOf("google.com", "/", "__Secure-STRP")].Value; got != "4a91c2f8" {
		t.Errorf("after undo = %q, want the value the answer found", got)
	}
}

// A line that names no domain, and one that names the host that answered, both belong to that host
// alone — a browser reads the second as the first (RFC 6265bis §5.6). Only a line naming a parent
// domain reaches everything under it.
func TestALineWithoutADomainBelongsToTheHostAlone(t *testing.T) {
	uc, store := newUseCase()

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.example.com/",
		answer("a=1; Path=/", "b=2; Domain=www.example.com; Path=/", "c=3; Domain=.example.com; Path=/"))
	if len(writes) != 3 {
		t.Fatalf("writes = %+v, want three", writes)
	}
	for i, want := range []bool{true, true, false} {
		if writes[i].HostOnly != want {
			t.Errorf("write %d = %+v, want host only = %v", i, writes[i], want)
		}
	}
	if got := store.cookies[keyOf("www.example.com", "/", "a")].HostOnly; !got {
		t.Error("the cookie of a line with no domain is not for that host alone")
	}
	if got := store.cookies[keyOf("example.com", "/", "c")].HostOnly; got {
		t.Error("the cookie of a line naming a domain is for that host alone")
	}
}

// The reach of a cookie is the domain it carries: a row the manager rewrites reaches as far as the
// domain drawn in its field says. Saving a row unchanged leaves the cookie where it was, and a dot
// put in front of the domain in the form widens it — which is the whole of how the manager is told.
func TestTheReachOfACookieIsTheDomainItCarries(t *testing.T) {
	uc, _ := newUseCase()
	uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.example.com/", answer("a=1; Domain=www.example.com; Path=/"))

	levels, err := uc.Jar(context.Background())
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	if len(levels) != 1 || len(levels[0].Cookies) != 1 || !levels[0].Cookies[0].HostOnly {
		t.Fatalf("jar = %+v, want the host-only cookie it was written as", levels)
	}

	saved := levels[0].Cookies[0]
	if err := uc.Save(context.Background(), saved); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !hostOnlyIn(t, uc) {
		t.Errorf("saving a row unchanged moved the cookie it stands for")
	}

	saved = levels[0].Cookies[0]
	saved.Domain = ".example.com"
	if err := uc.Save(context.Background(), saved); err != nil {
		t.Fatalf("Save: %v", err)
	}
	levels, err = uc.Jar(context.Background())
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	wide := levels[0].Cookies[0]
	if wide.HostOnly || wide.Domain != "example.com" {
		t.Errorf("jar = %+v, want the dotted domain to have widened the cookie", levels)
	}
}

// hostOnlyIn is whether the jar's only cookie belongs to its host alone, which is what the two
// saves above are compared by.
func hostOnlyIn(t *testing.T, uc *UseCase) bool {
	t.Helper()
	levels, err := uc.Jar(context.Background())
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	if len(levels) != 1 || len(levels[0].Cookies) != 1 {
		t.Fatalf("jar = %+v, want the one cookie it holds", levels)
	}
	return levels[0].Cookies[0].HostOnly
}

// A domain whose writes are blocked is the manager's promise that nothing a server says changes the
// jar — and every cookie of the answer is reported as not saved rather than silently dropped.
func TestABlockedDomainTakesNothingFromTheAnswer(t *testing.T) {
	uc, store := newUseCase()
	store.blocked["api.example.com"] = true

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("session=abc; Path=/"))
	if len(writes) != 1 || writes[0].State != domain.CookieNotSaved {
		t.Fatalf("writes = %+v, want one cookie not saved", writes)
	}
	if len(store.cookies) != 0 {
		t.Errorf("jar = %+v, want nothing taken", store.cookies)
	}
}

// A cookie with a domain of its own belongs to that domain and not to the host that answered, which
// is what makes a `.example.com` cookie reach every host under it.
func TestACookieOfItsOwnDomainLandsOnThatDomain(t *testing.T) {
	uc, store := newUseCase()

	uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("wide=1; Domain=.example.com; Path=/"))
	if _, ok := store.cookies[keyOf("example.com", "/", "wide")]; !ok {
		t.Errorf("jar = %+v, want the cookie on the domain the line named", store.cookies)
	}
}

// Taking a write back is the answer's change undone: what it added goes, and what it replaced comes
// back as it was — whole, because the record kept the row and not only its value.
func TestUndoPutsBackWhatTheAnswerChanged(t *testing.T) {
	uc, store := newUseCase()
	before := domain.JarCookie{
		ID: "old", Domain: "api.example.com", Name: "session", Value: "first", Path: "/",
		ExpiresAt: at(2030), Secure: true, Origin: domain.CookieFromResponse, WrittenAt: 1,
	}
	store.cookies[keyOf("api.example.com", "/", "session")] = before

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("session=second; Path=/", "locale=en-US; Path=/"))
	if err := uc.Undo(context.Background(), domain.WorkspacePersonalID, writes); err != nil {
		t.Fatalf("Undo: %v", err)
	}

	back := store.cookies[keyOf("api.example.com", "/", "session")]
	if back != before {
		t.Errorf("session = %+v, want the row the write found", back)
	}
	if _, ok := store.cookies[keyOf("api.example.com", "/", "locale")]; ok {
		t.Errorf("jar = %+v, want the added cookie gone", store.cookies)
	}
}

// A write is a fact about one moment: a row somebody has changed since is not restored over, or
// taking an answer back would throw away what was written after it.
func TestUndoLeavesARowThatMovedOn(t *testing.T) {
	uc, store := newUseCase()
	store.cookies[keyOf("api.example.com", "/", "session")] = domain.JarCookie{
		ID: "old", Domain: "api.example.com", Name: "session", Value: "first", Path: "/",
	}
	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("session=second; Path=/"))

	// Somebody else writes the cookie after the answer did.
	store.cookies[keyOf("api.example.com", "/", "session")] = domain.JarCookie{
		ID: "old", Domain: "api.example.com", Name: "session", Value: "third", Path: "/",
	}

	if err := uc.Undo(context.Background(), domain.WorkspacePersonalID, writes); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := store.cookies[keyOf("api.example.com", "/", "session")].Value; got != "third" {
		t.Errorf("session = %q, want the value somebody wrote after the answer", got)
	}
}

// What one request sends: the cookies of its host, as the header carries them, and nothing of
// another host's. The header is the whole of what a request reads from the jar — it has no cookies
// of its own and nothing to say about the ones it has.
func TestTheHeaderIsWhatOneAddressSends(t *testing.T) {
	uc, store := newUseCase()
	store.cookies[keyOf("api.example.com", "/", "session")] = domain.JarCookie{
		ID: "c1", Domain: "api.example.com", Name: "session", Value: "abc", Path: "/",
	}
	store.cookies[keyOf("api.example.com", "/", "locale")] = domain.JarCookie{
		ID: "c2", Domain: "api.example.com", Name: "locale", Value: "en-US", Path: "/",
	}
	store.cookies[keyOf("auth.example.com", "/", "token")] = domain.JarCookie{
		ID: "c3", Domain: "auth.example.com", Name: "token", Value: "t", Path: "/",
	}
	store.cookies[keyOf("api.example.com", "/", "dead")] = domain.JarCookie{
		ID: "c4", Domain: "api.example.com", Name: "dead", Value: "x", Path: "/",
		ExpiresAt: at(2020),
	}

	header, err := uc.CookieHeader(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/articles")
	if err != nil {
		t.Fatalf("CookieHeader: %v", err)
	}
	if header != "locale=en-US; session=abc" {
		t.Errorf("header = %q, want this host's living cookies, in the order they are sent", header)
	}
	// A cookie with no name is not one HTTP can carry, and an address the jar knows nothing about
	// gets no header at all.
	store.cookies[keyOf("api.example.com", "/", "nameless")] = domain.JarCookie{
		ID: "c5", Domain: "api.example.com", Name: "  ", Value: "x", Path: "/",
	}
	header, err = uc.CookieHeader(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/")
	if err != nil {
		t.Fatalf("CookieHeader: %v", err)
	}
	if header != "locale=en-US; session=abc" {
		t.Errorf("header = %q, want the nameless cookie left out", header)
	}
	header, err = uc.CookieHeader(context.Background(), domain.WorkspacePersonalID,
		"https://other.example.net/")
	if err != nil {
		t.Fatalf("CookieHeader: %v", err)
	}
	if header != "" {
		t.Errorf("header = %q, want nothing for a host the jar does not know", header)
	}
}

// A cookie typed by hand says how far it reaches with the dot in front of its domain, the way a
// browser's own cookie list writes it: a dot means everything under the domain, no dot means the
// host alone. It is decided from the row rather than carried in it, so the manager's form has no
// switch for it — what somebody typed is the whole of the answer.
func TestACookieTypedByHandSaysHowFarItReaches(t *testing.T) {
	uc, store := newUseCase()

	err := uc.Save(context.Background(), domain.JarCookie{
		Domain: ".example.com", Name: "wide", Value: "1",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	wide := store.cookies[keyOf("example.com", "/", "wide")]
	if wide.HostOnly {
		t.Errorf("hostOnly = true, want a dotted domain to reach everything under it")
	}
	if wide.Domain != "example.com" {
		t.Errorf("domain = %q, want the dot trimmed", wide.Domain)
	}
	if !domain.HostMatches(wide.Domain, "www.example.com") {
		t.Errorf("the saved cookie does not reach the host it was told to")
	}

	err = uc.Save(context.Background(), domain.JarCookie{
		Domain: "API.Example.com", Name: "narrow", Value: "2",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	narrow := store.cookies[keyOf("api.example.com", "/", "narrow")]
	if !narrow.HostOnly {
		t.Errorf("hostOnly = false, want a bare domain to belong to that host alone")
	}
}

// The manager's row is saved whole: a row just typed is given an identity, and one that came from
// the table keeps the identity it was found by, because that is what the window addresses it by. A
// cookie with no name is not one HTTP can carry, and the jar says so rather than keeping a row
// nothing could ever send.
func TestARowOfTheTableIsSavedWhole(t *testing.T) {
	uc, store := newUseCase()

	typed := domain.JarCookie{Domain: "api.example.com", Name: "session", Value: "abc"}
	if err := uc.Save(context.Background(), typed); err != nil {
		t.Fatalf("Save: %v", err)
	}
	saved := store.saved[0]
	if saved.ID == "" {
		t.Errorf("id = %q, want one minted for a row that had none", saved.ID)
	}
	if saved.Path != "/" || saved.Origin != domain.CookieByHand {
		t.Errorf("path = %q, origin = %q, want the root and a hand-written cookie",
			saved.Path, saved.Origin)
	}

	known := saved
	known.Value = "def"
	if err := uc.Save(context.Background(), known); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if store.saved[1].ID != saved.ID {
		t.Errorf("id = %q, want the row keeping the identity it was found by", store.saved[1].ID)
	}
	if store.cookies[keyOf("api.example.com", "/", "session")].Value != "def" {
		t.Errorf("value = %q, want the row written over",
			store.cookies[keyOf("api.example.com", "/", "session")].Value)
	}

	err := uc.Save(context.Background(), domain.JarCookie{Domain: "api.example.com", Value: "x"})
	if err == nil {
		t.Errorf("Save: want a nameless cookie refused")
	}
}

// A cookie is kept under a domain, and one with none would be a row the rail has no name for and no
// request could ever match.
func TestACookieWithoutADomainIsRefused(t *testing.T) {
	uc, store := newUseCase()

	for _, written := range []string{"", "  ", "."} {
		err := uc.Save(context.Background(), domain.JarCookie{Domain: written, Name: "session"})
		if domain.CodeOf(err) != domain.CodeDomainEmpty {
			t.Errorf("Save(%q) = %q, want %q", written, domain.CodeOf(err), domain.CodeDomainEmpty)
		}
	}
	if len(store.saved) != 0 {
		t.Errorf("saved = %+v, want nothing written", store.saved)
	}
}

// What the manager's footer deletes is the domain on screen, written the way the rail draws it:
// with or without the dot, in any case. The cookies of the other domains stay.
func TestDeletingADomainTakesItsCookiesAndItsFlag(t *testing.T) {
	uc, store := newUseCase()
	ctx := context.Background()
	for _, host := range []string{"api.example.com", "other.example.org"} {
		if err := uc.Save(ctx, domain.JarCookie{Domain: host, Name: "session", Value: "1"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	if err := uc.SetBlocked(ctx, "api.example.com", true); err != nil {
		t.Fatalf("SetBlocked: %v", err)
	}

	if err := uc.DeleteDomain(ctx, ".API.example.com"); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}

	if _, kept := store.cookies[keyOf("api.example.com", "/", "session")]; kept {
		t.Errorf("the cookie of the deleted domain is still in the jar")
	}
	if store.blocked["api.example.com"] {
		t.Errorf("the flag of the deleted domain is still set")
	}
	if _, kept := store.cookies[keyOf("other.example.org", "/", "session")]; !kept {
		t.Errorf("the cookie of another domain went with it")
	}
}

// The table under an answer draws the same cookies the header is built from, and draws them whole:
// a value and the attributes a `Cookie` header has no room for. One list read twice is what keeps a
// person from reading a table of one jar and sending another.
func TestTheRowsOfAnAddressAreWhatItsHeaderIs(t *testing.T) {
	uc, store := newUseCase()
	store.cookies[keyOf("api.example.com", "/", "session")] = domain.JarCookie{
		ID: "c1", Domain: "api.example.com", Name: "session", Value: "abc", Path: "/",
		Secure: true, HTTPOnly: true, SameSite: "Lax",
	}
	store.cookies[keyOf("api.example.com", "/v1", "narrow")] = domain.JarCookie{
		ID: "c2", Domain: "api.example.com", Name: "narrow", Value: "n", Path: "/v1",
	}
	store.cookies[keyOf("other.example.net", "/", "elsewhere")] = domain.JarCookie{
		ID: "c3", Domain: "other.example.net", Name: "elsewhere", Value: "x", Path: "/",
	}
	store.cookies[keyOf("api.example.com", "/", "dead")] = domain.JarCookie{
		ID: "c4", Domain: "api.example.com", Name: "dead", Value: "x", Path: "/", ExpiresAt: at(2020),
	}

	const address = "https://api.example.com/v1/articles"
	rows, err := uc.Rows(context.Background(), address)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "narrow" || rows[1].Name != "session" {
		t.Fatalf("rows = %+v, want this address's two cookies, the narrower path first", rows)
	}
	if !rows[1].Secure || !rows[1].HTTPOnly || rows[1].SameSite != "Lax" {
		t.Errorf("row = %+v, want the attributes the header leaves out", rows[1])
	}

	header, err := uc.CookieHeader(context.Background(), domain.WorkspacePersonalID, address)
	if err != nil {
		t.Fatalf("CookieHeader: %v", err)
	}
	if header != "narrow=n; session=abc" {
		t.Errorf("header = %q, want the same cookies the table drew", header)
	}
}

// A store that cannot be written is not a send that failed. The answer arrived, and what the window
// draws about it is that the jar did not take its cookies — a record of an answer that set nothing
// would be the same lie told the other way round.
func TestAStoreThatCannotBeWrittenReportsTheCookiesAsNotSaved(t *testing.T) {
	uc, store := newUseCase()
	store.fail = errors.New("the jar is not writable")

	writes := uc.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("session=abc; Path=/; HttpOnly"))
	if len(writes) != 1 || writes[0].State != domain.CookieNotSaved {
		t.Fatalf("writes = %+v, want the answer's cookie marked as not saved", writes)
	}
	if writes[0].Name != "session" || !writes[0].HTTPOnly || writes[0].Domain != "api.example.com" {
		t.Errorf("write = %+v, want the answer's own cookie, drawn as the jar would have kept it",
			writes[0])
	}
}

// at is an instant, in unix milliseconds, for the expiries above.
func at(year int) *int64 {
	millis := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	return &millis
}
