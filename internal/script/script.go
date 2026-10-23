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

// KindCollection is what a level calls a collection: the one being run, or one inside it, which is
// a level of the chain like any other. KindRequest is the request itself, the last level of a
// chain. KindDraft is a request that is not in a tree at all, the command line's.
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

	// The run's own scope, which `pm.variables` reads and writes. It lives here because it belongs
	// to no feature that outlives the run: a few strings per run. Only a run whose scripts actually
	// wrote something has an entry.
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

// Scripts is what a level runs of its own: a collection's, a collection's inside it, a request's or
// a tab's draft. Nil is "not set here", a different answer from a script that is simply empty; a
// level no row has yet (a tab whose draft was never written) reads the same way.
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

// SaveScripts writes what a level has to say about the requests it runs around, and nil puts it
// back to "not set here" — the state a level returns to when its code is taken off it. The answer
// is what was written, so the editor draws the state the database is in.
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

// Before runs the pre-request scripts of everything above a request and reports whether the request
// should be sent at all.
//
// What the scripts do to the request is on the pass, and they all see the same one: a collection's
// script changes what a folder's is handed, and the folder's changes what the request's own sees.
//
// What they did goes back in the pass rather than into the store: a report hangs off the record it
// ran around, and that record is written only after the request has been answered.
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

// After writes down what the first half did (the record it belongs to exists by now) and runs the
// second half, in the same order.
//
// It returns nothing because there is nothing left to say: the answer came back and the request is
// a record already. A report that cannot be written is lost, rather than failing a request that had
// worked, and the ones behind it are lost with it, because a store that refuses one will refuse the
// next.
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

// scriptSource is what a level has to run for one scope. A script of nothing but whitespace is a
// level with nothing to say, and such a level does not throw away the ones above it: it simply is
// not in the pass. A half that is switched off reads the same way, here and only here: the level
// keeps its code, and this is the one place that decides what runs.
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

// Variables is what a script writes and reads outside its own run: the environment this app is
// working in and the globals under it. The run's own scope is kept by this feature, since it is
// nothing but this run's memory.
type Variables interface {
	Variable(ctx context.Context, scope domain.VarScope, name string) (string, bool, error)
	SetVariable(ctx context.Context, scope domain.VarScope, name string, value string) error
	// Capture is SetVariable that can also hide the value.
	Capture(
		ctx context.Context, scope domain.VarScope, name string, value string, secret bool,
	) error
}
