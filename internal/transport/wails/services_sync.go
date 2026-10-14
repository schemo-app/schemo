package wails

import (
	"context"

	"schemo/internal/usecase/sync"
)

// SyncService exposes synchronization engine operations and status to the presentation layer.
type SyncService struct {
	sync *sync.UseCase
}

func NewSyncService(uc *sync.UseCase) *SyncService {
	return &SyncService{sync: uc}
}

// Status returns the current global sync state, including connected spaces and errors.
func (s *SyncService) Status(ctx context.Context) (sync.Status, error) {
	return s.sync.Status(ctx)
}

// RunRound triggers a synchronous sync round and returns the updated sync status.
func (s *SyncService) RunRound(ctx context.Context) (sync.Status, error) {
	return s.sync.RunRound(ctx)
}

// Wake triggers an asynchronous sync round without blocking for completion.
func (s *SyncService) Wake() {
	s.sync.Wake()
}

// Pause suspends sync operations on the local machine.
func (s *SyncService) Pause(ctx context.Context) (sync.Status, error) {
	return s.sync.Pause(ctx)
}

// Resume resumes suspended sync operations on the local machine.
func (s *SyncService) Resume(ctx context.Context) (sync.Status, error) {
	return s.sync.Resume(ctx)
}

// Resync resets sync history for a workspace and re-synchronizes all local records.
func (s *SyncService) Resync(ctx context.Context, spaceID string) (sync.Status, error) {
	return s.sync.Resync(ctx, spaceID)
}
