package wails

import (
	"context"
	"errors"

	"schemo/internal/domain"
	"schemo/internal/usecase/scripting"
)

// ScriptingService is the code of a collection as the window works with it: what a level runs of
// its own, what runs around a node, and what the scripts of a record did. It holds the feature
// itself and nothing else, because all three are questions scripting answers whoever the level
// belongs to — a collection, a node of one, or the draft the command line is composing.
type ScriptingService struct {
	scripts *scripting.UseCase
}

func NewScriptingService(scripts *scripting.UseCase) *ScriptingService {
	return &ScriptingService{scripts: scripts}
}

// Scripts is what one level has of its own. Nil is "not set here" — the levels above are what runs
// — and that is a different answer from a script that is simply empty.
//
// A level no row has is read the same way: the editor opens on what is selected, the command line
// opens before its draft is written, and neither is a failure to show code for.
func (s *ScriptingService) Scripts(ctx context.Context, id string) (*domain.Scripts, error) {
	scripts, err := s.scripts.Scripts(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	return scripts, err
}

// SaveScripts writes what a level has of its own, and nil puts it back to inheriting. The answer is
// what was written, so the editor draws the state the database is in rather than the one it hoped
// for.
func (s *ScriptingService) SaveScripts(
	ctx context.Context,
	id string,
	scripts *domain.Scripts,
) (*domain.Scripts, error) {
	if err := s.scripts.SaveScripts(ctx, id, scripts); err != nil {
		return nil, err
	}
	return s.scripts.Scripts(ctx, id)
}

// SaveCaptures replaces a level's capture rules, keeps its code, and returns the level's scripts.
func (s *ScriptingService) SaveCaptures(
	ctx context.Context,
	id string,
	captures []domain.Capture,
) (*domain.Scripts, error) {
	return s.scripts.SaveCaptures(ctx, id, captures)
}

// Chain is what runs around a node, outermost first. The editor needs it to say what a level would
// inherit: the code a person sees when their own script is empty is somebody else's.
func (s *ScriptingService) Chain(ctx context.Context, nodeID string) ([]scripting.Level, error) {
	return s.scripts.Chain(ctx, nodeID)
}

// Runs is what the scripts of one record did, in the order they ran. An empty answer is a request
// nothing runs around, which is most of them.
func (s *ScriptingService) Runs(ctx context.Context, recordID string) ([]domain.ScriptRun, error) {
	return s.scripts.Runs(ctx, recordID)
}
