package script

import (
	"context"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func benchRun(b *testing.B, event Event, src string, withResponse bool) {
	b.Helper()
	scripts := []*model.Script{{File: "bench.js", Source: src}}
	in := Input{
		Event:   event,
		Request: model.NewRequest("Bench", "GET", "https://api.example.com/v1/things?page=1"),
		Vars:    newStore(),
		Info:    Info{RequestName: "Bench", RequestID: "b-1"},
	}
	if withResponse {
		in.Response = jsonResponse(200, `{"data":{"id":1,"items":[1,2,3]}}`)
	}
	// Warm the compiled-program and bundle caches.
	if _, err := Run(context.Background(), scripts, in); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		res, err := Run(context.Background(), scripts, in)
		if err != nil {
			b.Fatal(err)
		}
		if res.Err != nil {
			b.Fatalf("script error: %v", res.Err)
		}
	}
}

// A trivial pre-request script: this is the number the brief budgets at 5 ms.
func BenchmarkTrivial(b *testing.B) {
	benchRun(b, EventPreRequest, `pm.environment.set('x', 1);`, false)
}

func BenchmarkEmptyScript(b *testing.B) {
	benchRun(b, EventPreRequest, `var noop = 1;`, false)
}

// A representative pre-request script: variables, a timestamp and an HMAC.
func BenchmarkSigningPreRequest(b *testing.B) {
	benchRun(b, EventPreRequest, `
const CryptoJS = require('crypto-js');
const ts = Date.now().toString();
const payload = pm.request.method + '\n' + pm.request.url.getPath() + '\n' + ts;
pm.request.headers.upsert({key: 'X-Timestamp', value: ts});
pm.request.headers.upsert({
    key: 'X-Signature',
    value: CryptoJS.HmacSHA256(payload, 'secret').toString(CryptoJS.enc.Hex)
});
`, false)
}

// A representative test script: chai assertions through chai-postman.
func BenchmarkAssertingTests(b *testing.B) {
	benchRun(b, EventTest, `
pm.test('status', function () { pm.response.to.have.status(200); });
pm.test('body', function () {
    pm.expect(pm.response.json().data.items).to.be.an('array').that.is.not.empty;
    pm.response.to.have.jsonBody('data.id', 1);
});
`, true)
}
