package cookiejar

import (
	"context"

	"schemo/internal/domain"
)

// Store keeps the jar: the cookies of one workspace and the flag each domain carries. Nothing here
// reads a request or a record — the jar is its own table because it outlives every request that
// touched it.
type Store interface {
	// Jar is every domain with what it holds, the domains with nothing but a flag included.
	Jar(ctx context.Context, workspaceID string) ([]domain.JarDomain, error)
	// JarCookie is one row by the identity HTTP gives a cookie — its host, its path and its name —
	// so that writing one can say what it replaced. Absent answers domain.ErrNotFound.
	JarCookie(
		ctx context.Context,
		workspaceID, host, path, name string,
	) (domain.JarCookie, error)
	SaveJarCookie(ctx context.Context, workspaceID string, cookie domain.JarCookie) error
	DeleteJarCookie(ctx context.Context, workspaceID, id string) error
	// DeleteJarDomain removes a domain with its cookies and its flag. One that is not there is not
	// an error: the caller asked for the jar to be without it, and it is.
	DeleteJarDomain(ctx context.Context, workspaceID, host string) error
	JarDomain(ctx context.Context, workspaceID, host string) (bool, error)
	SaveJarDomain(ctx context.Context, workspaceID, host string, blocked bool) error
}
