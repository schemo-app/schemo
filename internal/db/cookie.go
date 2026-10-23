package db

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"schemo/internal/domain"
)

// jarFlag is what a jar_domain row holds: whether cookies from a domain are kept.
type jarFlag struct {
	Blocked bool `json:"blocked"`
}

func cookieRow(workspace string, c domain.JarCookie) (entity, error) {
	if c.Path == "" {
		c.Path = "/"
	}
	if c.WrittenAt == 0 {
		c.WrittenAt = time.Now().UnixMilli()
	}
	data, err := encodeData(c)
	if err != nil {
		return entity{}, err
	}
	return entity{Kind: domain.EntityCookie, ID: c.ID, Workspace: workspace, Parent: c.Domain,
		Name: c.Name, Key: naturalKey(c.Domain, c.Path, c.Name), Data: data, UpdatedAt: c.WrittenAt}, nil
}

func cookieFrom(e entity) (domain.JarCookie, error) {
	c, err := decodeData[domain.JarCookie](e)
	c.ID, c.Domain, c.Name = e.ID, e.Parent, e.Name
	return c, err
}

func (s *Store) Jar(ctx context.Context, workspaceID string) ([]domain.JarDomain, error) {
	rows, err := listEntities(ctx, s.db, workspaceID, kinds(domain.EntityCookie, domain.EntityJarDomain), "")
	if err != nil {
		return nil, err
	}
	byDomain := map[string]*domain.JarDomain{}
	level := func(host string) *domain.JarDomain {
		if byDomain[host] == nil {
			byDomain[host] = &domain.JarDomain{Domain: host, Cookies: []domain.JarCookie{}}
		}
		return byDomain[host]
	}
	for _, row := range rows {
		if row.Kind == domain.EntityJarDomain {
			flag, err := decodeData[jarFlag](row)
			if err != nil {
				return nil, err
			}
			level(row.Name).Blocked = flag.Blocked
			continue
		}
		cookie, err := cookieFrom(row)
		if err != nil {
			return nil, err
		}
		held := level(cookie.Domain)
		held.Cookies = append(held.Cookies, cookie)
		held.WrittenAt = max(held.WrittenAt, cookie.WrittenAt)
	}
	out := make([]domain.JarDomain, 0, len(byDomain))
	for _, held := range byDomain {
		slices.SortFunc(held.Cookies, func(a, b domain.JarCookie) int {
			return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Path, b.Path))
		})
		out = append(out, *held)
	}
	slices.SortFunc(out, func(a, b domain.JarDomain) int { return cmp.Compare(a.Domain, b.Domain) })
	return out, nil
}

func (s *Store) JarCookie(ctx context.Context, workspaceID, host, path, name string) (domain.JarCookie, error) {
	rows, err := listEntities(ctx, s.db, workspaceID, kinds(domain.EntityCookie), `key = ?`,
		naturalKey(host, path, name))
	if err != nil {
		return domain.JarCookie{}, err
	}
	if len(rows) == 0 {
		return domain.JarCookie{}, domain.ErrNotFound
	}
	return cookieFrom(rows[0])
}

func (s *Store) SaveJarCookie(ctx context.Context, workspaceID string, cookie domain.JarCookie) error {
	row, err := cookieRow(workspaceID, cookie)
	if err != nil {
		return err
	}
	return s.inTx(ctx, "saving cookie "+cookie.Name, func(tx *writeTx) error {
		return putEntity(ctx, tx, row)
	})
}

func (s *Store) DeleteJarCookie(ctx context.Context, workspaceID, id string) error {
	return s.inTx(ctx, "deleting cookie "+id, func(tx *writeTx) error {
		row, err := getEntity(ctx, tx, domain.EntityCookie, id)
		if err != nil || row.Workspace != workspaceID {
			return nil
		}
		return deleteEntities(ctx, tx, workspaceID, []entity{row})
	})
}

func rank(e entity) int {
	if e.Kind == domain.EntityJarDomain {
		return 0
	}
	return 1
}

// DeleteJarDomain forgets a domain: its flag and every cookie it holds.
func (s *Store) DeleteJarDomain(ctx context.Context, workspaceID, host string) error {
	return s.inTx(ctx, "deleting the domain "+host, func(tx *writeTx) error {
		rows, err := listEntities(ctx, tx, workspaceID, kinds(domain.EntityJarDomain, domain.EntityCookie),
			`(kind = ? AND name = ?) OR (kind = ? AND parent_id = ?)`,
			domain.EntityJarDomain, host, domain.EntityCookie, host)
		if err != nil {
			return err
		}
		// The flag goes first, then the cookies under it.
		slices.SortStableFunc(rows, func(a, b entity) int {
			return cmp.Compare(rank(a), rank(b))
		})
		return deleteEntities(ctx, tx, workspaceID, rows)
	})
}

func (s *Store) JarDomain(ctx context.Context, workspaceID, host string) (bool, error) {
	rows, err := listEntities(ctx, s.db, workspaceID, kinds(domain.EntityJarDomain), `name = ?`, host)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	flag, err := decodeData[jarFlag](rows[0])
	return flag.Blocked, err
}

func (s *Store) SaveJarDomain(ctx context.Context, workspaceID, host string, blocked bool) error {
	data, err := encodeData(jarFlag{Blocked: blocked})
	if err != nil {
		return err
	}
	return s.inTx(ctx, "saving the flag of "+host, func(tx *writeTx) error {
		row := entity{Kind: domain.EntityJarDomain, ID: newID(), Workspace: workspaceID, Name: host,
			Key: naturalKey(host), Data: data}
		held, err := listEntities(ctx, tx, workspaceID, kinds(domain.EntityJarDomain), `name = ?`, host)
		if err != nil {
			return err
		}
		if len(held) > 0 {
			row.ID = held[0].ID
		}
		err = putEntity(ctx, tx, row)
		if errors.Is(err, domain.ErrConflict) {
			return nil
		}
		return err
	})
}
