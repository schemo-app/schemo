package db

import (
	"context"
	"errors"
	"testing"

	"schemo/internal/domain"
)

// saveJar is the store's one write with the workspace every test below works in, so that a call is
// short enough to read on one line.
func saveJar(t *testing.T, store *Store, cookie domain.JarCookie) {
	t.Helper()
	if err := store.SaveJarCookie(context.Background(), ws, cookie); err != nil {
		t.Fatalf("SaveJarCookie: %v", err)
	}
}

// jarCookie is one cookie of the tests below: everything the manager can write and read back.
func jarCookie(id, host, name, value string) domain.JarCookie {
	return domain.JarCookie{
		ID: id, Domain: host, Name: name, Value: value, Path: "/",
		Secure: true, HTTPOnly: true, SameSite: "Lax",
		Origin: domain.CookieFromResponse, WrittenAt: 1_700_000_000_000,
	}
}

// What the manager draws: the domains with their cookies, and the flag that says whether a response
// may write there at all.
func TestJarComesBackByDomain(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	saveJar(t, store, jarCookie("c1", "api.example.com", "session", "abc"))
	saveJar(t, store, jarCookie("c2", "auth.example.com", "token", "t"))
	if err := store.SaveJarDomain(ctx, ws, "auth.example.com", true); err != nil {
		t.Fatalf("SaveJarDomain: %v", err)
	}

	jar, err := store.Jar(ctx, ws)
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	if len(jar) != 2 {
		t.Fatalf("jar = %+v, want the two domains", jar)
	}
	if jar[0].Domain != "api.example.com" || jar[1].Domain != "auth.example.com" {
		t.Errorf("jar = %+v, want the domains in order", jar)
	}
	if jar[0].Blocked || !jar[1].Blocked {
		t.Errorf("flags = %v/%v, want only the second domain blocked", jar[0].Blocked, jar[1].Blocked)
	}
	if len(jar[0].Cookies) != 1 || jar[0].Cookies[0].Value != "abc" {
		t.Errorf("cookies = %+v, want the one saved", jar[0].Cookies)
	}
	if !jar[0].Cookies[0].HTTPOnly || jar[0].Cookies[0].Origin != domain.CookieFromResponse {
		t.Errorf("cookie = %+v, want what was written", jar[0].Cookies[0])
	}
}

// A cookie is found by what HTTP calls it — host, path and name — which is what a response asks
// before it writes: what it is about to replace.
func TestACookieIsFoundByIdentity(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	saveJar(t, store, jarCookie("c1", "api.example.com", "session", "abc"))

	found, err := store.JarCookie(ctx, ws, "api.example.com", "/", "session")
	if err != nil {
		t.Fatalf("JarCookie: %v", err)
	}
	if found.ID != "c1" || found.Value != "abc" {
		t.Errorf("found = %+v, want the cookie that was saved", found)
	}
	_, err = store.JarCookie(ctx, ws, "api.example.com", "/", "other")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a name nobody set = %v, want ErrNotFound", err)
	}
}

// The manager edits a cookie that is already there, and an edit can move it: a row is addressed by
// id, so renaming one moves the row it is rather than leaving a second one behind.
func TestRenamingACookieMovesItsRow(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	saveJar(t, store, jarCookie("c1", "api.example.com", "session", "abc"))
	moved := jarCookie("c1", "api.example.com", "session_id", "abc")
	moved.Path = "/v1"
	if err := store.SaveJarCookie(ctx, ws, moved); err != nil {
		t.Fatalf("SaveJarCookie after a rename: %v", err)
	}

	jar, err := store.Jar(ctx, ws)
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	if len(jar) != 1 || len(jar[0].Cookies) != 1 {
		t.Fatalf("jar = %+v, want the one row", jar)
	}
	if got := jar[0].Cookies[0]; got.Name != "session_id" || got.Path != "/v1" {
		t.Errorf("cookie = %+v, want it where the edit moved it", got)
	}
}

// How far a cookie reaches is stored with it, because it is part of what the cookie is: a jar read
// back tomorrow must send what it sent today, and a cookie for one host is not a cookie for the
// domain under its name.
func TestTheReachOfACookieIsStoredWithIt(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	host := jarCookie("c1", "www.example.com", "session", "abc")
	host.HostOnly = true
	saveJar(t, store, host)
	saveJar(t, store, jarCookie("c2", "example.com", "locale", "en"))

	jar, err := store.Jar(ctx, ws)
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	if len(jar) != 2 {
		t.Fatalf("jar = %+v, want the two cookies", jar)
	}
	for _, level := range jar {
		want := level.Domain == "www.example.com"
		if level.Cookies[0].HostOnly != want {
			t.Errorf("%s = %+v, want host only = %v", level.Domain, level.Cookies[0], want)
		}
	}
	// The lookup by identity — what an answer asks before it replaces a cookie — answers it too.
	found, err := store.JarCookie(ctx, ws, "www.example.com", "/", "session")
	if err != nil {
		t.Fatalf("JarCookie: %v", err)
	}
	if !found.HostOnly {
		t.Errorf("identity lookup = %+v, want the reach it was stored with", found)
	}
}

// The one collision the jar refuses: two cookies with the same name on the same host and path are
// one cookie, and moving a row onto one that is already there is not something to guess about.
func TestMovingARowOntoAnotherCookieIsAConflict(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	saveJar(t, store, jarCookie("c1", "api.example.com", "session", "abc"))
	second := jarCookie("c2", "api.example.com", "locale", "en")
	saveJar(t, store, second)

	err := store.SaveJarCookie(ctx, ws, jarCookie("c2", "api.example.com", "session", "en"))
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("moving a row onto another cookie = %v, want ErrConflict", err)
	}
}

// A domain goes with its flag and with nothing else: the cookies of the others, and their flags,
// are still in the jar.
func TestDeletingADomainLeavesTheOthers(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	saveJar(t, store, jarCookie("c1", "api.example.com", "session", "abc"))
	saveJar(t, store, jarCookie("c2", "other.example.org", "session", "def"))
	for _, host := range []string{"api.example.com", "other.example.org"} {
		if err := store.SaveJarDomain(ctx, ws, host, true); err != nil {
			t.Fatalf("SaveJarDomain: %v", err)
		}
	}
	if err := store.DeleteJarDomain(ctx, ws, "api.example.com"); err != nil {
		t.Fatalf("DeleteJarDomain: %v", err)
	}

	jar, err := store.Jar(ctx, ws)
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	if len(jar) != 1 || jar[0].Domain != "other.example.org" || len(jar[0].Cookies) != 1 {
		t.Fatalf("jar = %+v, want only the other domain with its cookie", jar)
	}
	if blocked, err := store.JarDomain(ctx, ws, "api.example.com"); err != nil || blocked {
		t.Errorf("flag = %v (%v), want it gone with the domain", blocked, err)
	}
	if blocked, err := store.JarDomain(ctx, ws, "other.example.org"); err != nil || !blocked {
		t.Errorf("flag of the other domain = %v (%v), want it kept", blocked, err)
	}
}

// A jar belongs to a workspace: another one's cookies are not this one's to send, and not this one's to
// see.
func TestAJarIsOneWorkspace(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()
	made := domain.Workspace{
		ID: "w2", Name: "Other", Kind: domain.WorkspaceTeam, CreatedAt: 1, UpdatedAt: 1,
	}
	if err := store.SaveWorkspace(ctx, made); err != nil {
		t.Fatalf("SaveWorkspace: %v", err)
	}

	saveJar(t, store, jarCookie("c1", "api.example.com", "session", "abc"))
	// The same host and the same name in another workspace: a jar is the workspace's, so the two do not meet.
	other := jarCookie("c2", "api.example.com", "session", "other")
	if err := store.SaveJarCookie(ctx, "w2", other); err != nil {
		t.Fatalf("SaveJarCookie: %v", err)
	}

	jar, err := store.Jar(ctx, ws)
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	if len(jar) != 1 || jar[0].Cookies[0].Value != "abc" {
		t.Errorf("jar = %+v, want only this workspace's cookie", jar)
	}
}

// A deleted cookie leaves the jar, and its name is free again: the pair of things that has to hold
// for a response setting the same cookie later to be a new row rather than a row nobody can see.
func TestADeletedCookieFreesItsName(t *testing.T) {
	store := newMigratedStore(t)
	ctx := context.Background()

	saveJar(t, store, jarCookie("c1", "api.example.com", "session", "abc"))
	if err := store.DeleteJarCookie(ctx, ws, "c1"); err != nil {
		t.Fatalf("DeleteJarCookie: %v", err)
	}

	jar, err := store.Jar(ctx, ws)
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	if len(jar) != 0 {
		t.Errorf("the jar still holds %+v", jar)
	}
	// The identity HTTP gives the cookie no longer names anything, so a response setting it again
	// writes a row of its own instead of inheriting one that is gone.
	_, err = store.JarCookie(ctx, ws, "api.example.com", "/", "session")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a deleted cookie = %v, want not found", err)
	}
	saveJar(t, store, jarCookie("c2", "api.example.com", "session", "def"))

	var marked, living int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM entities WHERE kind = 'cookie' AND deleted_at IS NOT NULL`,
	).Scan(&marked); err != nil {
		t.Fatalf("counting the marked cookies: %v", err)
	}
	if err := store.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM entities WHERE kind = 'cookie' AND deleted_at IS NULL`,
	).Scan(&living); err != nil {
		t.Fatalf("counting the living cookies: %v", err)
	}
	if marked != 1 || living != 1 {
		t.Errorf("marked = %d, living = %d, want one of each", marked, living)
	}
}
