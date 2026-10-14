// Package scriptengine runs a user's script in a sandbox: goja, the `pm` API, no network, no files,
// no timers, and a few seconds to finish.
//
// What a script can reach is the whole of its world: the request it is running around, the answer
// if there is one, and the three scopes its variables live in. There is no `pm.sendRequest`, and
// that is not a gap to be filled later — a script that could send a request would need a callback
// to come back through, and five seconds of sandbox would stop meaning anything.
package scriptengine

import (
	_ "embed"
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"

	"schemo/internal/domain"
)

// The prelude is embedded rather than read: it is part of the program, and a file next to
// the binary is a file that can go missing.
//
//go:embed prelude.js
var preludeSource string

// The two halves of a script's name in a stack trace: what failed should say at which moment of the
// request it was running, because the same mistake reads very differently in each.
const (
	preName  = "pre-request.js"
	postName = "post-response.js"
)

// Engine runs scripts. It keeps nothing between runs: goja is not thread-safe, so every run gets a
// runtime of its own, and a collection's fifty requests do not share one.
type Engine struct {
	// within is how long a script may run. It is a field and not a constant so that a test can watch
	// the limit work without waiting five seconds for it.
	within time.Duration
}

func NewEngine() *Engine {
	return &Engine{within: maxScriptDuration}
}

// Run executes one script and answers with its report. A script that throws, or that never
// finishes, is a report as well: the caller is not left to guess what a failure means, and the tab
// draws the same shape either way.
//
// The report carries no ids and no timestamps — who ran, for which record, and when — because that
// is what the caller knows and this does not.
func (e *Engine) Run(in domain.ScriptInput) domain.ScriptRun {
	started := time.Now()
	run := domain.ScriptRun{
		Scope: in.Scope,
		OK:    true,
		Logs:  []domain.ScriptLog{},
		Tests: []domain.TestResult{},
	}

	vm := goja.New()
	state := &runState{vm: vm, in: in, run: &run}
	if err := state.bridge(); err != nil {
		run.OK, run.Error = false, err.Error()
		run.DurationUs = time.Since(started).Microseconds()
		return run
	}

	stop := limitScript(vm, e.within)
	defer stop()

	program, err := goja.Compile(scriptName(in.Scope), in.Source, false)
	if err != nil {
		run.OK, run.Error = false, err.Error()
		run.DurationUs = time.Since(started).Microseconds()
		return run
	}
	if _, err := vm.RunProgram(program); err != nil {
		run.OK, run.Error = false, state.failure(err)
	}

	state.readBack(in.Request)
	state.closeReport()
	run.DurationUs = time.Since(started).Microseconds()
	return run
}

// scriptName is what a stack trace calls the script.
func scriptName(scope domain.ScriptScope) string {
	if scope == domain.ScriptPost {
		return postName
	}
	return preName
}

var (
	prelude     *goja.Program
	preludeErr  error
	preludeOnce sync.Once
)

// The prelude is compiled once: it is the same file for every run, and a collection of fifty
// requests would otherwise parse it fifty times. A Program stays safe to run in many runtimes.
func preludeProgram() (*goja.Program, error) {
	preludeOnce.Do(func() { prelude, preludeErr = goja.Compile("prelude.js", preludeSource, false) })
	if preludeErr != nil {
		return nil, fmt.Errorf("the sandbox does not compile: %w", preludeErr)
	}
	return prelude, nil
}
