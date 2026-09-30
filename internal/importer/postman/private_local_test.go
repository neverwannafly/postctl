package postman

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/importer/postmanlocal"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

// TestPrivateLocalExtractInMemoryToStore runs the real path a user hits with
// `postctl import postman-local`, with nothing staged as JSON in between:
// postmanlocal.Extract (in-memory documents) → ConvertObject → MergeDocuments →
// store.Save into a temp workspace over the in-memory SecretStore. The staged
// JSON tests could not see a Go-typed extractor value being dropped; this one
// can.
//
// It reads the local Postman install read-only (Extract copies the IndexedDB
// to a private temp directory first) and is skipped when Postman is absent.
// It logs counts, key names and collection names only.
func TestPrivateLocalExtractInMemoryToStore(t *testing.T) {
	install, err := postmanlocal.Locate()
	if err != nil {
		t.Skip("local Postman data is not present")
	}
	result, err := postmanlocal.Extract(context.Background(), install, postmanlocal.Options{})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	cov := result.Coverage
	if cov.Requests == 0 {
		t.Skip("the local Postman cache holds no requests")
	}

	// Every value the extractor hands over is a JSON shape.
	for i, document := range result.Collections {
		assertPrivateJSONShaped(t, fmt.Sprintf("collection %d", i+1), map[string]any(document))
	}
	for i, document := range result.Environments {
		assertPrivateJSONShaped(t, fmt.Sprintf("environment %d", i+1), map[string]any(document))
	}
	assertPrivateJSONShaped(t, "globals", map[string]any(result.Globals))

	var (
		documents []*Document
		report    = Report{}
	)
	add := func(label string, raw postmanlocal.Document) {
		document, converted, err := ConvertObject(map[string]any(raw), Options{})
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		documents = append(documents, document)
		report.Merge(converted)
	}
	for i, document := range result.Collections {
		add(fmt.Sprintf("collection %d", i+1), document)
	}
	for i, document := range result.Environments {
		add(fmt.Sprintf("environment %d", i+1), document)
	}
	if result.Globals != nil {
		add("globals", result.Globals)
	}

	// The importer's own script counts equal the extractor's: the extractor
	// counts request-level scripts, the importer every level.
	containerPre, containerTest := containerScriptCounts(result.Collections)
	if want := cov.RequestsWithPreRequestScripts + containerPre; report.Counts.PreScripts != want {
		t.Errorf("converted %d pre-request scripts, extractor found %d (requests %d + containers %d)",
			report.Counts.PreScripts, want, cov.RequestsWithPreRequestScripts, containerPre)
	}
	if want := cov.RequestsWithTestScripts + containerTest; report.Counts.TestScripts != want {
		t.Errorf("converted %d test scripts, extractor found %d (requests %d + containers %d)",
			report.Counts.TestScripts, want, cov.RequestsWithTestScripts, containerTest)
	}

	CheckEmptyInputs(documents, &report)
	workspace := &model.Workspace{ID: "private-local", Name: "Private local"}
	MergeDocuments(workspace, documents, &report)

	root := t.TempDir()
	secrets := store.NewMemorySecrets()
	saved, err := store.Init(root, workspace.Name, store.WithSecretStore(secrets))
	if err != nil {
		t.Fatal("private test workspace could not be initialized")
	}
	if err := saved.Save(context.Background(), workspace); err != nil {
		t.Fatalf("private test workspace could not be saved: %v", err)
	}

	// .js files on disk: request scripts (<stem>.pre.js, <stem>.test.js) equal
	// the extractor's coverage; container scripts (pre.js, test.js) equal the
	// collection and folder events the extractor emitted.
	files := countScriptFiles(t, filepath.Join(root, "collections"))
	if files.requestPre != cov.RequestsWithPreRequestScripts || files.requestTest != cov.RequestsWithTestScripts {
		t.Errorf("wrote %d request pre-request and %d test .js files, extractor coverage is %d and %d",
			files.requestPre, files.requestTest, cov.RequestsWithPreRequestScripts, cov.RequestsWithTestScripts)
	}
	if files.containerPre != containerPre || files.containerTest != containerTest {
		t.Errorf("wrote %d collection/folder pre-request and %d test .js files, extractor emitted %d and %d",
			files.containerPre, files.containerTest, containerPre, containerTest)
	}
	if files.empty > 0 {
		t.Errorf("%d .js files are empty", files.empty)
	}

	// Reloaded from disk, the scripts have their source.
	loaded, err := saved.Load()
	if err != nil {
		t.Fatalf("saved workspace did not load: %v", err)
	}
	loadedPre, loadedTest := 0, 0
	for _, collection := range loaded.Collections {
		count := func(scripts model.Scripts) {
			if scripts.PreRequest != nil && scripts.PreRequest.Source != "" {
				loadedPre++
			}
			if scripts.Test != nil && scripts.Test.Source != "" {
				loadedTest++
			}
		}
		count(collection.Scripts)
		_ = collection.Walk(func(item model.Item, _ model.Container, _ int) error {
			switch typed := item.(type) {
			case *model.Folder:
				count(typed.Scripts)
			case *model.Request:
				count(typed.Scripts)
			}
			return nil
		})
	}
	if loadedPre != files.requestPre+files.containerPre || loadedTest != files.requestTest+files.containerTest {
		t.Errorf("reloaded %d pre-request and %d test scripts with source, expected %d and %d",
			loadedPre, loadedTest, files.requestPre+files.containerPre, files.requestTest+files.containerTest)
	}

	// Nothing that went to the (in-memory) secret store is in any file.
	stored := 0
	for _, ref := range secrets.Refs() {
		value, err := secrets.Get(context.Background(), ref)
		if err != nil || len(value) < 6 {
			continue
		}
		stored++
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			if data, readErr := os.ReadFile(path); readErr == nil && strings.Contains(string(data), value) {
				t.Errorf("%s holds a value that belongs in the secret store", filepath.Base(path))
			}
			return nil
		})
	}

	// Names and counts only.
	t.Logf("coverage: %d collections, %d folders, %d requests, %d requests with pre-request scripts, %d with test scripts, %d environments, %d globals",
		cov.Collections, cov.Folders, cov.Requests, cov.RequestsWithPreRequestScripts,
		cov.RequestsWithTestScripts, cov.Environments, cov.Globals)
	t.Logf("container scripts: %d pre-request, %d test", containerPre, containerTest)
	t.Logf(".js files written: request %d pre + %d test; collection/folder %d pre + %d test",
		files.requestPre, files.requestTest, files.containerPre, files.containerTest)
	t.Logf("secret store entries with a value: %d", stored)
	t.Logf("marked secret by name: %d (%d with a value)", report.Counts.SecretsMarked, report.Counts.SecretsMarkedWithValue)
	for _, scope := range report.Marked {
		t.Logf("  marked in %s: %s", scope.Scope, strings.Join(scope.Keys, ", "))
	}
	// Why each marked key is (not) warned about, and whether a script could
	// create a secret-looking variable in an environment that lacks it.
	scan := newInputScan()
	var environments []*model.Environment
	for _, document := range documents {
		switch document.Kind {
		case KindEnvironment:
			scan.environment(document.Environment)
			environments = append(environments, document.Environment)
		case KindGlobals:
			scan.variables("globals", "", document.Globals.Variables)
		case KindCollection:
			scan.collection(document.Collection)
		}
	}
	markedKeys := map[string]bool{}
	for _, scope := range report.Marked {
		for _, key := range scope.Keys {
			markedKeys[key] = true
		}
	}
	names := make([]string, 0, len(markedKeys))
	for key := range markedKeys {
		names = append(names, key)
	}
	sort.Strings(names)
	for _, key := range names {
		t.Logf("  %s: read=%v setByScript=%v valuedAnywhere=%v", key, scan.read[key], scan.set[key], scan.valued[key])
	}
	for key := range scan.set {
		if !LooksSecretName(key) {
			continue
		}
		var missing []string
		for _, env := range environments {
			found := false
			for _, variable := range env.Variables {
				found = found || variable.Key == key
			}
			if !found {
				missing = append(missing, env.Name)
			}
		}
		t.Logf("  script-set secret-looking key %s is missing from %d of %d environments %v", key, len(missing), len(environments), missing)
	}
	var warned []string
	for _, warning := range report.Warnings {
		if warning.Code == "empty-input" {
			warned = append(warned, warning.Message)
		}
	}
	sort.Strings(warned)
	t.Logf("empty-input warnings: %d", len(warned))
	for _, message := range warned {
		t.Logf("  warn: %s", message)
	}
}

// containerScriptCounts counts the collection- and folder-level prerequest
// and test events in extractor documents.
func containerScriptCounts(collections []postmanlocal.Document) (pre, test int) {
	var visit func(node map[string]any)
	visit = func(node map[string]any) {
		for _, kind := range eventKinds(node) {
			if kind == "prerequest" {
				pre++
			} else {
				test++
			}
		}
		items, _ := node["item"].([]any)
		for _, entry := range items {
			child, _ := entry.(map[string]any)
			if child == nil {
				if document, ok := entry.(postmanlocal.Document); ok {
					child = map[string]any(document)
				}
			}
			if _, isFolder := child["item"]; isFolder {
				visit(child)
			}
		}
	}
	for _, collection := range collections {
		visit(map[string]any(collection))
	}
	return pre, test
}

// eventKinds returns "prerequest" and/or "test", once each, for a node that
// has at least one event of that kind.
func eventKinds(node map[string]any) []string {
	seen := map[string]bool{}
	events, _ := node["event"].([]any)
	for _, entry := range events {
		event, _ := entry.(map[string]any)
		if listen, _ := event["listen"].(string); listen == "prerequest" || listen == "test" {
			seen[listen] = true
		}
	}
	var out []string
	for _, kind := range []string{"prerequest", "test"} {
		if seen[kind] {
			out = append(out, kind)
		}
	}
	return out
}

type scriptFiles struct {
	requestPre, requestTest     int
	containerPre, containerTest int
	empty                       int
}

func countScriptFiles(t *testing.T, root string) scriptFiles {
	t.Helper()
	var counts scriptFiles
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".js") {
			return err
		}
		name := entry.Name()
		switch {
		case name == "pre.js":
			counts.containerPre++
		case name == "test.js":
			counts.containerTest++
		case strings.HasSuffix(name, ".pre.js"):
			counts.requestPre++
		case strings.HasSuffix(name, ".test.js"):
			counts.requestTest++
		default:
			t.Errorf("unexpected script file name %q", name)
		}
		if data, readErr := os.ReadFile(path); readErr != nil || strings.TrimSpace(string(data)) == "" {
			counts.empty++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

// assertPrivateJSONShaped fails for any value encoding/json would not produce
// when decoding into an any. It reports paths and Go types, never values.
func assertPrivateJSONShaped(t *testing.T, path string, value any) {
	t.Helper()
	switch typed := value.(type) {
	case nil, string, bool, float64:
	case []any:
		for i, entry := range typed {
			assertPrivateJSONShaped(t, fmt.Sprintf("%s[%d]", path, i), entry)
		}
	case map[string]any:
		for key, entry := range typed {
			assertPrivateJSONShaped(t, path+"."+key, entry)
		}
	default:
		t.Errorf("%s has non-JSON type %T", path, value)
	}
}
