package store

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

func setVar(vs []model.Variable, key, value string) []model.Variable {
	out := append([]model.Variable(nil), vs...)
	for i := range out {
		if out[i].Key == key {
			out[i].Value = value
			return out
		}
	}
	return append(out, model.Variable{Key: key, Value: value})
}

func find(vs []model.Variable, key string) (model.Variable, bool) {
	for _, v := range vs {
		if v.Key == key {
			return v, true
		}
	}
	return model.Variable{}, false
}

func TestUpdateVariablesKeepsRestOfFile(t *testing.T) {
	ctx := context.Background()
	s, ms, _ := newTestStore(t)
	env := &model.Environment{ID: model.NewID(), Name: "Staging", Production: true, Variables: []model.Variable{
		{Key: "plain", Value: "1"},
		{Key: "sec", Type: model.VarSecret, Value: "synthetic-secret-a"},
	}}
	col := model.NewCollection("API")
	col.Variables = []model.Variable{{Key: "c", Value: "1"}}
	ws := &model.Workspace{ID: model.NewID(), Name: "test", Collections: []*model.Collection{col},
		Environments: []*model.Environment{env}, Globals: &model.Globals{ID: model.NewID(), Name: "Globals"}}
	if err := s.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}

	mutate := func(vs []model.Variable) []model.Variable { return setVar(vs, "added", "x") }
	if err := s.UpdateEnvironmentVariables(ctx, env.ID, mutate); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCollectionVariables(ctx, col.ID, mutate); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateGlobalsVariables(ctx, mutate); err != nil {
		t.Fatal(err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	e := got.Environments[0]
	if e.Name != "Staging" || !e.Production {
		t.Fatalf("environment metadata changed: %q production=%v", e.Name, e.Production)
	}
	if _, ok := find(e.Variables, "added"); !ok || len(e.Variables) != 3 {
		t.Fatalf("environment variables = %d, want 3 including the new key", len(e.Variables))
	}
	sec, _ := find(e.Variables, "sec")
	if sec.Value != "" || sec.SecretRef == "" {
		t.Fatalf("secret must stay a ref in the file: ref=%q hasValue=%v", sec.SecretRef, sec.Value != "")
	}
	if v, err := ms.Get(ctx, sec.SecretRef); err != nil || v != "synthetic-secret-a" {
		t.Fatalf("stored secret disturbed by an unrelated update: err=%v", err)
	}
	if _, ok := find(got.Collections[0].Variables, "added"); !ok {
		t.Fatal("collection update lost")
	}
	if g := got.Globals; g == nil || len(g.Variables) != 1 {
		t.Fatalf("globals = %+v", g)
	}
}

func TestUpdateVariablesSecretAndUnset(t *testing.T) {
	ctx := context.Background()
	s, ms, _ := newTestStore(t)
	env := &model.Environment{ID: model.NewID(), Name: "E", Variables: []model.Variable{
		{Key: "sec", Type: model.VarSecret, Value: "synthetic-old"},
		{Key: "gone", Type: model.VarSecret, Value: "synthetic-gone"},
	}}
	if err := s.Save(ctx, &model.Workspace{ID: model.NewID(), Name: "t", Environments: []*model.Environment{env}}); err != nil {
		t.Fatal(err)
	}
	err := s.UpdateEnvironmentVariables(ctx, env.ID, func(cur []model.Variable) []model.Variable {
		var out []model.Variable
		for _, v := range cur {
			switch v.Key {
			case "sec":
				v.Value = "synthetic-new" // a script wrote it
			case "gone":
				continue
			}
			out = append(out, v)
		}
		return out
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Load()
	vs := got.Environments[0].Variables
	if len(vs) != 1 {
		t.Fatalf("variables = %d, want 1", len(vs))
	}
	if v, _ := ms.Get(ctx, vs[0].SecretRef); v != "synthetic-new" {
		t.Fatal("changed secret not stored")
	}
	if len(ms.Refs()) != 1 {
		t.Fatalf("keychain refs = %d, want the removed variable's secret deleted", len(ms.Refs()))
	}
}

func TestUpdateGlobalsCreatesFileAndSkipsNoop(t *testing.T) {
	ctx := context.Background()
	s, _, _ := newTestStore(t)
	if err := s.UpdateGlobalsVariables(ctx, func(cur []model.Variable) []model.Variable { return cur }); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.LoadGlobals(); g != nil {
		t.Fatal("a no-op update created globals.yaml")
	}
	if err := s.UpdateGlobalsVariables(ctx, func(cur []model.Variable) []model.Variable { return setVar(cur, "k", "v") }); err != nil {
		t.Fatal(err)
	}
	g, err := s.LoadGlobals()
	if err != nil || g == nil || g.ID == "" || len(g.Variables) != 1 {
		t.Fatalf("globals = %+v err=%v", g, err)
	}
}

func TestUpdateMissingOwner(t *testing.T) {
	s, _, _ := newTestStore(t)
	noop := func(cur []model.Variable) []model.Variable { return setVar(cur, "k", "v") }
	if err := s.UpdateEnvironmentVariables(context.Background(), "nope", noop); err == nil {
		t.Fatal("want ErrNotFound for a missing environment")
	}
	if err := s.UpdateCollectionVariables(context.Background(), "nope", noop); err == nil {
		t.Fatal("want ErrNotFound for a missing collection")
	}
}

// TestConcurrentWritersAcrossHandles has several Store handles over one
// directory (the TUI, the engine and a CLI each open their own) mixing narrow
// updates with whole-file saves. Run under -race; every narrow update must
// survive.
func TestConcurrentWritersAcrossHandles(t *testing.T) {
	ctx := context.Background()
	s, ms, root := newTestStore(t)
	env := &model.Environment{ID: model.NewID(), Name: "E"}
	col := model.NewCollection("API")
	if err := s.Save(ctx, &model.Workspace{ID: model.NewID(), Name: "t",
		Collections: []*model.Collection{col}, Environments: []*model.Environment{env}}); err != nil {
		t.Fatal(err)
	}
	const n = 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		h, err := Open(root, WithSecretStore(ms))
		if err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("k%d", i)
		add := func(cur []model.Variable) []model.Variable { return setVar(cur, key, "v") }
		wg.Add(4)
		go func() { defer wg.Done(); check(t, h.UpdateEnvironmentVariables(ctx, env.ID, add)) }()
		go func() { defer wg.Done(); check(t, h.UpdateCollectionVariables(ctx, col.ID, add)) }()
		go func() { defer wg.Done(); check(t, h.UpdateGlobalsVariables(ctx, add)) }()
		go func() { defer wg.Done(); check(t, h.SaveMeta(&model.Workspace{ID: model.NewID(), Name: "t"})) }()
	}
	wg.Wait()
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	g, _ := s.LoadGlobals()
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("k%d", i)
		for name, vs := range map[string][]model.Variable{
			"environment": got.Environments[0].Variables,
			"collection":  got.Collections[0].Variables,
			"globals":     g.Variables,
		} {
			if _, ok := find(vs, key); !ok {
				t.Errorf("%s lost %s", name, key)
			}
		}
	}
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Error(err)
	}
}
