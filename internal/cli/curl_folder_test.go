package cli

import (
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

// TestCurlResolvesFolderVariables checks copy-as-curl resolves folder
// variables the way send does: inner folder over outer over collection, and
// the environment above all of them.
func TestCurlResolvesFolderVariables(t *testing.T) {
	st, err := store.Init(t.TempDir(), "demo", store.WithSecretStore(store.NewMemorySecrets()))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	col := model.NewCollection("API")
	col.Variables = []model.Variable{{Key: "host", Value: "collection.invalid"}, {Key: "layer", Value: "collection"}}
	outer, inner := model.NewFolder("Outer"), model.NewFolder("Inner")
	outer.Variables = []model.Variable{{Key: "host", Value: "outer.invalid"}, {Key: "layer", Value: "outer"}}
	inner.Variables = []model.Variable{{Key: "host", Value: "inner.invalid"}}
	req := model.NewRequest("Ping", "GET", "https://{{host}}/x")
	req.Headers = []model.KV{{Key: "X-Layer", Value: "{{layer}}"}}
	inner.Items = []model.Item{req}
	outer.Items = []model.Item{inner}
	col.Items = []model.Item{outer}
	ws.Collections = []*model.Collection{col}
	ws.Environments = []*model.Environment{{ID: model.NewID(), Name: "Env",
		Variables: []model.Variable{{Key: "layer", Value: "environment"}}}}
	if err := st.Save(t.Context(), ws); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := exec(st, "curl", "API/Outer/Inner/Ping", "-e", "env")
	if code != ExitOK {
		t.Fatalf("curl: code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, "https://inner.invalid/x") || strings.Contains(out, "{{") {
		t.Fatalf("folder variable not resolved by curl:\n%s", out)
	}
	if !strings.Contains(out, "X-Layer: environment") {
		t.Fatalf("environment must win over folder variables:\n%s", out)
	}
}
