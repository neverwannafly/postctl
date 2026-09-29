package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

// Concurrent sends each start from the same stale copy of the environment.
// Every write must still land: a persist adds to what other sends saved
// instead of overwriting the file with its own snapshot.
func TestConcurrentSendsLoseNoWrites(t *testing.T) {
	const n = 24
	srv := newServer(t, jsonHandler(`{"ok":true}`))
	f := newFixture(t, srv)
	f.env.Variables = append(f.env.Variables, model.Variable{Key: "s", Value: "secret-value-0001", Type: model.VarSecret})
	f.ws.Globals = &model.Globals{ID: model.NewID(), Name: "Globals"}
	for i := 0; i < n; i++ {
		req := model.NewRequest(fmt.Sprintf("R%d", i), "GET", fmt.Sprintf("{{baseUrl}}/r%d", i))
		req.Scripts.PreRequest = js(fmt.Sprintf(`
			pm.environment.set("env%d", "e%d");
			pm.collectionVariables.set("col%d", "c%d");
			pm.globals.set("glob%d", "g%d");
			pm.environment.set("shared", "writer%d");
		`, i, i, i, i, i, i, i))
		req.Scripts.Test = js(fmt.Sprintf(`pm.test("t%d", () => pm.response.to.have.status(200));`, i))
		f.col.Items = append(f.col.Items, req)
	}
	f.save()

	var wg sync.WaitGroup
	outcomes := make([]*Outcome, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := f.eng.Send(context.Background(), SendInput{
				Collection: f.col, Request: f.findRequest(fmt.Sprintf("R%d", i)), Environment: f.env,
			}, nil)
			if err != nil {
				t.Error(err)
				return
			}
			outcomes[i] = out
		}()
	}
	wg.Wait()

	for i, out := range outcomes {
		if out == nil || out.Err != nil || out.ScriptError != nil || out.TestCounts().Passed != 1 {
			t.Fatalf("send %d: %+v", i, out)
		}
		if len(out.Warnings) != 0 {
			t.Fatalf("send %d warnings: %v", i, out.Warnings)
		}
	}
	env := f.reloadEnv()
	col := f.reloadCollection()
	ws, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if v, _ := varValue(env.Variables, fmt.Sprintf("env%d", i)); v != fmt.Sprintf("e%d", i) {
			t.Errorf("environment write %d lost", i)
		}
		if v, _ := varValue(col.Variables, fmt.Sprintf("col%d", i)); v != fmt.Sprintf("c%d", i) {
			t.Errorf("collection write %d lost", i)
		}
		if v, _ := varValue(ws.Globals.Variables, fmt.Sprintf("glob%d", i)); v != fmt.Sprintf("g%d", i) {
			t.Errorf("globals write %d lost", i)
		}
	}
	if _, ok := varValue(env.Variables, "shared"); !ok {
		t.Error("shared variable missing")
	}
	// The untouched secret is still held by reference.
	for _, v := range env.Variables {
		if v.Key == "s" && (v.Value != "" || v.SecretRef == "") {
			t.Errorf("secret variable damaged: %+v", v)
		}
	}
	// One history entry per send.
	if got, _ := f.eng.cfg.History.(interface{ Len() (int, error) }).Len(); got != n {
		t.Errorf("history has %d entries, want %d", got, n)
	}
}

func TestConcurrentRunAndSend(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	for i := 0; i < 4; i++ {
		req := model.NewRequest(fmt.Sprintf("R%d", i), "GET", "{{baseUrl}}/x")
		req.Scripts.Test = js(fmt.Sprintf(`pm.environment.set("k%d", "v");`, i))
		f.col.Items = append(f.col.Items, req)
	}
	f.save()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := f.eng.Run(context.Background(), RunInput{Collection: f.col, Environment: f.env, Iterations: 3}, nil); err != nil {
			t.Error(err)
		}
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = f.eng.Send(context.Background(), SendInput{
				Collection: f.col, Request: f.findRequest(fmt.Sprintf("R%d", i)), Environment: f.env,
			}, nil)
		}()
	}
	wg.Wait()
	env := f.reloadEnv()
	for i := 0; i < 4; i++ {
		if _, ok := varValue(env.Variables, fmt.Sprintf("k%d", i)); !ok {
			t.Errorf("k%d missing", i)
		}
	}
}
