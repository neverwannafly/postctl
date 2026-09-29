package script

import (
	"context"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func TestSyntaxErrorMapsToFileAndLine(t *testing.T) {
	scripts := []*model.Script{{File: "pre.js", Source: "const a = 1;\nconst b = ;\n"}}
	res, err := Run(context.Background(), scripts, Input{
		Event:   EventPreRequest,
		Request: model.NewRequest("R", "GET", "https://x.example"),
		Vars:    newStore(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Err == nil {
		t.Fatal("expected a ScriptError")
	}
	if res.Err.Name != "SyntaxError" {
		t.Errorf("name = %q", res.Err.Name)
	}
	if res.Err.File != "pre.js" {
		t.Errorf("file = %q", res.Err.File)
	}
	if res.Err.Line != 2 {
		t.Errorf("line = %d, want 2 (message %q)", res.Err.Line, res.Err.Message)
	}
}

func TestRuntimeErrorMapsToFileAndLine(t *testing.T) {
	scripts := []*model.Script{{File: "pre.js", Source: "const a = 1;\nconsole.log('before');\nnotDefinedAnywhere();\n"}}
	res, err := Run(context.Background(), scripts, Input{
		Event:   EventPreRequest,
		Request: model.NewRequest("R", "GET", "https://x.example"),
		Vars:    newStore(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Err == nil {
		t.Fatal("expected a ScriptError")
	}
	if res.Err.Name != "ReferenceError" {
		t.Errorf("name = %q (%s)", res.Err.Name, res.Err.Message)
	}
	if res.Err.File != "pre.js" || res.Err.Line != 3 {
		t.Errorf("position = %s:%d:%d", res.Err.File, res.Err.Line, res.Err.Column)
	}
	if len(res.Console) != 1 {
		t.Errorf("console output before the error was lost: %+v", res.Console)
	}
	if !strings.Contains(res.Err.Error(), "pre.js:3:") {
		t.Errorf("Error() = %q", res.Err.Error())
	}
}

// The sandbox wraps a script body in a one-line prefix, so a position on the
// first line has to be shifted back to the column the user typed.
func TestFirstLineColumnIsUnshifted(t *testing.T) {
	// "throw new Error('x');" — the frame points at `new Error`, column 7.
	scripts := []*model.Script{{File: "pre.js", Source: "throw new Error('x');"}}
	res, _ := Run(context.Background(), scripts, Input{
		Event:   EventPreRequest,
		Request: model.NewRequest("R", "GET", "https://x.example"),
		Vars:    newStore(),
	})
	if res.Err == nil {
		t.Fatal("expected a ScriptError")
	}
	if res.Err.Line != 1 || res.Err.Column != 7 {
		t.Errorf("position = %d:%d, want 1:7", res.Err.Line, res.Err.Column)
	}

	// A syntax error on the first line gets the same treatment.
	res, _ = Run(context.Background(), []*model.Script{{File: "pre.js", Source: "const = 5;"}}, Input{
		Event:   EventPreRequest,
		Request: model.NewRequest("R", "GET", "https://x.example"),
		Vars:    newStore(),
	})
	if res.Err == nil || res.Err.Line != 1 {
		t.Fatalf("syntax error = %+v", res.Err)
	}
	if res.Err.Column != 7 {
		t.Errorf("syntax column = %d, want 7", res.Err.Column)
	}
}

func TestChainStopsAtFirstFailureAndAttributesIt(t *testing.T) {
	scripts := []*model.Script{
		{File: "collection.js", Source: "pm.variables.set('a', 1); console.log('one');"},
		{File: "folder.js", Source: "console.log('two'); undefinedFunction();"},
		{File: "request.js", Source: "console.log('three');"},
	}
	store := newStore()
	res, err := Run(context.Background(), scripts, Input{
		Event:   EventPreRequest,
		Request: model.NewRequest("R", "GET", "https://x.example"),
		Vars:    store,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Err == nil || res.Err.File != "folder.js" {
		t.Fatalf("err = %+v", res.Err)
	}
	if res.Err.ScriptIndex != 1 {
		t.Errorf("index = %d", res.Err.ScriptIndex)
	}
	if got := res.log(); got != "log: one\nlog: two\n" {
		t.Errorf("console = %q; the third script should not have run", got)
	}
}

func TestChainSharesVariablesButNotDeclarations(t *testing.T) {
	scripts := []*model.Script{
		{File: "a.js", Source: "const token = 'from-a'; pm.variables.set('token', token);"},
		{File: "b.js", Source: "const token = 'from-b'; console.log(pm.variables.get('token'), token);"},
	}
	res, err := Run(context.Background(), scripts, Input{
		Event:   EventPreRequest,
		Request: model.NewRequest("R", "GET", "https://x.example"),
		Vars:    newStore(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Err != nil {
		t.Fatalf("redeclaring const across scripts must not fail: %v", res.Err)
	}
	if got := res.log(); got != "log: from-a from-b\n" {
		t.Errorf("console = %q", got)
	}
}

func TestDisabledAndEmptyScriptsAreSkipped(t *testing.T) {
	scripts := []*model.Script{
		{File: "a.js", Source: "console.log('a');", Disabled: true},
		{File: "b.js", Source: "   \n\t\n"},
		nil,
		{File: "c.js", Source: "console.log('c');"},
	}
	res, err := Run(context.Background(), scripts, Input{
		Event:   EventPreRequest,
		Request: model.NewRequest("R", "GET", "https://x.example"),
		Vars:    newStore(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := res.log(); got != "log: c\n" {
		t.Errorf("console = %q", got)
	}
}

func TestNoScriptsReturnsCopyOfRequest(t *testing.T) {
	req := model.NewRequest("R", "GET", "https://x.example")
	res, err := Run(context.Background(), nil, Input{Event: EventPreRequest, Request: req, Vars: newStore()})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Request == req {
		t.Error("Result.Request must not alias Input.Request")
	}
	if res.Request.URL.Raw != req.URL.Raw {
		t.Errorf("url = %q", res.Request.URL.Raw)
	}
}

func TestInputRequestIsNeverMutated(t *testing.T) {
	req := model.NewRequest("R", "GET", "https://x.example")
	req.Headers = []model.KV{{Key: "Keep", Value: "1"}}
	h := newHarness(EventPreRequest)
	h.in.Request = req
	h.script(`pm.request.headers.add({key: 'Added', value: '2'}); pm.request.method = 'DELETE';`)
	res := h.mustRun(t)

	if len(req.Headers) != 1 || req.Method != "GET" {
		t.Errorf("input request was mutated: %+v %s", req.Headers, req.Method)
	}
	if len(res.Request.Headers) != 2 || res.Request.Method != "DELETE" {
		t.Errorf("result request = %+v %s", res.Request.Headers, res.Request.Method)
	}
}

func TestExecutionControls(t *testing.T) {
	t.Run("setNextRequest name", func(t *testing.T) {
		res := newHarness(EventTest).script(`pm.execution.setNextRequest('Next One');`).mustRun(t)
		if res.NextRequest == nil || *res.NextRequest != "Next One" {
			t.Errorf("next = %v", res.NextRequest)
		}
	})
	t.Run("setNextRequest null stops the run", func(t *testing.T) {
		res := newHarness(EventTest).script(`pm.execution.setNextRequest(null);`).mustRun(t)
		if res.NextRequest == nil || *res.NextRequest != "" {
			t.Errorf("next = %v", res.NextRequest)
		}
	})
	t.Run("legacy postman.setNextRequest", func(t *testing.T) {
		res := newHarness(EventTest).script(`postman.setNextRequest('Legacy');`).mustRun(t)
		if res.NextRequest == nil || *res.NextRequest != "Legacy" {
			t.Errorf("next = %v", res.NextRequest)
		}
	})
	t.Run("unset", func(t *testing.T) {
		res := newHarness(EventTest).script(`console.log('nothing');`).mustRun(t)
		if res.NextRequest != nil {
			t.Errorf("next = %v", *res.NextRequest)
		}
	})
	t.Run("skipRequest stops the chain", func(t *testing.T) {
		h := newHarness(EventPreRequest)
		h.script(`console.log('one'); pm.execution.skipRequest();`)
		h.script(`console.log('two');`)
		res := h.mustRun(t)
		if !res.SkipRequest {
			t.Error("SkipRequest = false")
		}
		if got := res.log(); got != "log: one\n" {
			t.Errorf("console = %q", got)
		}
	})
}
