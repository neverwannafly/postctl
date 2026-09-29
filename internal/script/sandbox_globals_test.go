package script

import (
	"strings"
	"testing"
)

// bridgeFunctionNames are the Go bridge functions (bridge.object). None may be
// reachable from user script code.
var bridgeFunctionNames = []string{
	"varGet", "varSet", "varUnset", "varHas", "varClear", "varObject", "varResolve",
	"console", "test", "setNextRequest", "skipRequest", "randomBytes", "sendRequest",
	"cookieList", "cookieSet", "cookieUnset", "cookieClear", "scriptError",
}

// TestSandboxHostGlobalIsNotScriptVisible pins that the Go bridge (`host`) and
// the bootstrap entry points are not part of the global scope a script sees.
func TestSandboxHostGlobalIsNotScriptVisible(t *testing.T) {
	names := "[" + quoteAll(bridgeFunctionNames) + "]"
	h := newHarness(EventPreRequest).script(`
const bridgeNames = ` + names + `;
console.log('typeof host=' + typeof host);
console.log('has host=' + ('host' in globalThis) + '/' + Object.prototype.hasOwnProperty.call(globalThis, 'host'));
console.log('typeof globalThis.host=' + typeof globalThis.host);
console.log('typeof this.host=' + typeof (function () { return this; })().host);

const own = Object.getOwnPropertyNames(globalThis);
const enumerated = [];
for (const k in globalThis) { enumerated.push(k); }
console.log('bootstrap globals=' + own.concat(enumerated).filter(function (k) {
    return k.indexOf('__postctl') === 0 || k === 'host' || k === 'bridge';
}).length);

// Walk everything reachable from globalThis (own names and symbols, any
// enumerability, prototypes, values) and count Go-native functions that carry
// a bridge function's name. The pm API implements sendRequest and friends in
// JavaScript, so a native function under one of these names is the bridge.
const seen = new Set();
let found = 0;
function isNative(fn) {
    try { return Function.prototype.toString.call(fn).indexOf('[native code]') >= 0; } catch (e) { return false; }
}
function walk(value, depth) {
    if (value === null || (typeof value !== 'object' && typeof value !== 'function')) { return; }
    if (seen.has(value) || depth > 6) { return; }
    seen.add(value);
    let keys = [];
    try { keys = Object.getOwnPropertyNames(value).concat(Object.getOwnPropertySymbols(value)); } catch (e) { return; }
    keys.forEach(function (k) {
        let v;
        try { v = value[k]; } catch (e) { return; }
        if (typeof v === 'function' && typeof k === 'string' && bridgeNames.indexOf(k) >= 0 && isNative(v)
            && v.name === k && !(value === RegExp.prototype && k === 'test')) {
            found++;
        }
        walk(v, depth + 1);
    });
    try { walk(Object.getPrototypeOf(value), depth + 1); } catch (e) { /* ignore */ }
}
walk(globalThis, 0);
console.log('reachable bridge functions=' + found);
console.log('walked=' + (seen.size > 100));
`)
	res := h.mustRun(t)
	log := res.log()
	for _, want := range []string{
		"typeof host=undefined",
		"has host=false/false",
		"typeof globalThis.host=undefined",
		"typeof this.host=undefined",
		"bootstrap globals=0",
		"reachable bridge functions=0",
		"walked=true",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("missing %q in script output:\n%s", want, log)
		}
	}
}

// TestSandboxStillWorksWithoutHostGlobal makes sure hiding the bridge did not
// break the pm API that is built on it.
func TestSandboxStillWorksWithoutHostGlobal(t *testing.T) {
	h := newHarness(EventPreRequest).script(`
pm.variables.set('k', 'v');
console.log('got=' + pm.variables.get('k'));
pm.test('works', function () { pm.expect(pm.variables.get('k')).to.eql('v'); });
`)
	res := h.mustRun(t)
	if !strings.Contains(res.log(), "got=v") || len(res.Tests) != 1 || !res.Tests[0].Passed {
		t.Fatalf("log=%q tests=%+v", res.log(), res.Tests)
	}
}

func quoteAll(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = `'` + n + `'`
	}
	return strings.Join(quoted, ", ")
}
