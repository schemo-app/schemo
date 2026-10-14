package domain

import (
	"cmp"
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

	if seconds, err := strconv.ParseInt(maxAge, 10, 64); err == nil {
		if seconds > maxCookieAge {
			seconds = maxCookieAge
		}
		expires := at.Add(time.Duration(seconds) * time.Second).UnixMilli()
		cookie.ExpiresAt = &expires
	}
	return cookie, true
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
	if cookie.Secure && address.Scheme != "https" {
		return false
	}
	return HostMatches(cookie.Domain, address.Hostname()) &&
		pathMatches(cookie.Path, address.EscapedPath())
}

func NotSavedWrites(cookies []ResponseCookie, host string) []CookieWrite {
	out := make([]CookieWrite, 0, len(cookies))
	for _, cookie := range cookies {
		named, hostOnly := JarDomainOf(cookie.Domain, host)
		out = append(out, CookieWrite{
			Name: cookie.Name, Value: cookie.Value, Domain: named, Path: cookie.Path,
			ExpiresAt: cookie.ExpiresAt, Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly,
			SameSite: cookie.SameSite, HostOnly: hostOnly, State: CookieNotSaved,
		})
	}
	return out
}

func JarDomainOf(named, host string) (string, bool) {
	trimmed := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(named)), ".")
	if trimmed == "" || strings.EqualFold(trimmed, host) {
		return strings.ToLower(host), true
	}
	return trimmed, false
}

func HostMatches(domain, host string) bool {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	host = strings.ToLower(host)
	if domain == "" {
		return false
	}
	if host == domain {
		return true
	}
	return strings.HasSuffix(host, "."+domain)
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
