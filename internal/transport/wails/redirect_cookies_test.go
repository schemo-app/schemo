package wails

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"schemo/internal/domain"
	"schemo/internal/infra/httpx"
	"schemo/internal/infra/sqlite"
	"schemo/internal/platform"
	"schemo/internal/transport/desktop"
	"schemo/internal/usecase/cookiejar"
	"schemo/internal/usecase/record"
)

// A redirect is a second request, and the cookies of the first answer are what it carries. This is
// the whole path the app runs — the record's use case asking the engine, the engine asking the jar,
// the jar in the database — and the one thing the pieces cannot show apart: that they are wired to
// each other.
//
// Both hops are on one host on purpose. A redirect to another name would need `Domain=` for both,
// and what is being checked is the order — written before the next hop leaves, not after the whole
// send is over — which a second name would only make harder to see.

// storeOn is a migrated store under a data directory of the test's own: what these tests send
// through, and what they read back.
func storeOn(t *testing.T, dir platform.DataDir) *sqlite.Store {
	t.Helper()

	store, err := sqlite.NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
			return
		}
		if err := removeStoreDirectory(string(dir)); err != nil {
			t.Errorf("remove temporary store: %v", err)
		}
	})
	return store
}

func removeStoreDirectory(dir string) error {
	err := os.RemoveAll(dir)
	// Windows can report an empty directory as nonempty while deleted files
	// are still pending removal. testing.TempDir retries sharing errors, but
	// does not retry ERROR_DIR_NOT_EMPTY. Never retry a directory with live files.
	deadline := time.Now().Add(time.Second)
	for runtime.GOOS == "windows" && errors.Is(err, os.ErrExist) && time.Now().Before(deadline) {
		entries, readErr := os.ReadDir(dir)
		if readErr != nil || len(entries) != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
		err = os.Remove(dir)
	}
	return err
}

func TestARedirectedSendCarriesTheJarBetweenHops(t *testing.T) {
	var carried []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		carried = append(carried, r.Header.Get("Cookie"))
		if r.URL.Path == "/start" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/"})
			http.Redirect(w, r, "/next", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("done"))
	}))
	defer srv.Close()

	dir := platform.DataDir(t.TempDir())
	store := storeOn(t, dir)
	ctx := context.Background()
	if err := desktop.OpenStorage(store, desktop.NewStatus(), dir)(ctx); err != nil {
		t.Fatalf("desktop.OpenStorage: %v", err)
	}

	jar := cookiejar.NewUseCase(store, store, platform.NewIDGen())
	executor := engineExecutor{
		engine:  httpx.NewEngine(httpx.Config{}, platform.NewIDGen()),
		jar:     jar,
		cookies: newCookiePolicy(),
	}
	uc := record.NewUseCase(store, store, jar, executor, &heard{},
		record.RetentionSourceFunc(func(context.Context) (domain.Retention, error) {
			return domain.RetainForever, nil
		}), nil, nil, nil, platform.NewIDGen(), platform.BuildInfo{Name: "Schemo"})

	sent, err := uc.SendAndWait(ctx, domain.WorkspacePersonalID, record.SendInput{
		Method:        http.MethodGet,
		URL:           srv.URL + "/start",
		MaskedURL:     srv.URL + "/start",
		MaskedHeaders: []domain.HeaderPair{},
	})
	if err != nil {
		t.Fatalf("SendAndWait: %v", err)
	}

	if len(carried) != 2 {
		t.Fatalf("hops = %v, want the request and the one it was sent to", carried)
	}
	if carried[0] != "" {
		t.Errorf("first hop carried %q, want nothing: the jar held nothing", carried[0])
	}
	if carried[1] != "session=abc" {
		t.Errorf("second hop carried %q, want what the answer before it set", carried[1])
	}

	// And the record owns up to it: the window draws what this exchange changed in the jar.
	if len(sent.CookieWrites) != 1 || sent.CookieWrites[0].Name != "session" {
		t.Fatalf("recorded writes = %+v, want the cookie the first answer set", sent.CookieWrites)
	}

	// The row is in the database, not only in the answer: a jar is what the next send reads.
	stored, err := jar.Jar(ctx)
	if err != nil {
		t.Fatalf("Jar: %v", err)
	}
	if len(stored) != 1 || len(stored[0].Cookies) != 1 || stored[0].Cookies[0].Value != "abc" {
		t.Errorf("jar = %+v, want the cookie the answer set", stored)
	}
}

// The jar is read for the second hop with its own space in hand: a send that asked it about no
// workspace would find nothing — the jar is keyed by the space, and an empty key is not one.
func TestTheHopsAskTheJarInTheWorkspaceOfTheSend(t *testing.T) {
	var carried []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		carried = append(carried, r.Header.Get("Cookie"))
		if r.URL.Path == "/start" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/"})
			http.Redirect(w, r, "/next", http.StatusFound)
			return
		}
	}))
	defer srv.Close()

	dir := platform.DataDir(t.TempDir())
	store := storeOn(t, dir)
	ctx := context.Background()
	if err := desktop.OpenStorage(store, desktop.NewStatus(), dir)(ctx); err != nil {
		t.Fatalf("desktop.OpenStorage: %v", err)
	}

	// A cookie already in the jar of the space the send will be filed in.
	if err := store.SaveJarCookie(ctx, domain.WorkspacePersonalID, domain.JarCookie{
		ID: "c1", Domain: domain.HostOf(srv.URL), Name: "kept", Value: "1", Path: "/",
		Origin: domain.CookieByHand, WrittenAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("SaveJarCookie: %v", err)
	}

	jar := cookiejar.NewUseCase(store, store, platform.NewIDGen())
	executor := engineExecutor{
		engine:  httpx.NewEngine(httpx.Config{}, platform.NewIDGen()),
		jar:     jar,
		cookies: newCookiePolicy(),
	}
	uc := record.NewUseCase(store, store, jar, executor, &heard{},
		record.RetentionSourceFunc(func(context.Context) (domain.Retention, error) {
			return domain.RetainForever, nil
		}), nil, nil, nil, platform.NewIDGen(), platform.BuildInfo{Name: "Schemo"})

	if _, err := uc.SendAndWait(ctx, domain.WorkspacePersonalID, record.SendInput{
		Method:        http.MethodGet,
		URL:           srv.URL + "/start",
		MaskedURL:     srv.URL + "/start",
		MaskedHeaders: []domain.HeaderPair{},
	}); err != nil {
		t.Fatalf("SendAndWait: %v", err)
	}

	if len(carried) != 2 || carried[1] != "kept=1; session=abc" {
		t.Errorf("second hop carried %q, want the jar of this space, its own cookie first",
			carried[len(carried)-1])
	}
}
