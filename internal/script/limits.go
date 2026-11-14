package script

import (
	"fmt"
	"time"

	"github.com/dop251/goja"
)

// Script limits. The deadline is the only way to end a runaway loop (no timers or network); the
// others cap the report, since goja has no memory limit.
const (
	maxScriptDuration = 5 * time.Second
	maxLogLines       = 200
	maxLogLineRunes   = 4000
	maxTests          = 200
)

// limitScript interrupts a script past its deadline (uncatchable from inside) and returns the call
// that disarms the timer.
func limitScript(vm *goja.Runtime, within time.Duration) func() {
	timer := time.AfterFunc(within, func() { vm.Interrupt(tooLong(within)) })
	return func() { timer.Stop() }
}

// tooLong is what a script that ran out of time is told, and what its report says. One wording for
// both, because the script cannot see one and the reader the other.
func tooLong(within time.Duration) string {
	return fmt.Sprintf("the script ran for longer than %g s", within.Seconds())
}
