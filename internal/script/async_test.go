package script

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
)

func TestSendRequestCallback(t *testing.T) {
	sender := &fakeSender{respond: func(*model.Request) (*httpx.Response, error) {
		return jsonResponse(200, `{"token":"t-123"}`), nil
	}}
	h := newHarness(EventPreRequest)
	h.in.Sender = sender
	h.script(`
pm.sendRequest({
    url: 'https://auth.example.com/token',
    method: 'POST',
    header: {'Content-Type': 'application/json'},
    body: {mode: 'raw', raw: JSON.stringify({user: 'u'})}
}, function (err, res) {
    if (err) { console.error('err', err.message); return; }
    pm.environment.set('token', res.json().token);
    console.log('code', res.code, res.status);
});
`)
	res := h.mustRun(t)

	if got := res.log(); got != "log: code 200 OK\n" {
		t.Errorf("console = %q", got)
	}
	if sender.count() != 1 {
		t.Fatalf("sent %d requests", sender.count())
	}
	sent := sender.sent[0]
	if sent.Method != "POST" || sent.URL.Raw != "https://auth.example.com/token" {
		t.Errorf("sent = %s %s", sent.Method, sent.URL.Raw)
	}
	if sent.Body.Mode != model.BodyRaw || sent.Body.Raw != `{"user":"u"}` {
		t.Errorf("body = %+v", sent.Body)
	}
	if v, _ := h.in.Vars.GetIn(3 /* Data */, "token"); v != nil {
		t.Errorf("token leaked into the wrong scope")
	}
	if v, _, ok := h.in.Vars.Get("token"); !ok || v != "t-123" {
		t.Errorf("token = %v (%t)", v, ok)
	}
}

func TestSendRequestPromiseAndAwait(t *testing.T) {
	sender := &fakeSender{respond: func(*model.Request) (*httpx.Response, error) {
		return jsonResponse(201, `{"id":9}`), nil
	}}
	h := newHarness(EventPreRequest)
	h.in.Sender = sender
	h.script(`
const res = await pm.sendRequest('https://api.example.com/things');
console.log('awaited', res.code, res.json().id);
pm.sendRequest('https://api.example.com/other').then(function (r) {
    console.log('thened', r.code);
});
`)
	res := h.mustRun(t)

	want := "log: awaited 201 9\nlog: thened 201\n"
	if res.log() != want {
		t.Errorf("console = %q, want %q", res.log(), want)
	}
}

func TestSendRequestError(t *testing.T) {
	h := newHarness(EventPreRequest)
	h.in.Sender = &fakeSender{failWith: errors.New("dial tcp: connection refused")}
	h.script(`
pm.sendRequest('https://down.example.com', function (err, res) {
    console.log('err?', Boolean(err), err && err.message, res === undefined);
});
`)
	res := h.mustRun(t)

	if got := res.log(); !strings.Contains(got, "connection refused") {
		t.Errorf("console = %q", got)
	}
}

func TestSendRequestWithoutSender(t *testing.T) {
	h := newHarness(EventPreRequest).script(`
pm.sendRequest('https://api.example.com', function (err) { console.log(err.message); });
`)
	res := h.mustRun(t)
	if !strings.Contains(res.log(), "no HTTP sender was configured") {
		t.Errorf("console = %q", res.log())
	}
}

func TestAsyncTestWithDoneCallback(t *testing.T) {
	h := newHarness(EventTest)
	h.in.Response = jsonResponse(200, `{}`)
	h.in.Sender = &fakeSender{delay: 5 * time.Millisecond}
	h.script(`
pm.test('async done passes', function (done) {
    pm.sendRequest('https://api.example.com/ping', function (err, res) {
        pm.expect(res.code).to.eql(200);
        done();
    });
});
pm.test('async done fails', function (done) {
    setTimeout(function () { done(new Error('nope')); }, 1);
});
pm.test('async promise', function () {
    return new Promise(function (resolve) { setTimeout(resolve, 1); });
});
`)
	res := h.mustRun(t)

	if len(res.Tests) != 3 {
		t.Fatalf("got %d tests: %+v", len(res.Tests), res.Tests)
	}
	byName := map[string]TestResult{}
	for _, tr := range res.Tests {
		byName[tr.Name] = tr
	}
	if tr := byName["async done passes"]; !tr.Passed || !tr.Async {
		t.Errorf("passes = %+v", tr)
	}
	if tr := byName["async done fails"]; tr.Passed {
		t.Errorf("fails = %+v", tr)
	}
	if tr := byName["async promise"]; !tr.Passed || !tr.Async {
		t.Errorf("promise = %+v", tr)
	}
}

func TestTimers(t *testing.T) {
	h := newHarness(EventPreRequest).script(`
setTimeout(function () { console.log('later'); }, 5);
var n = 0;
var id = setInterval(function () {
    n++;
    console.log('tick', n);
    if (n === 3) { clearInterval(id); }
}, 1);
var cancelled = setTimeout(function () { console.log('never'); }, 2);
clearTimeout(cancelled);
console.log('now');
`)
	res := h.mustRun(t)

	got := res.log()
	if !strings.HasPrefix(got, "log: now\n") {
		t.Errorf("synchronous log should come first:\n%s", got)
	}
	for _, want := range []string{"log: tick 1", "log: tick 3", "log: later"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "never") {
		t.Errorf("cleared timeout still fired:\n%s", got)
	}
}

func TestTimeoutInterruptsRunawayLoop(t *testing.T) {
	h := newHarness(EventPreRequest)
	h.in.Timeout = 150 * time.Millisecond
	h.script(`
console.log('start');
while (true) { /* spin */ }
`)
	start := time.Now()
	res := h.run(t)
	elapsed := time.Since(start)

	if !res.TimedOut {
		t.Errorf("TimedOut = false")
	}
	if res.Err == nil || !strings.Contains(res.Err.Message, "timed out") {
		t.Errorf("err = %v", res.Err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %s to interrupt", elapsed)
	}
	if !strings.Contains(res.log(), "start") {
		t.Errorf("console output before the hang was lost")
	}
}

func TestTimeoutWithPendingTimer(t *testing.T) {
	h := newHarness(EventPreRequest)
	h.in.Timeout = 120 * time.Millisecond
	h.script(`setInterval(function () {}, 5);`)

	res := h.run(t)
	if !res.TimedOut {
		t.Errorf("TimedOut = false")
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(60 * time.Millisecond); cancel() }()

	res, err := Run(ctx, []*model.Script{{Source: `while (true) {}`}}, Input{
		Event:   EventPreRequest,
		Request: model.NewRequest("R", "GET", "https://x.example"),
		Vars:    newStore(),
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.TimedOut || res.Err == nil {
		t.Errorf("res = %+v", res)
	}
}

func TestPendingAsyncTestIsReportedAsFailed(t *testing.T) {
	h := newHarness(EventTest)
	h.in.Response = jsonResponse(200, `{}`)
	h.script(`
pm.test('never finishes', function (done) { /* done is never called */ });
`)
	res := h.mustRun(t)

	if len(res.Tests) != 1 || res.Tests[0].Passed {
		t.Fatalf("tests = %+v", res.Tests)
	}
	if !strings.Contains(res.Tests[0].Error, "did not finish") {
		t.Errorf("error = %q", res.Tests[0].Error)
	}
}
