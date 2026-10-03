package complete

import (
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/vars"
)

const sentinel = "SENTINEL-secret-value-0001"

func glyphs() Glyphs { return Glyphs{Mask: "••••", Dot: "·", Ellipsis: "…"} }

func complete(t *testing.T, layers []Layer, before string) (labels []string, details map[string]string, replace int) {
	t.Helper()
	items, replace := Vars(func() []Layer { return layers }, glyphs()).Complete(before)
	details = map[string]string{}
	for _, it := range items {
		labels = append(labels, it.Label)
		details[it.Label] = it.Detail
		if !strings.HasSuffix(it.Insert, "}}") {
			t.Fatalf("%q inserts %q, want name}}", it.Label, it.Insert)
		}
	}
	return labels, details, replace
}

func TestVarsPrecedenceMatchesTheEngine(t *testing.T) {
	folder := model.NewFolder("Charges")
	folder.Variables = []model.Variable{{Key: "b", Value: "folder"}, {Key: "c", Value: "folder"}, {Key: "d", Value: "folder"}}
	layers := []Layer{
		{Scope: "global", Variables: []model.Variable{{Key: "a", Value: "global"}, {Key: "b", Value: "global"}, {Key: "c", Value: "global"}, {Key: "d", Value: "global"}}},
		{Scope: "collection", Variables: []model.Variable{{Key: "b", Value: "collection"}, {Key: "c", Value: "collection"}, {Key: "d", Value: "collection"}}},
		{Scope: "folder Charges", Variables: folder.Variables},
		{Scope: "environment", Variables: []model.Variable{{Key: "d", Value: "environment"}, {Key: "off", Value: "x", Disabled: true}}},
	}
	_, details, _ := complete(t, layers, "{{")
	want := map[string]string{
		"a": "global · global",
		"b": "folder Charges · folder",
		"c": "folder Charges · folder",
		"d": "environment · environment",
	}
	for name, detail := range want {
		if details[name] != detail {
			t.Errorf("%s: detail %q, want %q", name, details[name], detail)
		}
	}
	if _, ok := details["off"]; ok {
		t.Error("a disabled variable was offered")
	}
	// The same layers through the engine give the same winners.
	engine := vars.New(layers[0].Variables, layers[1].Variables, layers[3].Variables, nil)
	engine.SetFolderVariables(vars.FolderVariables([]model.Container{model.NewCollection("C"), folder}))
	for name, scope := range map[string]string{"a": "global", "b": "folder", "c": "folder", "d": "environment"} {
		got, _, _ := engine.Get(name)
		if got != scope {
			t.Errorf("engine resolves %s from %v, completion says %s", name, got, details[name])
		}
	}
}

func TestVarsSecretsAreMasked(t *testing.T) {
	layers := []Layer{{Scope: "environment", Variables: []model.Variable{
		{Key: "token", Value: sentinel, Type: model.VarSecret},
		{Key: "host", Value: "https://api.example.test"},
	}}}
	labels, details, _ := complete(t, layers, "Bearer {{to")
	if len(labels) == 0 || labels[0] != "token" {
		t.Fatalf("labels = %v", labels)
	}
	if !strings.Contains(details["token"], "••••") || strings.Contains(details["token"], sentinel) {
		t.Fatalf("secret detail = %q", details["token"])
	}
	_, details, _ = complete(t, layers, "{{ho")
	if details["host"] != "environment · https://api.example.test" {
		t.Fatalf("plain detail = %q", details["host"])
	}
}

func TestVarsFilteringAndReplaceLength(t *testing.T) {
	layers := []Layer{{Scope: "global", Variables: []model.Variable{{Key: "baseUrl"}, {Key: "apiBase"}, {Key: "token"}}}}
	labels, _, replace := complete(t, layers, `{"u": "{{ba`)
	if replace != len("ba") || len(labels) < 2 || labels[0] != "baseUrl" || labels[1] != "apiBase" {
		t.Fatalf("labels=%v replace=%d (prefix matches first, then substring)", labels, replace)
	}
	for _, l := range labels {
		if strings.HasPrefix(l, "$") {
			t.Fatalf("dynamic %q offered for prefix ba", l)
		}
	}
	labels, _, _ = complete(t, layers, "{{$gu")
	if len(labels) == 0 || labels[0] != "$guid" {
		t.Fatalf("dynamic labels = %v", labels)
	}
	labels, _, replace = complete(t, layers, "{{ ba")
	if replace != len(" ba") || len(labels) == 0 {
		t.Fatalf("leading space: labels=%v replace=%d", labels, replace)
	}
}

func TestVarsNothingOutsideAnOpenReference(t *testing.T) {
	layers := []Layer{{Scope: "global", Variables: []model.Variable{{Key: "a"}}}}
	for _, before := range []string{"", "plain", "{{a}}", "{{a}} and ", "{ {", `{{a "`, "{{a b"} {
		if labels, _, _ := complete(t, layers, before); len(labels) != 0 {
			t.Errorf("%q offered %v", before, labels)
		}
	}
	if labels, _, _ := complete(t, layers, "{{a}} {{"); len(labels) == 0 {
		t.Error("a second reference after a closed one must complete")
	}
}

func TestChainReturnsTheFirstNonEmpty(t *testing.T) {
	layers := []Layer{{Scope: "global", Variables: []model.Variable{{Key: "a"}}}}
	c := Chain(nil, PM(), Vars(func() []Layer { return layers }, glyphs()))
	if items, _ := c.Complete("pm.env"); len(items) == 0 || items[0].Label != "environment" {
		t.Fatalf("pm items = %v", items)
	}
	if items, _ := c.Complete("x = '{{"); len(items) == 0 {
		t.Fatal("vars were not consulted")
	}
	if Chain() != nil {
		t.Fatal("an empty chain must be nil")
	}
}
