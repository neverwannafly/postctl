package request

import (
	"testing"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

// TestScopeMatchesEngineFolderPrecedence keeps the URL-bar preview honest: it
// must resolve every name to the value the engine sends (folder variables over
// collection variables, inner folder over outer, environment over folders).
// The engine installs folder variables with vars.MemStore.SetFolderVariables;
// see docs/engine.md, "Variable precedence".
func TestScopeMatchesEngineFolderPrecedence(t *testing.T) {
	col := model.NewCollection("C")
	col.Variables = []model.Variable{
		{Key: "a", Value: "collection"}, {Key: "b", Value: "collection"},
		{Key: "c", Value: "collection"}, {Key: "d", Value: "collection"},
	}
	outer, inner := model.NewFolder("Outer"), model.NewFolder("Inner")
	outer.Variables = []model.Variable{
		{Key: "b", Value: "outer"}, {Key: "c", Value: "outer"}, {Key: "d", Value: "outer"},
		{Key: "off", Value: "outer", Disabled: true},
	}
	inner.Variables = []model.Variable{{Key: "c", Value: "inner"}, {Key: "d", Value: "inner"}}
	env := &model.Environment{ID: model.NewID(), Name: "E", Variables: []model.Variable{{Key: "d", Value: "environment"}}}
	globals := &model.Globals{ID: model.NewID(), Variables: []model.Variable{
		{Key: "a", Value: "global"}, {Key: "g", Value: "global"}, {Key: "off", Value: "global"},
	}}
	ws := &model.Workspace{ID: model.NewID(), Globals: globals}
	chain := []model.Container{col, outer, inner}

	const template = "{{a}}/{{b}}/{{c}}/{{d}}/{{g}}/{{off}}/{{missing}}"
	preview, _ := newScope(ws, chain, env, "*", true).resolve(template)

	engine := vars.New(globals.Variables, col.Variables, env.Variables, nil)
	engine.SetFolderVariables(vars.FolderVariables(chain))
	want := engine.Replace(template)

	if preview != want {
		t.Fatalf("preview = %q, engine = %q", preview, want)
	}
	const expected = "collection/outer/inner/environment/global/global/{{missing}}"
	if want != expected {
		t.Fatalf("engine resolution = %q, want %q", want, expected)
	}
}
