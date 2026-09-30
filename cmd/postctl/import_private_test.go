package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/importer/postman"
	"github.com/shubham/postctl/internal/importer/postmanlocal"
	"github.com/shubham/postctl/internal/store"
)

// TestPrivateImportPostmanLocalCommand runs `import postman-local` end to end
// against the local Postman install, into a temp workspace over the in-memory
// secret store (never the Keychain): a dry run, the real import, and a second
// import that must report no changes. Skipped when Postman is absent. It
// checks counts only.
func TestPrivateImportPostmanLocalCommand(t *testing.T) {
	if _, err := postmanlocal.Locate(); err != nil {
		t.Skip("local Postman data is not present")
	}
	memory := store.NewMemorySecrets()
	old := importSecrets
	importSecrets = func() store.SecretStore { return memory }
	t.Cleanup(func() { importSecrets = old })

	workspace := filepath.Join(t.TempDir(), "ws")
	dry := runLocalImport(t, workspace, "--dry-run")
	if !dry.DryRun {
		t.Fatal("dry run flag missing from the report")
	}
	if _, err := os.Stat(filepath.Join(workspace, "postctl.yaml")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote a workspace")
	}

	first := runLocalImport(t, workspace)
	if first.Counts.PreScripts+first.Counts.TestScripts == 0 {
		t.Fatal("the import produced no scripts")
	}
	jsFiles := 0
	_ = filepath.WalkDir(workspace, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(path, ".js") {
			jsFiles++
		}
		return nil
	})
	if jsFiles != first.Counts.PreScripts+first.Counts.TestScripts {
		t.Errorf("wrote %d .js files for %d pre-request and %d test scripts",
			jsFiles, first.Counts.PreScripts, first.Counts.TestScripts)
	}
	if len(dry.Changes.Added) != len(first.Changes.Added) {
		t.Errorf("dry run would add %d, the import added %d", len(dry.Changes.Added), len(first.Changes.Added))
	}

	second := runLocalImport(t, workspace)
	if n := len(second.Changes.Added) + len(second.Changes.Changed) + len(second.Changes.Removed); n != 0 {
		t.Errorf("re-import reported %d changes (%d added, %d changed, %d removed)", n,
			len(second.Changes.Added), len(second.Changes.Changed), len(second.Changes.Removed))
	}
	t.Logf("import: %d collections, %d folders, %d requests, %d environments, %d pre-request + %d test scripts, %d .js files, %d marked secret by name, %d warnings",
		first.Counts.Collections, first.Counts.Folders, first.Counts.Requests, first.Counts.Environments,
		first.Counts.PreScripts, first.Counts.TestScripts, jsFiles, first.Counts.SecretsMarked, len(first.Warnings))
	t.Logf("re-import: %d added, %d changed, %d removed", len(second.Changes.Added), len(second.Changes.Changed), len(second.Changes.Removed))
}

func runLocalImport(t *testing.T, workspace string, extra ...string) postman.Report {
	t.Helper()
	args := append([]string{"import", "postman-local", "--workspace", workspace, "--json"}, extra...)
	code, out, errOut := runCLI(args...)
	if code != exitOK {
		t.Fatalf("import postman-local %v: code=%d stderr=%q", extra, code, errOut)
	}
	var report postman.Report
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("report is not JSON: %v", err)
	}
	return report
}
