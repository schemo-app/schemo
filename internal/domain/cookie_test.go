package domain

import (
	"testing"
	"time"
)

// The whole of one cookie, as the table draws it: the pair, then the attributes a surface can be
// asked for.
func TestOneSetCookieLineBecomesOneCookie(t *testing.T) {
	headers := []HeaderPair{
		{Name: "Content-Type", Value: "text/html"},
		{Name: "Set-Cookie", Value: "session_id=9f3c1b7e; Domain=api.example.com; Path=/v1; " +
			"Expires=Wed, 31 Dec 2026 10:00:00 GMT; Secure; HttpOnly"},
		{Name: "ETag", Value: "abc"},
	}

	cookies := ParseResponseCookies(headers, time.UnixMilli(0))
	if len(cookies) != 1 {
		t.Fatalf("cookies = %+v, want the one Set-Cookie row", cookies)
	}
	got := cookies[0]
	want := ResponseCookie{
		Name:      "session_id",
		Value:     "9f3c1b7e",
		Domain:    "api.example.com",
		Path:      "/v1",
		ExpiresAt: at("2026-12-31T10:00:00Z"),
		Secure:    true,
		HTTPOnly:  true,
	}
	if !sameCookie(got, want) {
		t.Errorf("cookie = %+v, want %+v", got, want)
	}
}

// sameCookie is one parsed cookie against another by value. A struct holding a pointer compares its
// identity rather than what it names, and every test here is one whole cookie against another.
func sameCookie(a, b ResponseCookie) bool {
	if (a.ExpiresAt == nil) != (b.ExpiresAt == nil) {
		return false
	}
	if a.ExpiresAt != nil && *a.ExpiresAt != *b.ExpiresAt {
		return false
	}
	a.ExpiresAt, b.ExpiresAt = nil, nil
	return a == b
}

// at is the instant an expiry is compared against.
func at(stamp string) *int64 {
	when, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		panic(err)
	}
	millis := when.UnixMilli()
	return &millis
}

// Every answer line is a cookie of its own. A map would keep the first and drop the rest, which is
// the bug the headers are a sequence for in the first place.
func TestEverySetCookieRowSurvives(t *testing.T) {
	headers := []HeaderPair{
		{Name: "Set-Cookie", Value: "a=1"},
		{Name: "set-cookie", Value: "b=2"},
		{Name: "Set-Cookie", Value: "c=3"},
	}

	cookies := ParseResponseCookies(headers, time.UnixMilli(0))
	if len(cookies) != 3 {
		t.Fatalf("cookies = %+v, want all three", cookies)
	}
	for i, want := range []string{"a", "b", "c"} {
		if cookies[i].Name != want {
			t.Errorf("cookie %d = %q, want %q", i, cookies[i].Name, want)
		}
	}
}

// The dates servers actually write. `Expires` is not written the way HTTP/1.1 says: dashes stand
// where RFC 1123 wants spaces, the pieces come in whatever order, and a two-digit year is as common
// as a four-digit one. This is the whole list of shapes worth reading, and a date none of them
// matches stays unread — a session cookie, which is what a line that said nothing gives too.
func TestACookieDateIsReadTheWayServersWriteIt(t *testing.T) {
	cases := []struct {
		given string
		want  string
	}{
		// What Google sends: dashes, four-digit year.
		{"Sat, 19-Sep-2026 15:47:37 GMT", "2026-09-19T15:47:37Z"},
		{"Thu, 18-Mar-2027 15:44:02 GMT", "2027-03-18T15:44:02Z"},
		// The spelling the RFC prescribes.
		{"Wed, 31 Dec 2026 10:00:00 GMT", "2026-12-31T10:00:00Z"},
		// The two-digit year: 70–99 is the last century, 00–69 is this one.
		{"Wed, 18-Mar-27 10:00:00 GMT", "2027-03-18T10:00:00Z"},
		{"Wed, 18-Mar-70 10:00:00 GMT", "1970-03-18T10:00:00Z"},
		// The epoch, which is how a server asks for a cookie to be taken away.
		{"Thu, 01-Jan-1970 00:00:00 GMT", "1970-01-01T00:00:00Z"},
		// No weekday, no zone, a time written without its seconds.
		{"18-Mar-2027 10:00", "2027-03-18T10:00:00Z"},
		{"Sun, 06 Nov 1994 08:49:37 GMT", "1994-11-06T08:49:37Z"},
		// A date with no time is a date at midnight, not a line that said nothing.
		{"Wed, 31 Dec 2026", "2026-12-31T00:00:00Z"},
		// Nothing readable: a session cookie rather than a cookie dated to the beginning of time.
		{"", ""},
		{"not a date at all", ""},
		{"Wed, 31 Dec", ""},
	}

	for _, c := range cases {
		got := cookieTime(c.given)
		if c.want == "" {
			if got != nil {
				t.Errorf("cookieTime(%q) = %s, want nothing", c.given, timeAt(*got))
			}
			continue
		}
		if got == nil {
			t.Errorf("cookieTime(%q) = nothing, want %s", c.given, c.want)
			continue
		}
		if *got != *at(c.want) {
			t.Errorf("cookieTime(%q) = %s, want %s", c.given, timeAt(*got), c.want)
		}
	}
}

// timeAt is an instant as a test failure writes it: the same spelling the cases above are given in.
func timeAt(millis int64) string {
	return time.UnixMilli(millis).UTC().Format(time.RFC3339)
}

// A date in the past is how a server takes a cookie away: the line carries no value and the date it
// names has already been. Read as a live cookie instead — which is what a date nobody can parse
// turns it into — the jar would send `name=` to that host for the rest of the session.
func TestADeletionIsWrittenWithADateInThePast(t *testing.T) {
	headers := []HeaderPair{
		{Name: "Set-Cookie", Value: "__Secure-STRP=; expires=Thu, 01-Jan-1970 00:00:00 GMT; " +
			"path=/; domain=.google.com; Secure"},
	}

	cookies := ParseResponseCookies(headers, time.UnixMilli(0))
	if len(cookies) != 1 {
		t.Fatalf("cookies = %+v, want the one line", cookies)
	}
	if cookies[0].ExpiresAt == nil {
		t.Fatalf("cookie = %+v, want the date it dies at", cookies[0])
	}
	if *cookies[0].ExpiresAt > time.UnixMilli(0).UnixMilli() {
		t.Errorf("expires at %s, want an instant already past", timeAt(*cookies[0].ExpiresAt))
	}
}

// One word, three spellings: servers write `strict`, `Lax` and `NONE` for the same attribute, and a
// list that draws each as it came reads as three different things.
func TestSameSiteIsReadInOneSpelling(t *testing.T) {
	for given, want := range map[string]string{
		"strict": "Strict", "Lax": "Lax", "NONE": "None", " lax ": "Lax", "": "",
	} {
		headers := []HeaderPair{{Name: "Set-Cookie", Value: "a=1; SameSite=" + given}}
		cookies := ParseResponseCookies(headers, time.UnixMilli(0))
		if len(cookies) != 1 || cookies[0].SameSite != want {
			t.Errorf("SameSite=%q gave %q, want %q", given, cookies[0].SameSite, want)
		}
	}
}

// The date a `Max-Age` comes to is the answer's own clock, not the reader's: a record opened a week
// later must not push the cookie's death a week out.
func TestMaxAgeIsDatedFromTheAnswer(t *testing.T) {
	arrived := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	headers := []HeaderPair{{Name: "Set-Cookie", Value: "nid=42; Max-Age=86400"}}

	cookies := ParseResponseCookies(headers, arrived)
	if len(cookies) != 1 || !sameCookie(cookies[0], ResponseCookie{
		Name: "nid", Value: "42", ExpiresAt: at("2026-03-02T12:00:00Z"),
	}) {
		t.Errorf("cookies = %+v, want one dying a day after the answer arrived", cookies)
	}
}

// A negative `Max-Age` deletes the cookie however large it is: the product with a second must not
// wrap around into a date in the future.
func TestAHugeNegativeMaxAgeStillDeletes(t *testing.T) {
	arrived := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	for _, age := range []string{"-1", "-10000000000", "-9223372036854775808"} {
		headers := []HeaderPair{{Name: "Set-Cookie", Value: "nid=; Max-Age=" + age}}

		cookies := ParseResponseCookies(headers, arrived)
		if len(cookies) != 1 || cookies[0].ExpiresAt == nil || *cookies[0].ExpiresAt > arrived.UnixMilli() {
			t.Errorf("Max-Age=%s gave %+v, want a date no later than the answer", age, cookies)
		}
	}
}

// `Max-Age` is what a browser obeys, so where a line carries both it is the one that decides.
func TestMaxAgeOutranksExpires(t *testing.T) {
	arrived := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	headers := []HeaderPair{
		{Name: "Set-Cookie", Value: "nid=42; Expires=Wed, 31 Dec 2031 10:00:00 GMT; Max-Age=60"},
	}

	cookies := ParseResponseCookies(headers, arrived)
	if len(cookies) != 1 || cookies[0].ExpiresAt == nil ||
		*cookies[0].ExpiresAt != arrived.Add(time.Minute).UnixMilli() {
		t.Errorf("cookies = %+v, want the Max-Age minute", cookies)
	}
}

// How far a cookie reaches is part of what it is: one that belongs to a single host is not sent to
// anything under it, and one that belongs to a domain is. A browser keeps the two apart, and a jar
// that does not would send www.example.com's cookie to api.example.com, which never set it.
func TestAHostOnlyCookieReachesOneHost(t *testing.T) {
	all := []JarCookie{
		{ID: "host", Domain: "www.example.com", Name: "host_only", HostOnly: true, Path: "/"},
		{ID: "wide", Domain: "example.com", Name: "wide", Path: "/"},
	}

	got := namesOf(JarCookiesFor(all, "https://www.example.com/x", time.UnixMilli(0)))
	if len(got) != 2 {
		t.Errorf("to www.example.com = %v, want both cookies", got)
	}
	got = namesOf(JarCookiesFor(all, "https://api.example.com/x", time.UnixMilli(0)))
	if len(got) != 1 || got[0] != "wide" {
		t.Errorf("to api.example.com = %v, want the domain cookie alone", got)
	}
	got = namesOf(JarCookiesFor(all, "https://example.com/x", time.UnixMilli(0)))
	if len(got) != 1 || got[0] != "wide" {
		t.Errorf("to example.com = %v, want the domain cookie alone", got)
	}
}

// Max-Age is digits with an optional minus (RFC 6265 §5.2.2); a plus sign or a fraction makes it an
// attribute to ignore, and a number too large for an int64 still points the way it was going.
func TestMaxAgeIsReadByTheGrammarOfTheRFC(t *testing.T) {
	arrived := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	for value, want := range map[string]string{
		"60":                      "future",
		"-60":                     "past",
		"+60":                     "ignored",
		"6.5":                     "ignored",
		"":                        "ignored",
		"-":                       "ignored",
		"99999999999999999999999": "future",
		"-9999999999999999999999": "past",
	} {
		headers := []HeaderPair{{Name: "Set-Cookie", Value: "a=1; Max-Age=" + value}}
		cookies := ParseResponseCookies(headers, arrived)
		if len(cookies) != 1 {
			t.Fatalf("Max-Age=%q: cookies = %+v", value, cookies)
		}
		got := "ignored"
		if at := cookies[0].ExpiresAt; at != nil && *at > arrived.UnixMilli() {
			got = "future"
		} else if at != nil {
			got = "past"
		}
		if got != want {
			t.Errorf("Max-Age=%q came to %s, want %s", value, got, want)
		}
	}
}

func TestCookiePathIsTheAttributeOrTheDirectoryOfTheRequest(t *testing.T) {
	for _, c := range []struct{ attribute, request, want string }{
		{"/app", "/x/y", "/app"},
		{"", "/api/v1/login", "/api/v1"},
		{"relative", "/api/v1/login", "/api/v1"},
		{"", "/login", "/"},
		{"", "/", "/"},
		{"", "", "/"},
		{"", "login", "/"},
		{"", "/a/b/", "/a/b"},
	} {
		if got := CookiePath(c.attribute, c.request); got != c.want {
			t.Errorf("CookiePath(%q, %q) = %q, want %q", c.attribute, c.request, got, c.want)
		}
	}
}

// namesOf is the cookies of a list by name, in the order they came.
func namesOf(cookies []JarCookie) []string {
	out := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		out = append(out, cookie.Name)
	}
	return out
}

// A cookie with no life of its own is a session cookie, and the table says so by leaving the date
// empty — the row draws its own word for that.
func TestACookieWithNoLifeHasNoDate(t *testing.T) {
	headers := []HeaderPair{{Name: "Set-Cookie", Value: "locale=en-US; Path=/"}}

	cookies := ParseResponseCookies(headers, time.UnixMilli(0))
	if len(cookies) != 1 || cookies[0].ExpiresAt != nil || cookies[0].Path != "/" {
		t.Errorf("cookies = %+v, want a session cookie with its path", cookies)
	}
}

// A line that names no cookie is not a cookie: there is no name to put in the row, and an empty row
// is worse than none.
func TestALineNamingNoCookieIsDropped(t *testing.T) {
	headers := []HeaderPair{
		{Name: "Set-Cookie", Value: "no-equals-sign; Path=/"},
		{Name: "Set-Cookie", Value: "=nameless"},
	}

	if cookies := ParseResponseCookies(headers, time.UnixMilli(0)); len(cookies) != 0 {
		t.Errorf("cookies = %+v, want none: neither line names one", cookies)
	}
	if cookies := ParseResponseCookies(nil, time.UnixMilli(0)); len(cookies) != 0 {
		t.Errorf("cookies = %+v, want none from an answer with no lines", cookies)
	}
}

// What a request sends. A domain cookie reaches its hosts, and nothing that merely ends
// the same way — the difference between example.com and notexample.com is one dot.
func TestADomainCookieReachesItsHostsAndNoLookalike(t *testing.T) {
	jar := []JarCookie{{ID: "1", Domain: ".example.com", Name: "a", Path: "/"}}

	if sent := send(t, jar, "https://api.example.com/v1"); len(sent) != 1 {
		t.Errorf("sent = %+v, want the domain cookie on a host of it", sent)
	}
	if sent := send(t, jar, "https://notexample.com/"); len(sent) != 0 {
		t.Errorf("sent = %+v, want nothing on a host that is not one", sent)
	}
}

// A path is a corner of a site, and its neighbour is not in it: `/foo` is a prefix of `/foobar` and
// of nothing else that matters.
func TestAPathCookieStopsAtASeparator(t *testing.T) {
	jar := []JarCookie{{ID: "1", Domain: "api.example.com", Name: "a", Path: "/v1"}}

	if sent := send(t, jar, "https://api.example.com/v1/articles"); len(sent) != 1 {
		t.Errorf("sent = %+v, want the cookie on a path inside it", sent)
	}
	if sent := send(t, jar, "https://api.example.com/v1beta"); len(sent) != 0 {
		t.Errorf("sent = %+v, want nothing on a path that only starts the same", sent)
	}
}

// Two of a body: one dead, one with no life of its own. Only the session cookie travels.
func TestAnExpiredCookieIsNotSent(t *testing.T) {
	jar := []JarCookie{
		{ID: "dead", Domain: "api.example.com", Name: "old", Path: "/",
			ExpiresAt: at("2020-01-01T00:00:00Z")},
		{ID: "live", Domain: "api.example.com", Name: "now", Path: "/"},
	}

	sent := send(t, jar, "https://api.example.com/")
	if len(sent) != 1 || sent[0].Name != "now" {
		t.Errorf("sent = %+v, want the session cookie alone", sent)
	}
}

// The order is the browser's: the narrower path first, so a server reading the first of two cookies
// with one name gets the one written for the corner it is being asked about.
func TestTheNarrowerPathComesFirst(t *testing.T) {
	jar := []JarCookie{
		{ID: "1", Domain: "api.example.com", Name: "id", Path: "/"},
		{ID: "2", Domain: "api.example.com", Name: "id", Path: "/v1/articles"},
	}

	sent := send(t, jar, "https://api.example.com/v1/articles")
	if len(sent) != 2 || sent[0].ID != "2" {
		t.Errorf("sent = %+v, want the longer path first", sent)
	}
}

// send is one request's cookies: the jar and an address. The clock is a fixed day, so that what the
// jar's expiries mean is decided by the test and not by today.
func send(t *testing.T, jar []JarCookie, address string) []JarCookie {
	t.Helper()
	return JarCookiesFor(jar, address, time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC))
}
