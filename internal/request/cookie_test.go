package request

import (
	"context"
	"errors"
	"reflect"
	"schemo/internal/db"
	"schemo/internal/db/dbtest"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"strings"
	"testing"
	"time"
)

// jar reads and writes the real store behind the service, for setting a test up and checking it.
type jar struct {
	t  *testing.T
	db *db.Store
}

func newCookies(t *testing.T) (*Cookies, jar) {
	db := dbtest.New(t)
	return NewCookies(db, domain.WorkspaceScope(domain.WorkspacePersonalID), platform.NewIDGen()), jar{t, db}
}

func (j jar) put(cookie domain.JarCookie) {
	j.t.Helper()
	if err := j.db.SaveJarCookie(j.t.Context(), domain.WorkspacePersonalID, cookie); err != nil {
		j.t.Fatal(err)
	}
}

func (j jar) get(host, path, name string) (domain.JarCookie, bool) {
	j.t.Helper()
	cookie, err := j.db.JarCookie(j.t.Context(), domain.WorkspacePersonalID, host, path, name)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.JarCookie{}, false
	}
	if err != nil {
		j.t.Fatal(err)
	}
	return cookie, true
}

func (j jar) cookie(host, path, name string) domain.JarCookie {
	cookie, _ := j.get(host, path, name)
	return cookie
}

func (j jar) all() []domain.JarCookie {
	j.t.Helper()
	domains, err := j.db.Jar(j.t.Context(), domain.WorkspacePersonalID)
	if err != nil {
		j.t.Fatal(err)
	}
	return allCookies(domains)
}

func (j jar) blocked(host string) bool {
	j.t.Helper()
	blocked, err := j.db.JarDomain(j.t.Context(), domain.WorkspacePersonalID, host)
	if err != nil {
		j.t.Fatal(err)
	}
	return blocked
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
	cookies, store := newCookies(t)

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
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

	stored := store.cookie("api.example.com", "/", "session")
	if stored.Value != "abc" || !stored.HTTPOnly || stored.Origin != domain.CookieFromResponse {
		t.Errorf("jar = %+v, want the answer's cookie", stored)
	}
}

// The same name, path and domain is one cookie set twice: a replacement, and the record keeps what
// it replaced so the window can say what it was and take the write back.
func TestTheSameCookieSetAgainIsAReplacement(t *testing.T) {
	cookies, store := newCookies(t)
	store.put(domain.JarCookie{
		ID: "old", Domain: "api.example.com", Name: "session", Value: "first", Path: "/",
	})

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("session=second; Path=/"))
	if len(writes) != 1 || writes[0].State != domain.CookieReplaced {
		t.Fatalf("writes = %+v, want one replacement", writes)
	}
	if writes[0].Previous == nil || writes[0].Previous.Value != "first" {
		t.Errorf("previous = %+v, want the value it replaced", writes[0].Previous)
	}
	if stored := store.cookie("api.example.com", "/", "session"); stored.Value != "second" {
		t.Errorf("jar = %+v, want the new value", stored)
	}
	// The row keeps the id it had: it is the same cookie set again, and the window addresses a row by
	// id — a new one would be a row that moved under the cursor.
	if store.cookie("api.example.com", "/", "session").ID != "old" {
		t.Errorf("jar = %+v, want the row it already had", store.all())
	}
}

// A cookie whose life has run out is the server asking for it to be removed, not for another value.
func TestACookieThatDiedIsADeletion(t *testing.T) {
	cookies, store := newCookies(t)
	store.put(domain.JarCookie{
		ID: "old", Domain: "api.example.com", Name: "session", Value: "first", Path: "/",
	})

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("session=; Path=/; Max-Age=0"))
	if len(writes) != 1 || writes[0].State != domain.CookieDeleted {
		t.Fatalf("writes = %+v, want one deletion", writes)
	}
	if len(store.all()) != 0 {
		t.Errorf("jar = %+v, want the cookie gone", store.all())
	}
	if writes[0].Previous == nil || writes[0].Previous.Value != "first" {
		t.Errorf("previous = %+v, want what the deletion took", writes[0].Previous)
	}
}

// A server takes a cookie away by sending it with no value and a date in the past, and that is how
// google.com clears cookies. Read as a live cookie — which is what a date nobody can parse turns it
// into — the jar would keep it empty and send `__Secure-STRP=` on every request to that host.
func TestACookieTakenAwayWithAnExpiresIsDeleted(t *testing.T) {
	cookies, store := newCookies(t)
	store.put(domain.JarCookie{
		ID: "old", Domain: "www.google.com", Name: "__Secure-STRP", Value: "ANmZwa0m", Path: "/",
	})

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.google.com/",
		answer("__Secure-STRP=; expires=Thu, 01-Jan-1970 00:00:00 GMT; path=/; Secure"))
	if len(writes) != 1 || writes[0].State != domain.CookieDeleted {
		t.Fatalf("writes = %+v, want the one deletion", writes)
	}
	if writes[0].Previous == nil || writes[0].Previous.Value != "ANmZwa0m" {
		t.Errorf("previous = %+v, want what the deletion took", writes[0].Previous)
	}
	if len(store.all()) != 0 {
		t.Errorf("jar = %+v, want the cookie gone", store.all())
	}
}

// The same line for a cookie the jar never had is a deletion of nothing, and the answer is reported
// as having changed nothing: a row saying "deleted" over a cookie nobody held is a row about
// nothing, and a browser's own jar has no answer to give for it either.
func TestADeletionOfACookieTheJarNeverHadChangesNothing(t *testing.T) {
	cookies, store := newCookies(t)

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.google.com/",
		answer("__Secure-STRP=; expires=Thu, 01-Jan-1970 00:00:00 GMT; path=/; Secure"))
	if len(writes) != 0 {
		t.Errorf("writes = %+v, want none: nothing was there to take away", writes)
	}
	if len(store.all()) != 0 {
		t.Errorf("jar = %+v, want nothing written", store.all())
	}
}

// A line may only name a domain its own host owns. Without this any response could write a cookie
// this app would later send to a host that never asked for it — and the one thing worth showing a
// person is that the answer tried.
func TestADomainTheAnswerDoesNotOwnIsKeptOut(t *testing.T) {
	cookies, store := newCookies(t)

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/articles",
		answer("session=abc; Domain=evil.example.net; Path=/"))
	if len(writes) != 1 || writes[0].State != domain.CookieNotSaved {
		t.Fatalf("writes = %+v, want the one cookie kept out", writes)
	}
	if writes[0].Domain != "evil.example.net" {
		t.Errorf("domain = %q, want the domain the line named", writes[0].Domain)
	}
	if len(store.all()) != 0 {
		t.Errorf("jar = %+v, want nothing written", store.all())
	}
}

func TestADomainCookieLandsOnTheDomainItNames(t *testing.T) {
	for from, line := range map[string]string{
		"https://www.google.com/": "wide=1; Domain=google.com; Path=/",
		"https://api.google.com/": "wide=1; Domain=.google.com; Path=/",
	} {
		cookies, store := newCookies(t)
		cookies.Apply(context.Background(), domain.WorkspacePersonalID, from, answer(line))
		if _, ok := store.get("google.com", "/", "wide"); !ok {
			t.Errorf("%s with %q: jar = %+v, want the cookie on google.com", from, line, store.all())
		}
	}
}

// The answer google.com gives a request that already carries its cookies: a fresh __Secure-STRP,
// then a deletion for every host variant of it, then NID. It reads as one cookie named five times,
// four of them deletions, and what it comes to is two changes: the cookie google.com set and
// cleared is gone, and NID is set.
//
// The four lines that name www.google.com clear a cookie of that host, which the jar never held:
// nothing came of them, so nothing is reported. Chrome, given these six lines through the same
// redirect, ends holding NID alone, which is what the jar is held to here.
func TestTheAnswerThatNamesOneCookieFiveTimes(t *testing.T) {
	cookies, store := newCookies(t)
	store.put(domain.JarCookie{
		ID: "old", Domain: "google.com", Name: "__Secure-STRP", Value: "4a91c2f8", Path: "/",
	})

	lines := answer(
		"__Secure-STRP=ANmZwa2P9Olw; expires=Sat, 19-Sep-2026 16:04:13 GMT; path=/; "+
			"domain=.google.com; Secure; SameSite=strict",
		"__Secure-STRP=; expires=Mon, 01-Jan-1990 00:00:00 GMT; path=/; domain=www.google.com; Secure",
		"__Secure-STRP=; expires=Mon, 01-Jan-1990 00:00:00 GMT; path=/; domain=.www.google.com; Secure",
		"__Secure-STRP=; expires=Mon, 01-Jan-1990 00:00:00 GMT; path=/; domain=google.com; Secure",
		"__Secure-STRP=; expires=Mon, 01-Jan-1990 00:00:00 GMT; path=/; domain=.google.com; Secure",
		"NID=CVIB; expires=Sun, 21-Mar-2027 15:58:58 GMT; path=/; domain=.google.com; HttpOnly")

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
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
	jar, err := store.db.Jar(context.Background(), domain.WorkspacePersonalID)
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
	cookies, store := newCookies(t)
	store.put(domain.JarCookie{
		ID: "old", Domain: "google.com", Name: "__Secure-STRP", Value: "4a91c2f8", Path: "/",
	})

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.google.com/",
		answer(
			"__Secure-STRP=ANmZwa2P9Olw; expires=Sat, 19-Sep-2026 16:04:13 GMT; path=/; "+
				"domain=.google.com; Secure; SameSite=strict",
			"__Secure-STRP=; expires=Mon, 01-Jan-1990 00:00:00 GMT; path=/; domain=.google.com; Secure",
		))
	if err := cookies.Undo(context.Background(), domain.WorkspacePersonalID, writes); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := store.cookie("google.com", "/", "__Secure-STRP").Value; got != "4a91c2f8" {
		t.Errorf("after undo = %q, want the value the answer found", got)
	}
}

// A line that names no domain belongs to the host that answered, alone. A Domain attribute, even the
// host's own, makes a cookie for everything under it (RFC 6265 §5.3 step 6).
func TestALineWithoutADomainBelongsToTheHostAlone(t *testing.T) {
	cookies, store := newCookies(t)

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.example.com/",
		answer("a=1; Path=/", "b=2; Domain=www.example.com; Path=/", "c=3; Domain=.example.com; Path=/"))
	if len(writes) != 3 {
		t.Fatalf("writes = %+v, want three", writes)
	}
	for i, want := range []bool{true, false, false} {
		if writes[i].HostOnly != want {
			t.Errorf("write %d = %+v, want host only = %v", i, writes[i], want)
		}
	}
	if got := store.cookie("www.example.com", "/", "a").HostOnly; !got {
		t.Error("the cookie of a line with no domain is not for that host alone")
	}
	if got := store.cookie("example.com", "/", "c").HostOnly; got {
		t.Error("the cookie of a line naming a domain is for that host alone")
	}
}

// The reach of a cookie is the domain it carries: a row the manager rewrites reaches as far as the
// domain drawn in its field says. Saving a row unchanged leaves the cookie where it was, and a dot
// put in front of the domain in the form widens it — which is the whole of how the manager is told.
func TestTheReachOfACookieIsTheDomainItCarries(t *testing.T) {
	cookies, _ := newCookies(t)
	cookies.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://www.example.com/", answer("a=1; Path=/"))

	levels, err := cookies.Jar(context.Background())
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	if len(levels) != 1 || len(levels[0].Cookies) != 1 || !levels[0].Cookies[0].HostOnly {
		t.Fatalf("jar = %+v, want the host-only cookie it was written as", levels)
	}

	saved := levels[0].Cookies[0]
	if err := cookies.Save(context.Background(), saved); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !hostOnlyIn(t, cookies) {
		t.Errorf("saving a row unchanged moved the cookie it stands for")
	}

	saved = levels[0].Cookies[0]
	saved.Domain = ".example.com"
	if err := cookies.Save(context.Background(), saved); err != nil {
		t.Fatalf("Save: %v", err)
	}
	levels, err = cookies.Jar(context.Background())
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
func hostOnlyIn(t *testing.T, cookies *Cookies) bool {
	t.Helper()
	levels, err := cookies.Jar(context.Background())
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
	cookies, store := newCookies(t)
	if err := store.db.SaveJarDomain(t.Context(), domain.WorkspacePersonalID, "api.example.com", true); err != nil {
		t.Fatal(err)
	}

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("session=abc; Path=/"))
	if len(writes) != 1 || writes[0].State != domain.CookieNotSaved {
		t.Fatalf("writes = %+v, want one cookie not saved", writes)
	}
	if len(store.all()) != 0 {
		t.Errorf("jar = %+v, want nothing taken", store.all())
	}
}

// Taking a write back is the answer's change undone: what it added goes, and what it replaced comes
// back as it was — whole, because the record kept the row and not only its value.
func TestUndoPutsBackWhatTheAnswerChanged(t *testing.T) {
	cookies, store := newCookies(t)
	before := domain.JarCookie{
		ID: "old", Domain: "api.example.com", Name: "session", Value: "first", Path: "/",
		ExpiresAt: at(2030), Secure: true, Origin: domain.CookieFromResponse, WrittenAt: 1,
	}
	store.put(before)

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("session=second; Path=/", "locale=en-US; Path=/"))
	if err := cookies.Undo(context.Background(), domain.WorkspacePersonalID, writes); err != nil {
		t.Fatalf("Undo: %v", err)
	}

	back := store.cookie("api.example.com", "/", "session")
	if !reflect.DeepEqual(back, before) {
		t.Errorf("session = %+v, want the row the write found", back)
	}
	if _, ok := store.get("api.example.com", "/", "locale"); ok {
		t.Errorf("jar = %+v, want the added cookie gone", store.all())
	}
}

// A write is a fact about one moment: a row somebody has changed since is not restored over, or
// taking an answer back would throw away what was written after it.
func TestUndoLeavesARowThatMovedOn(t *testing.T) {
	cookies, store := newCookies(t)
	store.put(domain.JarCookie{
		ID: "old", Domain: "api.example.com", Name: "session", Value: "first", Path: "/",
	})
	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/", answer("session=second; Path=/"))

	// Somebody else writes the cookie after the answer did.
	store.put(domain.JarCookie{
		ID: "old", Domain: "api.example.com", Name: "session", Value: "third", Path: "/",
	})

	if err := cookies.Undo(context.Background(), domain.WorkspacePersonalID, writes); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := store.cookie("api.example.com", "/", "session").Value; got != "third" {
		t.Errorf("session = %q, want the value somebody wrote after the answer", got)
	}
}

// What one request sends: the cookies of its host, as the header carries them, and nothing of
// another host's. The header is the whole of what a request reads from the jar — it has no cookies
// of its own and nothing to say about the ones it has.
func TestTheHeaderIsWhatOneAddressSends(t *testing.T) {
	cookies, store := newCookies(t)
	store.put(domain.JarCookie{
		ID: "c1", Domain: "api.example.com", Name: "session", Value: "abc", Path: "/",
	})
	store.put(domain.JarCookie{
		ID: "c2", Domain: "api.example.com", Name: "locale", Value: "en-US", Path: "/",
	})
	store.put(domain.JarCookie{
		ID: "c3", Domain: "auth.example.com", Name: "token", Value: "t", Path: "/",
	})
	store.put(domain.JarCookie{
		ID: "c4", Domain: "api.example.com", Name: "dead", Value: "x", Path: "/",
		ExpiresAt: at(2020),
	})

	header, err := cookies.CookieHeader(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/articles")
	if err != nil {
		t.Fatalf("CookieHeader: %v", err)
	}
	if header != "locale=en-US; session=abc" {
		t.Errorf("header = %q, want this host's living cookies, in the order they are sent", header)
	}
	// A cookie with no name is not one HTTP can carry, and an address the jar knows nothing about
	// gets no header at all.
	store.put(domain.JarCookie{
		ID: "c5", Domain: "api.example.com", Name: "  ", Value: "x", Path: "/",
	})
	header, err = cookies.CookieHeader(context.Background(), domain.WorkspacePersonalID,
		"https://api.example.com/")
	if err != nil {
		t.Fatalf("CookieHeader: %v", err)
	}
	if header != "locale=en-US; session=abc" {
		t.Errorf("header = %q, want the nameless cookie left out", header)
	}
	header, err = cookies.CookieHeader(context.Background(), domain.WorkspacePersonalID,
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
	cookies, store := newCookies(t)

	err := cookies.Save(context.Background(), domain.JarCookie{
		Domain: ".example.com", Name: "wide", Value: "1",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	wide := store.cookie("example.com", "/", "wide")
	if wide.HostOnly {
		t.Errorf("hostOnly = true, want a dotted domain to reach everything under it")
	}
	if wide.Domain != "example.com" {
		t.Errorf("domain = %q, want the dot trimmed", wide.Domain)
	}
	if !domain.HostMatches(wide.Domain, "www.example.com") {
		t.Errorf("the saved cookie does not reach the host it was told to")
	}

	err = cookies.Save(context.Background(), domain.JarCookie{
		Domain: "API.Example.com", Name: "narrow", Value: "2",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	narrow := store.cookie("api.example.com", "/", "narrow")
	if !narrow.HostOnly {
		t.Errorf("hostOnly = false, want a bare domain to belong to that host alone")
	}
}

// The manager's row is saved whole: a row just typed is given an identity, and one that came from
// the table keeps the identity it was found by, because that is what the window addresses it by. A
// cookie with no name is not one HTTP can carry, and the jar says so rather than keeping a row
// nothing could ever send.
func TestARowOfTheTableIsSavedWhole(t *testing.T) {
	cookies, store := newCookies(t)

	typed := domain.JarCookie{Domain: "api.example.com", Name: "session", Value: "abc"}
	if err := cookies.Save(context.Background(), typed); err != nil {
		t.Fatalf("Save: %v", err)
	}
	saved := store.cookie("api.example.com", "/", "session")
	if saved.ID == "" {
		t.Errorf("id = %q, want one minted for a row that had none", saved.ID)
	}
	if saved.Path != "/" || saved.Origin != domain.CookieByHand {
		t.Errorf("path = %q, origin = %q, want the root and a hand-written cookie",
			saved.Path, saved.Origin)
	}

	known := saved
	known.Value = "def"
	if err := cookies.Save(context.Background(), known); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if store.cookie("api.example.com", "/", "session").ID != saved.ID {
		t.Errorf("id = %q, want the row keeping the identity it was found by", store.cookie("api.example.com", "/", "session").ID)
	}
	if store.cookie("api.example.com", "/", "session").Value != "def" {
		t.Errorf("value = %q, want the row written over",
			store.cookie("api.example.com", "/", "session").Value)
	}

	err := cookies.Save(context.Background(), domain.JarCookie{Domain: "api.example.com", Value: "x"})
	if err == nil {
		t.Errorf("Save: want a nameless cookie refused")
	}
}

// A cookie is kept under a domain, and one with none would be a row the rail has no name for and no
// request could ever match.
func TestACookieWithoutADomainIsRefused(t *testing.T) {
	cookies, store := newCookies(t)

	for _, written := range []string{"", "  ", "."} {
		err := cookies.Save(context.Background(), domain.JarCookie{Domain: written, Name: "session"})
		if domain.CodeOf(err) != domain.CodeDomainEmpty {
			t.Errorf("Save(%q) = %q, want %q", written, domain.CodeOf(err), domain.CodeDomainEmpty)
		}
	}
	if len(store.all()) != 0 {
		t.Errorf("saved = %+v, want nothing written", store.all())
	}
}

// What the manager's footer deletes is the domain on screen, written the way the rail draws it:
// with or without the dot, in any case. The cookies of the other domains stay.
func TestDeletingADomainTakesItsCookiesAndItsFlag(t *testing.T) {
	cookies, store := newCookies(t)
	ctx := context.Background()
	for _, host := range []string{"api.example.com", "other.example.org"} {
		if err := cookies.Save(ctx, domain.JarCookie{Domain: host, Name: "session", Value: "1"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	if err := cookies.SetBlocked(ctx, "api.example.com", true); err != nil {
		t.Fatalf("SetBlocked: %v", err)
	}

	if err := cookies.DeleteDomain(ctx, ".API.example.com"); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}

	if _, kept := store.get("api.example.com", "/", "session"); kept {
		t.Errorf("the cookie of the deleted domain is still in the jar")
	}
	if store.blocked("api.example.com") {
		t.Errorf("the flag of the deleted domain is still set")
	}
	if _, kept := store.get("other.example.org", "/", "session"); !kept {
		t.Errorf("the cookie of another domain went with it")
	}
}

// The table under an answer draws the same cookies the header is built from, and draws them whole:
// a value and the attributes a `Cookie` header has no room for. One list read twice is what keeps a
// person from reading a table of one jar and sending another.
func TestTheRowsOfAnAddressAreWhatItsHeaderIs(t *testing.T) {
	cookies, store := newCookies(t)
	store.put(domain.JarCookie{
		ID: "c1", Domain: "api.example.com", Name: "session", Value: "abc", Path: "/",
		Secure: true, HTTPOnly: true, SameSite: "Lax",
	})
	store.put(domain.JarCookie{
		ID: "c2", Domain: "api.example.com", Name: "narrow", Value: "n", Path: "/v1",
	})
	store.put(domain.JarCookie{
		ID: "c3", Domain: "other.example.net", Name: "elsewhere", Value: "x", Path: "/",
	})
	store.put(domain.JarCookie{
		ID: "c4", Domain: "api.example.com", Name: "dead", Value: "x", Path: "/", ExpiresAt: at(2020),
	})

	const address = "https://api.example.com/v1/articles"
	rows, err := cookies.ForURL(context.Background(), address)
	if err != nil {
		t.Fatalf("ForURL: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "narrow" || rows[1].Name != "session" {
		t.Fatalf("rows = %+v, want this address's two cookies, the narrower path first", rows)
	}
	if !rows[1].Secure || !rows[1].HTTPOnly || rows[1].SameSite != "Lax" {
		t.Errorf("row = %+v, want the attributes the header leaves out", rows[1])
	}

	header, err := cookies.CookieHeader(context.Background(), domain.WorkspacePersonalID, address)
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
	cookies, store := newCookies(t)
	_ = store.db.Close()

	writes := cookies.Apply(context.Background(), domain.WorkspacePersonalID,
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

func apply(cookies *Cookies, from string, lines ...string) []domain.CookieWrite {
	return cookies.Apply(context.Background(), domain.WorkspacePersonalID, from, answer(lines...))
}

func headerTo(t *testing.T, cookies *Cookies, to string) string {
	t.Helper()
	header, err := cookies.CookieHeader(context.Background(), domain.WorkspacePersonalID, to)
	if err != nil {
		t.Fatalf("CookieHeader(%s): %v", to, err)
	}
	return header
}

func stateOf(t *testing.T, writes []domain.CookieWrite, name string) domain.CookieState {
	t.Helper()
	for _, write := range writes {
		if write.Name == name {
			return write.State
		}
	}
	t.Fatalf("writes = %+v, want one for %s", writes, name)
	return ""
}

// A Domain attribute above the host is the host's own domain's cookie, and a public suffix is
// nobody's: a cookie for `com` would be sent to every site there (RFC 6265 §5.3 step 5).
func TestAPublicSuffixIsNotADomainACookieMayNameIt(t *testing.T) {
	cookies, _ := newCookies(t)

	for from, line := range map[string]string{
		"https://evil.example.com/": "tracker=1; Domain=com",
		"https://evil.co.uk/":       "tracker=1; Domain=co.uk",
		"https://evil.github.io/":   "tracker=1; Domain=github.io",
	} {
		if got := stateOf(t, apply(cookies, from, line), "tracker"); got != domain.CookieNotSaved {
			t.Errorf("%s with %q: state = %s, want it refused", from, line, got)
		}
	}
	for _, to := range []string{"https://bank.com/", "https://other.co.uk/", "https://other.github.io/"} {
		if header := headerTo(t, cookies, to); header != "" {
			t.Errorf("to %s = %q, want nothing", to, header)
		}
	}
}

// A host that is itself a suffix (a local name, an intranet one) may still keep a cookie for itself.
func TestAHostThatIsASuffixKeepsACookieToItself(t *testing.T) {
	cookies, store := newCookies(t)

	writes := apply(cookies, "http://localhost:3000/", "a=1; Domain=localhost")
	if got := stateOf(t, writes, "a"); got != domain.CookieAdded {
		t.Fatalf("state = %s, want the cookie kept", got)
	}
	if !store.cookie("localhost", "/", "a").HostOnly {
		t.Error("the cookie of a suffix host reaches beyond that host")
	}
}

func TestADomainAboveThePrivateSuffixIsAccepted(t *testing.T) {
	cookies, _ := newCookies(t)

	apply(cookies, "https://a.example.co.uk/", "ok=1; Domain=example.co.uk")
	if header := headerTo(t, cookies, "https://b.example.co.uk/"); header != "ok=1" {
		t.Errorf("header = %q, want the domain cookie", header)
	}
}

// A cookie with no usable Path belongs to the directory of the request that set it (§5.1.4), not to
// the whole site.
func TestACookieWithoutAPathBelongsToTheDirectoryThatSetIt(t *testing.T) {
	cookies, store := newCookies(t)

	apply(cookies, "https://app.test/api/v1/login", "sid=abc")
	apply(cookies, "https://app.test/login", "root=1")
	apply(cookies, "https://app.test/a/b", "rel=1; Path=relative")

	if _, ok := store.get("app.test", "/api/v1", "sid"); !ok {
		t.Errorf("cookies = %v, want sid kept under /api/v1", store.all())
	}
	if _, ok := store.get("app.test", "/", "root"); !ok {
		t.Errorf("cookies = %v, want root kept under /", store.all())
	}
	if _, ok := store.get("app.test", "/a", "rel"); !ok {
		t.Errorf("cookies = %v, want a relative Path to give way to the directory", store.all())
	}
	if got := headerTo(t, cookies, "https://app.test/api/v1/users"); got != "root=1; sid=abc" && got != "sid=abc; root=1" {
		t.Errorf("header under /api/v1 = %q, want sid and root", got)
	}
	if got := headerTo(t, cookies, "https://app.test/other"); got != "root=1" {
		t.Errorf("header to /other = %q, want root alone", got)
	}
}

func TestTheUndoOfADefaultPathCookieFindsIt(t *testing.T) {
	cookies, store := newCookies(t)

	writes := apply(cookies, "https://app.test/api/v1/login", "sid=abc")
	if err := cookies.Undo(context.Background(), domain.WorkspacePersonalID, writes); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if len(store.all()) != 0 {
		t.Errorf("cookies = %v, want the cookie the answer added taken back", store.all())
	}
}

// An address has no parent: a Domain attribute is accepted from it only when it spells the address,
// and "0.0.1" is not above "10.0.0.1".
func TestAnAddressHasNoDomainAboveIt(t *testing.T) {
	cookies, _ := newCookies(t)

	if got := stateOf(t, apply(cookies, "https://10.0.0.1/", "a=1; Domain=0.0.1"), "a"); got != domain.CookieNotSaved {
		t.Errorf("Domain=0.0.1 from 10.0.0.1: state = %s, want it refused", got)
	}
	if got := stateOf(t, apply(cookies, "https://10.0.0.1/", "b=1; Domain=10.0.0.1"), "b"); got != domain.CookieAdded {
		t.Errorf("Domain=10.0.0.1 from 10.0.0.1: state = %s, want it kept", got)
	}
	if got := headerTo(t, cookies, "https://20.0.0.1/"); got != "" {
		t.Errorf("header to another address = %q, want nothing", got)
	}
	if got := headerTo(t, cookies, "https://10.0.0.1/"); got != "b=1" {
		t.Errorf("header to the address = %q, want b", got)
	}
}

// Either spelling of an international name is the same host (§5.1.2).
func TestAnInternationalNameAndItsASCIIFormShareTheirCookies(t *testing.T) {
	cookies, store := newCookies(t)
	unicode := "https://" + string([]rune{0x43f, 0x440, 0x438, 0x43c, 0x435, 0x440}) + ".xn--p1ai/"

	apply(cookies, unicode, "idn=1")
	if _, ok := store.get("xn--e1afmkfd.xn--p1ai", "/", "idn"); !ok {
		t.Fatalf("cookies = %v, want the cookie under the ASCII form", store.all())
	}
	for _, to := range []string{unicode, "https://xn--e1afmkfd.xn--p1ai/"} {
		if got := headerTo(t, cookies, to); got != "idn=1" {
			t.Errorf("header to %s = %q, want the cookie", to, got)
		}
	}
}

// A Secure cookie is set from a secure connection (rfc6265bis §5.7), and this machine counts as one.
func TestASecureCookieNeedsASecureOrigin(t *testing.T) {
	cookies, _ := newCookies(t)

	if got := stateOf(t, apply(cookies, "http://insecure.test/", "sec=1; Secure"), "sec"); got != domain.CookieNotSaved {
		t.Errorf("Secure over http: state = %s, want it refused", got)
	}
	if got := stateOf(t, apply(cookies, "https://secure.test/", "sec=1; Secure"), "sec"); got != domain.CookieAdded {
		t.Errorf("Secure over https: state = %s, want it kept", got)
	}
	if got := stateOf(t, apply(cookies, "http://localhost:3000/", "dev=1; Secure"), "dev"); got != domain.CookieAdded {
		t.Errorf("Secure over http on this machine: state = %s, want it kept", got)
	}
	if got := headerTo(t, cookies, "http://localhost:3000/x"); got != "dev=1" {
		t.Errorf("header to this machine = %q, want the Secure cookie", got)
	}
	if got := headerTo(t, cookies, "http://secure.test/"); got != "" {
		t.Errorf("header over http = %q, want the Secure cookie withheld", got)
	}
}

// A reserved prefix is a promise about the cookie, and a line that breaks it is refused
// (rfc6265bis §4.1.3).
func TestAPrefixedNameKeepsThePromiseItMakes(t *testing.T) {
	cookies, _ := newCookies(t)
	from := "https://app.test/"

	for line, want := range map[string]domain.CookieState{
		"__Host-a=1; Secure; Path=/":                  domain.CookieAdded,
		"__Host-b=1; Secure; Path=/; Domain=app.test": domain.CookieNotSaved,
		"__Host-c=1; Secure; Path=/app":               domain.CookieNotSaved,
		"__Host-d=1; Path=/":                          domain.CookieNotSaved,
		"__Secure-e=1; Secure; Domain=app.test":       domain.CookieAdded,
		"__Secure-f=1":                                domain.CookieNotSaved,
		"__secure-g=1":                                domain.CookieNotSaved,
	} {
		name, _, _ := strings.Cut(line, "=")
		if got := stateOf(t, apply(cookies, from, line), name); got != want {
			t.Errorf("%q: state = %s, want %s", line, got, want)
		}
	}
}

// The jar keys a domain by the host alone, in ASCII: what the manager opens on is that key, and an
// address with a port, a user or capitals would otherwise open on a domain the jar does not have.
func TestTheDomainOfAnAddressIsItsHostAlone(t *testing.T) {
	cookies, _ := newCookies(t)
	unicode := string([]rune{0x43f, 0x440, 0x438, 0x43c, 0x435, 0x440})

	for address, want := range map[string]string{
		"https://localhost:8443/x?y=1":      "localhost",
		"https://me:secret@API.Example.com": "api.example.com",
		"http://[::1]:3000/":                "::1",
		"https://10.0.0.1:8080/a":           "10.0.0.1",
		"https://" + unicode + ".xn--p1ai/": "xn--e1afmkfd.xn--p1ai",
		"/just/a/path":                      "",
		"":                                  "",
	} {
		if got := cookies.DomainOf(address); got != want {
			t.Errorf("DomainOf(%q) = %q, want %q", address, got, want)
		}
	}
}
