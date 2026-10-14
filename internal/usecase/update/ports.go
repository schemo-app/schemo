package update

import (
	"context"

	"schemo/internal/domain"
)

type Releases interface {
	Latest(ctx context.Context, channel domain.UpdateChannel) (domain.Release, error)
	Install(ctx context.Context, channel domain.UpdateChannel, version string) error
}

type State interface {
	LoadUpdateState(ctx context.Context) (domain.UpdateState, error)
	SaveUpdateState(ctx context.Context, state domain.UpdateState) error
}

type Settings interface {
	Snapshot(ctx context.Context) (domain.Settings, error)
}
