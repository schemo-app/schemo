// Package cookiejar owns the cookies: the jar one workspace keeps, what a response writes into it,
// and what every request to a domain is sent from it.
//
// It is a feature of its own because the jar outlives everything that touches it: a request reads
// it and never writes it, a response writes it and is gone. What it holds belongs to a domain
// rather than to either of them, which is the whole of the difference between it and the
// hand-written list a request used to carry.
package cookiejar

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

type UseCase struct {
	store Store
	scope domain.Scope
	ids   platform.IDGen
}

func NewUseCase(store Store, scope domain.Scope, ids platform.IDGen) *UseCase {
	return &UseCase{store: store, scope: scope, ids: ids}
}

// Jar is the whole jar of the space on screen, which is what the manager draws.
func (u *UseCase) Jar(ctx context.Context) ([]domain.JarDomain, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return u.store.Jar(ctx, workspace)
}

// Rows is the jar as one address sees it: the cookies that apply to it, whole — the value and the
// attributes included, which a `Cookie` header never carried. It is what the response's tab draws
// under an answer, so that a table of what the jar holds for an address and the next request to it
// cannot disagree: they are the same list read twice.
//
// It is the active workspace's jar, not a named one: the window draws the space on screen, and the
// draft — the only caller that sends for a workspace that is not the active one — asks for the
// header instead.
func (u *UseCase) Rows(ctx context.Context, rawURL string) ([]domain.JarCookie, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return u.rows(ctx, workspace, rawURL)
}

// CookieHeader is what one address goes out with: the cookies of this workspace that apply to it,
// as the header carries them. The rule is the matching one — host, path, expiry and scheme — so
// what a request sends and what the window says it sends cannot disagree.
func (u *UseCase) CookieHeader(
	ctx context.Context,
	workspace, rawURL string,
) (string, error) {
	cookies, err := u.rows(ctx, workspace, rawURL)
	if err != nil {
		return "", err
	}
	return cookieHeader(cookies), nil
}

// rows is that list before either of the two shapes it is read in: the matching rule, applied to
// the jar of one workspace.
func (u *UseCase) rows(
	ctx context.Context,
	workspace, rawURL string,
) ([]domain.JarCookie, error) {
	domains, err := u.store.Jar(ctx, workspace)
	if err != nil {
		return nil, err
	}
	return domain.JarCookiesFor(jarOf(domains), rawURL, time.Now()), nil
}

// cookieHeader is what a `Cookie` header carries: name=value pairs separated by a semicolon.
// Domain, path and the flags are about deciding whether to send a cookie and were never part of
// the header. A cookie with no name is skipped too: there is nothing to write.
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

// Save writes one cookie of the manager's table. The domain it belongs to is the row's own, and the
// row keeps the identity it was found by: what changes is the value and the attributes.
//
// How far the cookie reaches is read off the domain it was written with, the way a browser's own
// cookie list writes it: a leading dot means the whole domain, and no dot means the host alone. It
// is the same spelling the jar stores every cookie as, and the same distinction the manager draws —
// so a cookie typed by hand is not a third kind of cookie, only one nobody spelt out in full.
func (u *UseCase) Save(ctx context.Context, cookie domain.JarCookie) error {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cookie.Name) == "" {
		return domain.Refuse(domain.CodeNameEmpty, domain.ErrNotAllowed, nil)
	}
	written := strings.TrimSpace(cookie.Domain)
	if bareHost(written) == "" {
		return domain.Refuse(domain.CodeDomainEmpty, domain.ErrNotAllowed, nil)
	}
	if cookie.ID == "" {
		cookie.ID = u.ids()
	}
	if cookie.Path == "" {
		cookie.Path = "/"
	}
	if cookie.Origin == "" {
		cookie.Origin = domain.CookieByHand
	}
	cookie.HostOnly = !strings.HasPrefix(written, ".")
	cookie.Domain = bareHost(written)
	cookie.WrittenAt = time.Now().UnixMilli()
	return u.store.SaveJarCookie(ctx, workspace, cookie)
}

func (u *UseCase) Delete(ctx context.Context, id string) error {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	return u.store.DeleteJarCookie(ctx, workspace, id)
}

// DeleteDomain takes one domain out of the jar with its cookies and its flag: the manager's rail is
// domains, and what its footer deletes is the one on screen. The other domains are not touched.
func (u *UseCase) DeleteDomain(ctx context.Context, host string) error {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	return u.store.DeleteJarDomain(ctx, workspace, bareHost(host))
}

// SetBlocked is the manager's one switch per domain: whether responses from it may change the jar.
func (u *UseCase) SetBlocked(ctx context.Context, host string, blocked bool) error {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	return u.store.SaveJarDomain(ctx, workspace, bareHost(host), blocked)
}

// bareHost is the spelling the jar keys a domain by: lower case, and without the dot that only
// says how far a cookie reaches.
func bareHost(host string) string {
	return strings.TrimPrefix(strings.ToLower(host), ".")
}

// Apply puts an answer's cookies into the jar and answers with what became of each of them. It is
// what a send calls when the response is in hand, and the list it answers with is what the record
// keeps: the window draws it as what this answer changed, and Undo is the same list read backwards.
//
// One cookie, one write, and a line that changed nothing writes nothing. An answer may name the
// same cookie several times — google.com sets a cookie and then clears every host variant of it in
// the same breath — and what the record is about is the change that came of the answer, not the
// lines it took: a browser ends such an answer with the last of them applied, and so does this. The
// write that stands for a cookie is the last line's, and it carries the row the first line found,
// because that is the row the jar held before the answer and so the row taking the writes back has
// to put there.
//
// A deletion of a cookie the jar does not hold is one of those lines that changed nothing: a server
// clearing a variant that was never kept is not a fact about the jar, and a row saying "deleted"
// over a cookie nobody had is a row about nothing — the same reason a browser's own jar has no
// answer to give for it.
//
// A jar that cannot be written answers with the answer's cookies marked as not saved rather than
// failing: what the window draws about them is that the jar did not take them, which is the truth
// about this answer — and an answer that arrived is worth a record either way. The one failure this
// leaves is a response with no address at all, which is no address to write under.
func (u *UseCase) Apply(
	ctx context.Context,
	workspace, rawURL string,
	headers []domain.HeaderPair,
) []domain.CookieWrite {
	host := hostOf(rawURL)
	parsed := domain.ParseResponseCookies(headers, time.Now())
	if len(parsed) == 0 {
		return []domain.CookieWrite{}
	}
	writes, err := u.applyAll(ctx, workspace, host, parsed)
	if err != nil {
		return domain.NotSavedWrites(parsed, host)
	}
	return writes
}

// applyAll is every cookie of one answer going into the jar, in the order the lines named them.
func (u *UseCase) applyAll(
	ctx context.Context,
	workspace, host string,
	parsed []domain.ResponseCookie,
) ([]domain.CookieWrite, error) {
	writes := make([]domain.CookieWrite, 0, len(parsed))
	now := time.Now()

	blocked := map[string]bool{}
	// The write standing for each cookie named so far, by the identity the jar keys one by.
	met := map[cookieKey]int{}
	for _, cookie := range parsed {
		domainName, hostOnly := domain.JarDomainOf(cookie.Domain, host)
		// A line may only name a domain its own host owns: `Domain=example.com` is taken from an
		// answer on example.com or on any host under it, and ignored from anywhere else. Without
		// this, any response could write a cookie this app would later send to a host that never
		// asked for it. The cookie is reported rather than dropped: a server trying to set one for
		// somebody else's domain is exactly what a person reading this tab wants to see.
		if cookie.Domain != "" && !domain.HostMatches(cookie.Domain, host) {
			writes = append(writes, writeOf(cookie, domainName, hostOnly, domain.CookieNotSaved, nil))
			continue
		}
		if _, asked := blocked[domainName]; !asked {
			flag, err := u.store.JarDomain(ctx, workspace, domainName)
			if err != nil {
				return nil, err
			}
			blocked[domainName] = flag
		}
		if blocked[domainName] {
			writes = append(writes, writeOf(cookie, domainName, hostOnly, domain.CookieNotSaved, nil))
			continue
		}
		write, changed, err := u.apply(ctx, workspace, domainName, hostOnly, cookie, now)
		if err != nil {
			return nil, err
		}
		if !changed {
			continue
		}
		key := cookieKey{host: domainName, path: pathOf(write.Path), name: write.Name}
		if at, seen := met[key]; seen {
			write.Previous = writes[at].Previous
			writes[at] = write
			continue
		}
		met[key] = len(writes)
		writes = append(writes, write)
	}
	return writes, nil
}

// cookieKey names a cookie the way the jar does: where it applies, and what it is called. A struct
// rather than a joined string, because a path may hold any separator a string could use.
type cookieKey struct {
	host, path, name string
}

// pathOf is the path the jar stores a cookie under: a line that named none is the root.
func pathOf(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

// apply is one cookie of an answer going into the jar. A cookie whose life has run out is the
// server asking for it to be removed rather than for another value, which is the one write that is
// a deletion.
func (u *UseCase) apply(
	ctx context.Context,
	workspace, host string,
	hostOnly bool,
	cookie domain.ResponseCookie,
	now time.Time,
) (domain.CookieWrite, bool, error) {
	path := cookie.Path
	if path == "" {
		path = "/"
	}
	dead := cookie.ExpiresAt != nil && *cookie.ExpiresAt <= now.UnixMilli()

	previous, err := u.store.JarCookie(ctx, workspace, host, path, cookie.Name)
	switch {
	case err == nil:
		// There was one, which is what a replacement and a deletion both need to know.
	case errors.Is(err, domain.ErrNotFound):
		previous, err = domain.JarCookie{}, nil
	}
	if err != nil {
		return domain.CookieWrite{}, false, err
	}
	found := previous.ID != ""
	var was *domain.JarCookie
	if found {
		was = &previous
	}

	if dead {
		if !found {
			// Nothing to take away: the jar never held this cookie, and a line that changed nothing
			// is not part of what the answer came to.
			return domain.CookieWrite{}, false, nil
		}
		if err := u.store.DeleteJarCookie(ctx, workspace, previous.ID); err != nil {
			return domain.CookieWrite{}, false, err
		}
		return writeOf(cookie, host, hostOnly, domain.CookieDeleted, was), true, nil
	}

	saved := domain.JarCookie{
		ID:        u.ids(),
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
	if found {
		// The row keeps the id it had: it is the same cookie set again, and the window addresses a
		// row by id — a new one would be a row that moved under the cursor.
		saved.ID = previous.ID
	}
	if err := u.store.SaveJarCookie(ctx, workspace, saved); err != nil {
		return domain.CookieWrite{}, false, err
	}

	state := domain.CookieAdded
	if found {
		state = domain.CookieReplaced
	}
	return writeOf(cookie, host, hostOnly, state, was), true, nil
}

// Undo takes back what one answer wrote. A cookie the answer added goes; one it replaced or deleted
// comes back as it was, whole — which is why the record keeps the row and not only its value.
//
// A row that has changed since is not restored over: the write is a fact about one moment, and
// putting back what it found would throw away whoever wrote after it.
func (u *UseCase) Undo(
	ctx context.Context,
	workspace string,
	writes []domain.CookieWrite,
) error {
	for _, write := range writes {
		if write.State == domain.CookieNotSaved {
			continue
		}
		host := strings.TrimPrefix(write.Domain, ".")
		path := write.Path
		if path == "" {
			path = "/"
		}

		current, err := u.store.JarCookie(ctx, workspace, host, path, write.Name)
		switch {
		case err == nil:
		case errors.Is(err, domain.ErrNotFound):
			current, err = domain.JarCookie{}, nil
		}
		if err != nil {
			return err
		}

		if write.Previous == nil {
			// The answer added it: taking the write back is the row going, and only if it is still
			// the row the write left.
			if current.ID != "" && current.Value == write.Value {
				if err := u.store.DeleteJarCookie(ctx, workspace, current.ID); err != nil {
					return err
				}
			}
			continue
		}
		if current.ID != "" && current.Value != write.Value {
			continue
		}
		if err := u.store.SaveJarCookie(ctx, workspace, *write.Previous); err != nil {
			return err
		}
	}
	return nil
}

// jarOf is every cookie of the jar in one list, which is what the matching rule takes.
func jarOf(domains []domain.JarDomain) []domain.JarCookie {
	out := []domain.JarCookie{}
	for _, level := range domains {
		out = append(out, level.Cookies...)
	}
	return out
}

// hostOf is the host of an address, lowercased: a host is case-insensitive, and the jar keys on it.
func hostOf(rawURL string) string {
	address, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(address.Hostname())
}

// writeOf is one cookie of an answer as the record keeps it.
func writeOf(
	cookie domain.ResponseCookie,
	host string,
	hostOnly bool,
	state domain.CookieState,
	previous *domain.JarCookie,
) domain.CookieWrite {
	return domain.CookieWrite{
		Name: cookie.Name, Value: cookie.Value, Domain: host, Path: cookie.Path,
		ExpiresAt: cookie.ExpiresAt, Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly,
		SameSite: cookie.SameSite, HostOnly: hostOnly, State: state, Previous: previous,
	}
}
