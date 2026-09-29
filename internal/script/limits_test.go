package script

import (
	"context"
	"github.com/dop251/goja"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

// runBounded runs h and fails the test if Run itself takes longer than max, so
// a regression that hangs fails fast instead of hitting the go test timeout.
// try is run without the t.Fatalf, so it can be used off the test goroutine.
func (h *harness) try() (*Result, error) {
	scripts := make([]*model.Script, 0, len(h.source))
	for _, s := range h.source {
		scripts = append(scripts, &model.Script{Source: s})
	}
	return Run(context.Background(), scripts, h.in)
}

func runBounded(t *testing.T, h *harness, max time.Duration) (*Result, time.Duration) {
	t.Helper()
	type outcome struct {
		res *Result
		d   time.Duration
	}
	ch := make(chan outcome, 1)
	go func() {
		start := time.Now()
		res, err := h.try()
		if err != nil {
			t.Errorf("Run: %v", err)
		}
		ch <- outcome{res, time.Since(start)}
	}()
	select {
	case o := <-ch:
		if o.res == nil {
			t.FailNow()
		}
		if o.d > max {
			t.Errorf("run took %s, want under %s", o.d, max)
		}
		return o.res, o.d
	case <-time.After(max + 10*time.Second):
		t.Fatalf("Run did not return within %s", max+10*time.Second)
		return nil, 0
	}
}

func TestInfiniteLoopVariantsTerminate(t *testing.T) {
	cases := map[string]string{
		"top level":     `while (true) {}`,
		"for ever":      `for (;;) {}`,
		"timer":         `setTimeout(function () { while (true) {} }, 1);`,
		"immediate":     `setImmediate(function () { while (true) {} });`,
		"interval":      `setInterval(function () {}, 1);`,
		"promise":       `Promise.resolve().then(function () { while (true) {} });`,
		"async":         `(async function () { await null; while (true) {} })();`,
		"async test":    `pm.test('spins', async function () { while (true) {} });`,
		"done callback": `pm.test('never', function (done) { setTimeout(function () { while (true) {} }, 1); });`,
		"in eval":       `eval('while (true) {}');`,
		"in Function":   `new Function('while (true) {}')();`,
		"in sort":       `[3, 2, 1].sort(function () { while (true) {} });`,
		"in replace":    `'abc'.replace(/b/, function () { while (true) {} });`,
		"in JSON":       `JSON.parse('[1]', function () { while (true) {} });`,
		"in getter":     `({ get x() { while (true) {} } }).x;`,
		"in toString":   `String({ toString: function () { while (true) {} } });`,
		"in catch":      `while (true) { try { throw 1; } catch (e) {} }`,
		"in finally":    `try { while (true) {} } finally { while (true) {} }`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(EventTest)
			h.in.Response = jsonResponse(200, `{}`)
			h.in.Timeout = 120 * time.Millisecond
			h.script(src)
			res, _ := runBounded(t, h, 2*time.Second)
			if !res.TimedOut {
				t.Errorf("TimedOut = false")
			}
			if res.Err == nil || !strings.Contains(res.Err.Message, "timed out") {
				t.Errorf("err = %v, want a timeout", res.Err)
			}
		})
	}
}

func TestSwallowedInterruptStillTerminates(t *testing.T) {
	h := newHarness(EventPreRequest)
	h.in.Timeout = 120 * time.Millisecond
	// A script cannot catch the interrupt and keep going.
	h.script(`
for (;;) {
    try { while (true) {} } catch (e) { /* ignore */ }
}`)
	res, _ := runBounded(t, h, 2*time.Second)
	if !res.TimedOut {
		t.Errorf("TimedOut = false")
	}
}

func TestUnboundedRecursionIsAScriptError(t *testing.T) {
	h := newHarness(EventPreRequest)
	h.in.Timeout = 5 * time.Second
	h.script(`function f() { return f() + 1; } f();`)
	res, d := runBounded(t, h, 3*time.Second)
	if res.Err == nil || !strings.Contains(res.Err.Message, "call stack") {
		t.Fatalf("err = %v, want a call stack error (took %s)", res.Err, d)
	}
	if res.TimedOut {
		t.Errorf("TimedOut = true, want a plain RangeError")
	}
	if res.Err.Name != "RangeError" {
		t.Errorf("name = %q", res.Err.Name)
	}
}

func TestRunawayAllocationIsStopped(t *testing.T) {
	cases := map[string]string{
		"array push":   `var a = []; while (true) { a.push(new Array(100000).fill(1)); }`,
		"string grow":  `var s = 'xxxxxxxx'; while (true) { s = s + s; }`,
		"object graph": `var o = {}; for (var i = 0; ; i++) { o['k' + i] = { a: new Array(1000).fill(i) }; }`,
		"recursion":    `var keep = []; function f(n) { keep.push(new Array(1000).fill(n)); f(n + 1); } f(0);`,
		"in a timer":   `var a = []; setTimeout(function () { while (true) { a.push(new Array(100000).fill(1)); } }, 1);`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(EventPreRequest)
			h.in.Timeout = 30 * time.Second // the memory limit, not the clock, must end it
			h.in.MemoryLimit = 8 << 20
			h.script(src)
			res, _ := runBounded(t, h, 20*time.Second)
			if res.Err == nil {
				t.Fatalf("no script error")
			}
			// Deep recursion can hit the call depth cap first; both are clear errors.
			if !strings.Contains(res.Err.Message, "memory limit") && !strings.Contains(res.Err.Message, "call stack") {
				t.Errorf("err = %q, want a memory limit error", res.Err.Message)
			}
			if res.TimedOut {
				t.Errorf("TimedOut = true for a memory stop")
			}
		})
	}
}

func TestMemoryLimitMessageNamesTheLimit(t *testing.T) {
	h := newHarness(EventPreRequest)
	h.in.Timeout = 30 * time.Second
	h.in.MemoryLimit = 8 << 20
	h.script(`var a = []; while (true) { a.push(new Array(100000).fill(1)); }`)
	res, _ := runBounded(t, h, 20*time.Second)
	if res.Err == nil || !strings.Contains(res.Err.Message, "8 MiB") {
		t.Errorf("err = %v", res.Err)
	}
	if res.Err != nil && res.Err.File == "" {
		t.Errorf("error is not attributed to a script file")
	}
}

func TestModestAllocationIsNotStopped(t *testing.T) {
	h := newHarness(EventPreRequest)
	h.in.MemoryLimit = 64 << 20
	h.script(`
var rows = [];
for (var i = 0; i < 20000; i++) { rows.push({ id: i, name: 'row ' + i }); }
pm.variables.set('n', String(JSON.stringify(rows).length > 0));`)
	res := h.mustRun(t)
	if res.TimedOut {
		t.Errorf("TimedOut")
	}
	if v, _ := h.in.Vars.GetIn(vars.Local, "n"); v != "true" {
		t.Errorf("script did not finish: n = %v", v)
	}
}

func TestMemoryLimitCanBeDisabled(t *testing.T) {
	if m := newMemoryWatch(-1); m != nil {
		t.Errorf("negative limit should disable the watch")
	}
	if m := newMemoryWatch(0); m == nil || m.limit != uint64(DefaultMemoryLimit) {
		t.Errorf("zero limit should mean the default")
	}
}

func TestOverriddenToJSONCannotHangTheWrapUp(t *testing.T) {
	h := newHarness(EventPreRequest)
	h.in.Timeout = 2 * time.Second
	h.script(`pm.request.toJSON = function () { while (true) {} };`)
	res, d := runBounded(t, h, 4*time.Second)
	if !res.TimedOut {
		t.Errorf("TimedOut = false")
	}
	if res.Err == nil || !strings.Contains(res.Err.Message, "wrap-up") {
		t.Errorf("err = %v, want a wrap-up timeout", res.Err)
	}
	if d < finishBudget/2 {
		t.Errorf("returned in %s, before the wrap-up budget", d)
	}
}

func TestCatastrophicRegexpTerminates(t *testing.T) {
	// Backreferences and lookahead make goja fall back to a backtracking
	// engine whose match runs inside one native call.
	cases := map[string]string{
		"backreference": `/^(a+)+\1$/.test('a'.repeat(60) + 'b');`,
		"lookahead":     `/^(?=(a+)+$)a*b/.test('a'.repeat(60) + 'c');`,
		"exec":          `/(x+x+)+(?=y)\1z/.exec('x'.repeat(60));`,
		"replace":       `'a'.repeat(60).replace(/^(a+)+\1$/, 'b');`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(EventPreRequest)
			h.in.Timeout = 300 * time.Millisecond
			h.script(src)
			res, _ := runBounded(t, h, regexMatchTimeout+abandonGrace+time.Second)
			// Either the match timed out inside the engine and the script
			// went on, or the run was cut off; it must not hang.
			if res == nil {
				t.Fatal("no result")
			}
		})
	}
}

// blockingJar is a cookie store whose SetCookie blocks until released, which
// gives a native call the interrupt cannot reach.
type blockingJar struct {
	*fakeJar
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingJar() *blockingJar {
	return &blockingJar{
		fakeJar: newJar(),
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (j *blockingJar) SetCookie(rawURL string, c model.Cookie) error {
	j.once.Do(func() { close(j.entered) })
	<-j.release
	return j.fakeJar.SetCookie(rawURL, c)
}

func TestStuckNativeCallIsAbandonedAndCannotWriteLater(t *testing.T) {
	jar := newBlockingJar()
	store := newStore()
	h := newHarness(EventPreRequest)
	h.in.Vars = store
	h.in.Cookies = jar
	h.in.Timeout = 100 * time.Millisecond
	// The blocked call ignores the interrupt. When it returns, the VM stops at
	// the next instruction and the wrap-up runs, which settles the unfinished
	// test (a bridge write) after Run has already returned.
	h.script(`
pm.variables.set('before', '1');
console.log('before the hang');
pm.test('never finishes', function (done) { /* done is never called */ });
pm.cookies.jar().set('https://api.example.com', 'sid', 'x', function () {});
`)

	res, d := runBounded(t, h, 100*time.Millisecond+abandonGrace+time.Second)
	if !res.TimedOut || res.Err == nil || !strings.Contains(res.Err.Message, "timed out") {
		t.Fatalf("res = TimedOut %v, err %v", res.TimedOut, res.Err)
	}
	if d < abandonGrace {
		t.Errorf("gave up after %s, before the grace period", d)
	}
	select {
	case <-jar.entered:
	default:
		t.Fatal("script never reached the blocking call")
	}
	if !strings.Contains(res.log(), "before the hang") {
		t.Errorf("console output recorded before the hang was lost: %q", res.log())
	}
	if len(res.Tests) != 0 {
		t.Fatalf("tests before release = %+v", res.Tests)
	}

	// Wake the stuck VM and give its wrap-up time to run. Nothing it does
	// from here may reach the caller's Result.
	close(jar.release)
	time.Sleep(400 * time.Millisecond)

	if len(res.Tests) != 0 {
		t.Errorf("abandoned wrap-up wrote into the returned Result: %+v", res.Tests)
	}
	if v, ok := store.GetIn(vars.Local, "before"); !ok || v != "1" {
		t.Errorf("variable set before the hang = %v, %v", v, ok)
	}
}

func TestAbandonedBridgeRefusesEveryWrite(t *testing.T) {
	store := newStore()
	jar := newJar()
	res := &Result{}
	b := &bridge{in: Input{Vars: store, Cookies: jar}, res: res, now: time.Now}

	b.console("log", "before")
	b.varSet(funcCall("local", "kept", "1"))
	snap := b.abandon()

	b.console("log", "after")
	b.test("late", true, false, false, "", 0)
	b.varSet(funcCall("local", "late", "1"))
	b.varUnset("local", "kept")
	b.varClear("local")
	b.setNextRequest(nil)
	b.skipRequest()
	b.scriptError(0, "Error", "late", "")
	b.cookieSet("https://api.example.com", map[string]any{"name": "late", "value": "1"})

	if len(snap.Console) != 1 || len(snap.Tests) != 0 || snap.Err != nil || snap.NextRequest != nil || snap.SkipRequest {
		t.Errorf("snapshot changed: %+v", snap)
	}
	if len(res.Console) != 1 || len(res.Tests) != 0 || res.Err != nil || res.SkipRequest {
		t.Errorf("result changed after abandonment: %+v", res)
	}
	if v, ok := store.GetIn(vars.Local, "kept"); !ok || v != "1" {
		t.Errorf("kept = %v, %v", v, ok)
	}
	if _, ok := store.GetIn(vars.Local, "late"); ok {
		t.Errorf("variable written after abandonment")
	}
	if cs, _ := jar.Cookies("https://api.example.com"); len(cs) != 0 {
		t.Errorf("cookie written after abandonment")
	}
}

func funcCall(args ...string) goja.FunctionCall {
	vm := goja.New()
	call := goja.FunctionCall{}
	for _, a := range args {
		call.Arguments = append(call.Arguments, vm.ToValue(a))
	}
	return call
}

func TestSendRequestIsBounded(t *testing.T) {
	sender := &countingSender{}
	h := newHarness(EventPreRequest)
	h.in.Sender = sender
	h.in.Timeout = 10 * time.Second
	h.script(`
var failures = 0, ok = 0;
for (var i = 0; i < 300; i++) {
    pm.sendRequest('https://api.example.com/x', function (err) { err ? failures++ : ok++; });
}
setTimeout(function () { pm.variables.set('failures', String(failures)); pm.variables.set('ok', String(ok)); }, 500);
`)
	h.mustRun(t)
	if v, _ := h.in.Vars.GetIn(vars.Local, "ok"); v != "256" {
		t.Errorf("ok = %v, want %d", v, maxSendRequests)
	}
	if v, _ := h.in.Vars.GetIn(vars.Local, "failures"); v != "44" {
		t.Errorf("failures = %v, want 44", v)
	}
	if got, limit := sender.maxInFlight(), sendConcurrency; got > limit {
		t.Errorf("max in flight = %d, want at most %d", got, limit)
	}
}

// countingSender records the most requests it saw in flight at once.
type countingSender struct {
	mu       sync.Mutex
	inFlight int
	peak     int
}

func (c *countingSender) Send(ctx context.Context, _ *model.Request, _ httpx.Options) (*httpx.Response, error) {
	c.mu.Lock()
	c.inFlight++
	c.peak = max(c.peak, c.inFlight)
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.inFlight--
		c.mu.Unlock()
	}()
	select {
	case <-time.After(2 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return jsonResponse(200, `{}`), nil
}

func (c *countingSender) maxInFlight() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.peak
}
