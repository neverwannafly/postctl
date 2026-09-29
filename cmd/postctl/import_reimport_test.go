package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/importer/postman"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

const reimportSchema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

// reimportExport builds a synthetic export; withRefund and withInner choose
// which items the "Postman side" still has.
func reimportExport(withRefund, withInner bool) string {
	items := `{"id":"r-list","name":"List","request":{"method":"GET","url":"https://example.invalid/list"}}`
	if withRefund {
		items += `,{"id":"r-refund","name":"Refund","request":{"method":"POST","url":"https://example.invalid/refund",
		 "auth":{"type":"awsv4","awsv4":[{"key":"secretKey","value":"synthetic-secret-a"},{"key":"sessionToken","value":"synthetic-session-b"}]}}}`
	}
	if withInner {
		items += `,{"id":"f-inner","name":"Inner","item":[{"id":"r-deep","name":"Deep","request":{"method":"GET","url":"https://example.invalid/deep"}}]}`
	}
	return `{"info":{"_postman_id":"col-r","name":"Shop","schema":"` + reimportSchema + `"},"item":[` + items + `]}`
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func importReport(t *testing.T, args ...string) postman.Report {
	t.Helper()
	code, out, errOut := runCLI(append([]string{"import", "postman"}, args...)...)
	if code != exitOK || errOut != "" {
		t.Fatalf("import %v: code=%d stderr=%q", args, code, errOut)
	}
	var report postman.Report
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("report is not JSON: %v", err)
	}
	return report
}

// TestReimportDryRunMatchesRealImport runs the command end to end over a real
// workspace directory with the in-memory secret store: the dry run reports
// exactly what the real import then does, removed items really disappear
// (files and Keychain secrets), and .postctl/ is untouched.
func TestReimportDryRunMatchesRealImport(t *testing.T) {
	memory := store.NewMemorySecrets()
	old := importSecrets
	importSecrets = func() store.SecretStore { return memory }
	t.Cleanup(func() { importSecrets = old })

	dir := t.TempDir()
	workspace := filepath.Join(dir, "ws")
	v1 := writeFile(t, filepath.Join(dir, "v1.json"), reimportExport(true, true))
	v2 := writeFile(t, filepath.Join(dir, "v2.json"), reimportExport(false, false))

	first := importReport(t, v1, "--workspace", workspace, "--json")
	if first.Counts.SecretsMoved != 2 {
		t.Fatalf("first import moved %d secrets, want the AWS secret key and session token", first.Counts.SecretsMoved)
	}
	if len(memory.Refs()) != 2 {
		t.Fatalf("keychain holds %d secrets, want 2", len(memory.Refs()))
	}
	// The moved values are out of every YAML file.
	err := filepath.WalkDir(workspace, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		for _, secret := range []string{"synthetic-secret-a", "synthetic-session-b"} {
			if strings.Contains(string(data), secret) {
				t.Errorf("%s holds a credential in plaintext", filepath.Base(path))
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	state := writeFile(t, filepath.Join(workspace, ".postctl", "state.yaml"), "activeEnvironment: x\n")
	history := writeFile(t, filepath.Join(workspace, ".postctl", "history", "history.jsonl"), "{\"id\":\"h\"}\n")

	dry := importReport(t, v2, "--workspace", workspace, "--dry-run", "--json")
	if !dry.DryRun {
		t.Fatal("dry run flag missing from the report")
	}
	loaded := loadWorkspace(t, workspace, memory)
	if n := len(loaded.Collections[0].Requests()); n != 3 {
		t.Fatalf("dry run changed the workspace: %d requests, want 3", n)
	}

	real := importReport(t, v2, "--workspace", workspace, "--json")
	if !reflect.DeepEqual(dry.Changes, real.Changes) {
		t.Fatalf("dry run and import disagree:\n dry  %+v\n real %+v", dry.Changes, real.Changes)
	}
	wantRemoved := []string{"Shop › Inner", "Shop › Inner › Deep", "Shop › Refund"}
	// The collection itself changed too: its two generated secret variables
	// went away with the request that needed them.
	if !reflect.DeepEqual(real.Changes.Removed, wantRemoved) || len(real.Changes.Added) != 0 ||
		!reflect.DeepEqual(real.Changes.Changed, []string{"Shop"}) {
		t.Fatalf("changes = %+v, want %v removed and the collection changed", real.Changes, wantRemoved)
	}

	loaded = loadWorkspace(t, workspace, memory)
	if names := loaded.Collections[0].Requests(); len(names) != 1 || names[0].Name != "List" {
		t.Fatalf("workspace after re-import has %d requests, want only List", len(names))
	}
	if len(memory.Refs()) != 0 {
		t.Fatalf("keychain still holds %d secrets of removed items", len(memory.Refs()))
	}
	for path, want := range map[string]string{state: "activeEnvironment: x\n", history: "{\"id\":\"h\"}\n"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("%s changed by the re-import (err=%v)", filepath.Base(path), err)
		}
	}

	// Importing the same export again is a no-op for the report.
	again := importReport(t, v2, "--workspace", workspace, "--dry-run", "--json")
	if c := again.Changes; len(c.Added)+len(c.Changed)+len(c.Removed) != 0 {
		t.Fatalf("an identical re-import reported changes: %+v", c)
	}
}

func loadWorkspace(t *testing.T, root string, secrets store.SecretStore) *model.Workspace {
	t.Helper()
	st, err := store.Open(root, store.WithSecretStore(secrets))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}
