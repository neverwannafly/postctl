package engine

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/model"
)

// collect runs in and returns the summary with every event, in order.
func (f *fixture) run(in RunInput) (*RunSummary, []RunEvent) {
	f.t.Helper()
	return f.runCtx(context.Background(), in)
}

func (f *fixture) runCtx(ctx context.Context, in RunInput) (*RunSummary, []RunEvent) {
	f.t.Helper()
	if in.Collection == nil {
		in.Collection = f.col
	}
	if in.Environment == nil {
		in.Environment = f.env
	}
	events := make(chan RunEvent, 1024)
	done := make(chan []RunEvent)
	go func() {
		var all []RunEvent
		for ev := range events {
			all = append(all, ev)
			if _, ok := ev.(RunFinished); ok {
				break
			}
		}
		done <- all
	}()
	sum, err := f.eng.Run(ctx, in, events)
	if err != nil {
		f.t.Fatal(err)
	}
	all := <-done
	if fin, ok := all[len(all)-1].(RunFinished); !ok || fin.Summary != sum {
		f.t.Fatalf("last event is %T, want RunFinished with the returned summary", all[len(all)-1])
	}
	return sum, all
}

// requestOrder lists "iteration:name" for every RequestStarted event.
func requestOrder(events []RunEvent) []string {
	var out []string
	for _, ev := range events {
		if rs, ok := ev.(RequestStarted); ok {
			out = append(out, fmt.Sprintf("%d:%s", rs.Iteration, rs.RequestName))
		}
	}
	return out
}

func addRequests(f *fixture, container model.Container, names ...string) []*model.Request {
	var out []*model.Request
	for _, n := range names {
		r := model.NewRequest(n, "GET", "{{baseUrl}}/"+n)
		out = append(out, r)
		switch c := container.(type) {
		case *model.Collection:
			c.Items = append(c.Items, r)
		case *model.Folder:
			c.Items = append(c.Items, r)
		}
	}
	return out
}

func TestRunDepthFirstOrderAndEventNesting(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	addRequests(f, f.col, "a")
	folder := model.NewFolder("F")
	addRequests(f, folder, "b")
	sub := model.NewFolder("Sub")
	addRequests(f, sub, "c")
	folder.Items = append(folder.Items, sub)
	addRequests(f, folder, "d")
	f.col.Items = append(f.col.Items, folder)
	addRequests(f, f.col, "e")
	f.col.Scripts.Test = js(`pm.test("collection test", () => {});`)
	f.save()

	sum, events := f.run(RunInput{})
	wantHits := []string{"/a", "/b", "/c", "/d", "/e"}
	if got := srv.hits(); !reflect.DeepEqual(got, wantHits) {
		t.Fatalf("server saw %v, want depth-first %v", got, wantHits)
	}
	if sum.Requests != 5 || sum.Iterations != 1 || sum.Tests.Passed != 5 || sum.Tests.Failed != 0 || sum.StopReason != "" {
		t.Fatalf("summary: %+v", sum)
	}
	if sum.ExitCode() != 0 {
		t.Fatalf("exit code %d", sum.ExitCode())
	}

	// Event grammar: RunStarted, then per iteration IterationStarted,
	// (RequestStarted, RequestProgress*, RequestFinished)*, IterationFinished,
	// then RunFinished.
	var kinds []string
	for _, ev := range events {
		switch ev.(type) {
		case RunStarted:
			kinds = append(kinds, "RS")
		case IterationStarted:
			kinds = append(kinds, "IS")
		case RequestStarted:
			kinds = append(kinds, "qs")
		case RequestProgress:
			kinds = append(kinds, "p")
		case RequestFinished:
			kinds = append(kinds, "qf")
		case IterationFinished:
			kinds = append(kinds, "IF")
		case RunFinished:
			kinds = append(kinds, "RF")
		}
	}
	grammar := strings.Join(kinds, " ")
	if !strings.HasPrefix(grammar, "RS IS qs p") || !strings.HasSuffix(grammar, "qf IF RF") {
		t.Fatalf("event order: %s", grammar)
	}
	if started := events[0].(RunStarted); started.Requests != 5 || started.Iterations != 1 || started.CollectionName != "API" {
		t.Fatalf("RunStarted: %+v", started)
	}
	fin := events[len(events)-2].(IterationFinished)
	if fin.Requests != 5 || fin.Failed != 0 {
		t.Fatalf("IterationFinished: %+v", fin)
	}
	if !reflect.DeepEqual(events[2].(RequestStarted).Path, []string{"API", "a"}) {
		t.Fatalf("path: %+v", events[2])
	}
	// Progress events carry the plain step text.
	var step string
	for _, ev := range events {
		if p, ok := ev.(RequestProgress); ok {
			step = p.Step.Text
			break
		}
	}
	if step != "Resolving variables" {
		t.Fatalf("first step: %q", step)
	}
}

func TestRunIterationsWithCSVData(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("user", "GET", "{{baseUrl}}/users/{{name}}?n={{n}}")
	req.Scripts.PreRequest = js(`
		// pm.variables is reset every iteration.
		pm.test("local reset", () => pm.expect(pm.variables.has("mark")).to.be.false);
		pm.variables.set("mark", "1");
		pm.test("iteration data", () => pm.expect(pm.iterationData.get("name")).to.eql(pm.variables.get("name")));
	`)
	req.Scripts.Test = js(`
		pm.test("info", () => {
			pm.expect(pm.info.iteration).to.be.a("number");
			pm.expect(pm.info.iterationCount).to.eql(3);
		});
	`)
	f.col.Items = append(f.col.Items, req)
	f.save()

	rows, err := LoadCSV(strings.NewReader("\ufeffname,n\nalice,1\nbob,2\n\"carol, jr\",3\n"))
	if err != nil {
		t.Fatal(err)
	}
	sum, events := f.run(RunInput{Data: rows})
	want := []string{"/users/alice?n=1", "/users/bob?n=2", "/users/carol,%20jr?n=3"}
	got := srv.hits()
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || !strings.HasPrefix(got[2], "/users/carol,") {
		t.Fatalf("server saw %v", got)
	}
	if sum.Iterations != 3 || sum.Requests != 3 || sum.Tests.Failed != 0 || sum.Tests.Passed != 9 {
		t.Fatalf("summary: %+v (failures %+v)", sum, sum.Failures)
	}
	if !reflect.DeepEqual(requestOrder(events), []string{"0:user", "1:user", "2:user"}) {
		t.Fatalf("order: %v", requestOrder(events))
	}
}

func TestRunIterationCountVersusRows(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.col.Items = append(f.col.Items, model.NewRequest("r", "GET", "{{baseUrl}}/{{id}}"))
	f.save()
	rows := []map[string]any{{"id": "x"}, {"id": "y"}}
	sum, _ := f.run(RunInput{Data: rows, Iterations: 5})
	if sum.Iterations != 5 {
		t.Fatalf("iterations = %d", sum.Iterations)
	}
	if got := srv.hits(); !reflect.DeepEqual(got, []string{"/x", "/y", "/x", "/y", "/x"}) {
		t.Fatalf("rows should cycle: %v", got)
	}
	srv.paths = nil
	sum, _ = f.run(RunInput{Data: rows})
	if sum.Iterations != 2 {
		t.Fatalf("without -n the row count decides: %d", sum.Iterations)
	}
}

func TestRunSetNextRequestJumpsAndLoops(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	rs := addRequests(f, f.col, "start", "skipped", "target", "after")
	rs[0].Scripts.Test = js(`pm.execution.setNextRequest("target");`)
	f.save()
	sum, events := f.run(RunInput{})
	if got := requestOrder(events); !reflect.DeepEqual(got, []string{"0:start", "0:target", "0:after"}) {
		t.Fatalf("order: %v", got)
	}
	if sum.StopReason != "" || sum.Requests != 3 {
		t.Fatalf("summary: %+v", sum)
	}

	// A loop: "counter" jumps back to itself until an environment variable
	// reaches 3, which also proves variable writes are visible to later
	// requests in the same run.
	f2 := newFixture(t, newServer(t, jsonHandler(`{}`)))
	rs = addRequests(f2, f2.col, "first", "counter", "last")
	rs[1].Scripts.Test = js(`
		const n = Number(pm.environment.get("n") || 0) + 1;
		pm.environment.set("n", n);
		if (n < 3) { pm.execution.setNextRequest("counter"); }
	`)
	f2.save()
	sum, events = f2.run(RunInput{})
	if got := requestOrder(events); !reflect.DeepEqual(got, []string{"0:first", "0:counter", "0:counter", "0:counter", "0:last"}) {
		t.Fatalf("order: %v", got)
	}
	if v, _ := varValue(f2.reloadEnv().Variables, "n"); v != "3" {
		t.Fatalf("n on disk = %q", v)
	}
	if sum.Requests != 5 {
		t.Fatalf("requests = %d", sum.Requests)
	}
}

func TestRunSetNextRequestByIDAndInLegacyForm(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	rs := addRequests(f, f.col, "a", "b", "c")
	rs[0].Scripts.Test = js(fmt.Sprintf(`postman.setNextRequest(%q);`, string(rs[2].ID)))
	f.save()
	_, events := f.run(RunInput{})
	if got := requestOrder(events); !reflect.DeepEqual(got, []string{"0:a", "0:c"}) {
		t.Fatalf("order: %v", got)
	}
}

func TestRunSetNextRequestNullStopsRun(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	rs := addRequests(f, f.col, "a", "b", "c")
	rs[1].Scripts.Test = js(`pm.execution.setNextRequest(null);`)
	f.save()
	sum, events := f.run(RunInput{Iterations: 3})
	if got := requestOrder(events); !reflect.DeepEqual(got, []string{"0:a", "0:b"}) {
		t.Fatalf("order: %v", got)
	}
	if sum.StopReason != StopNextNull || sum.Iterations != 1 {
		t.Fatalf("summary: %+v", sum)
	}
}

func TestRunSetNextRequestUnknownTarget(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	rs := addRequests(f, f.col, "a", "b")
	rs[0].Scripts.Test = js(`pm.execution.setNextRequest("nope");`)
	f.save()
	sum, _ := f.run(RunInput{})
	if sum.StopReason != StopNextMissing || sum.ScriptErrors != 1 || len(sum.Failures) != 1 || sum.ExitCode() != 1 {
		t.Fatalf("summary: %+v", sum)
	}
	if len(srv.hits()) != 1 {
		t.Fatalf("server saw %v", srv.hits())
	}
}

func TestRunFolderScopeKeepsAncestorScripts(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	addRequests(f, f.col, "outside")
	folder := model.NewFolder("F")
	folder.Scripts.PreRequest = js(`pm.variables.set("who", "folder");`)
	addRequests(f, folder, "inside1", "inside2")
	f.col.Items = append(f.col.Items, folder)
	f.col.Scripts.PreRequest = js(`pm.variables.set("who", "collection");`)
	for _, r := range folder.Items {
		r.(*model.Request).URL = model.ParseURL("{{baseUrl}}/{{who}}/" + r.ItemName())
	}
	f.save()

	folderID := f.col.Items[1].ItemID()
	sum, _ := f.run(RunInput{FolderID: folderID})
	if got := srv.hits(); !reflect.DeepEqual(got, []string{"/folder/inside1", "/folder/inside2"}) {
		t.Fatalf("server saw %v", got)
	}
	if sum.Requests != 2 {
		t.Fatalf("requests = %d", sum.Requests)
	}
	if _, err := f.eng.Run(context.Background(), RunInput{Collection: f.col, FolderID: "missing"}, nil); err == nil {
		t.Fatal("an unknown folder must be rejected")
	}
}

func TestRunBail(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	rs := addRequests(f, f.col, "a", "b", "c")
	rs[1].Scripts.Test = js(`pm.test("fails", () => pm.expect(1).to.eql(2));`)
	f.save()

	sum, events := f.run(RunInput{Bail: true, Iterations: 2})
	if got := requestOrder(events); !reflect.DeepEqual(got, []string{"0:a", "0:b"}) {
		t.Fatalf("order: %v", got)
	}
	if sum.StopReason != StopBail || sum.Tests.Failed != 1 || sum.Tests.Passed != 0 || sum.ExitCode() != 1 {
		t.Fatalf("summary: %+v", sum)
	}
	if len(sum.Failures) != 1 || sum.Failures[0].Kind != FailureTest || sum.Failures[0].Name != "fails" ||
		sum.Failures[0].RequestName != "b" || !reflect.DeepEqual(sum.Failures[0].Path, []string{"API", "b"}) ||
		sum.Failures[0].Message == "" {
		t.Fatalf("failures: %+v", sum.Failures)
	}

	// Without Bail the run carries on.
	srv.paths = nil
	sum, _ = f.run(RunInput{})
	if len(srv.hits()) != 3 || sum.StopReason != "" || sum.Tests.Failed != 1 {
		t.Fatalf("hits=%v summary=%+v", srv.hits(), sum)
	}
}

func TestRunScriptErrorsAndStopOnScriptError(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	rs := addRequests(f, f.col, "a", "b", "c")
	rs[1].Scripts.PreRequest = js(`throw new Error("bad");`)
	f.save()

	sum, _ := f.run(RunInput{})
	if got := srv.hits(); !reflect.DeepEqual(got, []string{"/a", "/c"}) {
		t.Fatalf("a failing pre-request script skips only its request: %v", got)
	}
	if sum.ScriptErrors != 1 || sum.Requests != 3 || sum.ExitCode() != 1 {
		t.Fatalf("summary: %+v", sum)
	}
	if sum.Failures[0].Kind != FailureScript || !strings.Contains(sum.Failures[0].Message, "bad") {
		t.Fatalf("failures: %+v", sum.Failures)
	}

	srv.paths = nil
	sum, _ = f.run(RunInput{StopOnScriptError: true})
	if got := srv.hits(); !reflect.DeepEqual(got, []string{"/a"}) || sum.StopReason != StopScriptError {
		t.Fatalf("hits=%v summary=%+v", got, sum)
	}
}

func TestRunTransportErrorsAndExitCode(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	rs := addRequests(f, f.col, "ok", "dead")
	rs[1].URL = model.ParseURL("http://127.0.0.1:1/nothing")
	rs[0].Scripts.Test = js(`pm.test("fails", () => pm.expect(1).to.eql(2));`)
	f.save()
	sum, _ := f.run(RunInput{})
	if sum.TransportErrors != 1 || sum.Tests.Failed != 1 || sum.ExitCode() != 2 {
		t.Fatalf("summary: %+v", sum)
	}
	kinds := []FailureKind{}
	for _, fl := range sum.Failures {
		kinds = append(kinds, fl.Kind)
	}
	if !reflect.DeepEqual(kinds, []FailureKind{FailureTest, FailureTransport}) {
		t.Fatalf("failure kinds: %v", kinds)
	}
}

func TestRunSkippedRequestsAreCounted(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	rs := addRequests(f, f.col, "a", "b")
	rs[0].Scripts.PreRequest = js(`pm.execution.skipRequest();`)
	f.save()
	sum, _ := f.run(RunInput{})
	if sum.Skipped != 1 || sum.Requests != 1 || len(srv.hits()) != 1 {
		t.Fatalf("summary: %+v hits=%v", sum, srv.hits())
	}
}

func TestRunCancellation(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/b" {
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte("{}"))
	})
	f := newFixture(t, srv)
	addRequests(f, f.col, "a", "b", "c")
	f.save()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for len(srv.hits()) < 2 {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()
	sum, events := f.runCtx(ctx, RunInput{Iterations: 4})
	if sum.StopReason != StopCancelled {
		t.Fatalf("summary: %+v", sum)
	}
	if got := requestOrder(events); !reflect.DeepEqual(got, []string{"0:a", "0:b"}) {
		t.Fatalf("order: %v", got)
	}
	if sum.TransportErrors != 0 {
		t.Fatalf("the cancelled request is not a transport error: %+v", sum)
	}
	// RunFinished is delivered even though the context is done.
	if _, ok := events[len(events)-1].(RunFinished); !ok {
		t.Fatal("no RunFinished")
	}
	// Cancelling before the run starts runs nothing.
	srv.paths = nil
	cancelled, cancel2 := context.WithCancel(context.Background())
	cancel2()
	sum, _ = f.runCtx(cancelled, RunInput{})
	if sum.StopReason != StopCancelled || len(srv.hits()) != 0 {
		t.Fatalf("summary: %+v hits=%v", sum, srv.hits())
	}
}

func TestRunDelay(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	addRequests(f, f.col, "a", "b", "c")
	f.save()
	start := time.Now()
	sum, _ := f.run(RunInput{Delay: 80 * time.Millisecond})
	if elapsed := time.Since(start); elapsed < 160*time.Millisecond {
		t.Fatalf("two delays expected between three requests, run took %v", elapsed)
	}
	if sum.TotalTime < 160*time.Millisecond {
		t.Fatalf("TotalTime %v does not include delays", sum.TotalTime)
	}
}

func TestRunAsync(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	addRequests(f, f.col, "a", "b")
	f.save()
	ch, err := f.eng.RunAsync(context.Background(), RunInput{Collection: f.col, Environment: f.env})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var last RunEvent
	for ev := range ch {
		n++
		last = ev
	}
	if _, ok := last.(RunFinished); !ok || n < 8 {
		t.Fatalf("got %d events, last %T", n, last)
	}
}

func TestRunSummaryTimes(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	addRequests(f, f.col, "a", "b")
	f.save()
	sum, _ := f.run(RunInput{})
	if sum.TotalTime <= 0 || sum.AvgResponseTime <= 0 || sum.AvgResponseTime > sum.TotalTime {
		t.Fatalf("times: total=%v avg=%v", sum.TotalTime, sum.AvgResponseTime)
	}
}

func TestRunPersistsSecretVariableWrites(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.env.Variables = append(f.env.Variables, model.Variable{Key: "tok", Value: "initial-token-1234", Type: model.VarSecret})
	rs := addRequests(f, f.col, "a", "b")
	rs[0].Scripts.Test = js(`pm.environment.set("tok", "run-token-" + "5678");`)
	rs[1].Headers = []model.KV{{Key: "X-Seen", Value: "{{tok}}"}}
	f.save()
	f.run(RunInput{})
	if got := srv.hdrs[1].Get("X-Seen"); got != "run-token-5678" {
		t.Fatalf("second request saw %q", got)
	}
	env := f.reloadEnv()
	for _, v := range env.Variables {
		if v.Key == "tok" {
			got, err := f.secrets.Get(context.Background(), v.SecretRef)
			if err != nil || got != "run-token-5678" || v.Value != "" {
				t.Fatalf("stored secret %q, %v, file value %q", got, err, v.Value)
			}
		}
	}
}

func TestLoadJSON(t *testing.T) {
	rows, err := LoadJSON(strings.NewReader(`[{"a":1,"b":"x","c":{"d":[1,2.5]},"big":12345678901234567890,"t":true},{"a":2}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["a"] != float64(1) || rows[0]["b"] != "x" || rows[0]["big"] != "12345678901234567890" || rows[0]["t"] != true {
		t.Fatalf("rows: %#v", rows)
	}
	if d := rows[0]["c"].(map[string]any)["d"].([]any); d[1] != 2.5 {
		t.Fatalf("nested: %#v", rows[0]["c"])
	}
	if one, err := LoadJSON(strings.NewReader(`{"only":"row"}`)); err != nil || len(one) != 1 {
		t.Fatalf("single object: %v %v", one, err)
	}
	for _, bad := range []string{`[1,2]`, `"str"`, `[{"a":1}] x`, ``, `{`} {
		if _, err := LoadJSON(strings.NewReader(bad)); err == nil {
			t.Errorf("LoadJSON(%q) should fail", bad)
		}
	}
}

func TestLoadCSVErrors(t *testing.T) {
	for _, bad := range []string{"", "a,a\n1,2\n", "a,\n1,2\n", "a,b\n1\n"} {
		if _, err := LoadCSV(strings.NewReader(bad)); err == nil {
			t.Errorf("LoadCSV(%q) should fail", bad)
		}
	}
	rows, err := LoadCSV(strings.NewReader("a,b\n"))
	if err != nil || len(rows) != 0 {
		t.Fatalf("header-only file: %v %v", rows, err)
	}
	if _, err := LoadDataFile("x.txt"); err == nil {
		t.Fatal("unsupported extension")
	}
}
