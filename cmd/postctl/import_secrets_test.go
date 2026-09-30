package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/store"
)

const (
	synthEnvSecretValue = "synthetic-cli-api-secret-0001"
	synthEnvExport      = `{"id":"env-p","name":"Preview","_postman_variable_scope":"environment","values":[
	 {"key":"auth_token","value":"","type":"default","enabled":true},
	 {"key":"krakenD_token","value":"","type":"default","enabled":true},
	 {"key":"api_secret","value":"` + synthEnvSecretValue + `","type":"default","enabled":true},
	 {"key":"token_url","value":"https://example.invalid/t","type":"default","enabled":true}]}`
)

func TestImportMarksSecretsByNameEndToEnd(t *testing.T) {
	memory := store.NewMemorySecrets()
	old := importSecrets
	importSecrets = func() store.SecretStore { return memory }
	t.Cleanup(func() { importSecrets = old })

	dir := t.TempDir()
	workspace := filepath.Join(dir, "ws")
	export := writeFile(t, filepath.Join(dir, "env.json"), synthEnvExport)

	// The dry run says what would be marked and writes nothing.
	code, out, errOut := runCLI("import", "postman", export, "--workspace", workspace, "--dry-run")
	if code != exitOK || errOut != "" {
		t.Fatalf("dry run: code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{
		"Secrets · 3 would be marked secret by name (1 with a value, would move to Keychain)",
		"Environment › Preview: auth_token, krakenD_token, api_secret",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, synthEnvSecretValue) {
		t.Fatal("dry run printed a value")
	}
	if _, err := os.Stat(filepath.Join(workspace, "postctl.yaml")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote a workspace")
	}

	first := importReport(t, export, "--workspace", workspace, "--json")
	if first.Counts.SecretsMarked != 3 || first.Counts.SecretsMarkedWithValue != 1 {
		t.Fatalf("counts = %+v", first.Counts)
	}
	yaml, err := os.ReadFile(filepath.Join(workspace, "environments", "preview.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(yaml), synthEnvSecretValue) {
		t.Fatal("the environment file holds the secret value")
	}
	if got := strings.Count(string(yaml), "type: secret"); got != 3 {
		t.Fatalf("environment file has %d secret variables, want 3:\n%s", got, yaml)
	}
	if len(memory.Refs()) != 1 {
		t.Fatalf("secret store holds %d values, want only the one that had a value", len(memory.Refs()))
	}

	// Re-import: nothing changes, and the stored value survives.
	second := importReport(t, export, "--workspace", workspace, "--json")
	if n := len(second.Changes.Added) + len(second.Changes.Changed) + len(second.Changes.Removed); n != 0 {
		t.Fatalf("re-import reported %d changes: %+v", n, second.Changes)
	}
	if len(memory.Refs()) != 1 {
		t.Fatalf("re-import changed the secret store: %d values", len(memory.Refs()))
	}
}

func TestImportNoMarkSecretsFlag(t *testing.T) {
	memory := store.NewMemorySecrets()
	old := importSecrets
	importSecrets = func() store.SecretStore { return memory }
	t.Cleanup(func() { importSecrets = old })

	dir := t.TempDir()
	workspace := filepath.Join(dir, "ws")
	export := writeFile(t, filepath.Join(dir, "env.json"), synthEnvExport)
	report := importReport(t, export, "--workspace", workspace, "--json", "--no-mark-secrets")
	if report.Counts.SecretsMarked != 0 || len(report.Marked) != 0 {
		t.Fatalf("--no-mark-secrets still marked variables: %+v", report.Counts)
	}
	yaml, err := os.ReadFile(filepath.Join(workspace, "environments", "preview.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(yaml), "type: secret") {
		t.Fatalf("variables were marked despite --no-mark-secrets:\n%s", yaml)
	}

	for _, source := range []string{"postman", "postman-local"} {
		options, err := parseImportOptions([]string{"--no-mark-secrets"})
		if err != nil || !options.noMarkSecrets || !options.convertOptions().NoMarkSecrets {
			t.Errorf("%s: --no-mark-secrets not parsed: %v", source, err)
		}
	}
	code, out, _ := runCLI("import", "--help")
	if code != exitOK || strings.Count(out, "--no-mark-secrets") < 2 {
		t.Fatalf("help must list --no-mark-secrets for both sources:\n%s", out)
	}
}

func TestImportWarnsAboutEmptyInputs(t *testing.T) {
	memory := store.NewMemorySecrets()
	old := importSecrets
	importSecrets = func() store.SecretStore { return memory }
	t.Cleanup(func() { importSecrets = old })

	dir := t.TempDir()
	collection := writeFile(t, filepath.Join(dir, "c.json"), `{"info":{"_postman_id":"c1","name":"Shop","schema":"`+reimportSchema+`"},
	 "event":[{"listen":"prerequest","script":{"exec":["pm.environment.set('krakenD_token', 'synthetic');"]}}],
	 "item":[{"id":"r","name":"R","event":[{"listen":"prerequest","script":{"exec":["pm.environment.get('krakenD_token');"]}}],
	  "request":{"method":"GET","url":"https://example.invalid/x","header":[{"key":"Authorization","value":"Bearer {{auth_token}}"}]}}]}`)
	env := writeFile(t, filepath.Join(dir, "env.json"), synthEnvExport)
	workspace := filepath.Join(dir, "ws")

	for _, extra := range [][]string{{"--dry-run"}, {}} {
		args := append([]string{"import", "postman", collection, env, "--workspace", workspace}, extra...)
		code, out, errOut := runCLI(args...)
		if code != exitOK || errOut != "" {
			t.Fatalf("%v: code=%d stderr=%q", extra, code, errOut)
		}
		want := "auth_token is empty in Preview; set it with `postctl env set Preview auth_token --secret`"
		if !strings.Contains(out, want) {
			t.Errorf("%v: missing warning %q in:\n%s", extra, want, out)
		}
		if strings.Contains(out, "krakenD_token is empty") {
			t.Errorf("%v: krakenD_token is set by a script and must not be warned about:\n%s", extra, out)
		}
		if strings.Contains(out, synthEnvSecretValue) {
			t.Fatalf("%v: output printed a value", extra)
		}
	}
}
