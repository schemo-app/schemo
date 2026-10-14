package sync

import (
	"schemo/internal/domain"
)

// Status represents the current sync engine snapshot for UI.
type Status struct {
	Running   bool                             `json:"running"`
	SignedIn  bool                             `json:"signedIn"`
	Round     bool                             `json:"round"`
	Pushed    int                              `json:"pushed"`
	Pulled    int                              `json:"pulled"`
	Spaces    []domain.SyncSpace               `json:"spaces"`
	Counts    map[string]domain.SyncCounts     `json:"counts"`
	Waiting   map[string][]domain.QueuedChange `json:"waiting,omitempty"`
	Failure   *domain.Failure                  `json:"failure,omitempty"`
	Conflict  *Conflict                        `json:"conflict,omitempty"`
	RoundAt   int64                            `json:"roundAt,omitempty"`
	ContentAt int64                            `json:"contentAt,omitempty"`
	HistoryAt int64                            `json:"historyAt,omitempty"`
	// RetryAt is when the engine will try the round that failed again, zero when the last round was
	// fine: the row counts the seconds down from it.
	RetryAt int64 `json:"retryAt,omitempty"`
}

// Conflict represents a local change overwritten by the server.
type Conflict struct {
	SpaceID  string            `json:"spaceId"`
	Name     string            `json:"name,omitempty"`
	EntityID string            `json:"entityId"`
	Kind     domain.EntityType `json:"kind"`
	At       int64             `json:"at"`
}
