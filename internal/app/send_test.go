package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"schemo/internal/domain"
	"schemo/internal/request"
)

// send sends a draft and waits for the record it leaves.
func send(t *testing.T, device *device, draft domain.Draft) domain.Record {
	t.Helper()
	before := device.events.count(request.TopicRequestFinished)
	if _, err := device.Drafts.Send(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	device.eventually("record", func() bool { return device.events.count(request.TopicRequestFinished) > before })
	return device.events.last(request.TopicRequestFinished).(request.RequestFinished).Record
}

func TestAnEditedDraftReachesTheServerWhole(t *testing.T) {
	seen := make(chan *http.Request, 1)
	bodies := make(chan string, 1)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen <- r
		bodies <- string(body)
	}))
	defer api.Close()
	device := newDevice(t)

	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		draft := domain.NewDraft()
		draft.ID = "tab:" + domain.DraftID(method)
		draft.Method, draft.URL = method, api.URL+"/submit?version=2"
		draft.Body, draft.BodyKind = "payload", domain.BodyJSON
		draft.Auth = domain.Auth{Type: domain.AuthBearer, Fields: map[string]string{"token": "secret"}}
		draft.Headers = []domain.Row{{ID: "h", Name: "X-Fixture", Value: "after", Enabled: true}}

		record := send(t, device, draft)
		if record.Status != http.StatusOK || record.Method != method {
			t.Errorf("%s record = status %d, method %s", method, record.Status, record.Method)
		}
		r, body := <-seen, <-bodies
		if r.Method != method || body != "payload" || r.Header.Get("Authorization") != "Bearer secret" ||
			r.Header.Get("Content-Type") != domain.MediaJSON || r.Header.Get("X-Fixture") != "after" ||
			r.URL.Query().Get("version") != "2" {
			t.Errorf("%s reached the server as %s %s %q %v", method, r.Method, r.URL, body, r.Header)
		}
	}
}

// A redirect is a second request, and it carries the workspace's jar plus what the first answer set.
func TestARedirectCarriesTheJarBetweenHops(t *testing.T) {
	var carried []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		carried = append(carried, r.Header.Get("Cookie"))
		if r.URL.Path == "/start" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/"})
			http.Redirect(w, r, "/next", http.StatusFound)
		}
	}))
	defer api.Close()
	device := newDevice(t)
	ctx := t.Context()
	if err := device.Cookies.Save(ctx, domain.JarCookie{Domain: domain.HostOf(api.URL), Name: "kept", Value: "1"}); err != nil {
		t.Fatal(err)
	}

	draft := domain.NewDraft()
	draft.URL = api.URL + "/start"
	record := send(t, device, draft)

	if len(carried) != 2 || carried[0] != "kept=1" || carried[1] != "kept=1; session=abc" {
		t.Fatalf("hops carried %q, want the jar, then the jar and the new cookie", carried)
	}
	if len(record.CookieWrites) != 1 || record.CookieWrites[0].Name != "session" {
		t.Errorf("record writes = %+v, want the cookie the first answer set", record.CookieWrites)
	}
	jar, err := device.Cookies.ForURL(ctx, api.URL)
	if err != nil || len(jar) != 2 {
		t.Errorf("jar = %+v, %v; want both cookies kept", jar, err)
	}
}
