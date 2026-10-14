package desktop

import (
	"encoding/json"
	"sync"

	"schemo/internal/domain"
)

// Failure kinds, so the UI can phrase a broken data directory differently from a broken schema.
const (
	FailureDataDir   = "data-dir"
	FailureDatabase  = "database"
	FailureMigration = "migration"
)

// StartupFailure is a startup problem — a data directory, a database, a migration — and the reason
// there is no window. Not the domain's Failure, which is a call refused and a sentence waiting.
// It cannot be a dialog: a dialog needs a running app, and these happen before one exists.
type StartupFailure struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

type Status struct {
	mu      sync.Mutex
	ready   bool
	failure *StartupFailure
}

func NewStatus() *Status {
	return &Status{}
}

func (s *Status) Fail(kind string, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = false
	s.failure = &StartupFailure{Kind: kind, Detail: detail}
}

func (s *Status) Succeed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = true
	s.failure = nil
}

func (s *Status) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

func (s *Status) Failure() *StartupFailure {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failure
}

// StartupStatus is what the frontend reads on mount: whether the app has a database, and where.
type StartupStatus struct {
	Ready   bool            `json:"ready"`
	Failure *StartupFailure `json:"failure,omitempty"`
	DataDir string          `json:"dataDir"`
	DBPath  string          `json:"dbPath"`
}

// marshalFailure is the code and the values a rejected call carries beside its message. Nothing
// comes back for an error the app did not build: there is no code worth inventing for those.
func marshalFailure(err error) []byte {
	code := domain.CodeOf(err)
	if code == "" {
		return nil
	}

	failure := domain.Failure{Code: code}
	if found := domain.AsFailure(err); found != nil {
		failure = *found
	}
	encoded, marshalErr := json.Marshal(failure)
	if marshalErr != nil {
		return nil
	}
	return encoded
}
