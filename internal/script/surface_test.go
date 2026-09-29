package script

import (
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestCookies(t *testing.T) {
	jar := newJar()
	jar.cookies["api.example.com"] = []model.Cookie{
		{Name: "session", Value: "s-1", Domain: "api.example.com", Path: "/", HTTPOnly: true},
		{Name: "theme", Value: "dark"},
	}

	h := newHarness(EventTest)
	h.in.Response = jsonResponse(200, `{}`)
	h.in.Cookies = jar
	h.script(`
console.log(pm.cookies.has('session'), pm.cookies.get('session'));
console.log(JSON.stringify(pm.cookies.toObject()));
console.log(pm.cookies.one('session').httpOnly);

const jar = pm.cookies.jar();
jar.set('https://api.example.com/x', 'added', 'v1', function (err) {
    if (err) { console.error(err.message); return; }
    jar.get('https://api.example.com/x', 'added', function (err2, value) {
        console.log('got', value);
    });
});
`)
	res := h.mustRun(t)

	want := "log: true s-1\n" +
		"log: {\"session\":\"s-1\",\"theme\":\"dark\"}\n" +
		"log: true\n" +
		"log: got v1\n"
	if res.log() != want {
		t.Errorf("console =\n%q\nwant\n%q", res.log(), want)
	}
	if len(jar.cookies["api.example.com"]) != 3 {
		t.Errorf("jar = %+v", jar.cookies)
	}
}

func TestCookieJarPromiseForm(t *testing.T) {
	jar := newJar()
	h := newHarness(EventPreRequest)
	h.in.Cookies = jar
	h.script(`
const j = pm.cookies.jar();
await j.set('https://api.example.com/', {name: 'a', value: '1'});
await j.set('https://api.example.com/', {name: 'b', value: '2'});
const all = await j.getAll('https://api.example.com/');
console.log(all.count(), JSON.stringify(all.toObject()));
await j.unset('https://api.example.com/', 'a');
console.log((await j.getAll('https://api.example.com/')).count());
await j.clear('https://api.example.com/');
console.log((await j.getAll('https://api.example.com/')).count());
`)
	res := h.mustRun(t)

	want := "log: 2 {\"a\":\"1\",\"b\":\"2\"}\nlog: 1\nlog: 0\n"
	if res.log() != want {
		t.Errorf("console = %q, want %q", res.log(), want)
	}
}

func TestCookieJarWithoutStore(t *testing.T) {
	h := newHarness(EventPreRequest).script(`
pm.cookies.jar().get('https://x.example', 'a', function (err, v) {
    console.log('err', err.message);
});
console.log('count', pm.cookies.count());
`)
	res := h.mustRun(t)
	if !strings.Contains(res.log(), "no cookie store") || !strings.Contains(res.log(), "count 0") {
		t.Errorf("console = %q", res.log())
	}
}

func TestLegacyGlobals(t *testing.T) {
	store := newStore()
	h := newHarness(EventTest)
	h.in.Vars = store
	h.in.Response = jsonResponse(201, `{"id":3}`)
	h.script(`
tests['legacy passes'] = responseCode.code === 201;
tests['legacy fails'] = responseBody.indexOf('nope') >= 0;
console.log(responseCode.name, responseTime > 0, typeof responseHeaders['content-type']);
console.log(postman.getResponseHeader('Content-Type'));
postman.setEnvironmentVariable('legacyEnv', 'v');
postman.setGlobalVariable('legacyGlobal', 'g');
console.log(postman.getEnvironmentVariable('legacyEnv'), postman.getGlobalVariable('legacyGlobal'));
console.log(request.method, typeof request.url);
console.log(JSON.stringify(xml2Json('<a><b>c</b></a>')));
`)
	res := h.mustRun(t)

	want := "log: Created true string\n" +
		"log: application/json\n" +
		"log: v g\n" +
		"log: GET string\n" +
		"log: {\"a\":{\"b\":\"c\"}}\n"
	if res.log() != want {
		t.Errorf("console =\n%q\nwant\n%q", res.log(), want)
	}

	if len(res.Tests) != 2 {
		t.Fatalf("tests = %+v", res.Tests)
	}
	if res.Tests[0].Name != "legacy passes" || !res.Tests[0].Passed {
		t.Errorf("test 0 = %+v", res.Tests[0])
	}
	if res.Tests[1].Name != "legacy fails" || res.Tests[1].Passed {
		t.Errorf("test 1 = %+v", res.Tests[1])
	}
}

func TestLegacyTestsFollowPmTests(t *testing.T) {
	h := newHarness(EventTest)
	h.in.Response = jsonResponse(200, `{}`)
	h.script(`
pm.test('first', function () {});
tests['second'] = true;
`)
	res := h.mustRun(t)
	if len(res.Tests) != 2 || res.Tests[0].Name != "first" || res.Tests[1].Name != "second" {
		t.Fatalf("tests = %+v", res.Tests)
	}
	if res.Tests[1].Index != 1 {
		t.Errorf("legacy test index = %d, want 1", res.Tests[1].Index)
	}
}

func TestBundledLibraries(t *testing.T) {
	h := newHarness(EventPreRequest).script(`
const _ = require('lodash');
const moment = require('moment');
const CryptoJS = require('crypto-js');
const uuid = require('uuid');
const Ajv = require('ajv');
const tv4 = require('tv4');
const xml2js = require('xml2js');

console.log(_.get({a: {b: [1, 2]}}, 'a.b[1]'), _.kebabCase('helloThere'));
console.log(moment('2020-01-02T03:04:05Z').utc().format('YYYY-MM-DD'));
console.log(CryptoJS.HmacSHA256('msg', 'key').toString(CryptoJS.enc.Hex).slice(0, 16));
console.log(CryptoJS.enc.Base64.stringify(CryptoJS.enc.Utf8.parse('hi')));
console.log(CryptoJS.MD5('abc').toString(), CryptoJS.SHA256('abc').toString().slice(0, 8));
console.log(uuid.v4().length, uuid.validate(uuid.v4()));
console.log(new Ajv({allErrors: true}).validate({type: 'object', required: ['a']}, {a: 1}));
console.log(tv4.validate({a: 1}, {type: 'object'}));

let parsed;
xml2js.parseString('<r><v>7</v></r>', {explicitArray: false}, function (e, o) { parsed = o; });
console.log(JSON.stringify(parsed));

console.log(btoa('hello'), atob('aGVsbG8='));
console.log(typeof Buffer, Buffer.from('hi').toString('base64'));
`)
	res := h.mustRun(t)

	want := []string{
		"log: 2 hello-there",
		"log: 2020-01-02",
		"log: 2d93cbc1be167bcb",
		"log: aGk=",
		"log: 900150983cd24fb0d6963f7d28e17f72 ba7816bf",
		"log: 36 true",
		"log: true",
		"log: true",
		`log: {"r":{"v":"7"}}`,
		"log: aGVsbG8= hello",
		"log: function aGk=",
	}
	got := strings.Split(strings.TrimSuffix(res.log(), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d lines:\n%s", len(got), res.log())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRequireRejectsUnknownModule(t *testing.T) {
	h := newHarness(EventPreRequest).script(`require('fs');`)
	res := h.run(t)
	if res.Err == nil || !strings.Contains(res.Err.Message, "Cannot find module 'fs'") {
		t.Fatalf("err = %+v", res.Err)
	}
}

func TestSandboxHasNoHostAccess(t *testing.T) {
	h := newHarness(EventPreRequest).script(`
const names = ['process', 'global', 'Deno', 'XMLHttpRequest', 'fetch', 'WebSocket', 'importScripts', 'eval$'];
names.forEach(function (n) { console.log(n, typeof globalThis[n]); });
`)
	res := h.mustRun(t)
	for _, line := range strings.Split(strings.TrimSuffix(res.log(), "\n"), "\n") {
		if !strings.HasSuffix(line, "undefined") {
			t.Errorf("host object reachable: %s", line)
		}
	}
}

func TestNoOpAPIsWarnInsteadOfCrashing(t *testing.T) {
	h := newHarness(EventTest)
	h.in.Response = jsonResponse(200, `{}`)
	h.script(`
pm.visualizer.set('<b>{{x}}</b>', {x: 1});
pm.require('some-package');
const v = await pm.vault.get('secret');
console.log('vault', v);
await pm.execution.runRequest('abc');
pm.test('still runs', function () { pm.expect(1).to.eql(1); });
`)
	res := h.mustRun(t)

	if len(res.Tests) != 1 || !res.Tests[0].Passed {
		t.Fatalf("tests = %+v", res.Tests)
	}
	var warnings int
	for _, e := range res.Console {
		if e.Level == LevelWarn {
			warnings++
		}
	}
	if warnings != 4 {
		t.Errorf("got %d warnings, want 4:\n%s", warnings, res.log())
	}
	if !strings.Contains(res.log(), "vault undefined") {
		t.Errorf("console = %s", res.log())
	}
}

func TestPmInfo(t *testing.T) {
	h := newHarness(EventTest)
	h.in.Response = jsonResponse(200, `{}`)
	h.in.Info = Info{RequestName: "Get thing", RequestID: "id-9", CollectionName: "Payments", Iteration: 2, IterationCount: 5}
	h.script(`
console.log(pm.info.eventName, pm.info.requestName, pm.info.requestId);
console.log(pm.info.iteration, pm.info.iterationCount, pm.info.collectionName);
console.log(pm.execution.location.join(' > '), pm.execution.location.current);
`)
	res := h.mustRun(t)

	want := "log: test Get thing id-9\nlog: 2 5 Payments\nlog: Payments > Get thing Get thing\n"
	if res.log() != want {
		t.Errorf("console = %q, want %q", res.log(), want)
	}
}

func TestIterationData(t *testing.T) {
	store := newStore()
	store.values[3] = map[string]any{"row": "one"} // vars.Data
	store.order[3] = []string{"row"}
	h := newHarness(EventPreRequest)
	h.in.Vars = store
	h.script(`console.log(pm.iterationData.get('row'), pm.variables.get('row'), JSON.stringify(pm.iterationData.toObject()));`)
	res := h.mustRun(t)

	if got := res.log(); got != "log: one one {\"row\":\"one\"}\n" {
		t.Errorf("console = %q", got)
	}
}

func TestVariablePrecedenceAndReplaceIn(t *testing.T) {
	store := newStore()
	store.with(0, map[string]any{"host": "global.example", "only": "g"}) // Global
	store.with(2, map[string]any{"host": "env.example"})                 // Environment
	h := newHarness(EventPreRequest)
	h.in.Vars = store
	h.script(`
console.log(pm.variables.get('host'), pm.globals.get('host'), pm.environment.get('host'));
console.log(pm.variables.replaceIn('https://{{host}}/{{only}}/{{missing}}'));
console.log(pm.globals.replaceIn('{{host}}|{{missing}}'));
console.log(pm.variables.replaceIn('{{$timestamp}}'));
`)
	res := h.mustRun(t)

	want := "log: env.example global.example env.example\n" +
		"log: https://env.example/g/{{missing}}\n" +
		"log: global.example|{{missing}}\n" +
		"log: 1700000000\n"
	if res.log() != want {
		t.Errorf("console =\n%q\nwant\n%q", res.log(), want)
	}
}
