package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

// TestConcurrentStoreWritersLoseNothing hammers the environment, globals and
// collection variables from many goroutines through the engine and through
// direct store writers (as the TUI does). Run under -race.
func TestConcurrentStoreWritersLoseNothing(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	const n = 8
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Scripts.Test = js(`
		pm.environment.set("e_" + pm.iterationData.get("i"), "v");
		pm.globals.set("g_" + pm.iterationData.get("i"), "v");
		pm.collectionVariables.set("c_" + pm.iterationData.get("i"), "v");`)
	f.col.Items = append(f.col.Items, req)
	f.ws.Globals = &model.Globals{ID: model.NewID(), Name: "Globals"}
	f.save()

	// A second engine over a second Store handle models `postctl run` next to
	// the TUI; both share the workspace directory.
	var wg sync.WaitGroup
	ctx := context.Background()
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			out, err := f.eng.Send(ctx, SendInput{
				Collection: f.col, Request: f.findRequest("R"), Environment: f.env,
				Data: map[string]any{"i": fmt.Sprint(i)},
			}, nil)
			if err != nil || out.Err != nil {
				t.Errorf("send %d: %v %v", i, err, out.Err)
			}
		}()
		go func() {
			defer wg.Done()
			// The TUI-style writer adds its own key through the store's
			// narrow update, which must serialize with the engine.
			key := fmt.Sprintf("t_%d", i)
			err := f.store.UpdateEnvironmentVariables(ctx, f.env.ID, func(vs []model.Variable) []model.Variable {
				return append(vs, model.Variable{Key: key, Value: "v"})
			})
			if err != nil {
				t.Errorf("tui writer %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	env := f.reloadEnv()
	for i := 0; i < n; i++ {
		for _, k := range []string{fmt.Sprintf("e_%d", i), fmt.Sprintf("t_%d", i)} {
			if _, ok := varValue(env.Variables, k); !ok {
				t.Errorf("environment lost %s (have %d keys)", k, len(env.Variables))
			}
		}
	}
	g, err := f.store.LoadGlobals()
	if err != nil {
		t.Fatal(err)
	}
	c := f.reloadCollection()
	for i := 0; i < n; i++ {
		if _, ok := varValue(g.Variables, fmt.Sprintf("g_%d", i)); !ok {
			t.Errorf("globals lost g_%d", i)
		}
		if _, ok := varValue(c.Variables, fmt.Sprintf("c_%d", i)); !ok {
			t.Errorf("collection variables lost c_%d", i)
		}
	}
}
