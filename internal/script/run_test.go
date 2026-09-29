package script

import (
	"context"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

type harness struct {
	in     Input
	source []string
}

func newHarness(event Event) *harness {
	return &harness{in: Input{
		Event:   event,
		Request: model.NewRequest("Example", "GET", "https://api.example.com/v1/things?page=1"),
		Vars:    newStore(),
		Info:    Info{RequestName: "Example", RequestID: "req-1", CollectionName: "Demo", IterationCount: 1},
	}}
}

func (h *harness) script(src string) *harness {
	h.source = append(h.source, src)
	return h
}

func (h *harness) run(t *testing.T) *Result {
	t.Helper()
	scripts := make([]*model.Script, 0, len(h.source))
	for _, s := range h.source {
		scripts = append(scripts, &model.Script{Source: s})
	}
	res, err := Run(context.Background(), scripts, h.in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// mustRun fails the test if the script raised a ScriptError.
func (h *harness) mustRun(t *testing.T) *Result {
	t.Helper()
	res := h.run(t)
	if res.Err != nil {
		t.Fatalf("script error: %v\n%s", res.Err, res.Err.Stack)
	}
	return res
}

func (r *Result) log() string {
	var b strings.Builder
	for _, e := range r.Console {
		b.WriteString(string(e.Level))
		b.WriteString(": ")
		b.WriteString(e.Message)
		b.WriteString("\n")
	}
	return b.String()
}

func TestConsoleCapture(t *testing.T) {
	h := newHarness(EventPreRequest).script(`
console.log('hello', 42, true);
console.warn({a: 1, b: ['x', 'y']});
console.error(new Error('boom').message);
console.debug(null, undefined);
`)
	res := h.mustRun(t)

	want := []struct {
		level ConsoleLevel
		msg   string
	}{
		{LevelLog, "hello 42 true"},
		{LevelWarn, "{ a: 1, b: [ 'x', 'y' ] }"},
		{LevelError, "boom"},
		{LevelDebug, "null undefined"},
	}
	if len(res.Console) != len(want) {
		t.Fatalf("got %d entries, want %d:\n%s", len(res.Console), len(want), res.log())
	}
	for i, w := range want {
		if res.Console[i].Level != w.level || res.Console[i].Message != w.msg {
			t.Errorf("entry %d = %s/%q, want %s/%q", i, res.Console[i].Level, res.Console[i].Message, w.level, w.msg)
		}
	}
}

func TestVariableScopes(t *testing.T) {
	store := newStore()
	h := newHarness(EventPreRequest)
	h.in.Vars = store
	h.script(`
pm.environment.set('token', 'abc');
pm.globals.set('g', 1);
pm.collectionVariables.set('c', true);
pm.variables.set('local', 'yes');
console.log(pm.environment.get('token'), pm.globals.get('g'), pm.collectionVariables.get('c'));
console.log(pm.variables.get('token'), pm.variables.get('local'));
console.log(pm.environment.has('token'), pm.environment.has('nope'));
pm.environment.unset('token');
console.log(pm.environment.has('token'));
console.log(JSON.stringify(pm.globals.toObject()));
`)
	res := h.mustRun(t)

	want := "log: abc 1 true\nlog: abc yes\nlog: true false\nlog: false\nlog: {\"g\":1}\n"
	if res.log() != want {
		t.Errorf("console =\n%q\nwant\n%q", res.log(), want)
	}
}

func TestPmTests(t *testing.T) {
	h := newHarness(EventTest)
	h.in.Response = jsonResponse(200, `{"data":{"id":7,"tags":["a","b"]}}`)
	h.script(`
pm.test('status is 200', function () {
    pm.response.to.have.status(200);
});
pm.test('json body path', function () {
    pm.expect(pm.response.json().data.id).to.eql(7);
    pm.expect(pm.response.json().data.tags).to.be.an('array').that.is.not.empty;
});
pm.test('fails', function () {
    pm.expect(1).to.eql(2);
});
pm.test.skip('skipped');
`)
	res := h.mustRun(t)

	if len(res.Tests) != 4 {
		t.Fatalf("got %d tests: %+v", len(res.Tests), res.Tests)
	}
	if !res.Tests[0].Passed || !res.Tests[1].Passed {
		t.Errorf("first two tests should pass: %+v", res.Tests[:2])
	}
	if res.Tests[2].Passed {
		t.Errorf("third test should fail")
	}
	if !strings.Contains(res.Tests[2].Error, "AssertionError") {
		t.Errorf("error = %q, want an AssertionError", res.Tests[2].Error)
	}
	if !res.Tests[3].Skipped || !res.Tests[3].Passed {
		t.Errorf("skipped test = %+v", res.Tests[3])
	}
	if res.Passed() {
		t.Errorf("Passed() should be false")
	}
}

func TestRequestMutation(t *testing.T) {
	h := newHarness(EventPreRequest)
	h.script(`
pm.request.headers.add({key: 'X-Trace', value: 'abc'});
pm.request.headers.upsert({key: 'Accept', value: 'application/json'});
pm.request.method = 'POST';
pm.request.url.addQueryParams('page=2');
pm.request.body = new (Object.getPrototypeOf(pm.request).constructor)({}).body;
`)
	res := h.mustRun(t)

	if res.Request.Method != "POST" {
		t.Errorf("method = %q", res.Request.Method)
	}
	var trace, accept bool
	for _, hd := range res.Request.Headers {
		if hd.Key == "X-Trace" && hd.Value == "abc" {
			trace = true
		}
		if hd.Key == "Accept" && hd.Value == "application/json" {
			accept = true
		}
	}
	if !trace || !accept {
		t.Errorf("headers = %+v", res.Request.Headers)
	}
	if !strings.Contains(res.Request.URL.Raw, "page=1&page=2") {
		t.Errorf("url = %q", res.Request.URL.Raw)
	}
}
