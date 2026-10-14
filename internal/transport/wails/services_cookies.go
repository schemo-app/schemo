package wails

import (
	"context"

	"schemo/internal/domain"
	"schemo/internal/usecase/cookiejar"
)

// CookiesService is the jar as the manager window draws it: the domains and what each holds, and
// what a person can do to them. Nothing here is about one request — what a request sends is part of
// the draft's own answer, and what an answer wrote is part of its record.
type CookiesService struct {
	jar *cookiejar.UseCase
}

func NewCookiesService(jar *cookiejar.UseCase) *CookiesService {
	return &CookiesService{jar: jar}
}

// Jar is every domain of the space on screen with its cookies: the manager in one call, because a
// rail and a table that arrived separately would be a rail of one jar and a table of another.
func (s *CookiesService) Jar(ctx context.Context) ([]domain.JarDomain, error) {
	return s.jar.Jar(ctx)
}

// ForURL is the jar as one address sees it: what a request to that address would carry, drawn as
// rows. The response's tab asks for it, because what an answer left in the jar is the answer's own
// history and what the jar holds is not — the two are read apart and drawn apart.
func (s *CookiesService) ForURL(ctx context.Context, url string) ([]domain.JarCookie, error) {
	return s.jar.Rows(ctx, url)
}

// Save writes one row of the manager's table, whole: a cookie is three lines of text and four
// flags, and a patch per keystroke would be four calls for one row.
func (s *CookiesService) Save(ctx context.Context, cookie domain.JarCookie) error {
	return s.jar.Save(ctx, cookie)
}

func (s *CookiesService) Delete(ctx context.Context, id string) error {
	return s.jar.Delete(ctx, id)
}

// DeleteDomain is what the manager's footer does to the domain on screen: its cookies and its flag
// go, and no other domain does.
func (s *CookiesService) DeleteDomain(ctx context.Context, host string) error {
	return s.jar.DeleteDomain(ctx, host)
}

// SetBlocked is the manager's one switch: whether responses from a domain may change the jar.
func (s *CookiesService) SetBlocked(ctx context.Context, host string, blocked bool) error {
	return s.jar.SetBlocked(ctx, host, blocked)
}
