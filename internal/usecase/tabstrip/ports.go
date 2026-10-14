package tabstrip

import (
	"context"

	"schemo/internal/domain"
)

// Store keeps the strip of one workspace: its tabs, their order, and the one on screen. The whole
// strip is read and written at once — a handful of rows of one moment, so there is no half a strip
// stored.
type Store interface {
	Tabs(ctx context.Context, workspaceID string) ([]domain.Tab, string, error)
	SaveTabs(ctx context.Context, workspaceID string, tabs []domain.Tab, active string) error
	ClearTabs(ctx context.Context, workspaceID string) error
}

// Policy is what the settings say about coming back: whether the strip of the last run is restored
// at all. It is read here and answered by the settings feature, which owns the switch.
type Policy interface {
	RestoreTabs(ctx context.Context) (bool, error)
}
