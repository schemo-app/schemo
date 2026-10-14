// Package scripting runs the code a collection keeps around its requests: before a request goes out
// and after the answer came back.
//
// What runs is a chain and not a single script: the collection's scripts, then every folder's on
// the way down, then the request's own. A level does not replace what is above it — all of it runs,
// in that order, which is what lets a collection count its requests while one request asserts about
// its own answer.
package scripting

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"schemo/internal/domain"
	"schemo/internal/platform"
)

// KindCollection is what a level calls a collection — the one being run, or one inside it, which is
// a level of the chain like any other. KindRequest is the request itself, the last level of a
// chain. KindDraft is a request that is not in a tree at all — the command line's.
const (
	KindCollection = "collection"
	KindRequest    = "request"
	KindDraft      = "draft"
)

type UseCase struct {
	engine Engine
	tree   Tree
	store  Store
	vars   Variables
	scope  domain.Scope
	ids    platform.IDGen

	// The run's own scope, which is what `pm.variables` reads and writes. It lives here because it
	// belongs to no feature that outlives the run: a few strings per run. Only a run whose scripts
	// actually wrote something has an entry, and a script that only reads leaves nothing behind.
	mu   sync.Mutex
	runs map[string]map[string]string
}

// maxRuns is how many runs keep a scope at once. Nothing reads a run's own memory once the run is
// over — it is the run's memory and no one else's — so a session of a thousand sends does not have
// to keep a thousand of them alive for the life of the process.
const maxRuns = 32

func NewUseCase(
	engine Engine,
	tree Tree,
	store Store,
	vars Variables,
	scope domain.Scope,
	ids platform.IDGen,
) *UseCase {
	return &UseCase{
		engine: engine, tree: tree, store: store, vars: vars, scope: scope, ids: ids,
		runs: map[string]map[string]string{},
	}
}

// Chain is what runs around a request, outermost first — the collection's scripts, then each
// collection inside it on the way down, then the request's own. A level with nothing to run is not
// in it: this is what runs, not the places it could have run from.
func (u *UseCase) Chain(ctx context.Context, nodeID string) ([]Level, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return u.chain(ctx, workspace, nodeID)
}

func (u *UseCase) chain(ctx context.Context, workspace, nodeID string) ([]Level, error) {
	// An empty id is a request nothing was composed around: one that came out of a link in a response,
	// or out of the browser. Nothing is above it because there is no it.
	if nodeID == "" {
		return []Level{}, nil
	}

	tree, err := u.tree.Collections(ctx, workspace)
	if err != nil {
		return nil, err
	}
	for _, collection := range tree {
		chain, ok, err := u.pathTo(ctx, workspace, collection, nodeID)
		if err != nil {
			return nil, err
		}
		if ok {
			return chain, nil
		}
	}

	// Not in any tree: the command line's request, whose code lives with the draft it is. It is a
	// chain of one, and it has no name to be drawn with — nobody named it.
	//
	// An id nothing knows is the same answer: a request whose level is gone — a card open on a node
	// deleted beside it — has nothing around it, and saying so is better than failing a send over code
	// that no longer exists.
	scripts, err := u.tree.Scripts(ctx, workspace, nodeID)
	if errors.Is(err, domain.ErrNotFound) {
		return []Level{}, nil
	}
	if err != nil {
		return nil, err
	}
	if scripts.Empty() {
		return []Level{}, nil
	}
	return []Level{{NodeID: nodeID, Kind: KindDraft, Scripts: *scripts}}, nil
}

// Scripts is what a level runs of its own: a collection's, a collection's inside it, a request's —
// or the command line's. Nil is "not set here", which is a different answer from a script that is
// simply empty.
func (u *UseCase) Scripts(ctx context.Context, id string) (*domain.Scripts, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	return u.store.Scripts(ctx, workspace, id)
}

// SaveScripts writes what a level has to say about the requests it runs around, and nil puts it
// back to "not set here" — the state a level returns to when its code is taken off it.
func (u *UseCase) SaveScripts(ctx context.Context, id string, scripts *domain.Scripts) error {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return err
	}
	// The editor writes code only: a write that omits captures keeps the level's.
	if scripts == nil || scripts.Captures == nil {
		if held, err := u.store.Scripts(ctx, workspace, id); err == nil && held != nil &&
			len(held.Captures) > 0 {
			if scripts == nil {
				scripts = &domain.Scripts{}
			}
			scripts.Captures = held.Captures
		}
	}
	return u.store.SaveScripts(ctx, workspace, id, scripts)
}

// SaveCaptures replaces a level's capture rules and keeps its code. Rules get ids; an invalid rule
// is refused.
func (u *UseCase) SaveCaptures(
	ctx context.Context,
	id string,
	captures []domain.Capture,
) (*domain.Scripts, error) {
	workspace, err := u.scope.ActiveWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	for i := range captures {
		if !captures[i].Valid() {
			return nil, domain.ErrNotAllowed
		}
		if captures[i].ID == "" {
			captures[i].ID = u.ids()
		}
	}

	held, err := u.store.Scripts(ctx, workspace, id)
	if err != nil {
		return nil, err
	}
	scripts := domain.Scripts{}
	if held != nil {
		scripts = *held
	}
	scripts.Captures = captures
	if len(captures) == 0 {
		scripts.Captures = nil
	}

	// A level left with nothing goes back to inheriting.
	write := &scripts
	if held == nil && len(captures) == 0 {
		write = nil
	}
	if err := u.store.SaveScripts(ctx, workspace, id, write); err != nil {
		return nil, err
	}
	return write, nil
}

// Before runs the pre-request scripts of everything above a request and answers whether the request
// should be sent at all.
//
// What the scripts do to the request is on the pass, and they all see the same one: a collection's
// script changes what a folder's is handed, and the folder's changes what the request's own sees.
//
// What they did goes back in the pass rather than into the store: a report hangs off the record it
// ran around, and that record is written only after the request has been answered.
func (u *UseCase) Before(
	ctx context.Context,
	workspace string,
	pass *domain.ScriptPass,
) (bool, error) {
	chain, err := u.chain(ctx, workspace, pass.NodeID)
	if err != nil {
		return false, err
	}

	skip := false
	for _, level := range chain {
		source := scriptSource(level, domain.ScriptPre)
		if source == "" {
			continue
		}
		report := u.script(ctx, *pass, level, domain.ScriptPre, source)
		pass.Ran = append(pass.Ran, report)
		// Only a pre-request script can call a request off; one that does it after the answer came
		// back is too late to matter, and saying so would be a lie about what happened.
		skip = skip || report.SkipRequest
	}
	return skip, nil
}

// After writes down what the first half did — the record it belongs to exists by now — and runs the
// second half, in the same order.
//
// It answers with nothing because there is nothing left to say: the answer came back and the
// request is a record already. A report that cannot be written is lost — the alternative is a
// request that failed after it had worked — and the ones behind it are lost with it, because a
// store that refuses one will refuse the next.
func (u *UseCase) After(ctx context.Context, workspace string, pass domain.ScriptPass) {
	for _, report := range pass.Ran {
		if err := u.store.SaveScriptRun(ctx, workspace, report); err != nil {
			return
		}
	}

	chain, err := u.chain(ctx, workspace, pass.NodeID)
	if err != nil {
		return
	}
	for _, level := range chain {
		// Captures are in place before the level's own code, which may read them.
		if report, ok := u.applyCaptures(ctx, pass, level); ok {
			if err := u.store.SaveScriptRun(ctx, workspace, report); err != nil {
				return
			}
		}
		source := scriptSource(level, domain.ScriptPost)
		if source == "" {
			continue
		}
		if err := u.store.SaveScriptRun(ctx, workspace,
			u.script(ctx, pass, level, domain.ScriptPost, source)); err != nil {
			return
		}
	}
}

// Runs is what the response viewer draws: every script that ran around one record, in the order
// they ran. An empty answer is a request whose collection has no scripts, which is most of them.
func (u *UseCase) Runs(ctx context.Context, recordID string) ([]domain.ScriptRun, error) {
	return u.store.ScriptRuns(ctx, recordID)
}

// scriptSource is what a level has to run for one scope. A script of nothing but whitespace is a
// level with nothing to say, and a level with nothing to say is not a level that throws the ones
// above it away: it simply is not in the pass. A half that is switched off reads the same way here
// and only here — the level keeps its code, and this is the one place that decides what runs.
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
func (u *UseCase) script(
	ctx context.Context,
	at domain.ScriptPass,
	level Level,
	scope domain.ScriptScope,
	source string,
) domain.ScriptRun {
	report := u.engine.Run(domain.ScriptInput{
		Scope:     scope,
		Source:    source,
		Request:   at.Request,
		Response:  at.Response,
		Variables: scriptVariables{ctx: ctx, owner: u, run: at.Run},
	})
	report.ID = u.ids()
	report.RecordID = at.RecordID
	report.NodeID = level.NodeID
	report.Scope = scope
	report.CreatedAt = time.Now().UnixMilli()
	return report
}

type scriptVariables struct {
	ctx   context.Context
	owner *UseCase
	run   string
}

var _ domain.VarStore = scriptVariables{}

// Lookup answers the run's own scope first, then the environment, then the globals — the order
// a request resolves in, so a script gets what the next request of the run will get. Asking for
// either of the other two reads that one alone, which is how a script tells where a name is set.
func (v scriptVariables) Lookup(scope domain.VarScope, name string) (string, bool, error) {
	if scope != domain.ScopeRun {
		return v.owner.vars.Variable(v.ctx, scope, name)
	}
	if value, ok := v.owner.runVariable(v.run, name); ok {
		return value, true, nil
	}
	value, ok, err := v.owner.vars.Variable(v.ctx, domain.ScopeEnvironment, name)
	if err != nil || ok {
		return value, ok, err
	}
	return v.owner.vars.Variable(v.ctx, domain.ScopeGlobals, name)
}

// Set writes where the scope says it goes. A value a script puts in the run's own scope is this
// run's and nobody else's: the next request of the same run sees it, another run does not, and a
// restart forgets it. The other two are stored, which is the point of them — and a secret written
// here is stored like any other value, because the app has nowhere else to keep it.
func (v scriptVariables) Set(scope domain.VarScope, name string, value string) error {
	if scope != domain.ScopeRun {
		err := v.owner.vars.SetVariable(v.ctx, scope, name, value)
		return v.owner.keptForTheRun(v.run, name, value, err)
	}
	v.owner.setRunVariable(v.run, name, value)
	return nil
}

// keptForTheRun turns the refusal of a space this person may only read into the run's own scope:
// a script that records a token for the next request still works, and what the space holds is not
// changed. Any other answer is the store's, and goes back as it came.
func (u *UseCase) keptForTheRun(run, name, value string, err error) error {
	if domain.CodeOf(err) != domain.CodeWorkspaceReadOnly {
		return err
	}
	u.setRunVariable(run, name, value)
	return nil
}

// RunVariables is what the scripts of a run keep for it alone, as the variables its next request is
// resolved with. They stand innermost, where a value set for one run overrides the space's own.
func (u *UseCase) RunVariables(run string) []domain.Variable {
	u.mu.Lock()
	defer u.mu.Unlock()

	kept := make([]domain.Variable, 0, len(u.runs[run]))
	for name, value := range u.runs[run] {
		kept = append(kept, domain.Variable{
			Name: name, Value: value, Kind: domain.VariableText, Enabled: true, HasValue: value != "",
		})
	}
	slices.SortFunc(kept, func(a, b domain.Variable) int { return cmp.Compare(a.Name, b.Name) })
	return kept
}

func (u *UseCase) runVariable(run string, name string) (string, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()

	value, ok := u.runs[run][name]
	return value, ok
}

func (u *UseCase) setRunVariable(run string, name string, value string) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.runs[run] == nil {
		// Which entry goes is not worth knowing: a run that is over never reads its scope again, and
		// only the run that is going reads its own. What is worth knowing is that the entry of the run
		// being written is never the one taken.
		if len(u.runs) >= maxRuns {
			for held := range u.runs {
				if held != run {
					delete(u.runs, held)
					break
				}
			}
		}
		u.runs[run] = map[string]string{}
	}
	u.runs[run][name] = value
}

// pathTo is the way down from a collection to one id, collections inside it included: what is
// above a request is what runs before it does, so this walk's order is the chain's order. A
// collection that is the id itself is a chain of one, with nothing above it.
func (u *UseCase) pathTo(
	ctx context.Context,
	workspace string,
	from domain.Collection,
	id string,
) ([]Level, bool, error) {
	own, err := u.levelOf(ctx, workspace, from.ID, from.Name, KindCollection)
	if err != nil {
		return nil, false, err
	}
	if from.ID == id {
		return levelsOf(own), true, nil
	}

	for _, entry := range from.Level() {
		if entry.Collection != nil {
			below, ok, err := u.pathTo(ctx, workspace, *entry.Collection, id)
			if err != nil {
				return nil, false, err
			}
			if ok {
				return append(levelsOf(own), below...), true, nil
			}
			continue
		}
		if entry.Node.ID != id {
			continue
		}

		last, err := u.levelOf(ctx, workspace, entry.Node.ID, entry.Node.Name, KindRequest)
		if err != nil {
			return nil, false, err
		}
		return levelsOf(own, last), true, nil
	}
	return nil, false, nil
}

// levelOf is one step of a chain, or nothing when the level has no code of its own to run: a level
// that runs nothing is not a step, it is a collection on the way.
func (u *UseCase) levelOf(
	ctx context.Context,
	workspace, id string,
	name string,
	kind string,
) (*Level, error) {
	scripts, err := u.tree.Scripts(ctx, workspace, id)
	if err != nil {
		return nil, err
	}
	if scripts.Empty() {
		return nil, nil
	}
	return &Level{NodeID: id, Name: name, Kind: kind, Scripts: *scripts}, nil
}

func levelsOf(levels ...*Level) []Level {
	out := []Level{}
	for _, level := range levels {
		if level != nil {
			out = append(out, *level)
		}
	}
	return out
}
