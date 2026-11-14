// Package script runs collection and request scripts in a goja sandbox with the `pm` API and no
// network, files or timers.
package script

import (
	"context"
	"errors"
	"schemo/internal/db"
	"schemo/internal/domain"
	"schemo/internal/platform"
	"strings"
	"sync"
	"time"
)

// Level kinds: a collection in the chain, the request itself (last), and a draft in no tree.
const (
	KindCollection = "collection"
	KindRequest    = "request"
	KindDraft      = "draft"
)

type Scripts struct {
	engine Runner
	store  *db.Store
	vars   Variables
	scope  domain.Scope
	ids    platform.IDGen

	// Run scopes (`pm.variables`), kept in memory: they live only as long as the run, and only runs
	// that wrote something have one.
	mu   sync.Mutex
	runs map[string]map[string]string
}

func NewScripts(
	engine Runner,
	store *db.Store,
	vars Variables,
	scope domain.Scope,
	ids platform.IDGen,
) *Scripts {
	return &Scripts{
		engine: engine, store: store, vars: vars, scope: scope, ids: ids,
		runs: map[string]map[string]string{},
	}
}

// Scripts reads a level's own code; nil means not set, distinct from empty code.
func (s *Scripts) Scripts(ctx context.Context, id string) (*domain.Scripts, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	scripts, err := s.store.Scripts(ctx, workspace, id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	return scripts, err
}

// SaveScripts writes a level's code (nil clears it) and returns what was stored.
func (s *Scripts) SaveScripts(
	ctx context.Context,
	id string,
	scripts *domain.Scripts,
) (*domain.Scripts, error) {
	workspace, err := s.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	// The editor writes code only: a write that omits captures keeps the level's.
	if scripts == nil || scripts.Captures == nil {
		if held, err := s.store.Scripts(ctx, workspace, id); err == nil && held != nil &&
			len(held.Captures) > 0 {
			if scripts == nil {
				scripts = &domain.Scripts{}
			}
			scripts.Captures = held.Captures
		}
	}
	if err := s.store.SaveScripts(ctx, workspace, id, scripts); err != nil {
		return nil, err
	}
	return s.store.Scripts(ctx, workspace, id)
}

// Before runs the pre-request scripts outermost first, each seeing the previous one's changes, and
// reports whether to send. Reports go back in the pass, since the record is written after the answer.
//
//wails:ignore
func (s *Scripts) Before(
	ctx context.Context,
	workspace string,
	pass *domain.ScriptPass,
) (bool, error) {
	chain, err := s.chain(ctx, workspace, pass.NodeID)
	if err != nil {
		return false, err
	}

	skip := false
	for _, level := range chain {
		source := scriptSource(level, domain.ScriptPre)
		if source == "" {
			continue
		}
		report := s.script(ctx, *pass, level, domain.ScriptPre, source)
		pass.Ran = append(pass.Ran, report)
		// Only a pre-request script can call a request off; one that does it after the answer came
		// back is too late to matter, and saying so would be a lie about what happened.
		skip = skip || report.SkipRequest
	}
	return skip, nil
}

// After stores the first half's reports and runs the post-request scripts. A report that cannot be
// stored is dropped rather than failing a request that worked.
//
//wails:ignore
func (s *Scripts) After(ctx context.Context, workspace string, pass domain.ScriptPass) {
	for _, report := range pass.Ran {
		if err := s.store.SaveScriptRun(ctx, workspace, report); err != nil {
			return
		}
	}

	chain, err := s.chain(ctx, workspace, pass.NodeID)
	if err != nil {
		return
	}
	for _, level := range chain {
		// Captures are in place before the level's own code, which may read them.
		if report, ok := s.applyCaptures(ctx, pass, level); ok {
			if err := s.store.SaveScriptRun(ctx, workspace, report); err != nil {
				return
			}
		}
		source := scriptSource(level, domain.ScriptPost)
		if source == "" {
			continue
		}
		if err := s.store.SaveScriptRun(ctx, workspace,
			s.script(ctx, pass, level, domain.ScriptPost, source)); err != nil {
			return
		}
	}
}

// Runs is what the response viewer draws: every script that ran around one record, in the order
// they ran. An empty answer is a request whose collection has no scripts, which is most of them.
func (s *Scripts) Runs(ctx context.Context, recordID string) ([]domain.ScriptRun, error) {
	return s.store.ScriptRuns(ctx, recordID)
}

// scriptSource is a level's code for one scope; blank or switched-off code is skipped without
// affecting the other levels.
func scriptSource(level Level, scope domain.ScriptScope) string {
	if level.Scripts.Off(scope) {
		return ""
	}
	if scope == domain.ScriptPost {
		return strings.TrimSpace(level.Scripts.Post)
	}
	return strings.TrimSpace(level.Scripts.Pre)
}

// script executes one level's script. Everything that says who ran — which record, which node, when
// — is stamped here: the sandbox knows none of it, and the tab draws all of it.
func (s *Scripts) script(
	ctx context.Context,
	at domain.ScriptPass,
	level Level,
	scope domain.ScriptScope,
	source string,
) domain.ScriptRun {
	report := s.engine.Run(domain.ScriptInput{
		Scope:     scope,
		Source:    source,
		Request:   at.Request,
		Response:  at.Response,
		Variables: scriptVariables{ctx: ctx, owner: s, run: at.Run},
	})
	report.ID = s.ids()
	report.RecordID = at.RecordID
	report.NodeID = level.NodeID
	report.Scope = scope
	report.CreatedAt = time.Now().UnixMilli()
	return report
}

var _ domain.VarStore = scriptVariables{}

// Runner runs one script and returns its report. What leaves the sandbox (the request a pre-request
// script changed, the variables it wrote) is what the engine put back where it found it.
type Runner interface {
	Run(in domain.ScriptInput) domain.ScriptRun
}

// Variables is the stored scopes a script reads and writes: the active environment and the globals.
type Variables interface {
	Variable(ctx context.Context, scope domain.VarScope, name string) (string, bool, error)
	SetVariable(ctx context.Context, scope domain.VarScope, name string, value string) error
	// Capture is SetVariable that can also hide the value.
	Capture(
		ctx context.Context, scope domain.VarScope, name string, value string, secret bool,
	) error
}
