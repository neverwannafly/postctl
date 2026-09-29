package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/importer/postman"
	"github.com/shubham/postctl/internal/store"
)

// privateStaging is the user's extracted Postman data. It is gitignored and
// may be absent. The test copies it and only asserts names.
const privateStaging = "/Users/shubham/work/postctl/testdata/private/staging"

func TestPrivateStagingLs(t *testing.T) {
	if _, err := os.Stat(privateStaging); err != nil {
		t.Skip("private staging not present")
	}
	copy, err := copyTree(privateStaging, t.TempDir())
	if err != nil {
		t.Fatalf("copy failed: %v", err)
	}
	dir := t.TempDir()
	secrets := store.NewMemorySecrets()
	st, err := store.Init(dir, "private", store.WithSecretStore(secrets))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	var docs []*postman.Document
	var report postman.Report
	err = filepath.WalkDir(copy, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		doc, converted, convErr := postman.Convert(data, postman.Options{})
		if convErr != nil {
			t.Fatalf("convert %s failed", filepath.Base(path))
		}
		docs = append(docs, doc)
		report.Merge(converted)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	postman.MergeDocuments(ws, docs, &report)
	if err := st.Save(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := exec(st, "ls", "--json")
	if code != ExitOK || errOut != "" {
		t.Fatalf("ls code=%d err=%q bytes=%d", code, errOut, len(out))
	}
	var doc lsDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var requests int
	var walk func(lsItem)
	walk = func(it lsItem) {
		names[it.Name] = true
		if it.Type == "request" {
			requests++
		}
		for _, c := range it.Children {
			walk(c)
		}
	}
	for _, it := range doc.Items {
		walk(it)
	}
	for _, name := range []string{"Integrity APIs", "Integrity ML Service"} {
		if !names[name] {
			t.Fatalf("missing collection %q (saw %d items)", name, len(doc.Items))
		}
	}
	if requests == 0 {
		t.Fatal("private ls listed no requests")
	}
	code, out, errOut = exec(st, "env", "ls")
	if code != ExitOK || errOut != "" {
		t.Fatalf("env ls code=%d err=%q", code, errOut)
	}
	for _, name := range []string{"Preview", "Private", "Production"} {
		if !strings.Contains(out, name) {
			t.Fatalf("env ls missing %q", name)
		}
	}
	t.Logf("private ls: %d collections, %d requests, %d env names checked", len(doc.Items), requests, 3)
}

func copyTree(src, dst string) (string, error) {
	root := filepath.Join(dst, "staging")
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		return "", err
	}
	return root, nil
}
