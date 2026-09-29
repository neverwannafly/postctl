package tuiengine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
)

type setup struct {
	sender *Sender
	col    *model.Collection
	env    *model.Environment
	url    string
}

func newSetup(t *testing.T, mutate func(col *model.Collection)) *setup {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	st, err := store.Init(t.TempDir(), "w", store.WithSecretStore(store.NewMemorySecrets()))
	if err != nil {
		t.Fatal(err)
	}
	col := model.NewCollection("C")
	env := &model.Environment{ID: model.NewID(), Name: "E", Variables: []model.Variable{{Key: "base", Value: srv.URL}}}
	mutate(col)
	ws, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	ws.Collections, ws.Environments = []*model.Collection{col}, []*model.Environment{env}
	if err := st.Save(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(engine.Config{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	return &setup{sender: New(eng), col: col, env: env, url: srv.URL}
}

func TestSendMapsOutcome(t *testing.T) {
	var req *model.Request
	s := newSetup(t, func(col *model.Collection) {
		req = model.NewRequest("R", "GET", "{{base}}/x/{{missing}}")
		req.Scripts.PreRequest = &model.Script{Source: `console.warn("before");`}
		req.Scripts.Test = &model.Script{Source: `
			pm.test("passes", () => pm.response.to.have.status(200));
			pm.test("fails", () => pm.expect(1).to.eql(2));
			pm.test.skip("skipped");
			console.log("after");
		`}
		col.Items = append(col.Items, req)
	})

	var steps []string
	res := s.sender.Send(context.Background(), tui.SendRequest{Collection: s.col, Request: req, Environment: s.env},
		func(st tui.SendStep) { steps = append(steps, st.Text) })

	if res.Err != nil || res.ScriptError != "" || res.Skipped {
		t.Fatalf("result: %+v", res)
	}
	if res.RequestID != req.ID || res.Started.IsZero() || res.Response == nil || res.Response.StatusCode != 200 {
		t.Fatalf("result: %+v", res)
	}
	wantSteps := []string{"Running pre-request script", "Resolving variables", "Waiting for response", "Running tests"}
	if !reflect.DeepEqual(steps, wantSteps) {
		t.Fatalf("steps %q, want %q", steps, wantSteps)
	}
	if len(res.Tests) != 3 || !res.Tests[0].Passed || res.Tests[1].Passed || res.Tests[1].Message == "" || !res.Tests[2].Skipped {
		t.Fatalf("tests: %+v", res.Tests)
	}
	var got []string
	for _, c := range res.Console {
		got = append(got, c.Event+"/"+c.Level+"/"+c.Text)
	}
	want := []string{
		"prerequest/warn/before",
		"test/log/after",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("console:\n got %q\nwant %q", got, want)
	}
	if wantWarnings := []string{"Unresolved variable {{missing}} in the URL"}; !reflect.DeepEqual(res.Warnings, wantWarnings) {
		t.Fatalf("warnings: got %q want %q", res.Warnings, wantWarnings)
	}
}

func TestSendMapsSkipAndScriptError(t *testing.T) {
	var skip, boom *model.Request
	s := newSetup(t, func(col *model.Collection) {
		skip = model.NewRequest("skip", "GET", "{{base}}/x")
		skip.Scripts.PreRequest = &model.Script{Source: `pm.execution.skipRequest();`}
		boom = model.NewRequest("boom", "GET", "{{base}}/x")
		boom.Scripts.PreRequest = &model.Script{Source: `throw new Error("kaput");`}
		col.Scripts.Test = &model.Script{Source: `nope();`}
		col.Items = append(col.Items, skip, boom)
	})
	in := func(r *model.Request) tui.SendRequest {
		return tui.SendRequest{Collection: s.col, Request: r, Environment: s.env}
	}

	res := s.sender.Send(context.Background(), in(skip), nil)
	if !res.Skipped || res.Response != nil || res.Err != nil {
		t.Fatalf("skip: %+v", res)
	}
	res = s.sender.Send(context.Background(), in(boom), nil)
	if res.Response != nil || !strings.HasPrefix(res.ScriptError, "Pre-request script failed: ") || !strings.Contains(res.ScriptError, "kaput") {
		t.Fatalf("boom: %+v", res)
	}

	ok := model.NewRequest("ok", "GET", "{{base}}/x")
	s.col.Items = append(s.col.Items, ok)
	res = s.sender.Send(context.Background(), in(ok), nil)
	if res.Response == nil || !strings.HasPrefix(res.ScriptError, "Test script (collection) failed: ") {
		t.Fatalf("collection test script error: %+v", res)
	}
}

func TestSendMapsErrors(t *testing.T) {
	s := newSetup(t, func(col *model.Collection) {})
	res := s.sender.Send(context.Background(), tui.SendRequest{Request: model.NewRequest("R", "GET", "x")}, nil)
	if !errors.Is(res.Err, engine.ErrInvalidInput) {
		t.Fatalf("err: %v", res.Err)
	}

	dead := model.NewRequest("dead", "GET", "http://127.0.0.1:1/")
	s.col.Items = append(s.col.Items, dead)
	res = s.sender.Send(context.Background(), tui.SendRequest{Collection: s.col, Request: dead, Environment: s.env}, nil)
	if res.Err == nil || res.Response != nil || res.RequestID != dead.ID {
		t.Fatalf("transport error: %+v", res)
	}
}

func TestOutcomeMasksConsoleTestsAndScriptErrors(t *testing.T) {
	const secret = "synthetic-tui-output-secret"
	res := OutcomeToResult(&engine.Outcome{
		Redactor: history.NewRedactor([]string{secret}),
		Console:  []engine.ConsoleLine{{Level: "log", Text: "console " + secret}},
		Tests:    []engine.TestResult{{Name: "test " + secret, Message: "message " + secret}},
		ScriptError: &engine.ScriptFailure{
			Event: "test",
			Text:  "script " + secret,
		},
	})
	joined := res.Console[0].Text + res.Tests[0].Name + res.Tests[0].Message + res.ScriptError
	if strings.Contains(joined, secret) {
		t.Fatalf("TUI result exposed a known secret")
	}
	if !strings.Contains(joined, history.Mask) {
		t.Fatalf("TUI result did not use the shared history mask: %q", joined)
	}
}
