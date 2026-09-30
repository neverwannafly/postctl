package engine

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
)

// An imported workspace has secret-typed variables with no value (Postman does
// not keep current values). When a script assigns one, the value must land in
// the SecretStore under the variable's ref and never in a YAML file, at every
// persisted scope (plan R3, D2).
func TestScriptWriteToEmptySecretVariableGoesToSecretStoreNotYAML(t *testing.T) {
	const (
		envValue    = "synthetic-env-token-value-aaaa"
		collValue   = "synthetic-coll-token-value-bbbb"
		globalValue = "synthetic-global-secret-value-cccc"
	)
	srv := newServer(t, jsonHandler(`{"ok":true}`))
	f := newFixture(t, srv)
	f.env.Variables = append(f.env.Variables,
		model.Variable{Key: "krakenD_token", Type: model.VarSecret},
		model.Variable{Key: "plain", Value: "1"})
	f.col.Variables = append(f.col.Variables,
		model.Variable{Key: "collection_token", Type: model.VarSecret})
	f.ws.Globals = &model.Globals{ID: model.NewID(), Name: "Globals", Variables: []model.Variable{
		{Key: "global_secret", Type: model.VarSecret},
	}}

	setter := model.NewRequest("Login", "GET", "{{baseUrl}}/login")
	setter.Scripts.PreRequest = js(`
		pm.environment.set("krakenD_token", "synthetic-env-token-" + "value-aaaa");
		pm.collectionVariables.set("collection_token", "synthetic-coll-token-" + "value-bbbb");
		pm.globals.set("global_secret", "synthetic-global-secret-" + "value-cccc");
		pm.environment.set("plain", "2");
	`)
	reader := model.NewRequest("Use", "GET", "{{baseUrl}}/use")
	reader.Headers = []model.KV{{Key: "X-Token", Value: "{{krakenD_token}}"}}
	f.col.Items = append(f.col.Items, setter, reader)
	f.save()

	out := f.send(f.findRequest("Login"), nil)
	if out.Err != nil || out.ScriptError != nil {
		t.Fatalf("err=%v scriptErr=%v", out.Err, out.ScriptError)
	}
	for _, warning := range out.Warnings {
		if strings.Contains(warning, "Could not save") {
			t.Fatalf("a script write could not be saved: %s", warning)
		}
	}

	// Files on disk: no secret value anywhere; the variables are typed secret
	// and carry a ref.
	for path, content := range filesUnder(t, f.dir) {
		for _, value := range []string{envValue, collValue, globalValue} {
			if strings.Contains(content, value) {
				t.Errorf("secret value found in %s", strings.TrimPrefix(path, f.dir))
			}
		}
	}
	check := func(scope string, variables []model.Variable, key, want string) {
		t.Helper()
		var found model.Variable
		for _, v := range variables {
			if v.Key == key {
				found = v
			}
		}
		if !found.IsSecret() || found.Value != "" || found.SecretRef == "" {
			t.Fatalf("%s %s should be a secret held by reference: %+v", scope, key, found)
		}
		if got, err := f.secrets.Get(context.Background(), found.SecretRef); err != nil || got != want {
			t.Fatalf("%s %s: secret store has %d bytes (err %v), want the script's value", scope, key, len(got), err)
		}
	}
	ws, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	check("environment", ws.Environments[0].Variables, "krakenD_token", envValue)
	check("collection", ws.Collections[0].Variables, "collection_token", collValue)
	check("globals", ws.Globals.Variables, "global_secret", globalValue)
	if v, _ := varValue(ws.Environments[0].Variables, "plain"); v != "2" {
		t.Fatalf("the plain variable was not persisted: %q", v)
	}

	// A later send reads the stored value back through the secret store.
	f.send(f.findRequest("Use"), nil)
	srv.mu.Lock()
	hits := append([]http.Header(nil), srv.hdrs...)
	srv.mu.Unlock()
	if len(hits) < 2 || hits[len(hits)-1].Get("X-Token") != envValue {
		t.Fatal("the next request did not resolve the stored secret")
	}
}
