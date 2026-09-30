package postmanlocal

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
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
			"scripts": []any{
				map[string]any{"type": "beforeRequest", "code": "pm.variables.set('synthetic', true);"},
				map[string]any{"type": "afterResponse", "code": "pm.test('synthetic', function () {});"},
			},
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
	if result.Coverage.RequestsWithPreRequestScripts != 1 || result.Coverage.RequestsWithTestScripts != 1 {
		t.Fatalf("unexpected script coverage: %+v", result.Coverage)
	}
	if result.Coverage.Environments != 1 || result.Coverage.Globals != 1 {
		t.Fatalf("unexpected variable coverage: %+v", result.Coverage)
	}
	if err := ValidateCollection(result.Collections[0]); err != nil {
		t.Fatal(err)
	}
	event := array(result.Collections[0]["event"])
	exec, _ := object(object(event[0])["script"])["exec"].([]any)
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

// assertJSONShaped fails the test for any value that encoding/json would not
// produce when decoding into an any.
func assertJSONShaped(t *testing.T, path string, value any) {
	t.Helper()
	switch typed := value.(type) {
	case nil, string, bool, float64:
	case []any:
		for i, entry := range typed {
			assertJSONShaped(t, fmt.Sprintf("%s[%d]", path, i), entry)
		}
	case map[string]any:
		for key, entry := range typed {
			assertJSONShaped(t, path+"."+key, entry)
		}
	case Document:
		t.Errorf("%s is a Document, want map[string]any", path)
	default:
		t.Errorf("%s has non-JSON type %T", path, value)
	}
}

func TestBuildResultEmitsOnlyJSONShapes(t *testing.T) {
	// The V8 decoder yields int64, uint64, time.Time, []byte and NaN; extractor
	// code adds Go slices of its own. None of them may reach a document.
	records := []map[string]any{
		wrapped(map[string]any{
			"$kind": "collection", "id": "root", "name": "Synthetic",
			"items":     []any{map[string]any{"$kind": "http-request", "id": "request"}},
			"variables": []any{map[string]any{"key": "n", "value": int64(7), "enabled": true}},
			"scripts":   []any{map[string]any{"type": "beforeRequest", "code": "a\nb"}},
		}),
		wrapped(map[string]any{
			"$kind": "http-request", "id": "request", "name": "Request", "parentId": "root", "collectionId": "root",
			"method": "GET", "url": "https://example.invalid",
			"headers": []any{map[string]any{"key": "K", "value": uint64(9), "disabled": false, "$type": "header"}},
			"body":    map[string]any{"type": "formdata", "content": []any{map[string]any{"key": "f", "value": []byte("xy"), "at": time.Unix(0, 0)}}},
			"scripts": []any{map[string]any{"type": "afterResponse", "code": "pm.test('t', function () {});"}},
			"auth": map[string]any{"type": "bearer", "credentials": []any{
				map[string]any{"key": "token", "value": "synthetic", "extra": math.NaN()},
			}},
		}),
	}
	environments := []map[string]any{wrapped(map[string]any{
		"id": "env", "name": "Env",
		"values": []any{map[string]any{"key": "n", "value": int64(1), "big": big.NewInt(1 << 62).String()}},
	})}
	sessions := []map[string]any{{
		"model": "globals", "modelId": "g", "values": []any{map[string]any{"key": "g", "value": uint64(2)}},
	}}

	result := buildResult(records, environments, sessions)
	for i, document := range result.Collections {
		assertJSONShaped(t, fmt.Sprintf("collection[%d]", i), map[string]any(document))
	}
	for i, document := range result.Environments {
		assertJSONShaped(t, fmt.Sprintf("environment[%d]", i), map[string]any(document))
	}
	assertJSONShaped(t, "globals", map[string]any(result.Globals))
	// It must also survive the JSON encoder the staging writer uses.
	if _, err := json.Marshal(result.Collections); err != nil {
		t.Fatal(err)
	}
}

func TestJSONShapeConversions(t *testing.T) {
	got := jsonShape(map[string]any{
		"i": int(1), "i64": int64(-2), "u": uint32(3), "f": float32(1.5),
		"nan": math.NaN(), "inf": math.Inf(1),
		"time":    time.Date(2026, 9, 30, 1, 2, 3, 0, time.UTC),
		"bytes":   []byte("ab"),
		"strings": []string{"a", "b"},
		"maps":    []map[string]any{{"k": int8(1)}},
		"typed":   map[string]string{"a": "b"},
		"nilmap":  map[string]any(nil),
		"nilarr":  []any(nil),
		"ptr":     new(int),
	})
	want := map[string]any{
		"i": float64(1), "i64": float64(-2), "u": float64(3), "f": 1.5,
		"nan": nil, "inf": nil,
		"time":    "2026-09-30T01:02:03Z",
		"bytes":   "YWI=",
		"strings": []any{"a", "b"},
		"maps":    []any{map[string]any{"k": float64(1)}},
		"typed":   map[string]any{"a": "b"},
		"nilmap":  nil,
		"nilarr":  nil,
		"ptr":     float64(0),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("jsonShape mismatch:\n got %#v\nwant %#v", got, want)
	}

	// A self-referencing graph (V8 object references) must terminate.
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if jsonShape(cyclic) == nil {
		t.Fatal("cyclic input lost its top level")
	}
}
