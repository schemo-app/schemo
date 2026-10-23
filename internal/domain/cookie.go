package domain

import (
	"cmp"
	"errors"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

type ResponseCookie struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Domain    string `json:"domain,omitempty"`
	Path      string `json:"path,omitempty"`
	ExpiresAt *int64 `json:"expiresAt,omitempty"`
	Secure    bool   `json:"secure,omitempty"`
	HTTPOnly  bool   `json:"httpOnly,omitempty"`
	SameSite  string `json:"sameSite,omitempty"`
}

type CookieOrigin string

const (
	CookieFromResponse CookieOrigin = "response"
	CookieByHand       CookieOrigin = "manual"
)

type JarCookie struct {
	ExpiresAt *int64       `json:"expiresAt,omitempty"`
	ID        string       `json:"id"`
	Domain    string       `json:"domain"`
	Name      string       `json:"name"`
	Value     string       `json:"value"`
	Path      string       `json:"path"`
	SameSite  string       `json:"sameSite,omitempty"`
	Origin    CookieOrigin `json:"origin"`
	WrittenAt int64        `json:"writtenAt"`
	Secure    bool         `json:"secure,omitempty"`
	HTTPOnly  bool         `json:"httpOnly,omitempty"`
	HostOnly  bool         `json:"hostOnly,omitempty"`
}

type CookieState string

const (
	CookieAdded    CookieState = "added"
	CookieReplaced CookieState = "replaced"
	CookieDeleted  CookieState = "deleted"
	CookieNotSaved CookieState = "notSaved"
)

type CookieWrite struct {
	ExpiresAt        *int64      `json:"expiresAt,omitempty"`
	Previous         *JarCookie  `json:"previous,omitempty"`
	Name             string      `json:"name"`
	Value            string      `json:"value"`
	Domain           string      `json:"domain,omitempty"`
	Path             string      `json:"path,omitempty"`
	SameSite         string      `json:"sameSite,omitempty"`
	State            CookieState `json:"state"`
	Secure           bool        `json:"secure,omitempty"`
	HTTPOnly         bool        `json:"httpOnly,omitempty"`
	HostOnly         bool        `json:"hostOnly,omitempty"`
	Withheld         bool        `json:"withheld,omitempty"`
	PreviousWithheld bool        `json:"previousWithheld,omitempty"`
}

type JarDomain struct {
	Domain    string      `json:"domain"`
	Blocked   bool        `json:"blocked"`
	Cookies   []JarCookie `json:"cookies"`
	WrittenAt int64       `json:"writtenAt"`
}

const maxCookieAge = int64(100 * 365 * 24 * 60 * 60)

func ParseResponseCookies(headers []HeaderPair, at time.Time) []ResponseCookie {
	out := []ResponseCookie{}
	for _, header := range headers {
		if !strings.EqualFold(header.Name, "Set-Cookie") {
			continue
		}
		cookie, ok := parseSetCookie(header.Value, at)
		if !ok {
			continue
		}
		out = append(out, cookie)
	}
	return out
}

func parseSetCookie(line string, at time.Time) (ResponseCookie, bool) {
	pair, attributes, _ := strings.Cut(line, ";")
	name, value, ok := strings.Cut(pair, "=")
	if !ok || strings.TrimSpace(name) == "" {
		return ResponseCookie{}, false
	}

	cookie := ResponseCookie{Name: strings.TrimSpace(name), Value: strings.TrimSpace(value)}
	maxAge := ""
	for _, attribute := range strings.Split(attributes, ";") {
		key, value, _ := strings.Cut(strings.TrimSpace(attribute), "=")
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "domain":
			cookie.Domain = strings.TrimSpace(value)
		case "path":
			cookie.Path = strings.TrimSpace(value)
		case "expires":
			cookie.ExpiresAt = cookieTime(value)
		case "max-age":
			maxAge = strings.TrimSpace(value)
		case "secure":
			cookie.Secure = true
		case "httponly":
			cookie.HTTPOnly = true
		case "samesite":
			cookie.SameSite = sameSiteNow(value)
		}
	}

	if seconds, ok := maxAgeSeconds(maxAge); ok {
		// Held to the same span both ways, so the product with a second cannot wrap into the future.
		seconds = min(max(seconds, -maxCookieAge), maxCookieAge)
		expires := at.Add(time.Duration(seconds) * time.Second).UnixMilli()
		cookie.ExpiresAt = &expires
	}
	return cookie, true
}

// maxAgeSeconds reads a Max-Age value the way RFC 6265 §5.2.2 has it: digits, optionally led by a
// minus. Anything else (a plus sign, a fraction) is an attribute to ignore, and a number too large
// for an int64 still says "far away" in its direction.
func maxAgeSeconds(value string) (int64, bool) {
	digits := strings.TrimPrefix(value, "-")
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return 0, false
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return seconds, true
}

const cookieYearCutoff = 70

func cookieTime(value string) *int64 {
	hour, minute, second := -1, -1, -1
	day, month, year := -1, -1, -1

	for _, token := range dateTokens(value) {
		if hour < 0 {
			if h, m, s, ok := clockOf(token); ok {
				hour, minute, second = h, m, s
				continue
			}
		}
		if day < 0 {
			if n, ok := number(token); ok && len(token) <= 2 && n >= 1 && n <= 31 {
				day = n
				continue
			}
		}
		if month < 0 {
			if named, ok := monthOf(token); ok {
				month = named
				continue
			}
		}
		if year < 0 {
			if n, ok := number(token); ok && len(token) >= 2 && len(token) <= 4 {
				year = n
			}
		}
	}

	if day < 0 || month < 0 || year < 0 || hour > 23 || minute > 59 || second > 59 {
		return nil
	}
	if year >= cookieYearCutoff && year < 100 {
		year += 1900
	} else if year < cookieYearCutoff {
		year += 2000
	}
	if hour < 0 {
		hour, minute, second = 0, 0, 0
	}

	at := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC).UnixMilli()
	return &at
}

func dateTokens(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == ':':
			return false
		}
		return true
	})
}

func clockOf(token string) (int, int, int, bool) {
	parts := strings.Split(token, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, 0, 0, false
	}
	values := [3]int{}
	for i, part := range parts {
		n, ok := number(part)
		if !ok {
			return 0, 0, 0, false
		}
		values[i] = n
	}
	return values[0], values[1], values[2], true
}

func number(token string) (int, bool) {
	if token == "" {
		return 0, false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(token)
	return n, err == nil
}

func monthOf(token string) (int, bool) {
	if len(token) < 3 {
		return 0, false
	}
	for i, name := range months {
		if strings.EqualFold(token[:3], name) {
			return i + 1, true
		}
	}
	return 0, false
}

var months = []string{
	"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec",
}

func sameSiteNow(value string) string {
	trimmed := strings.TrimSpace(value)
	switch strings.ToLower(trimmed) {
	case "lax":
		return "Lax"
	case "strict":
		return "Strict"
	case "none":
		return "None"
	}
	return trimmed
}

func HostOf(rawURL string) string {
	address, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return address.Hostname()
}

func JarCookiesFor(all []JarCookie, rawURL string, now time.Time) []JarCookie {
	address, err := url.Parse(rawURL)
	if err != nil || address.Host == "" {
		return nil
	}

	out := []JarCookie{}
	for _, cookie := range all {
		if !cookieApplies(cookie, address, now) {
			continue
		}
		out = append(out, cookie)
	}
	slices.SortStableFunc(out, func(a, b JarCookie) int {
		if c := cmp.Compare(len(b.Path), len(a.Path)); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.Domain, b.Domain)
	})
	return out
}

func cookieApplies(cookie JarCookie, address *url.URL, now time.Time) bool {
	if cookie.ExpiresAt != nil && *cookie.ExpiresAt <= now.UnixMilli() {
		return false
	}
	if cookie.Secure && !SecureOrigin(address.Scheme, address.Hostname()) {
		return false
	}
	return cookieReaches(cookie, address.Hostname()) &&
		pathMatches(cookie.Path, address.EscapedPath())
}

// SecureOrigin says whether a cookie marked Secure may travel to or from an origin: https, or this
// machine, which browsers trust as a secure context too.
func SecureOrigin(scheme, host string) bool {
	return strings.EqualFold(scheme, "https") || LoopbackHost(host)
}

// cookieReaches says whether a cookie goes to a host: a host-only cookie to that host alone, a domain
// cookie to the domain and everything under it.
func cookieReaches(cookie JarCookie, host string) bool {
	if cookie.HostOnly {
		return BareDomain(cookie.Domain) == strings.ToLower(host)
	}
	return HostMatches(cookie.Domain, host)
}

// NotSavedWrites are the cookies of a response as a record keeps them when none reached the jar:
// where each would have gone, and the state that says they did not.
func NotSavedWrites(cookies []ResponseCookie, rawURL string) []CookieWrite {
	host := strings.ToLower(HostOf(rawURL))
	requestPath := requestPathOf(rawURL)

	out := make([]CookieWrite, 0, len(cookies))
	for _, cookie := range cookies {
		named := BareDomain(cookie.Domain)
		if named == "" {
			named = host
		}
		out = append(out, CookieWrite{
			Name: cookie.Name, Value: cookie.Value, Domain: named,
			Path:      CookiePath(cookie.Path, requestPath),
			ExpiresAt: cookie.ExpiresAt, Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly,
			SameSite: cookie.SameSite, HostOnly: cookie.Domain == "", State: CookieNotSaved,
		})
	}
	return out
}

// BareDomain is a domain as the jar compares it: lower case, without the leading dot a Set-Cookie
// line may write.
func BareDomain(named string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(named)), ".")
}

// HostMatches is the domain-match of RFC 6265 §5.1.3: the host is the domain, or a name under it.
// An address is matched only by itself, since "0.0.1" is not a parent of "10.0.0.1".
func HostMatches(domain, host string) bool {
	domain = BareDomain(domain)
	host = strings.ToLower(host)
	if domain == "" {
		return false
	}
	if host == domain {
		return true
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}
	return strings.HasSuffix(host, "."+domain)
}

// CookiePath is the path a cookie is kept under: the Path attribute when it is an absolute one,
// otherwise the directory of the request that set it (RFC 6265 §5.1.4 and §5.2.4).
func CookiePath(attribute, requestPath string) string {
	if strings.HasPrefix(attribute, "/") {
		return attribute
	}
	if !strings.HasPrefix(requestPath, "/") {
		return "/"
	}
	if last := strings.LastIndex(requestPath, "/"); last > 0 {
		return requestPath[:last]
	}
	return "/"
}

// requestPathOf is the path of a request address as the jar compares paths.
func requestPathOf(rawURL string) string {
	address, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return address.EscapedPath()
}

func pathMatches(cookiePath, requestPath string) bool {
	if cookiePath == "" || cookiePath == "/" {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	if len(requestPath) == len(cookiePath) {
		return true
	}
	return strings.HasSuffix(cookiePath, "/") || requestPath[len(cookiePath)] == '/'
}
