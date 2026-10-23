package request

import (
	"cmp"
	"context"
	"errors"
	"net/netip"
	"net/url"
	"schemo/internal/db"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// Cookies is the jar of the workspace on screen. The cookie manager and the response tab call it
// from the window; the HTTP engine calls the ignored methods.
type Cookies struct {
	store *db.Store
	scope domain.Scope
	ids   platform.IDGen
}

func NewCookies(store *db.Store, scope domain.Scope, ids platform.IDGen) *Cookies {
	return &Cookies{store: store, scope: scope, ids: ids}
}

// Jar is the whole jar of the workspace on screen, which is what the manager draws.
func (s *Cookies) Jar(ctx context.Context) ([]domain.JarDomain, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return s.store.Jar(ctx, workspace)
}

// ForURL is the jar as one address sees it: the cookies that apply to it, whole, with the value and
// attributes a `Cookie` header never carried. The response tab draws it under an answer, so the
// table and the next request to that address cannot disagree: they are the same list read twice.
//
// It is the active workspace's jar, not a named one; the draft, the only caller that sends for
// another workspace, asks for the header instead.
func (s *Cookies) ForURL(ctx context.Context, rawURL string) ([]domain.JarCookie, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return s.matchingCookies(ctx, workspace, rawURL)
}

// CookieHeader is what one address goes out with: the cookies of this workspace that apply to it,
// as the header carries them. The rule is the matching one (host, path, expiry and scheme), so what
// a request sends and what the window says it sends cannot disagree.
//
//wails:ignore
func (s *Cookies) CookieHeader(
	ctx context.Context,
	workspace, rawURL string,
) (string, error) {
	cookies, err := s.matchingCookies(ctx, workspace, rawURL)
	if err != nil {
		return "", err
	}
	return cookieHeader(cookies), nil
}

// matchingCookies is that list before either of the two shapes it is read in: the matching rule,
// applied to the jar of one workspace.
func (s *Cookies) matchingCookies(
	ctx context.Context,
	workspace, rawURL string,
) ([]domain.JarCookie, error) {
	domains, err := s.store.Jar(ctx, workspace)
	if err != nil {
		return nil, err
	}
	return domain.JarCookiesFor(allCookies(domains), asciiURL(rawURL), time.Now()), nil
}

// cookieHeader is what a `Cookie` header carries: name=value pairs separated by a semicolon.
// Domain, path and the flags decide whether to send a cookie and are not part of the header. A
// cookie with no name is skipped.
func cookieHeader(cookies []domain.JarCookie) string {
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		name := strings.TrimSpace(cookie.Name)
		if name == "" {
			continue
		}
		parts = append(parts, name+"="+cookie.Value)
	}
	return strings.Join(parts, "; ")
}

// DomainOf is the domain the jar keeps the cookies of an address under, or nothing for an address
// with no host.
func (s *Cookies) DomainOf(rawURL string) string {
	from, ok := originOf(rawURL)
	if !ok {
		return ""
	}
	return from.host
}

// Save writes one cookie of the manager's table. The domain is the row's own and the row keeps the
// identity it was found by: what changes is the value and the attributes.
//
// How far the cookie reaches is read off the domain it was written with, as a browser's cookie list
// writes it: a leading dot means the whole domain, no dot the host alone. A cookie typed by hand is
// therefore not a third kind, only one nobody spelt out in full.
func (s *Cookies) Save(ctx context.Context, cookie domain.JarCookie) error {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cookie.Name) == "" {
		return domain.Refuse(domain.CodeNameEmpty, domain.ErrNotAllowed, nil)
	}
	written := strings.TrimSpace(cookie.Domain)
	named := canonicalDomain(written)
	if named == "" {
		return domain.Refuse(domain.CodeDomainEmpty, domain.ErrNotAllowed, nil)
	}
	if cookie.ID == "" {
		cookie.ID = s.ids()
	}
	if cookie.Path == "" {
		cookie.Path = "/"
	}
	if cookie.Origin == "" {
		cookie.Origin = domain.CookieByHand
	}
	cookie.HostOnly = !strings.HasPrefix(written, ".")
	cookie.Domain = named
	cookie.WrittenAt = time.Now().UnixMilli()
	return s.store.SaveJarCookie(ctx, workspace, cookie)
}

func (s *Cookies) Delete(ctx context.Context, id string) error {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	return s.store.DeleteJarCookie(ctx, workspace, id)
}

// DeleteDomain takes one domain out of the jar with its cookies and its flag: the manager's rail is
// domains, and what its footer deletes is the one on screen. The other domains are not touched.
func (s *Cookies) DeleteDomain(ctx context.Context, host string) error {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	return s.store.DeleteJarDomain(ctx, workspace, canonicalDomain(host))
}

// SetBlocked is the manager's one switch per domain: whether responses from it may change the jar.
func (s *Cookies) SetBlocked(ctx context.Context, host string, blocked bool) error {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	return s.store.SaveJarDomain(ctx, workspace, canonicalDomain(host), blocked)
}

// Apply puts an answer's cookies into the jar and returns what became of each. A send calls it when
// the response is in hand, and the record keeps the list: the window draws it as what this answer
// changed, and Undo reads it backwards.
//
// One cookie, one write, and a line that changed nothing writes nothing. An answer may name the
// same cookie several times (google.com sets one and clears every host variant in the same
// response), and what matters is the change that came of the answer, not the lines it took. As in a
// browser, the last line wins, and its write carries the row the first line found: that is the row
// the jar held before the answer, and the one taking the writes back has to restore.
//
// A deletion of a cookie the jar does not hold changed nothing and is not recorded, since a
// "deleted" row over a cookie nobody had is about nothing.
//
// A jar that cannot be written answers with the cookies marked as not saved rather than failing: an
// answer that arrived is worth a record either way. The one remaining failure is a response with no
// address, which has nothing to write under.
//
//wails:ignore
func (s *Cookies) Apply(
	ctx context.Context,
	workspace, rawURL string,
	headers []domain.HeaderPair,
) []domain.CookieWrite {
	now := time.Now()
	parsed := domain.ParseResponseCookies(headers, now)
	if len(parsed) == 0 {
		return []domain.CookieWrite{}
	}
	from, ok := originOf(rawURL)
	if !ok {
		return domain.NotSavedWrites(parsed, rawURL)
	}
	writes, err := s.applyCookies(ctx, workspace, from, parsed, now)
	if err != nil {
		return domain.NotSavedWrites(parsed, rawURL)
	}
	return writes
}

// applyCookies is every cookie of one answer going into the jar, in the order the lines named them.
func (s *Cookies) applyCookies(
	ctx context.Context,
	workspace string,
	from origin,
	parsed []domain.ResponseCookie,
	now time.Time,
) ([]domain.CookieWrite, error) {
	writes := make([]domain.CookieWrite, 0, len(parsed))

	blocked := map[string]bool{}
	// The write standing for each cookie named so far, by the identity the jar keys one by.
	writeIndex := map[cookieKey]int{}
	for _, cookie := range parsed {
		// A line may only name a domain its own host owns, and never a public suffix: without that,
		// any response could write a cookie this app would later send to a host that never asked for
		// it. It is reported rather than dropped, since a server trying to set one for somebody
		// else's domain is what a person reading this tab wants to see.
		domainName, hostOnly, allowed := from.domainOf(cookie.Domain)
		if !allowed || !from.mayBeSet(cookie) {
			writes = append(writes, domain.NotSavedWrites([]domain.ResponseCookie{cookie}, from.url)...)
			continue
		}
		if _, checked := blocked[domainName]; !checked {
			flag, err := s.store.JarDomain(ctx, workspace, domainName)
			if err != nil {
				return nil, err
			}
			blocked[domainName] = flag
		}
		if blocked[domainName] {
			writes = append(writes, domain.NotSavedWrites([]domain.ResponseCookie{cookie}, from.url)...)
			continue
		}
		path := domain.CookiePath(cookie.Path, from.path)
		write, changed, err := s.applyCookie(ctx, workspace, domainName, hostOnly, path, cookie, now)
		if err != nil {
			return nil, err
		}
		if !changed {
			continue
		}
		key := cookieKey{host: domainName, path: path, name: write.Name}
		if at, seen := writeIndex[key]; seen {
			write.Previous = writes[at].Previous
			writes[at] = write
			continue
		}
		writeIndex[key] = len(writes)
		writes = append(writes, write)
	}
	return writes, nil
}

// cookieKey names a cookie the way the jar does: where it applies, and what it is called. A struct
// rather than a joined string, because a path may hold any separator a string could use.
type cookieKey struct {
	host, path, name string
}

// applyCookie is one cookie of an answer going into the jar. A cookie whose life has run out is the
// server asking for its removal rather than for another value, which is the one write that is a
// deletion.
func (s *Cookies) applyCookie(
	ctx context.Context,
	workspace, host string,
	hostOnly bool,
	path string,
	cookie domain.ResponseCookie,
	now time.Time,
) (domain.CookieWrite, bool, error) {
	expired := cookie.ExpiresAt != nil && *cookie.ExpiresAt <= now.UnixMilli()

	previous, err := s.heldCookie(ctx, workspace, host, path, cookie.Name)
	if err != nil {
		return domain.CookieWrite{}, false, err
	}

	if expired {
		if previous == nil {
			// Nothing to take away: the jar never held this cookie, and a line that changed nothing
			// is not part of what the answer came to.
			return domain.CookieWrite{}, false, nil
		}
		if err := s.store.DeleteJarCookie(ctx, workspace, previous.ID); err != nil {
			return domain.CookieWrite{}, false, err
		}
		return cookieWriteOf(cookie, host, path, hostOnly, domain.CookieDeleted, previous), true, nil
	}

	saved := domain.JarCookie{
		Domain:    host,
		Name:      cookie.Name,
		Value:     cookie.Value,
		Path:      path,
		ExpiresAt: cookie.ExpiresAt,
		Secure:    cookie.Secure,
		HTTPOnly:  cookie.HTTPOnly,
		SameSite:  cookie.SameSite,
		Origin:    domain.CookieFromResponse,
		HostOnly:  hostOnly,
		WrittenAt: now.UnixMilli(),
	}
	state := domain.CookieAdded
	if previous != nil {
		// The row keeps the id it had: it is the same cookie set again, and the window addresses a
		// row by id — a new one would be a row that moved under the cursor.
		saved.ID = previous.ID
		state = domain.CookieReplaced
	} else {
		saved.ID = s.ids()
	}
	if err := s.store.SaveJarCookie(ctx, workspace, saved); err != nil {
		return domain.CookieWrite{}, false, err
	}
	return cookieWriteOf(cookie, host, path, hostOnly, state, previous), true, nil
}

// heldCookie is the cookie the jar holds under that identity, or nil when it holds none.
func (s *Cookies) heldCookie(
	ctx context.Context,
	workspace, host, path, name string,
) (*domain.JarCookie, error) {
	held, err := s.store.JarCookie(ctx, workspace, host, path, name)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &held, nil
}

// Undo takes back what one answer wrote. A cookie the answer added goes; one it replaced or deleted
// comes back whole, which is why the record keeps the row and not only its value.
//
// A row that has changed since is not restored over: the write is a fact about one moment, and
// putting back what it found would throw away whoever wrote after it.
//
//wails:ignore
func (s *Cookies) Undo(
	ctx context.Context,
	workspace string,
	writes []domain.CookieWrite,
) error {
	for _, write := range writes {
		if write.State == domain.CookieNotSaved {
			continue
		}
		current, err := s.heldCookie(ctx, workspace, write.Domain, cmp.Or(write.Path, "/"), write.Name)
		if err != nil {
			return err
		}

		if write.Previous == nil {
			// The answer added it: taking the write back is the row going, and only if it is still
			// the row the write left.
			if current != nil && current.Value == write.Value {
				if err := s.store.DeleteJarCookie(ctx, workspace, current.ID); err != nil {
					return err
				}
			}
			continue
		}
		if current != nil && current.Value != write.Value {
			continue
		}
		if err := s.store.SaveJarCookie(ctx, workspace, *write.Previous); err != nil {
			return err
		}
	}
	return nil
}

// allCookies is every cookie of the jar in one list, which is what the matching rule takes.
func allCookies(domains []domain.JarDomain) []domain.JarCookie {
	out := []domain.JarCookie{}
	for _, level := range domains {
		out = append(out, level.Cookies...)
	}
	return out
}

// cookieWriteOf is one cookie of an answer as the record keeps it, under the domain and path the
// jar holds it by.
func cookieWriteOf(
	cookie domain.ResponseCookie,
	host, path string,
	hostOnly bool,
	state domain.CookieState,
	previous *domain.JarCookie,
) domain.CookieWrite {
	return domain.CookieWrite{
		Name: cookie.Name, Value: cookie.Value, Domain: host, Path: path,
		ExpiresAt: cookie.ExpiresAt, Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly,
		SameSite: cookie.SameSite, HostOnly: hostOnly, State: state, Previous: previous,
	}
}

// The storage model of RFC 6265 §5.3, with the secure-origin and name-prefix rules of its
// successor (rfc6265bis): what a response may put in the jar, and under which domain and path.

const (
	securePrefix = "__Secure-"
	hostPrefix   = "__Host-"
)

// origin is where a response came from, in the spelling the jar keys on.
type origin struct {
	// url is the address as the request had it, which the record shows.
	url string
	// host is lower case and in its ASCII form, without port.
	host string
	// path is the request path, which a cookie without a Path attribute takes its directory from.
	path   string
	secure bool
}

func originOf(rawURL string) (origin, bool) {
	address, err := url.Parse(rawURL)
	if err != nil || address.Hostname() == "" {
		return origin{}, false
	}
	host := asciiHost(address.Hostname())
	return origin{
		url:    rawURL,
		host:   host,
		path:   address.EscapedPath(),
		secure: domain.SecureOrigin(address.Scheme, host),
	}, true
}

// asciiHost is a host as the jar keys it: lower case, and a name with international characters in
// its ASCII form (RFC 6265 §5.1.2), so that either spelling of an address finds the same cookies.
func asciiHost(host string) string {
	host = strings.ToLower(host)
	if ascii, err := idna.Lookup.ToASCII(host); err == nil {
		return ascii
	}
	return host
}

// canonicalDomain is a domain the manager or a Domain attribute names, as the jar keys it.
func canonicalDomain(named string) string {
	return asciiHost(domain.BareDomain(named))
}

// asciiURL is an address with its host in the form the jar keys on.
func asciiURL(rawURL string) string {
	address, err := url.Parse(rawURL)
	if err != nil || address.Hostname() == "" {
		return rawURL
	}
	host := asciiHost(address.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := address.Port(); port != "" {
		host += ":" + port
	}
	address.Host = host
	return address.String()
}

// domainOf is the domain a cookie is kept under and whether only this host is sent it, or false when
// the response may not set it: a Domain attribute is for the host's own domain or one above it, and
// never for a public suffix, which would make the cookie everybody's (§5.3 step 5).
func (from origin) domainOf(attribute string) (name string, hostOnly, ok bool) {
	attribute = canonicalDomain(attribute)
	if attribute == "" {
		return from.host, true, true
	}
	if _, err := netip.ParseAddr(from.host); err == nil {
		// An address has no domain above it: only the attribute that spells it is accepted.
		return from.host, true, attribute == from.host
	}
	if suffix, _ := publicsuffix.PublicSuffix(attribute); suffix == attribute {
		// A site served from the suffix itself (localhost, an intranet name) keeps its cookie to itself.
		return from.host, true, from.host == attribute
	}
	return attribute, false, domain.HostMatches(attribute, from.host)
}

// mayBeSet applies the rules that do not concern the domain: a Secure cookie comes over a secure
// connection, and a name with a reserved prefix keeps the promise the prefix makes.
func (from origin) mayBeSet(cookie domain.ResponseCookie) bool {
	if cookie.Secure && !from.secure {
		return false
	}
	if hasPrefixFold(cookie.Name, securePrefix) && !cookie.Secure {
		return false
	}
	if hasPrefixFold(cookie.Name, hostPrefix) {
		return cookie.Secure && cookie.Domain == "" && cookie.Path == "/"
	}
	return true
}

func hasPrefixFold(name, prefix string) bool {
	return len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix)
}
