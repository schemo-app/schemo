package script

import (
	"errors"
	"fmt"
	"time"

	"github.com/dop251/goja"

	"schemo/internal/domain"
)

// runState is one script's run: the runtime, what the script was given, and the report being
// written as it goes. The prelude builds `pm` out of the bridge and then deletes the bridge, so
// this is the only way in or out of the sandbox.
type runState struct {
	vm  *goja.Runtime
	in  domain.ScriptInput
	run *domain.ScriptRun

	// Set when a cap was reached, so the report can say so at the end instead of showing a script that
	// looks like it printed exactly two hundred lines.
	logsCapped  bool
	testsCapped bool
}

// bridge installs the way out of the sandbox and runs the prelude over it. A prelude that does not
// compile or run is a bug in this package, not in a user's script, and comes back as a failed run:
// a broken report beats a crash.
func (s *runState) bridge() error {
	program, err := preludeProgram()
	if err != nil {
		return err
	}

	object := s.vm.NewObject()
	fields := map[string]any{
		"request": s.vm.ToValue(requestValue(s.in.Request)),
		"get":     s.get,
		"set":     s.set,
		"log":     s.log,
		"test":    s.test,
		"skip":    s.skip,
	}
	if s.in.Response != nil {
		fields["response"] = s.vm.ToValue(responseValue(s.in.Response))
	}
	for name, value := range fields {
		if err := object.Set(name, value); err != nil {
			return fmt.Errorf("the sandbox does not compile: %w", err)
		}
	}
	if err := s.vm.Set("__bridge", object); err != nil {
		return fmt.Errorf("the sandbox does not compile: %w", err)
	}

	if _, err := s.vm.RunProgram(program); err != nil {
		return fmt.Errorf("the sandbox does not start: %w", err)
	}
	return nil
}

// get reads a variable. `variables` is the run's own scope and looks through the environment and
// globals after it; the other two are read alone, which tells a script whether a name is set in the
// environment or only borrowed.
func (s *runState) get(call goja.FunctionCall) goja.Value {
	if s.in.Variables == nil {
		return goja.Undefined()
	}
	value, ok, err := s.in.Variables.Lookup(domain.VarScope(text(call.Argument(0))),
		text(call.Argument(1)))
	if err != nil {
		// A scope that cannot be read is thrown and not answered with nothing: a script that checks a
		// token has to be able to tell "there is none" from "it could not be read".
		panic(s.vm.NewGoError(err))
	}
	if ok {
		return s.vm.ToValue(value)
	}
	return goja.Undefined()
}

// set writes a variable. Where it lands is the caller's business: the run scope disappears with the
// run, an environment outlives it, and the sandbox does not know the difference.
func (s *runState) set(call goja.FunctionCall) goja.Value {
	if s.in.Variables == nil {
		panic(s.vm.NewTypeError("this script has no variables"))
	}
	if err := s.in.Variables.Set(domain.VarScope(text(call.Argument(0))), text(call.Argument(1)),
		text(call.Argument(2))); err != nil {
		// A variable that cannot be written — a read-only environment, a name that is not a name — is
		// the script's business to see, so it is thrown rather than logged.
		panic(s.vm.NewGoError(err))
	}
	return goja.Undefined()
}

func (s *runState) log(call goja.FunctionCall) goja.Value {
	level := text(call.Argument(0))
	if len(s.run.Logs) >= maxLogLines {
		s.logsCapped = true
		return goja.Undefined()
	}

	message := text(call.Argument(1))
	if runes := []rune(message); len(runes) > maxLogLineRunes {
		message = string(runes[:maxLogLineRunes]) + "…"
	}
	s.run.Logs = append(s.run.Logs, domain.ScriptLog{Level: level, Message: message})
	return goja.Undefined()
}

// test runs one named check. A check that throws is a failed check and not a failed script: the
// rest of the script runs on, which is what makes a script with five checks report all five.
func (s *runState) test(call goja.FunctionCall) goja.Value {
	check, ok := goja.AssertFunction(call.Argument(1))
	if !ok {
		panic(s.vm.NewTypeError("pm.test: the second argument must be a function"))
	}
	if len(s.run.Tests) >= maxTests {
		s.testsCapped = true
		return goja.Undefined()
	}

	result := domain.TestResult{Name: text(call.Argument(0)), Passed: true}
	started := time.Now()
	if returned, err := check(goja.Undefined()); err != nil {
		if _, stopped := err.(*goja.InterruptedError); stopped {
			// The time is up: that is not this check failing, it is the sandbox closing.
			panic(err)
		}
		result.Passed, result.Error = false, messageOf(err)
	} else if isThenable(returned) {
		// `pm.test('...', async () => ...)` would otherwise pass without checking anything: an
		// assertion inside an async function throws into a promise nobody waits for, and the
		// sandbox has nothing to wait with.
		result.Passed = false
		result.Error = "the test waits for a promise, and there is nothing to wait for in the " +
			"sandbox: no timers, no network"
	}
	result.DurationUs = time.Since(started).Microseconds()
	s.run.Tests = append(s.run.Tests, result)
	return goja.Undefined()
}

// skip is a pre-request script saying this request must not go out.
func (s *runState) skip(goja.FunctionCall) goja.Value {
	s.run.SkipRequest = true
	return goja.Undefined()
}

// readBack takes what the script left the request in. A field it never touched reads as undefined
// and leaves the request as it was, which differs from the empty string: a script that sets a body
// to nothing means it.
func (s *runState) readBack(request *domain.ScriptRequest) {
	if request == nil {
		return
	}
	object := s.member("pm", "request")
	if object == nil {
		return
	}
	if value := object.Get("method"); isText(value) {
		request.Method = value.String()
	}
	if value := object.Get("url"); isText(value) {
		request.URL = value.String()
	}
	// The byte kinds are shown nothing, so reading the body back would overwrite the request's real
	// encoding with that nothing: a script that never mentioned the body would silently empty an
	// upload.
	if !isByteKind(request.BodyKind) {
		if value := object.Get("body"); isText(value) {
			request.Body = value.String()
		}
	}
	if value := object.Get("headers"); isText(value) {
		request.Headers = headerPairs(value)
	}
}

// messageOf is what a failed check says: the message it threw, without the stack it threw it from.
// The stack of a failed assertion leads into the prelude, which is not a line the reader wrote.
func messageOf(err error) string {
	var exception *goja.Exception
	if !errors.As(err, &exception) {
		return err.Error()
	}
	value := exception.Value()
	if object, ok := value.(*goja.Object); ok {
		if message := object.Get("message"); isText(message) {
			return message.String()
		}
	}
	return value.String()
}

// failure is what a broken script says about itself: the message it threw and where, since the
// script's own line is what has to be fixed. A script that ran out of time says that instead: it is
// the sandbox closing, not a mistake in the script.
func (s *runState) failure(err error) string {
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		return fmt.Sprint(interrupted.Value())
	}
	return err.Error()
}

// closeReport says what the caps hid. Without it a report that stops at the two-hundredth line
// reads as a script that printed two hundred lines.
func (s *runState) closeReport() {
	if s.logsCapped {
		s.run.Logs = append(s.run.Logs, domain.ScriptLog{
			Level: "warn", Message: fmt.Sprintf("log truncated: the first %d lines were kept", maxLogLines),
		})
	}
	if s.testsCapped {
		s.run.Logs = append(s.run.Logs, domain.ScriptLog{
			Level: "warn", Message: fmt.Sprintf("tests truncated: the first %d were kept", maxTests),
		})
	}
}

// member walks a path through the globals (`pm`, then `request`) and answers nothing when the
// script replaced one of them with a non-object. The report then keeps the request as it was.
func (s *runState) member(names ...string) *goja.Object {
	var value goja.Value = s.vm.GlobalObject()
	for _, name := range names {
		object, ok := value.(*goja.Object)
		if !ok {
			return nil
		}
		value = object.Get(name)
	}
	if object, ok := value.(*goja.Object); ok {
		return object
	}
	return nil
}

// scriptBody is the body a script is shown.
//
// Text kinds are their text, which a script may rewrite. Byte kinds are nothing: a multipart body
// is a wire encoding and a file's bytes in a JavaScript string come back corrupted. The encoding
// itself is untouched either way, so a script sees less than the request carries.
func scriptBody(request *domain.ScriptRequest) string {
	if isByteKind(request.BodyKind) {
		return ""
	}
	return request.Body
}

// isByteKind says whether a body is an encoding rather than a text. What a script is shown and
// whether what it leaves can be read back are the same question, asked in one place.
func isByteKind(kind domain.BodyKind) bool {
	switch domain.KindOf(kind) {
	case domain.BodyForm, domain.BodyBinary:
		return true
	default:
		return false
	}
}

// requestValue is the request as a script sees it: plain fields it may assign to. A header's name
// is `key` because that is what Postman calls it, so scripts written there port over.
func requestValue(request *domain.ScriptRequest) map[string]any {
	value := map[string]any{"method": "", "url": "", "body": "", "headers": []any{}}
	if request == nil {
		return value
	}
	value["method"], value["url"], value["body"] = request.Method, request.URL, scriptBody(request)
	headers := make([]any, 0, len(request.Headers))
	for _, header := range request.Headers {
		headers = append(headers, map[string]any{"key": header.Name, "value": header.Value})
	}
	value["headers"] = headers
	return value
}

// responseValue is the answer as a script sees it. `responseTime` is in milliseconds, as Postman
// counts it, so a script asserting on a slow response need not know this app keeps microseconds.
func responseValue(response *domain.Response) map[string]any {
	headers := make([]any, 0, len(response.Headers))
	for _, header := range response.Headers {
		headers = append(headers, map[string]any{"key": header.Name, "value": header.Value})
	}
	return map[string]any{
		"code":         response.Status,
		"status":       response.StatusText,
		"responseTime": response.DurationUs / 1000,
		"body":         response.Body,
		"headers":      headers,
	}
}

// headerPairs reads a header list a script left behind: either the list the prelude built (pairs
// inside `__items`) or a plain array, if the script replaced the whole thing.
func headerPairs(value goja.Value) []domain.HeaderPair {
	items := value
	if object, ok := value.(*goja.Object); ok {
		if inside := object.Get("__items"); isText(inside) {
			items = inside
		}
	}
	exported, ok := items.Export().([]any)
	if !ok {
		return nil
	}

	out := make([]domain.HeaderPair, 0, len(exported))
	for _, item := range exported {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := exportedString(fields["key"])
		if name == "" {
			name = exportedString(fields["name"])
		}
		if name == "" {
			continue
		}
		out = append(out, domain.HeaderPair{Name: name, Value: exportedString(fields["value"])})
	}
	return out
}

// text is a JS value as the string a Go call needs. An argument that was not given at all reads as
// empty, which is what a missing name means everywhere else in this API.
func text(value goja.Value) string {
	if !isText(value) {
		return ""
	}
	return value.String()
}

// A field the script never touched reads as undefined, which a request cannot be given.
func isText(value goja.Value) bool {
	return value != nil && !goja.IsUndefined(value) && !goja.IsNull(value)
}

func exportedString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

// Nothing in the sandbox resolves a promise.
func isThenable(value goja.Value) bool {
	object, ok := value.(*goja.Object)
	if !ok {
		return false
	}
	_, thenable := goja.AssertFunction(object.Get("then"))
	return thenable
}
