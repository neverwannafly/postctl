package postman

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/shubham/postctl/internal/importer/postmanlocal"
	"github.com/shubham/postctl/internal/model"
)

// goTypedCollection builds a collection the way the local extractor used to
// hand it over: Go-typed slices ([]string exec lines, []map[string]any events)
// and the extractor's named Document type where a file would decode to plain
// []any and map[string]any. Every script is synthetic.
func goTypedCollection() map[string]any {
	event := func(listen string, lines ...string) map[string]any {
		return map[string]any{
			"listen": listen,
			"script": map[string]any{"type": "text/javascript", "exec": lines},
		}
	}
	request := postmanlocal.Document{
		"id":   "req-1",
		"name": "Create",
		"request": postmanlocal.Document{
			"method": "POST",
			"url":    "{{baseUrl}}/things",
			"header": []map[string]any{{"key": "X-Trace", "value": "1"}},
		},
		"event": []map[string]any{
			event("prerequest", "// request pre 1", "// request pre 2"),
			event("test", "// request test"),
		},
	}
	folder := postmanlocal.Document{
		"id":   "folder-1",
		"name": "Folder",
		"item": []any{request},
		"event": []map[string]any{
			event("prerequest", "// folder pre"),
			event("test", "// folder test"),
		},
	}
	return map[string]any{
		"info": map[string]any{
			"_postman_id": "coll-1",
			"name":        "Synthetic",
			"schema":      schemaV21,
		},
		"item": []any{folder},
		"event": []map[string]any{
			event("prerequest", "// collection pre"),
			event("test", "// collection test 1", "// collection test 2"),
		},
		"variable": []map[string]any{
			{"key": "baseUrl", "value": "https://example.invalid"},
			{"key": "retries", "value": 3, "type": "any"},
		},
	}
}

func TestConvertObjectKeepsScriptsFromGoTypedDocument(t *testing.T) {
	doc, report, err := ConvertObject(goTypedCollection(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	collection := doc.Collection
	folder, ok := collection.Items[0].(*model.Folder)
	if !ok {
		t.Fatalf("first item is %T, want a folder", collection.Items[0])
	}
	request, ok := folder.Items[0].(*model.Request)
	if !ok {
		t.Fatalf("folder item is %T, want a request", folder.Items[0])
	}
	cases := []struct {
		level string
		got   model.Scripts
		pre   string
		test  string
	}{
		{"collection", collection.Scripts, "// collection pre", "// collection test 1\n// collection test 2"},
		{"folder", folder.Scripts, "// folder pre", "// folder test"},
		{"request", request.Scripts, "// request pre 1\n// request pre 2", "// request test"},
	}
	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			if tc.got.PreRequest == nil || tc.got.PreRequest.Source != tc.pre {
				t.Errorf("prerequest script = %#v, want source %q", tc.got.PreRequest, tc.pre)
			}
			if tc.got.Test == nil || tc.got.Test.Source != tc.test {
				t.Errorf("test script = %#v, want source %q", tc.got.Test, tc.test)
			}
		})
	}
	if report.Counts.PreScripts != 3 || report.Counts.TestScripts != 3 {
		t.Errorf("script counts = %d pre, %d test; want 3 and 3", report.Counts.PreScripts, report.Counts.TestScripts)
	}
	if len(request.Headers) != 1 || request.Headers[0].Key != "X-Trace" {
		t.Errorf("headers from a []map[string]any were lost: %#v", request.Headers)
	}
	if len(collection.Variables) != 2 || collection.Variables[1].Value != "3" {
		t.Errorf("variables from a []map[string]any were lost: %#v", collection.Variables)
	}
}

func TestConvertObjectMatchesJSONRoundTrip(t *testing.T) {
	inMemory := goTypedCollection()
	encoded, err := json.Marshal(inMemory)
	if err != nil {
		t.Fatal(err)
	}
	fromMemory, memoryReport, err := ConvertObject(inMemory, Options{})
	if err != nil {
		t.Fatal(err)
	}
	fromFile, fileReport, err := Convert(encoded, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromMemory, fromFile) {
		t.Errorf("in-memory and file conversions differ:\nmemory: %+v\nfile:   %+v", fromMemory.Collection, fromFile.Collection)
	}
	if !reflect.DeepEqual(memoryReport, fileReport) {
		t.Errorf("reports differ:\nmemory: %+v\nfile:   %+v", memoryReport, fileReport)
	}

	// The same document decoded as plain JSON shapes converts identically too.
	var plain map[string]any
	if err := json.Unmarshal(encoded, &plain); err != nil {
		t.Fatal(err)
	}
	fromPlain, _, err := ConvertObject(plain, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromMemory, fromPlain) {
		t.Error("Go-typed and JSON-shaped objects convert differently")
	}
}

func TestConvertObjectGoTypedEnvironmentAndGlobals(t *testing.T) {
	for _, scope := range []string{"environment", "globals"} {
		t.Run(scope, func(t *testing.T) {
			raw := map[string]any{
				"id": scope + "-1", "name": "Synthetic",
				"_postman_variable_scope": scope,
				"values": []map[string]any{
					{"key": "region", "value": "eu", "enabled": true, "type": "default"},
					{"key": "count", "value": int64(4), "enabled": false, "type": "any"},
				},
			}
			encoded, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			fromMemory, _, err := ConvertObject(raw, Options{})
			if err != nil {
				t.Fatal(err)
			}
			fromFile, _, err := Convert(encoded, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fromMemory, fromFile) {
				t.Fatalf("conversions differ: %+v vs %+v", fromMemory, fromFile)
			}
			var variables []model.Variable
			if fromMemory.Kind == KindEnvironment {
				variables = fromMemory.Environment.Variables
			} else {
				variables = fromMemory.Globals.Variables
			}
			if len(variables) != 2 || variables[0].Value != "eu" || variables[1].Value != "4" || !variables[1].Disabled {
				t.Fatalf("variables = %#v", variables)
			}
		})
	}
}

func TestConvertObjectRejectsValuesJSONCannotCarry(t *testing.T) {
	raw := goTypedCollection()
	raw["variable"] = []any{map[string]any{"key": "bad", "value": math.NaN()}}
	if _, _, err := ConvertObject(raw, Options{}); err == nil {
		t.Fatal("NaN was accepted; it must be an error, not silently dropped")
	}
	if _, _, err := ConvertObject(nil, Options{}); err == nil {
		t.Fatal("a nil document was accepted")
	}
}
