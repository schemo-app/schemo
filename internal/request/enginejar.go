package request

import (
	"context"

	"schemo/internal/domain"
)

type EngineJar interface {
	CookieHeader(ctx context.Context, workspace, rawURL string) (string, error)
	Apply(
		ctx context.Context,
		workspace, rawURL string,
		headers []domain.HeaderPair,
	) []domain.CookieWrite
}
