package engine

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

// editDuringSend runs one send whose response handler applies edit to the
// store, the way a TUI autosave lands while a send is in flight, and whose
// test script then runs script.
func editDuringSend(t *testing.T, script string, edit func(f *fixture)) *fixture {
	t.Helper()
	var f *fixture
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		edit(f)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	f = newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Scripts.Test = js(script)
	f.col.Items = append(f.col.Items, req)
	f.env.Variables = append(f.env.Variables, model.Variable{Key: "token", Value: "old"})
	f.col.Variables = []model.Variable{{Key: "c1", Value: "one"}}
	f.ws.Globals = &model.Globals{ID: model.NewID(), Name: "Globals", Variables: []model.Variable{{Key: "g1", Value: "one"}}}
	f.save()
	out := f.send(f.findRequest("R"), nil)
	if out.Err != nil || out.ScriptError != nil {
		t.Fatalf("send failed: %v %v", out.Err, out.ScriptError)
	}
	return f
}

func TestEnvPersistKeepsConcurrentEdit(t *testing.T) {
	f := editDuringSend(t, `pm.environment.set("token", "new");`, func(f *fixture) {
		env := f.reloadEnv()
		env.Variables = append(env.Variables, model.Variable{Key: "region", Value: "eu"})
		env.Production = true
		if err := f.store.SaveEnvironment(context.Background(), env); err != nil {
			panic(err)
		}
	})
	env := f.reloadEnv()
	if v, _ := varValue(env.Variables, "token"); v != "new" {
		t.Fatalf("token = %q, want the script's write", v)
	}
	if v, ok := varValue(env.Variables, "region"); !ok || v != "eu" {
		t.Fatalf("variable added during the send was dropped: %+v", keysOf(env.Variables))
	}
	if !env.Production {
		t.Fatal("environment metadata edited during the send was reverted")
	}
}

func TestGlobalsPersistKeepsConcurrentEdit(t *testing.T) {
	f := editDuringSend(t, `pm.globals.set("g1", "two");`, func(f *fixture) {
		g, err := f.store.LoadGlobals()
		if err != nil || g == nil {
			panic(fmt.Sprint("load globals: ", err))
		}
		g.Variables = append(g.Variables, model.Variable{Key: "g2", Value: "x"})
		if err := f.store.SaveGlobals(context.Background(), g); err != nil {
			panic(err)
		}
	})
	g, err := f.store.LoadGlobals()
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := varValue(g.Variables, "g1"); v != "two" {
		t.Fatalf("g1 = %q, want the script's write", v)
	}
	if _, ok := varValue(g.Variables, "g2"); !ok {
		t.Fatalf("global added during the send was dropped: %+v", keysOf(g.Variables))
	}
}

func TestCollectionVarsPersistKeepsConcurrentEdit(t *testing.T) {
	f := editDuringSend(t, `pm.collectionVariables.set("c1", "two");`, func(f *fixture) {
		c := f.reloadCollection()
		vs := append(c.Variables, model.Variable{Key: "c2", Value: "x"})
		if err := f.store.SaveCollectionVariables(context.Background(), c.ID, vs); err != nil {
			panic(err)
		}
	})
	c := f.reloadCollection()
	if v, _ := varValue(c.Variables, "c1"); v != "two" {
		t.Fatalf("c1 = %q, want the script's write", v)
	}
	if _, ok := varValue(c.Variables, "c2"); !ok {
		t.Fatalf("collection variable added during the send was dropped: %+v", keysOf(c.Variables))
	}
}

func keysOf(vs []model.Variable) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Key
	}
	return out
}
