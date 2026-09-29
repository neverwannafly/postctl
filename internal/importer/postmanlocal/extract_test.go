package postmanlocal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildResultPreservesTreeAndScripts(t *testing.T) {
	records := []map[string]any{
		wrapped(map[string]any{
			"$kind": "collection", "id": "root", "name": "Synthetic",
			"items":   []any{map[string]any{"$kind": "collection", "id": "folder"}},
			"scripts": []any{map[string]any{"type": "beforeRequest", "code": "line one\nline two"}},
		}),
		wrapped(map[string]any{
			"$kind": "collection", "id": "folder", "name": "Folder", "parentId": "root", "collectionId": "root",
			"items": []any{map[string]any{"$kind": "http-request", "id": "request"}},
		}),
		wrapped(map[string]any{
			"$kind": "http-request", "id": "request", "name": "Request", "parentId": "folder", "collectionId": "root",
			"method": "POST", "url": "https://example.invalid/resource",
			"headers": []any{map[string]any{"key": "Content-Type", "value": "application/json"}},
			"body":    map[string]any{"type": "json", "content": "{}"},
			"scripts": []any{map[string]any{"type": "afterResponse", "code": "pm.test('synthetic', function () {});"}},
		}),
	}
	environments := []map[string]any{wrapped(map[string]any{
		"id": "environment", "name": "Synthetic", "values": []any{},
	})}
	sessions := []map[string]any{{
		"model": "globals", "modelId": "global", "values": []any{},
	}}

	result := buildResult(records, environments, sessions)
	if result.Coverage.Collections != 1 || result.Coverage.Folders != 1 || result.Coverage.Requests != 1 {
		t.Fatalf("unexpected tree coverage: %+v", result.Coverage)
	}
	if result.Coverage.PreRequestScripts != 1 || result.Coverage.TestScripts != 1 {
		t.Fatalf("unexpected script coverage: %+v", result.Coverage)
	}
	if result.Coverage.Environments != 1 || result.Coverage.Globals != 1 {
		t.Fatalf("unexpected variable coverage: %+v", result.Coverage)
	}
	if err := ValidateCollection(result.Collections[0]); err != nil {
		t.Fatal(err)
	}
	event := array(result.Collections[0]["event"])
	exec, _ := object(object(event[0])["script"])["exec"].([]string)
	if len(exec) != 2 || exec[0] != "line one" || exec[1] != "line two" {
		t.Fatalf("script lines were not preserved: %#v", exec)
	}
}

func TestWriteStaging(t *testing.T) {
	result := buildResult([]map[string]any{wrapped(map[string]any{
		"$kind": "collection", "id": "root", "name": "Synthetic / Collection", "items": []any{},
	})}, nil, nil)
	dir := t.TempDir()
	if err := result.WriteStaging(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "collections", "Synthetic-Collection.postman_collection.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document Document
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCollection(document); err != nil {
		t.Fatal(err)
	}
}

func TestLocateAt(t *testing.T) {
	root := t.TempDir()
	partition := "synthetic-partition"
	if err := os.MkdirAll(filepath.Join(root, "storage"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Partitions", partition, "IndexedDB"), 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := `{"v8Partitions":{"synthetic-partition":{}},"v8PartitionsNamespaceMeta":{"users":{"activePartition":"synthetic-partition"}}}`
	if err := os.WriteFile(filepath.Join(root, "storage", "userPartitionData.json"), []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	install, err := locateAt(root)
	if err != nil {
		t.Fatal(err)
	}
	if install.PartitionID != partition {
		t.Fatalf("partition = %q", install.PartitionID)
	}
}

func TestPrivateIndexedDB(t *testing.T) {
	privateRoot := filepath.Join("testdata", "private")
	indexedDB := filepath.Join(privateRoot, "IndexedDB")
	if stat, err := os.Stat(indexedDB); err != nil || !stat.IsDir() {
		t.Skip("private IndexedDB fixture is not present")
	}
	result, err := Extract(context.Background(), Install{
		DataDir:     privateRoot,
		PartitionID: "private",
		IndexedDB:   indexedDB,
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, collection := range result.Collections {
		if err := ValidateCollection(collection); err != nil {
			t.Fatal(err)
		}
	}
}

func wrapped(value map[string]any) map[string]any {
	return map[string]any{"id": value["id"], "value": value}
}
