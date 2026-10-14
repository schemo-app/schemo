package settings

import (
	"context"

	"schemo/internal/domain"
)

type Store interface {
	Settings(ctx context.Context) (map[string]string, error)
	SaveSetting(ctx context.Context, key, value string) error
	// SyncScopes is the one setting read on its own: the engine asks for it before every push, from
	// another feature, and what it means is a thing both sides have to agree on rather than parse.
	SyncScopes(ctx context.Context) (domain.SyncScopes, error)
}

type Outbound interface {
	Apply(network domain.Network)
}
