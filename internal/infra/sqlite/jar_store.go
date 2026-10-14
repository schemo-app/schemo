package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"schemo/internal/domain"
)

// The jar is two tables: the cookies themselves and the per-domain flag. They are read together
// because the window draws them together — the manager's rail is domains with their counts — and
// written apart because a flag outlives the last cookie of a domain.

// Jar is every domain of one workspace with the cookies it holds. A domain appears when it has a
// cookie or a flag, so a domain whose writes are blocked and which holds nothing is still a thing
// jar remembers.
func (s *Store) Jar(ctx context.Context, workspaceID string) ([]domain.JarDomain, error) {
	blocked := map[string]bool{}
	rows, err := s.db.QueryContext(ctx,
		`SELECT domain, blocked FROM jar_domains
		  WHERE workspace_id = ? AND deleted_at IS NULL`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("reading the domains of the jar of %s: %w", workspaceID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			domain string
			flag   bool
		)
		if err := rows.Scan(&domain, &flag); err != nil {
			return nil, fmt.Errorf("reading the domains of the jar of %s: %w", workspaceID, err)
		}
		blocked[domain] = flag
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the domains of the jar of %s: %w", workspaceID, err)
	}

	cookies, err := s.jarCookies(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	out := []domain.JarDomain{}
	at := map[string]int{}
	for _, level := range sortedDomains(blocked, cookies) {
		out = append(out, level)
		at[level.Domain] = len(out) - 1
	}
	for _, cookie := range cookies {
		index := at[cookie.Domain]
		out[index].Cookies = append(out[index].Cookies, cookie)
		if cookie.WrittenAt > out[index].WrittenAt {
			out[index].WrittenAt = cookie.WrittenAt
		}
	}
	return out, nil
}

// sortedDomains is the rail's order: the domains with their cookies, plus the ones that carry
// nothing but a flag, by name. A map's order is random in Go, and a list that reshuffled between
// two reads would be a rail that moved under the hand.
func sortedDomains(
	blocked map[string]bool,
	cookies []domain.JarCookie,
) []domain.JarDomain {
	out := []domain.JarDomain{}
	seen := map[string]bool{}
	add := func(host string) {
		if seen[host] {
			return
		}
		seen[host] = true
		out = append(out, domain.JarDomain{
			Domain:  host,
			Blocked: blocked[host],
			Cookies: []domain.JarCookie{},
		})
	}
	for _, cookie := range cookies {
		add(cookie.Domain)
	}
	for host := range blocked {
		add(host)
	}
	slices.SortFunc(out, func(a, b domain.JarDomain) int { return cmp.Compare(a.Domain, b.Domain) })
	return out
}

// jarCookies is every cookie of one workspace, by domain and name. The jar is read whole and not by
// domain: what a request sends is decided from the whole of it, and a read per domain would be a
// query per host in the jar.
func (s *Store) jarCookies(ctx context.Context, workspaceID string) ([]domain.JarCookie, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, domain, name, value, path, expires_at, secure, http_only, same_site, origin,
		        host_only, written_at
		   FROM jar_cookies WHERE workspace_id = ? AND deleted_at IS NULL
		  ORDER BY domain, name, path`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("reading the jar of %s: %w", workspaceID, err)
	}
	defer rows.Close()

	out := []domain.JarCookie{}
	for rows.Next() {
		var (
			cookie           domain.JarCookie
			expiresAt        sql.NullInt64
			origin, sameSite string
		)
		if err := rows.Scan(&cookie.ID, &cookie.Domain, &cookie.Name, &cookie.Value, &cookie.Path,
			&expiresAt, &cookie.Secure, &cookie.HTTPOnly, &sameSite, &origin, &cookie.HostOnly,
			&cookie.WrittenAt); err != nil {
			return nil, fmt.Errorf("reading the jar of %s: %w", workspaceID, err)
		}
		if expiresAt.Valid {
			at := expiresAt.Int64
			cookie.ExpiresAt = &at
		}
		cookie.SameSite, cookie.Origin = sameSite, domain.CookieOrigin(origin)
		out = append(out, cookie)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the jar of %s: %w", workspaceID, err)
	}
	return out, nil
}

// JarCookie is one cookie by the identity HTTP gives it: its host, its path and its name. A cookie
// that is not there answers domain.ErrNotFound, because the caller is asking what it would replace.
func (s *Store) JarCookie(
	ctx context.Context,
	workspaceID, host, path, name string,
) (domain.JarCookie, error) {
	var (
		cookie           domain.JarCookie
		expiresAt        sql.NullInt64
		origin, sameSite string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, value, expires_at, secure, http_only, same_site, origin, host_only, written_at
		   FROM jar_cookies
		  WHERE workspace_id = ? AND domain = ? AND path = ? AND name = ? AND deleted_at IS NULL`,
		workspaceID, host, path, name).
		Scan(&cookie.ID, &cookie.Value, &expiresAt, &cookie.Secure, &cookie.HTTPOnly, &sameSite, &origin,
			&cookie.HostOnly, &cookie.WrittenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.JarCookie{}, fmt.Errorf("cookie %q of %s: %w", name, host, domain.ErrNotFound)
	}
	if err != nil {
		return domain.JarCookie{}, fmt.Errorf("reading cookie %q of %s: %w", name, host, err)
	}
	cookie.Domain, cookie.Name, cookie.Path = host, name, path
	if expiresAt.Valid {
		at := expiresAt.Int64
		cookie.ExpiresAt = &at
	}
	cookie.SameSite, cookie.Origin = sameSite, domain.CookieOrigin(origin)
	return cookie, nil
}

// SaveJarCookie writes one cookie. A row is addressed by id, because the manager edits a cookie
// that is already there and an edit can move it: renaming one, or giving it another path, changes
// what HTTP calls the cookie — the id is what stays. A response setting a cookie over one that is
// there is the same call: the row it replaces is found by that identity first, and its id is kept.
//
// What the unique index still refuses is the one real collision: moving a row onto an identity
// another row already holds, which is two cookies with one name and one path. That is a conflict,
// and it is reported as one rather than silently dropping either of them.
func (s *Store) SaveJarCookie(
	ctx context.Context,
	workspaceID string,
	cookie domain.JarCookie,
) error {
	var expiresAt any
	if cookie.ExpiresAt != nil {
		expiresAt = *cookie.ExpiresAt
	}
	if cookie.Path == "" {
		cookie.Path = "/"
	}
	if cookie.WrittenAt == 0 {
		cookie.WrittenAt = time.Now().UnixMilli()
	}

	return s.inTx(ctx, fmt.Sprintf("saving cookie %q", cookie.Name), func(tx *writeTx) error {
		action, err := actionFor(ctx, tx, "jar_cookies", cookie.ID)
		if err != nil {
			return fmt.Errorf("saving cookie %q: %w", cookie.Name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO jar_cookies (id, workspace_id, domain, name, value, path, expires_at, secure,
			                          http_only, same_site, origin, host_only, written_at, version)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			   domain = excluded.domain, name = excluded.name, value = excluded.value,
			   path = excluded.path, expires_at = excluded.expires_at, secure = excluded.secure,
			   http_only = excluded.http_only, same_site = excluded.same_site,
			   origin = excluded.origin, host_only = excluded.host_only,
			   written_at = excluded.written_at`,
			cookie.ID, workspaceID, cookie.Domain, cookie.Name, cookie.Value, cookie.Path, expiresAt,
			cookie.Secure, cookie.HTTPOnly, cookie.SameSite,
			string(cookie.Origin), cookie.HostOnly, cookie.WrittenAt,
			versionUnagreed); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("cookie %q: %w", cookie.Name, domain.ErrConflict)
			}
			return fmt.Errorf("saving cookie %q: %w", cookie.Name, err)
		}
		return enqueue(ctx, tx, workspaceID, entityCookie, cookie.ID, action)
	})
}

// DeleteJarCookie marks one cookie gone. A cookie that is not there is not an error: a double
// click, or a delete arriving after the domain went, has asked for exactly what it got.
//
// It is a write rather than a removal because the jar is shared: a cookie deleted on this machine
// and left alone on another is a cookie the other one goes on sending.
func (s *Store) DeleteJarCookie(ctx context.Context, workspaceID, id string) error {
	return s.inTx(ctx, fmt.Sprintf("deleting cookie %s", id), func(tx *writeTx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE jar_cookies SET deleted_at = ?
			  WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL`,
			time.Now().UnixMilli(), workspaceID, id)
		if err != nil {
			return fmt.Errorf("deleting cookie %s: %w", id, err)
		}
		changed, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("deleting cookie %s: %w", id, err)
		}
		if changed == 0 {
			return nil
		}
		return enqueue(ctx, tx, workspaceID, entityCookie, id, actionDelete)
	})
}

// DeleteJarDomain removes one domain from the jar: its cookies and its flag, each queued as a
// deletion of its own. The flag goes with them because one left behind is a domain the manager
// still draws, with nothing in it.
func (s *Store) DeleteJarDomain(ctx context.Context, workspaceID, host string) error {
	return s.inTx(ctx, fmt.Sprintf("deleting the domain %s", host), func(tx *writeTx) error {
		flags, err := idsOf(ctx, tx,
			`SELECT id FROM jar_domains
			  WHERE workspace_id = ? AND domain = ? AND deleted_at IS NULL ORDER BY id`,
			workspaceID, host)
		if err != nil {
			return fmt.Errorf("deleting the domain %s: %w", host, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE jar_domains SET deleted_at = ?
			  WHERE workspace_id = ? AND domain = ? AND deleted_at IS NULL`,
			time.Now().UnixMilli(), workspaceID, host); err != nil {
			return fmt.Errorf("deleting the domain %s: %w", host, err)
		}
		if err := queueAll(ctx, tx, workspaceID, entityJarDomain, actionDelete, flags); err != nil {
			return err
		}
		return clearCookies(ctx, tx, workspaceID, `domain = ?`, host)
	})
}

// clearCookies marks every cookie of a workspace that matches, and queues each of them: a cookie is
// a row of its own on the other side, so a jar that was emptied here is a change per cookie there.
func clearCookies(ctx context.Context, tx *writeTx, workspaceID, where string, args ...any) error {
	scope := `workspace_id = ? AND ` + where + ` AND deleted_at IS NULL`
	args = append([]any{workspaceID}, args...)
	ids, err := idsOf(ctx, tx, `SELECT id FROM jar_cookies WHERE `+scope+` ORDER BY id`, args...)
	if err != nil {
		return fmt.Errorf("clearing the cookies of %s: %w", workspaceID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE jar_cookies SET deleted_at = ? WHERE `+scope,
		append([]any{time.Now().UnixMilli()}, args...)...); err != nil {
		return fmt.Errorf("clearing the cookies of %s: %w", workspaceID, err)
	}
	return queueAll(ctx, tx, workspaceID, entityCookie, actionDelete, ids)
}

// JarDomain is one domain's flag. A domain that is not there answers false, which is the same
// answer as one whose flag is off: nothing has blocked it.
func (s *Store) JarDomain(ctx context.Context, workspaceID, domain string) (bool, error) {
	var blocked bool
	err := s.db.QueryRowContext(ctx,
		`SELECT blocked FROM jar_domains
		  WHERE workspace_id = ? AND domain = ? AND deleted_at IS NULL`,
		workspaceID, domain).Scan(&blocked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading the flag of %s: %w", domain, err)
	}
	return blocked, nil
}

// SaveJarDomain writes one domain's flag, making the domain if it is not there yet. The row is
// found by the pair and not by an id, which is what a manager's switch is: a domain nobody has
// blocked has no row until somebody does, and a domain that was blocked and unblocked keeps the one
// it has. Its id is the one that travels, so the row is enqueued by what the write found.
func (s *Store) SaveJarDomain(ctx context.Context, workspaceID, domain string, blocked bool) error {
	return s.inTx(ctx, fmt.Sprintf("saving the flag of %s", domain), func(tx *writeTx) error {
		var id string
		action := actionCreate
		err := tx.QueryRowContext(ctx,
			`SELECT id FROM jar_domains WHERE workspace_id = ? AND domain = ?`, workspaceID, domain).
			Scan(&id)
		if err == nil {
			action = actionUpdate
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("saving the flag of %s: %w", domain, err)
		}

		// The id comes back from the write rather than from the read above: a domain that is not there
		// yet has none until it is inserted, and the entry has to name the row that is really there.
		if err := tx.QueryRowContext(ctx,
			`INSERT INTO jar_domains (id, workspace_id, domain, blocked, written_at, version)
			 VALUES (lower(hex(randomblob(16))), ?, ?, ?, ?, ?)
			 ON CONFLICT(workspace_id, domain) WHERE deleted_at IS NULL DO UPDATE SET
			   blocked = excluded.blocked, written_at = excluded.written_at, deleted_at = NULL
			 RETURNING id`,
			workspaceID, domain, blocked, time.Now().UnixMilli(),
			versionUnagreed).Scan(&id); err != nil {
			return fmt.Errorf("saving the flag of %s: %w", domain, err)
		}
		return enqueue(ctx, tx, workspaceID, entityJarDomain, id, action)
	})
}
