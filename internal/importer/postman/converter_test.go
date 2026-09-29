package postman

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestConvertComprehensiveCollection(t *testing.T) {
	document, report, err := Convert(readFixture(t, "comprehensive-v2.1.json"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	collection := document.Collection
	if document.Kind != KindCollection || collection.ID != "collection-1" ||
		collection.Version != "1.2.3" || collection.Description != "Synthetic collection" {
		t.Fatalf("collection metadata was not mapped")
	}
	wantCounts := Counts{
		Collections: 1, Folders: 2, Requests: 5, PreScripts: 2, TestScripts: 2,
		Examples: 1, SecretsMoved: 3,
	}
	if report.Counts != wantCounts {
		t.Fatalf("counts = %+v, want %+v", report.Counts, wantCounts)
	}
	if err := collection.Validate(); err != nil {
		t.Fatal(err)
	}
	if collection.Auth.Bearer.Token != "{{bearer_token}}" {
		t.Fatal("collection bearer literal was not rewritten")
	}
	if len(collection.Variables) != 4 {
		t.Fatalf("variables = %d, want 4", len(collection.Variables))
	}
	for _, key := range []string{"bearer_token", "basic_password", "apikey_value"} {
		variable, ok := model.Lookup(collection.Variables, key)
		if !ok || variable.Type != model.VarSecret || variable.Value == "" {
			t.Fatalf("missing generated secret variable %q", key)
		}
	}

	rawItem, _, _, ok := collection.Find("request-raw")
	if !ok {
		t.Fatal("raw request missing")
	}
	rawRequest := rawItem.(*model.Request)
	if rawRequest.Method != "POST" || len(rawRequest.Headers) != 2 ||
		!rawRequest.Headers[1].Disabled || rawRequest.Headers[0].Key != rawRequest.Headers[1].Key {
		t.Fatal("method or duplicate/disabled headers were not preserved")
	}
	if rawRequest.URL.Raw != "https://{{base}}/v1/:id?enabled=yes" ||
		len(rawRequest.URL.Query) != 2 || !rawRequest.URL.Query[1].Disabled ||
		len(rawRequest.URL.PathVars) != 1 || rawRequest.URL.PathVars[0].Value != "42" {
		t.Fatal("structured URL was not mapped")
	}
	if rawRequest.Body.Mode != model.BodyRaw || rawRequest.Body.Language != "json" ||
		rawRequest.Body.Extra["vendorBody"] != "kept" {
		t.Fatal("raw body was not mapped losslessly")
	}
	if rawRequest.Settings.FollowRedirects == nil || *rawRequest.Settings.FollowRedirects ||
		rawRequest.Settings.Timeout == nil || rawRequest.Settings.Extra["vendorSetting"] != "kept" {
		t.Fatal("protocol profile behavior was not mapped")
	}
	if len(rawRequest.Examples) != 1 || rawRequest.Examples[0].OriginalRequest == nil ||
		len(rawRequest.Examples[0].Cookies) != 1 {
		t.Fatal("saved example was not mapped")
	}
	if !strings.Contains(rawRequest.Scripts.PreRequest.Source, "pm.visualizer") ||
		!strings.Contains(rawRequest.Scripts.PreRequest.Source, "left-pad") ||
		strings.Contains(rawRequest.Scripts.Test.Source, "tests[") {
		t.Fatal("multiple or disabled events were not handled")
	}
	if _, ok := rawRequest.Extra["disabledEvents"]; !ok {
		t.Fatal("mixed disabled event was not preserved in extra")
	}
	if len(report.Warnings) < 3 {
		t.Fatalf("warnings = %d, want lint and unsupported auth warnings", len(report.Warnings))
	}
}

func TestBodyModes(t *testing.T) {
	document, _, err := Convert(readFixture(t, "comprehensive-v2.1.json"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		id    model.ID
		mode  model.BodyMode
		check func(model.Body) bool
	}{
		{"request-raw", model.BodyRaw, func(body model.Body) bool { return body.Language == "json" }},
		{"request-url", model.BodyURLEncoded, func(body model.Body) bool {
			return len(body.URLEncoded) == 1 && body.URLEncoded[0].Disabled
		}},
		{"request-form", model.BodyFormData, func(body model.Body) bool {
			return len(body.FormData) == 2 && body.FormData[1].Type == model.FormFile
		}},
		{"request-binary", model.BodyBinary, func(body model.Body) bool { return body.File != "" }},
		{"request-graphql", model.BodyGraphQL, func(body model.Body) bool {
			return body.GraphQL != nil && body.GraphQL.Variables == `{"id":1}`
		}},
	}
	for _, test := range tests {
		t.Run(string(test.id), func(t *testing.T) {
			item, _, _, ok := document.Collection.Find(test.id)
			if !ok {
				t.Fatal("request missing")
			}
			body := item.(*model.Request).Body
			if body.Mode != test.mode || !test.check(body) {
				t.Fatalf("body mode = %q", body.Mode)
			}
		})
	}
}

func TestFormatsAndVariables(t *testing.T) {
	tests := []struct {
		name string
		kind DocumentKind
	}{
		{"minimal-v2.0.json", KindCollection},
		{"environment.json", KindEnvironment},
		{"globals.json", KindGlobals},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, _, err := Convert(readFixture(t, test.name), Options{})
			if err != nil {
				t.Fatal(err)
			}
			if document.Kind != test.kind {
				t.Fatalf("kind = %q, want %q", document.Kind, test.kind)
			}
		})
	}
	environment, _, _ := Convert(readFixture(t, "environment.json"), Options{})
	if len(environment.Environment.Variables) != 2 ||
		environment.Environment.Variables[1].Type != model.VarSecret ||
		!environment.Environment.Variables[1].Disabled {
		t.Fatal("environment secret/disabled variable was not mapped")
	}
	for _, input := range []string{
		`{"info":{"name":"future","schema":"https://schema.getpostman.com/json/collection/v3/collection.json"}}`,
		`{"name":"unknown"}`,
		`not json`,
		`{"name":"unknown"} trailing`,
	} {
		if _, _, err := Convert([]byte(input), Options{}); err == nil {
			t.Fatalf("expected format error")
		}
	}
}

func TestConvertObjectAcceptsNamedNestedMaps(t *testing.T) {
	type namedMap map[string]any
	raw := map[string]any{
		"info": namedMap{
			"_postman_id": "named",
			"name":        "Named maps",
			"schema":      schemaV21,
		},
		"item": []any{namedMap{
			"id":   "request",
			"name": "Request",
			"request": namedMap{
				"method": "GET",
				"url":    "https://example.invalid",
			},
		}},
	}
	document, report, err := ConvertObject(raw, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.Requests != 1 || len(document.Collection.Requests()) != 1 {
		t.Fatal("named map documents were not converted")
	}
}

func TestAuthTypes(t *testing.T) {
	tests := []struct {
		authType string
		params   string
		want     model.AuthType
	}{
		{"bearer", `[{"key":"token","value":"{{token}}"}]`, model.AuthBearer},
		{"basic", `[{"key":"username","value":"u"},{"key":"password","value":"{{p}}"}]`, model.AuthBasic},
		{"apikey", `[{"key":"key","value":"X"},{"key":"value","value":"{{k}}"},{"key":"in","value":"query"}]`, model.AuthAPIKey},
		{"digest", `[{"key":"username","value":"u"},{"key":"password","value":"{{p}}"}]`, model.AuthDigest},
		{"oauth2", `[{"key":"grant_type","value":"authorization_code_with_pkce"},{"key":"clientSecret","value":"{{s}}"}]`, model.AuthOAuth2},
		{"awsv4", `[{"key":"accessKey","value":"a"},{"key":"secretKey","value":"{{s}}"}]`, model.AuthAWSV4},
		{"hawk", `[{"key":"authId","value":"i"},{"key":"authKey","value":"{{k}}"}]`, model.AuthHawk},
		{"ntlm", `[{"key":"username","value":"u"},{"key":"password","value":"{{p}}"}]`, model.AuthNTLM},
		{"noauth", `[]`, model.AuthNone},
		{"oauth1", `[{"key":"consumerKey","value":"x"}]`, model.AuthUnsupported},
	}
	for _, test := range tests {
		t.Run(test.authType, func(t *testing.T) {
			input := `{"info":{"_postman_id":"c","name":"C","schema":"` + schemaV21 +
				`"},"item":[{"id":"r","name":"R","request":{"method":"GET","url":"https://example.invalid","auth":{"type":"` +
				test.authType + `","` + test.authType + `":` + test.params + `}}}]}`
			document, _, err := Convert([]byte(input), Options{})
			if err != nil {
				t.Fatal(err)
			}
			request := document.Collection.Requests()[0]
			if request.Auth == nil || request.Auth.Type != test.want {
				t.Fatalf("auth = %#v", request.Auth)
			}
			if test.authType == "oauth2" &&
				request.Auth.OAuth2.GrantType != model.GrantAuthorizationCodePKCE {
				t.Fatal("oauth2 grant was not mapped")
			}
		})
	}
}

func TestKeepLiteralSecrets(t *testing.T) {
	document, report, err := Convert(readFixture(t, "comprehensive-v2.1.json"),
		Options{KeepLiteralSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Counts.SecretsMoved != 0 || document.Collection.Auth.Bearer.Token != "synthetic-token" ||
		len(document.Collection.Variables) != 1 {
		t.Fatal("keep-literal-secrets was not respected")
	}
}

func TestMergeKeepsItemsAbsentFromSource(t *testing.T) {
	existingRequest := model.NewRequest("Local only", "GET", "https://example.invalid/local")
	existingRequest.ID = "local"
	existing := &model.Collection{ID: "collection-v2", Name: "Old", Items: []model.Item{existingRequest}}
	workspace := &model.Workspace{ID: "workspace", Name: "Workspace", Collections: []*model.Collection{existing}}
	document, converted, err := Convert(readFixture(t, "minimal-v2.0.json"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	MergeDocuments(workspace, []*Document{document}, &converted)
	if len(workspace.Collections[0].Items) != 2 ||
		workspace.Collections[0].Items[1].ItemID() != "local" {
		t.Fatal("local-only item was not retained")
	}
	if !contains(converted.Changes.Kept, "Version 2 › Local only") ||
		!contains(converted.Changes.Added, "Version 2 › Ping") {
		t.Fatalf("changes = %+v", converted.Changes)
	}
}

func TestMergeDoesNotDuplicateMovedItem(t *testing.T) {
	request := model.NewRequest("Moved", "GET", "https://example.invalid")
	request.ID = "request"
	oldFolder := &model.Folder{ID: "old-folder", Name: "Old", Items: []model.Item{request}}
	existing := &model.Collection{ID: "collection", Name: "Collection", Items: []model.Item{oldFolder}}
	workspace := &model.Workspace{ID: "workspace", Name: "Workspace", Collections: []*model.Collection{existing}}

	importedRequest := model.NewRequest("Moved", "GET", "https://example.invalid")
	importedRequest.ID = "request"
	imported := &model.Collection{
		ID: "collection", Name: "Collection",
		Items: []model.Item{
			&model.Folder{ID: "old-folder", Name: "Old"},
			&model.Folder{ID: "new-folder", Name: "New", Items: []model.Item{importedRequest}},
		},
	}
	report := Report{}
	MergeDocuments(workspace, []*Document{{Kind: KindCollection, Collection: imported}}, &report)
	if len(workspace.Collections[0].Requests()) != 1 {
		t.Fatal("moved request was duplicated")
	}
}

func TestGoldenWorkspace(t *testing.T) {
	documents := make([]*Document, 0, 3)
	report := Report{}
	for _, fixture := range []string{"comprehensive-v2.1.json", "environment.json", "globals.json"} {
		document, converted, err := Convert(readFixture(t, fixture), Options{})
		if err != nil {
			t.Fatal(err)
		}
		documents = append(documents, document)
		report.Merge(converted)
	}
	workspace := &model.Workspace{ID: "workspace-golden", Name: "Imported"}
	MergeDocuments(workspace, documents, &report)
	root := t.TempDir()
	workspaceStore, err := store.Init(root, workspace.Name,
		store.WithSecretStore(store.NewMemorySecrets()))
	if err != nil {
		t.Fatal(err)
	}
	if err := workspaceStore.Save(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.RemoveAll(golden); err != nil {
			t.Fatal(err)
		}
		if err := copyTree(root, golden); err != nil {
			t.Fatal(err)
		}
	}
	assertTreesEqual(t, golden, root)
}

func TestPrivateStaging(t *testing.T) {
	staging := filepath.Join("..", "..", "..", "testdata", "private", "staging")
	if info, err := os.Stat(staging); err != nil || !info.IsDir() {
		t.Skip("private staging data is absent")
	}
	files, failures := collectFixtureJSON(staging)
	if len(failures) > 0 {
		t.Fatal("private staging could not be enumerated")
	}
	var documents []*Document
	report := Report{}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("private staging file could not be read")
		}
		document, converted, err := Convert(data, Options{})
		if err != nil {
			t.Fatal("private staging document could not be converted")
		}
		documents = append(documents, document)
		report.Merge(converted)
	}
	if report.Counts.Collections != 2 || report.Counts.Requests != 23 ||
		report.Counts.PreScripts != 17 || report.Counts.Environments != 3 {
		t.Fatalf("private staging counts did not match expected coverage: collections=%d requests=%d pre=%d environments=%d",
			report.Counts.Collections, report.Counts.Requests, report.Counts.PreScripts, report.Counts.Environments)
	}
	workspace := &model.Workspace{ID: "private-test", Name: "Private test"}
	MergeDocuments(workspace, documents, &report)
	root := t.TempDir()
	workspaceStore, err := store.Init(root, workspace.Name,
		store.WithSecretStore(store.NewMemorySecrets()))
	if err != nil {
		t.Fatal("private test workspace could not be initialized")
	}
	if err := workspaceStore.Save(context.Background(), workspace); err != nil {
		t.Fatal("private test workspace could not be saved")
	}
	if _, err := workspaceStore.Load(); err != nil {
		t.Fatal("private test workspace could not be loaded")
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func collectFixtureJSON(root string) ([]string, []error) {
	var files []string
	var failures []error
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		failures = append(failures, err)
	}
	sort.Strings(files)
	return files, failures
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Name() == "store.lock" {
			return nil // advisory lock file, not workspace content
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func assertTreesEqual(t *testing.T, expected, actual string) {
	t.Helper()
	read := func(root string) map[string]string {
		result := map[string]string{}
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || entry.Name() == ".gitignore" || entry.Name() == "store.lock" {
				return nil
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[relative] = string(data)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	want, got := read(expected), read(actual)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("golden workspace differs; regenerate with UPDATE_GOLDEN=1")
	}
}
