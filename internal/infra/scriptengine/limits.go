package scriptengine

import (
	"fmt"
	"time"

	"github.com/dop251/goja"
)

// What a script is given to work with. The time limit is the one that matters: there is no network
// and no timer in the sandbox, so an interruption is the only thing that can end a runaway loop.
// The rest are not a memory limit — goja has none — but a cap on what comes back out, so a runaway
// script cannot fill the report the tab draws.
const (
	maxScriptDuration = 5 * time.Second
	maxLogLines       = 200
	maxLogLineRunes   = 4000
	maxTests          = 200
)

// limitScript stops a script that is still running when the time is up, and answers with the call
// that stops the timer — a run that finished in two milliseconds has no business leaving one armed.
//
// The interruption is not catchable from inside: a script cannot swallow its own deadline with
// try/catch, which is the whole reason this works.
func limitScript(vm *goja.Runtime, within time.Duration) func() {
	timer := time.AfterFunc(within, func() { vm.Interrupt(tooLong(within)) })
	return func() { timer.Stop() }
}

// tooLong is what a script that ran out of time is told, and what its report says. One wording for
// both, because the script cannot see one and the reader the other.
func tooLong(within time.Duration) string {
	return fmt.Sprintf("the script ran for longer than %g s", within.Seconds())
}
