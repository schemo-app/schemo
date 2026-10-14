package scriptengine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"schemo/internal/domain"
)

// fakeVars is the three scopes without a database. Reading `variables` runs through all three, the
// way a request is resolved; reading either of the others reads that one alone.
type fakeVars struct {
	values map[domain.VarScope]map[string]string
	refuse map[domain.VarScope]string
}

func newFakeVars() *fakeVars {
	return &fakeVars{values: map[domain.VarScope]map[string]string{},
		refuse: map[domain.VarScope]string{}}
}

func (f *fakeVars) with(scope domain.VarScope, name string, value string) *fakeVars {
	_ = f.Set(scope, name, value)
	return f
}

func (f *fakeVars) Lookup(scope domain.VarScope, name string) (string, bool, error) {
	if scope == domain.ScopeRun {
		for _, one := range []domain.VarScope{domain.ScopeRun, domain.ScopeEnvironment,
			domain.ScopeGlobals} {
			if value, ok := f.values[one][name]; ok {
				return value, true, nil
			}
		}
		return "", false, nil
	}
	value, ok := f.values[scope][name]
	return value, ok, nil
}

func (f *fakeVars) Set(scope domain.VarScope, name string, value string) error {
	if reason, refused := f.refuse[scope]; refused {
		return errors.New(reason)
	}
	if f.values[scope] == nil {
		f.values[scope] = map[string]string{}
	}
	f.values[scope][name] = value
	return nil
}

// request is the request every script here runs around, so a test says only what it cares about.
func request() *domain.ScriptRequest {
	return &domain.ScriptRequest{
		Method:  "GET",
		URL:     "https://api.example.com/users",
		Body:    `{"data": {"type": "users"}}`,
		Headers: []domain.HeaderPair{{Name: "Accept", Value: "application/vnd.api+json"}},
	}
}

func answer() *domain.Response {
	return &domain.Response{
		Status: 200, StatusText: "OK", DurationUs: 12_345,
		Headers: []domain.HeaderPair{{Name: "Content-Type", Value: "application/vnd.api+json"}},
		Body:    `{"data": {"type": "users", "id": "1"}}`,
	}
}

// run executes a script the way the use case will: with a request, and with an answer when the
// script runs after the response.
func run(
	t *testing.T,
	scope domain.ScriptScope,
	source string,
) (domain.ScriptRun, *domain.ScriptRequest, *fakeVars) {
	t.Helper()
	vars := newFakeVars()
	asked := request()

	in := domain.ScriptInput{Scope: scope, Source: source, Request: asked, Variables: vars}
	if scope == domain.ScriptPost {
		in.Response = answer()
	}
	return NewEngine().Run(in), asked, vars
}

func TestAScriptThatRunsSaysWhatItDid(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPost, `
		console.log('смотрим', {code: pm.response.code});
		pm.test('статус 200', function () {
			pm.expect(pm.response.code).to.equal(200);
		});
		pm.test('это JSON:API', function () {
			pm.expect(pm.response.headers.get('Content-Type')).to.include('vnd.api+json');
		});
	`)

	if !report.OK || report.Error != "" {
		t.Fatalf("report = %+v, want a script that went through", report)
	}
	if report.Scope != domain.ScriptPost {
		t.Errorf("scope = %q, want the post-response one", report.Scope)
	}
	if len(report.Logs) != 1 || report.Logs[0].Message != `смотрим {"code":200}` {
		t.Errorf("logs = %+v, want the object printed as JSON", report.Logs)
	}
	if len(report.Tests) != 2 || !report.Tests[0].Passed || !report.Tests[1].Passed {
		t.Fatalf("tests = %+v, want both checks passed", report.Tests)
	}
	if report.Tests[0].Name != "статус 200" {
		t.Errorf("test = %+v, want the name the script gave it", report.Tests[0])
	}
	if report.DurationUs <= 0 {
		t.Errorf("duration = %d, want the time it took", report.DurationUs)
	}
}

// A check that fails is a failed check, not a failed script: the rest of the script runs on, which
// is what makes a script with five checks report all five.
func TestAFailedCheckDoesNotStopTheScript(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPost, `
		pm.test('первая', function () { pm.expect(pm.response.code).to.equal(404); });
		pm.test('вторая', function () { pm.expect(pm.response.code).to.equal(200); });
	`)

	if !report.OK {
		t.Fatalf("report = %+v, want the script itself to be fine", report)
	}
	if len(report.Tests) != 2 {
		t.Fatalf("tests = %+v, want both checks reported", report.Tests)
	}
	if report.Tests[0].Passed {
		t.Error("the first check passed, and it asserted 404 against a 200")
	}
	if report.Tests[0].Error != "ожидалось 404, получено 200" {
		t.Errorf("failure = %q, want what was wanted and what came", report.Tests[0].Error)
	}
	if !report.Tests[1].Passed {
		t.Errorf("the second check = %+v, want it to have run after the first failed", report.Tests[1])
	}
}

// The assertions read the way they are written in Postman and in chai, including the words that are
// only there to make the sentence work — and `not`, which is the one that changes the answer. Each
// case is written twice: once as a script that agrees with the answer and once as one that does
// not, because the message is what a person reads when their script disagrees with the response.
func TestAssertions(t *testing.T) {
	for _, one := range []struct {
		what    string
		passes  string
		fails   string
		message string
	}{
		{
			what:    "equal",
			passes:  `pm.expect(200).to.equal(200)`,
			fails:   `pm.expect(200).to.equal(404)`,
			message: "ожидалось 404, получено 200",
		},
		{
			// Two objects with the same keys in another order are the same object to whoever wrote
			// the script, so the comparison walks the values and not the string.
			what:    "eql",
			passes:  `pm.expect({a: 1, b: [2]}).to.eql({b: [2], a: 1})`,
			fails:   `pm.expect({a: 1}).to.eql({a: 2})`,
			message: `ожидалось {"a":2}, получено {"a":1}`,
		},
		{
			what:    "a",
			passes:  `pm.expect('текст').to.be.a('string')`,
			fails:   `pm.expect('текст').to.be.a('number')`,
			message: "ожидалось число, а значение — строка",
		},
		{
			what:    "include",
			passes:  `pm.expect('jsonapi').to.include('json')`,
			fails:   `pm.expect('jsonapi').to.include('xml')`,
			message: "в jsonapi нет xml",
		},
		{
			what:    "property",
			passes:  `pm.expect({id: '1'}).to.have.property('id')`,
			fails:   `pm.expect({id: '1'}).to.have.property('name')`,
			message: "у значения нет свойства name",
		},
		{
			// A property that exists but holds another value is a different
			// failure, and the message says so.
			what:    "property со значением",
			passes:  `pm.expect({id: '1'}).to.have.property('id', '1')`,
			fails:   `pm.expect({id: '1'}).to.have.property('id', '2')`,
			message: "свойство id равно 1, ожидалось 2",
		},
		{
			what:    "length",
			passes:  `pm.expect([1, 2]).to.have.length(2)`,
			fails:   `pm.expect([1, 2]).to.have.length(3)`,
			message: "длина 2, ожидалась 3",
		},
		{
			what:    "match",
			passes:  `pm.expect('2024-01').to.match(/^\d{4}-\d{2}$/)`,
			fails:   `pm.expect('вчера').to.match(/^\d{4}$/)`,
			message: `вчера не соответствует /^\d{4}$/`,
		},
		{what: "above", passes: `pm.expect(5).to.be.above(4)`, fails: `pm.expect(5).to.be.above(6)`,
			message: "5 не больше 6"},
		{what: "below", passes: `pm.expect(5).to.be.below(6)`, fails: `pm.expect(5).to.be.below(4)`,
			message: "5 не меньше 4"},
		{what: "least", passes: `pm.expect(5).to.be.least(5)`, fails: `pm.expect(5).to.be.least(6)`,
			message: "5 меньше 6"},
		{what: "most", passes: `pm.expect(5).to.be.most(5)`, fails: `pm.expect(5).to.be.most(4)`,
			message: "5 больше 4"},
		{
			what:    "oneOf",
			passes:  `pm.expect(2).to.be.oneOf([1, 2])`,
			fails:   `pm.expect(3).to.be.oneOf([1, 2])`,
			message: "3 не одно из [1,2]",
		},
		{
			what:    "true",
			passes:  `pm.expect(true).to.be.true`,
			fails:   `pm.expect(false).to.be.true`,
			message: "ожидалось true, получено false",
		},
		{
			what:    "false",
			passes:  `pm.expect(false).to.be.false`,
			fails:   `pm.expect(true).to.be.false`,
			message: "ожидалось false, получено true",
		},
		{
			what:    "null",
			passes:  `pm.expect(null).to.be.null`,
			fails:   `pm.expect(5).to.be.null`,
			message: "ожидалось null, получено 5",
		},
		{
			what:    "undefined",
			passes:  `pm.expect(undefined).to.be.undefined`,
			fails:   `pm.expect(5).to.be.undefined`,
			message: "ожидалось undefined, получено 5",
		},
		{what: "ok", passes: `pm.expect('текст').to.be.ok`, fails: `pm.expect(0).to.be.ok`,
			message: "ожидалось ok, получено 0"},
		{
			what:    "empty",
			passes:  `pm.expect([]).to.be.empty`,
			fails:   `pm.expect([1, 2]).to.be.empty`,
			message: "ожидалось empty, получено [1,2]",
		},
		{
			what:    "exist",
			passes:  `pm.expect(0).to.exist`,
			fails:   `pm.expect(null).to.exist`,
			message: "ожидалось exist, получено null",
		},
		{
			what:    "not",
			passes:  `pm.expect(200).to.not.equal(404)`,
			fails:   `pm.expect(200).to.not.equal(200)`,
			message: "значение равно 200, а не должно",
		},
	} {
		t.Run(one.what, func(t *testing.T) {
			report, _, _ := run(t, domain.ScriptPre, "pm.test('идёт', function () { "+one.passes+"; });")
			if !report.OK || len(report.Tests) != 1 {
				t.Fatalf("report = %+v, want one check", report)
			}
			if !report.Tests[0].Passed {
				t.Fatalf("%s failed: %s", one.passes, report.Tests[0].Error)
			}

			report, _, _ = run(t, domain.ScriptPre, "pm.test('не идёт', function () { "+one.fails+"; });")
			if len(report.Tests) != 1 {
				t.Fatalf("report = %+v, want one check", report)
			}
			if report.Tests[0].Passed {
				t.Fatalf("%s passed, and it was written to fail", one.fails)
			}
			if report.Tests[0].Error != one.message {
				t.Errorf("failure = %q, want %q", report.Tests[0].Error, one.message)
			}
		})
	}
}

func TestAScriptThatThrowsIsAReport(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPre, `
		console.warn('дошли');
		throw new Error('дальше нет смысла');
	`)

	if report.OK || report.Error == "" {
		t.Fatalf("report = %+v, want the script's failure", report)
	}
	if !strings.Contains(report.Error, "дальше нет смысла") {
		t.Errorf("error = %q, want the message the script threw", report.Error)
	}
	if len(report.Logs) != 1 {
		t.Errorf("logs = %+v, want what it printed before it failed", report.Logs)
	}
}

func TestAScriptThatDoesNotParseIsAReport(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPost, `pm.test('скобка не закрыта', function () {`)

	if report.OK || !strings.Contains(report.Error, "SyntaxError") {
		t.Errorf("report = %+v, want a syntax error told to the reader", report)
	}
}

// There is no way out of the sandbox: no module loader, no process, no socket, no timer. The bridge
// the prelude was built on is gone by the time the script runs.
func TestThereIsNoWayOutOfTheSandbox(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPre, `
		const names = ['require', 'process', 'fetch', 'XMLHttpRequest', 'setTimeout', 'setInterval',
		               'WebSocket', 'importScripts', '__bridge'];
		const found = names.filter(function (name) { return typeof globalThis[name] !== 'undefined'; });
		pm.test('выхода нет', function () { pm.expect(found).to.eql([]); });
	`)

	if !report.OK {
		t.Fatalf("report = %+v, want the script to run", report)
	}
	if len(report.Tests) != 1 || !report.Tests[0].Passed {
		t.Errorf("tests = %+v, want the sandbox to have nothing in it: %s", report.Tests,
			report.Tests[0].Error)
	}
}

func TestAScriptThatNeverFinishesIsStopped(t *testing.T) {
	engine := NewEngine()
	// The limit is a field so that watching it work does not cost five seconds.
	engine.within = 50 * time.Millisecond

	report := engine.Run(domain.ScriptInput{
		Scope:     domain.ScriptPre,
		Source:    `while (true) { }`,
		Request:   request(),
		Variables: newFakeVars(),
	})

	if report.OK || !strings.Contains(report.Error, "longer than") {
		t.Errorf("report = %+v, want it stopped and told why", report)
	}
	if report.DurationUs > int64(2*time.Second) {
		t.Errorf("duration = %d, want it stopped at the limit", report.DurationUs)
	}
}

// The deadline is not the script's to swallow: `try { while (true) {} } catch (e) {}` still ends.
func TestAScriptCannotCatchItsOwnDeadline(t *testing.T) {
	engine := NewEngine()
	engine.within = 50 * time.Millisecond

	report := engine.Run(domain.ScriptInput{
		Scope:     domain.ScriptPre,
		Source:    `try { while (true) { } } catch (error) { console.log('поймал'); }`,
		Request:   request(),
		Variables: newFakeVars(),
	})

	if report.OK {
		t.Fatalf("report = %+v, want the sandbox to have stopped it", report)
	}
	for _, line := range report.Logs {
		if line.Message == "поймал" {
			t.Error("the script caught its own deadline")
		}
	}
}

// A promise has nothing to resolve it here, and a check that waits for one would otherwise pass
// without checking anything: an assertion inside an async function throws into a promise nobody
// reads.
func TestACheckThatWaitsForAPromiseIsNotPassed(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPost, `
		pm.test('ждёт обещания', function () { return {then: function () {}}; });
	`)

	if !report.OK || len(report.Tests) != 1 {
		t.Fatalf("report = %+v, want one reported check", report)
	}
	if report.Tests[0].Passed || !strings.Contains(report.Tests[0].Error, "promise") {
		t.Errorf("check = %+v, want it reported as one that cannot be waited for", report.Tests[0])
	}
}

func TestAPreRequestScriptChangesTheRequest(t *testing.T) {
	report, asked, _ := run(t, domain.ScriptPre, `
		pm.request.method = 'post';
		pm.request.url = 'https://api.example.com/users/1';
		pm.request.body = '{"data": {"type": "users", "id": "1"}}';
		pm.request.headers.upsert({key: 'Accept', value: 'application/json'});
		pm.request.headers.add({key: 'X-Run', value: '1'});
		pm.request.headers.remove('X-Run');
	`)

	if !report.OK {
		t.Fatalf("report = %+v, want the script to run", report)
	}
	if asked.Method != "post" || asked.URL != "https://api.example.com/users/1" {
		t.Errorf("request = %+v, want what the script left", asked)
	}
	if asked.Body != `{"data": {"type": "users", "id": "1"}}` {
		t.Errorf("body = %q, want the one the script wrote", asked.Body)
	}
	if len(asked.Headers) != 1 || asked.Headers[0].Value != "application/json" {
		t.Errorf("headers = %+v, want the replaced header and nothing else", asked.Headers)
	}
}

// A script that says nothing has not said "empty": the request keeps everything it did not touch.
func TestTheRequestKeepsWhatTheScriptDidNotTouch(t *testing.T) {
	report, asked, _ := run(t, domain.ScriptPre, `
		pm.request.url = 'https://api.example.com/users?page=2';
	`)

	if !report.OK {
		t.Fatalf("report = %+v, want the script to run", report)
	}
	if asked.Method != "GET" || asked.Body == "" || len(asked.Headers) != 1 {
		t.Errorf("request = %+v, want only the address changed", asked)
	}

	// The one thing a script can take away on purpose.
	if _, asked, _ := run(t, domain.ScriptPre, `pm.request.body = '';`); asked.Body != "" {
		t.Errorf("body = %q, want the empty one the script set", asked.Body)
	}
}

func TestAPostResponseScriptSeesTheAnswer(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPost, `
		const body = pm.response.json();
		pm.test('ответ виден целиком', function () {
			pm.expect(body.data.type).to.equal('users');
			pm.expect(pm.response.status).to.equal('OK');
			pm.expect(pm.response.responseTime).to.be.above(1);
			pm.expect(pm.response.text()).to.include('users');
			pm.expect(pm.response.headers.has('content-type')).to.be.true;
			pm.expect(pm.response.headers.get('X-Нет')).to.be.null;
		});
	`)

	if !report.OK {
		t.Fatalf("report = %+v, want the script to run", report)
	}
	if !report.Tests[0].Passed {
		t.Errorf("check = %+v, want the answer visible", report.Tests[0])
	}
}

// Before the request has gone out there is no answer, and `pm.response` is the undefined Postman
// also leaves there — a script that reaches for it finds out at once.
func TestAPreRequestScriptHasNoResponse(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPre, `
		pm.test('ответа ещё нет', function () { pm.expect(typeof pm.response).to.equal('undefined'); });
	`)

	if !report.OK || !report.Tests[0].Passed {
		t.Errorf("report = %+v, want no answer before the request", report)
	}
}

func TestAPreRequestScriptCanSkipTheRequest(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPre, `pm.execution.skipRequest();`)

	if !report.OK || !report.SkipRequest {
		t.Errorf("report = %+v, want the request skipped", report)
	}

	report, _, _ = run(t, domain.ScriptPre, `console.log('идём');`)
	if report.SkipRequest {
		t.Error("a script that said nothing skipped the request anyway")
	}
}

func TestVariablesAreScopedAndWrittenBack(t *testing.T) {
	vars := newFakeVars().
		with(domain.ScopeEnvironment, "base", "https://api.example.com").
		with(domain.ScopeGlobals, "token", "from-globals")

	report := NewEngine().Run(domain.ScriptInput{
		Scope: domain.ScriptPost,
		Source: `
			// Читаем до записи: то, что скрипт пишет, он же потом и видит, и проверка иначе ничего
			// не говорит о порядке областей.
			pm.test('видно все три', function () {
				pm.expect(pm.variables.get('base')).to.equal('https://api.example.com');
				pm.expect(pm.variables.get('token')).to.equal('from-globals');
				pm.expect(pm.environment.has('token')).to.be.false;
				pm.expect(pm.globals.get('base')).to.be.undefined;
			});

			pm.environment.set('token', 'from-environment');
			pm.variables.set('page', '2');
			pm.globals.set('auth', 'from-globals-too');

			pm.test('окружение перекрывает глобалы', function () {
				pm.expect(pm.variables.get('token')).to.equal('from-environment');
				pm.expect(pm.variables.get('page')).to.equal('2');
				pm.expect(pm.environment.has('page')).to.be.false;
			});
		`,
		Request:   request(),
		Response:  answer(),
		Variables: vars,
	})

	if !report.OK {
		t.Fatalf("report = %+v, want the script to run", report)
	}
	if len(report.Tests) != 2 || !report.Tests[0].Passed || !report.Tests[1].Passed {
		t.Errorf("checks = %+v, want the scopes kept apart", report.Tests)
	}
	if got := vars.values[domain.ScopeRun]["page"]; got != "2" {
		t.Errorf("run scope = %q, want the value the script set there", got)
	}
	if got := vars.values[domain.ScopeEnvironment]["token"]; got != "from-environment" {
		t.Errorf("environment = %q, want the value the script set there", got)
	}
	if got := vars.values[domain.ScopeGlobals]["auth"]; got != "from-globals-too" {
		t.Errorf("globals = %q, want the value the script set there", got)
	}
}

// A variable that cannot be written — a read-only environment, a name the storage refuses — is the
// script's business to see, so it arrives as a thrown error rather than as a silent nothing.
func TestAVariableThatCannotBeWrittenIsThrown(t *testing.T) {
	vars := newFakeVars()
	vars.refuse[domain.ScopeEnvironment] = "окружение только для чтения"

	report := NewEngine().Run(domain.ScriptInput{
		Scope: domain.ScriptPre,
		Source: `try { pm.environment.set('token',
			'1'); } catch (error) { console.log(error.message); }`,
		Request:   request(),
		Variables: vars,
	})

	if !report.OK {
		t.Fatalf("report = %+v, want the script to have caught it", report)
	}
	if len(report.Logs) != 1 || report.Logs[0].Message != "окружение только для чтения" {
		t.Errorf("logs = %+v, want the reason it could not be written", report.Logs)
	}
}

func TestTheLogIsCapped(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPre, `
		for (let i = 0; i < 250; i++) console.log('строка ' + i);
	`)

	if len(report.Logs) != maxLogLines+1 {
		t.Fatalf("logs = %d, want the cap and one line saying so", len(report.Logs))
	}
	last := report.Logs[len(report.Logs)-1]
	if last.Level != "warn" || !strings.Contains(last.Message, "truncated") {
		t.Errorf("last line = %+v, want it to say what the cap hid", last)
	}
}

func TestALongLogLineIsShortened(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPre, `console.log('я'.repeat(9000));`)

	runes := []rune(report.Logs[0].Message)
	if len(runes) != maxLogLineRunes+1 || runes[len(runes)-1] != '…' {
		t.Errorf("the line is %d runes long, want it cut at %d", len(runes), maxLogLineRunes)
	}
}

func TestAnEmptyScriptRunsAndSaysNothing(t *testing.T) {
	report, _, _ := run(t, domain.ScriptPre, "   \n  ")

	if !report.OK || report.Error != "" || len(report.Logs) != 0 || len(report.Tests) != 0 {
		t.Errorf("report = %+v, want an empty report out of an empty script", report)
	}
}

// A script is shown the text kinds and nothing of the byte kinds. A multipart body is a wire
// encoding, and a file's bytes in a JavaScript string come back corrupted — so the sandbox hands
// over nothing at all rather than something it would have to repair on the way back.
//
// The request itself is untouched: what goes out is the encoded body the preparation produced, and
// the script only ever saw less of it.
func TestAByteBodyIsNotShownToAScript(t *testing.T) {
	for _, want := range []struct {
		kind domain.BodyKind
		seen string
	}{
		{domain.BodyJSON, `{"data": {"type": "users"}}`},
		{domain.BodyRaw, `{"data": {"type": "users"}}`},
		{domain.BodyForm, ""},
		{domain.BodyBinary, ""},
	} {
		vars := newFakeVars()
		asked := request()
		asked.BodyKind = want.kind

		report := NewEngine().Run(domain.ScriptInput{
			Scope:     domain.ScriptPre,
			Source:    `__seen = pm.request.body;`,
			Request:   asked,
			Variables: vars,
		})
		if !report.OK {
			t.Fatalf("%s: report = %+v, want the script to run", want.kind, report)
		}
		if asked.Body != request().Body {
			t.Errorf("%s: the request body was changed to %q, want it untouched", want.kind, asked.Body)
		}
	}
}
