package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/store"
)

func importerFixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "internal", "importer", "postman", "testdata", name)
}

func TestImportPostmanDryRun(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	code, output, errorOutput := runCLI(
		"import", "postman", importerFixture(t, "minimal-v2.0.json"),
		"--workspace", workspace, "--dry-run",
	)
	if code != 0 || errorOutput != "" {
		t.Fatalf("code=%d stderr=%q", code, errorOutput)
	}
	if !strings.Contains(output, "Dry run — nothing was written") ||
		!strings.Contains(output, "Imported 1 collections") {
		t.Fatalf("unexpected report: %q", output)
	}
	if _, err := os.Stat(filepath.Join(workspace, "postctl.yaml")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote a workspace")
	}
}

func TestImportPostmanWritesWorkspace(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	code, output, errorOutput := runCLI(
		"import", "postman", importerFixture(t, "comprehensive-v2.1.json"),
		"--workspace="+workspace, "--keep-literal-secrets", "--json",
	)
	if code != 0 || errorOutput != "" {
		t.Fatalf("code=%d stderr=%q", code, errorOutput)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("invalid JSON report: %v", err)
	}
	workspaceStore, err := store.Open(workspace)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := workspaceStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Collections) != 1 || len(loaded.Collections[0].Requests()) != 5 {
		t.Fatal("saved workspace did not contain imported requests")
	}
}

func TestImportPartialFailure(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	code, output, errorOutput := runCLI(
		"import", "postman", importerFixture(t, "minimal-v2.0.json"),
		filepath.Join(t.TempDir(), "missing.json"), "--workspace", workspace, "--dry-run",
	)
	if code != exitPartial || errorOutput != "" ||
		!strings.Contains(output, "Not imported") || !strings.Contains(output, "missing.json") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, output, errorOutput)
	}
}

func TestImportUsage(t *testing.T) {
	for _, args := range [][]string{
		{"import"},
		{"import", "unknown"},
		{"import", "postman"},
		{"import", "postman-local", "unexpected.json"},
		{"import", "postman", "--unknown"},
	} {
		code, _, _ := runCLI(args...)
		if code != exitUsage {
			t.Errorf("%v: code=%d", args, code)
		}
	}
	code, output, _ := runCLI("import", "--help")
	if code != exitOK || !strings.Contains(output, "postman-local") {
		t.Fatalf("import help: code=%d output=%q", code, output)
	}
}
