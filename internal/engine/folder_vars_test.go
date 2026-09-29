package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

// folderFixture builds API > Outer > Inner > R. Every level defines "where"
// (and "outerOnly", "innerOnly" for their own level), the collection and
// environment define "layer" so precedence can be read off the wire.
func folderFixture(t *testing.T, srv *server) (*fixture, *model.Request) {
	t.Helper()
	f := newFixture(t, srv)
	f.col.Variables = []model.Variable{
		{Key: "where", Value: "collection"},
		{Key: "layer", Value: "collection"},
		{Key: "collOnly", Value: "c"},
	}
	f.env.Variables = append(f.env.Variables, model.Variable{Key: "layer", Value: "environment"})
	outer := model.NewFolder("Outer")
	outer.Variables = []model.Variable{
		{Key: "where", Value: "outer"},
		{Key: "layer", Value: "outer"},
		{Key: "outerOnly", Value: "o"},
		{Key: "off", Value: "outer-disabled", Disabled: true},
	}
	inner := model.NewFolder("Inner")
	inner.Variables = []model.Variable{{Key: "where", Value: "inner"}, {Key: "innerOnly", Value: "i"}}
	req := model.NewRequest("R", "GET", "{{baseUrl}}/{{where}}")
	req.Headers = []model.KV{
		{Key: "X-Layer", Value: "{{layer}}"},
		{Key: "X-Outer", Value: "{{outerOnly}}"},
		{Key: "X-Inner", Value: "{{innerOnly}}"},
		{Key: "X-Coll", Value: "{{collOnly}}"},
		{Key: "X-Off", Value: "{{off}}"},
	}
	inner.Items = []model.Item{req}
	outer.Items = []model.Item{inner}
	f.col.Items = append(f.col.Items, outer)
	return f, req
}

func TestFolderVariablesResolveOnSend(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f, _ := folderFixture(t, srv)
	f.save()

	out := f.send(f.findRequest("R"), nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if hits := srv.hits(); len(hits) != 1 || hits[0] != "/inner" {
		t.Fatalf("path = %v, want the innermost folder's value", hits)
	}
	h := srv.hdrs[0]
	for header, want := range map[string]string{
		"X-Layer": "environment", // environment beats folders and the collection
		"X-Outer": "o",           // outer folder variables reach nested requests
		"X-Inner": "i",
		"X-Coll":  "c", // collection variables still apply
		"X-Off":   "{{off}}",
	} {
		if got := h.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if len(out.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", out.Warnings)
	}
}

// `--var` (SendInput.Locals / RunInput.Locals) is the Local layer, the top of
// global < collection < folder < environment < data < local, so it beats the
// folder and environment layers on both send and run, and data sits under it.
func TestLocalVarsBeatFolderVariables(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f, _ := folderFixture(t, srv)
	f.save()
	locals := map[string]any{"where": "local", "layer": "local"}

	out, err := f.eng.Send(context.Background(), SendInput{
		Collection: f.col, Request: f.findRequest("R"), Environment: f.env,
		Data: map[string]any{"where": "data", "outerOnly": "data"}, Locals: locals,
	}, nil)
	if err != nil || out.Err != nil {
		t.Fatalf("send: %v %v", err, out.Err)
	}
	h := srv.hdrs[0]
	if got := srv.hits()[0]; got != "/local" {
		t.Errorf("send path = %q, want the local value over folder and data", got)
	}
	if got := h.Get("X-Layer"); got != "local" {
		t.Errorf("send X-Layer = %q, want local over environment", got)
	}
	if got := h.Get("X-Outer"); got != "data" {
		t.Errorf("send X-Outer = %q, want data over the folder value", got)
	}
	if got := h.Get("X-Inner"); got != "i" {
		t.Errorf("send X-Inner = %q, want the folder value (nothing overrides it)", got)
	}

	sum, _ := f.run(RunInput{Locals: locals})
	if sum == nil || sum.Requests != 1 {
		t.Fatalf("run summary: %+v", sum)
	}
	if got := srv.hits()[1]; got != "/local" {
		t.Errorf("run path = %q, want the local value over the folder value", got)
	}
	h = srv.hdrs[1]
	if got := h.Get("X-Layer"); got != "local" {
		t.Errorf("run X-Layer = %q, want local over environment", got)
	}
	if got := h.Get("X-Inner"); got != "i" {
		t.Errorf("run X-Inner = %q, want the folder value", got)
	}
}

// Sending a request outside the folders must not see their variables.
func TestFolderVariablesAreScopedToTheirFolder(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f, _ := folderFixture(t, srv)
	top := model.NewRequest("Top", "GET", "{{baseUrl}}/{{where}}")
	f.col.Items = append(f.col.Items, top)
	f.save()

	f.send(f.findRequest("Top"), nil)
	if hits := srv.hits(); len(hits) != 1 || hits[0] != "/collection" {
		t.Fatalf("path = %v, want the collection value", hits)
	}
}

// One run visits requests under different folders: each must resolve with
// its own folder chain, and the variables never leak between requests.
func TestFolderVariablesPerRequestInRun(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	a, b := model.NewFolder("A"), model.NewFolder("B")
	a.Variables = []model.Variable{{Key: "who", Value: "a"}, {Key: "onlyA", Value: "1"}}
	b.Variables = []model.Variable{{Key: "who", Value: "b"}}
	a.Items = []model.Item{model.NewRequest("RA", "GET", "{{baseUrl}}/{{who}}/{{onlyA}}")}
	b.Items = []model.Item{model.NewRequest("RB", "GET", "{{baseUrl}}/{{who}}/{{onlyA}}")}
	f.col.Items = append(f.col.Items, a, b)
	f.save()

	sum, err := f.eng.Run(context.Background(), RunInput{Collection: f.col, Environment: f.env}, nil)
	if err != nil || sum.Requests != 2 {
		t.Fatalf("run: %v %+v", err, sum)
	}
	got := strings.Join(srv.hits(), " ")
	if got != "/a/1 /b/%7B%7BonlyA%7D%7D" {
		t.Fatalf("paths = %q", got)
	}
}

// A script sees folder variables through pm.variables and writes to the
// collection scope, never into the folder.
func TestFolderVariablesInScripts(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f, req := folderFixture(t, srv)
	req.Scripts.PreRequest = js(`
		pm.environment.set("seenWhere", pm.variables.get("where"));
		pm.collectionVariables.set("written", "w");`)
	f.save()

	f.send(f.findRequest("R"), nil)
	if v, _ := varValue(f.reloadEnv().Variables, "seenWhere"); v != "inner" {
		t.Fatalf("pm.variables.get(where) = %q, want the folder value", v)
	}
	col := f.reloadCollection()
	if _, ok := varValue(col.Variables, "written"); !ok {
		t.Fatal("collection variable write lost")
	}
	if v, _ := varValue(col.Variables, "where"); v != "collection" {
		t.Fatalf("collection variable where = %q; a folder value must not be written into the collection", v)
	}
	var inner *model.Folder
	_ = col.Walk(func(it model.Item, _ model.Container, _ int) error {
		if fo, ok := it.(*model.Folder); ok && fo.Name == "Inner" {
			inner = fo
		}
		return nil
	})
	if inner == nil || len(inner.Variables) != 2 {
		t.Fatalf("folder variables were modified: %+v", inner)
	}
}

// A secret folder variable is hydrated from the secret store for the send
// and masked in history.
func TestFolderSecretVariable(t *testing.T) {
	const secretValue = "synthetic-folder-secret-91"
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	folder := model.NewFolder("F")
	folder.Variables = []model.Variable{{Key: "fkey", Type: model.VarSecret, Value: secretValue}}
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Headers = []model.KV{{Key: "X-Plain", Value: "{{fkey}}"}}
	folder.Items = []model.Item{req}
	f.col.Items = append(f.col.Items, folder)
	f.save()

	out := f.send(f.findRequest("R"), nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if got := srv.hdrs[0].Get("X-Plain"); got != secretValue {
		t.Fatal("secret folder variable was not resolved on send")
	}
	for path, content := range filesUnder(t, f.dir) {
		if strings.Contains(content, secretValue) {
			t.Fatalf("secret value written to %s", path)
		}
	}
}

func TestFolderVariablesHelper(t *testing.T) {
	col := model.NewCollection("C")
	col.Variables = []model.Variable{{Key: "c", Value: "1"}}
	outer, inner := model.NewFolder("O"), model.NewFolder("I")
	outer.Variables = []model.Variable{{Key: "k", Value: "outer"}, {Key: "d", Value: "x", Disabled: true}}
	inner.Variables = []model.Variable{{Key: "k", Value: "inner"}}
	got := vars.FolderVariables([]model.Container{col, outer, inner})
	if len(got) != 2 || got[0].Value != "outer" || got[1].Value != "inner" {
		t.Fatalf("FolderVariables = %+v", got)
	}
	s := vars.New(nil, col.Variables, nil, nil)
	s.SetFolderVariables(got)
	if v, _, _ := s.Get("k"); v != "inner" {
		t.Fatalf("k = %v", v)
	}
	s.SetFolderVariables(nil)
	if _, _, ok := s.Get("k"); ok {
		t.Fatal("folder variables were not replaced")
	}
}
