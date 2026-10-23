package script

import (
	"fmt"
	"time"

	"github.com/dop251/goja"
)

// What a script is given to work with. The time limit matters most: the sandbox has no network or
// timers, so an interruption is the only thing that ends a runaway loop. The other limits cap what
// comes back out (goja has no memory limit), so a runaway script cannot fill the report.
const (
	maxScriptDuration = 5 * time.Second
	maxLogLines       = 200
	maxLogLineRunes   = 4000
	maxTests          = 200
)

// limitScript stops a script that is still running when the time is up, and returns the call that
// stops the timer, so a run that finished in two milliseconds does not leave one armed.
//
// The interruption is not catchable from inside: a script cannot swallow its own deadline with
// try/catch.
func limitScript(vm *goja.Runtime, within time.Duration) func() {
	timer := time.AfterFunc(within, func() { vm.Interrupt(tooLong(within)) })
	return func() { timer.Stop() }
}

// tooLong is what a script that ran out of time is told, and what its report says. One wording for
// both, because the script cannot see one and the reader the other.
func tooLong(within time.Duration) string {
	return fmt.Sprintf("the script ran for longer than %g s", within.Seconds())
}
