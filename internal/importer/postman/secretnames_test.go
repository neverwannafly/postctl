package postman

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

func TestLooksSecretName(t *testing.T) {
	secret := []string{
		"auth_token", "krakenD_token", "CF_COOKIE_HEADER", "token", "TOKEN", "accessToken",
		"access_token", "refresh_token", "authtoken", "csrf-token", "token2", "oauth2_token",
		"secret", "client_secret", "clientSecret", "CLIENT_SECRET", "jwt_secret", "app_secrets",
		"password", "passwd", "user_password", "DB_PASSWORD", "passphrase", "pwd",
		"apikey", "api_key", "apiKey", "APIKey", "x-api-key", "API_KEY",
		"cookie", "session_cookie", "cookies", "session", "sessionId", "session_id",
		"jwt", "id_jwt", "bearer", "bearerToken", "Authorization", "credential", "credentials",
		"private_key", "privateKey", "signing_key", "encryption_key", "auth_key", "secretKey",
	}
	plain := []string{
		"", "baseUrl", "base_url", "host", "region", "username", "user", "client_id", "clientId",
		"token_url", "tokenUrl", "TOKEN_URL", "token_endpoint", "token_type", "tokenType",
		"token_expires_in", "tokenExpiry", "token_ttl", "access_token_url", "auth_url", "authUrl",
		"authorization_url", "authorization_endpoint", "session_timeout", "session_name",
		"cookie_name", "cookie_domain", "secret_name", "password_policy_url", "password_min_length",
		"bearer_prefix", "api_url", "api_version", "api_host", "key", "keyword", "monkey",
		"tokenizer", "secretary", "authority", "author", "auth", "oauth2_scope", "public_key_url",
	}
	for _, key := range secret {
		if !LooksSecretName(key) {
			t.Errorf("LooksSecretName(%q) = false, want true", key)
		}
	}
	for _, key := range plain {
		if LooksSecretName(key) {
			t.Errorf("LooksSecretName(%q) = true, want false", key)
		}
	}
}

func TestSplitKeyWords(t *testing.T) {
	for key, want := range map[string][]string{
		"krakenD_token":    {"kraken", "d", "token"},
		"APIKey":           {"api", "key"},
		"CF_COOKIE_HEADER": {"cf", "cookie", "header"},
		"token2":           {"token", "2"},
		"x-api-key":        {"x", "api", "key"},
		"clientSecret":     {"client", "secret"},
	} {
		if got := splitKeyWords(key); !reflect.DeepEqual(got, want) {
			t.Errorf("splitKeyWords(%q) = %v, want %v", key, got, want)
		}
	}
}

// Synthetic values only; the names mirror the shapes seen in real data.
const (
	valSyntheticEnvToken  = "synthetic-env-token-value-0001"
	valSyntheticCollToken = "synthetic-collection-token-0002"
	valSyntheticGlobal    = "synthetic-globals-secret-0003"
)

const namedEnvDoc = `{
 "id":"env-preview","name":"Preview","_postman_variable_scope":"environment",
 "values":[
  {"key":"auth_token","value":"","type":"default","enabled":true},
  {"key":"krakenD_token","value":"","type":"default","enabled":true},
  {"key":"CF_COOKIE_HEADER","value":"","type":"default","enabled":true},
  {"key":"api_secret","value":"` + valSyntheticEnvToken + `","type":"default","enabled":true},
  {"key":"already","value":"","type":"secret","enabled":true},
  {"key":"token_url","value":"https://example.invalid/token","type":"default","enabled":true},
  {"key":"baseUrl","value":"https://example.invalid","type":"default","enabled":true}
 ]}`

const namedCollectionDoc = `{
 "info":{"_postman_id":"coll-1","name":"Named","schema":"` + schemaV21 + `"},
 "variable":[{"key":"sessionToken","value":"` + valSyntheticCollToken + `"},{"key":"region","value":"eu"}],
 "item":[{"id":"f1","name":"Folder","variable":[{"key":"folder_password","value":""}],"item":[
   {"id":"r1","name":"Req","request":{"method":"GET","url":"https://example.invalid/x"}}]}]}`

const namedGlobalsDoc = `{
 "id":"globals-1","name":"Globals","_postman_variable_scope":"globals",
 "values":[{"key":"jwt","value":"` + valSyntheticGlobal + `","enabled":true},{"key":"theme","value":"dark","enabled":true}]}`

func convertNamed(t *testing.T, source string, options Options) (*Document, Report) {
	t.Helper()
	doc, report, err := Convert([]byte(source), options)
	if err != nil {
		t.Fatal(err)
	}
	return doc, report
}

func variableByKey(t *testing.T, variables []model.Variable, key string) model.Variable {
	t.Helper()
	for _, v := range variables {
		if v.Key == key {
			return v
		}
	}
	t.Fatalf("variable %q not found", key)
	return model.Variable{}
}

func TestImporterMarksSecretsByName(t *testing.T) {
	env, envReport := convertNamed(t, namedEnvDoc, Options{})
	vars := env.Environment.Variables
	for _, key := range []string{"auth_token", "krakenD_token", "CF_COOKIE_HEADER", "api_secret", "already"} {
		if v := variableByKey(t, vars, key); !v.IsSecret() {
			t.Errorf("environment variable %q is not secret", key)
		}
	}
	for _, key := range []string{"token_url", "baseUrl"} {
		if v := variableByKey(t, vars, key); v.IsSecret() {
			t.Errorf("environment variable %q was marked secret", key)
		}
	}
	// A value the variable already has stays in memory for the store to move
	// (plan D2); empty variables just get the type.
	if got := variableByKey(t, vars, "api_secret").Value; got != valSyntheticEnvToken {
		t.Errorf("api_secret lost its value in memory")
	}
	if got := variableByKey(t, vars, "auth_token").Value; got != "" {
		t.Errorf("auth_token got a value")
	}
	// "already" is secret by Postman's own type and is not counted as marked by name.
	if envReport.Counts.SecretsMarked != 4 || envReport.Counts.SecretsMarkedWithValue != 1 {
		t.Errorf("counts = %+v, want 4 marked, 1 with a value", envReport.Counts)
	}
	wantKeys := []string{"auth_token", "krakenD_token", "CF_COOKIE_HEADER", "api_secret"}
	if len(envReport.Marked) != 1 || envReport.Marked[0].Scope != "Environment › Preview" ||
		!reflect.DeepEqual(envReport.Marked[0].Keys, wantKeys) {
		t.Errorf("marked = %+v", envReport.Marked)
	}

	collection, collReport := convertNamed(t, namedCollectionDoc, Options{})
	if !variableByKey(t, collection.Collection.Variables, "sessionToken").IsSecret() ||
		variableByKey(t, collection.Collection.Variables, "region").IsSecret() {
		t.Errorf("collection variables not marked as expected: %+v", collection.Collection.Variables)
	}
	folder := collection.Collection.Items[0].(*model.Folder)
	if !variableByKey(t, folder.Variables, "folder_password").IsSecret() {
		t.Errorf("folder variable not marked")
	}
	if collReport.Counts.SecretsMarked != 2 || len(collReport.Marked) != 2 {
		t.Errorf("collection report = %+v %+v", collReport.Counts, collReport.Marked)
	}

	globals, globalsReport := convertNamed(t, namedGlobalsDoc, Options{})
	if !variableByKey(t, globals.Globals.Variables, "jwt").IsSecret() ||
		variableByKey(t, globals.Globals.Variables, "theme").IsSecret() {
		t.Errorf("globals not marked as expected: %+v", globals.Globals.Variables)
	}
	if globalsReport.Counts.SecretsMarked != 1 || globalsReport.Marked[0].Scope != "Globals" {
		t.Errorf("globals report = %+v", globalsReport)
	}
}

func TestNoMarkSecretsOptOut(t *testing.T) {
	env, report := convertNamed(t, namedEnvDoc, Options{NoMarkSecrets: true})
	for _, key := range []string{"auth_token", "krakenD_token", "CF_COOKIE_HEADER", "api_secret"} {
		if variableByKey(t, env.Environment.Variables, key).IsSecret() {
			t.Errorf("%q was marked despite NoMarkSecrets", key)
		}
	}
	// Postman's own secret type still applies.
	if !variableByKey(t, env.Environment.Variables, "already").IsSecret() {
		t.Error("a variable Postman typed secret lost the type")
	}
	if report.Counts.SecretsMarked != 0 || len(report.Marked) != 0 {
		t.Errorf("report = %+v %+v", report.Counts, report.Marked)
	}
}

func TestKeepLiteralSecretsKeepsValuedVariablesPlain(t *testing.T) {
	env, report := convertNamed(t, namedEnvDoc, Options{KeepLiteralSecrets: true})
	vars := env.Environment.Variables
	if variableByKey(t, vars, "api_secret").IsSecret() {
		t.Error("a variable with a value was marked although literal secrets are kept")
	}
	for _, key := range []string{"auth_token", "krakenD_token", "CF_COOKIE_HEADER"} {
		if !variableByKey(t, vars, key).IsSecret() {
			t.Errorf("empty variable %q did not get the type", key)
		}
	}
	if report.Counts.SecretsMarked != 3 || report.Counts.SecretsMarkedWithValue != 0 {
		t.Errorf("counts = %+v", report.Counts)
	}
}

func TestReportListsMarkedNamesNeverValues(t *testing.T) {
	_, report := convertNamed(t, namedEnvDoc, Options{})
	for _, dry := range []bool{false, true} {
		report.DryRun = dry
		var text strings.Builder
		report.WriteText(&text)
		encoded, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		out := text.String() + string(encoded)
		if strings.Contains(out, valSyntheticEnvToken) {
			t.Fatalf("report leaked a value (dry=%v):\n%s", dry, out)
		}
		verb := "4 marked secret by name"
		if dry {
			verb = "4 would be marked secret by name"
		}
		if !strings.Contains(text.String(), "Secrets · "+verb) {
			t.Errorf("dry=%v: missing %q in\n%s", dry, verb, text.String())
		}
		if !strings.Contains(text.String(), "Environment › Preview: auth_token, krakenD_token, CF_COOKIE_HEADER, api_secret") {
			t.Errorf("dry=%v: key names missing in\n%s", dry, text.String())
		}
		if !strings.Contains(text.String(), "1 with a value") {
			t.Errorf("dry=%v: with-value note missing in\n%s", dry, text.String())
		}
	}
}

// A name-marked variable's value goes to the SecretStore on save and never
// into YAML; an empty one gets a ref and no value.
func TestMarkedSecretsNeverReachYAML(t *testing.T) {
	root := t.TempDir()
	secrets := store.NewMemorySecrets()
	workspace := &model.Workspace{ID: "w", Name: "W"}
	env, _ := convertNamed(t, namedEnvDoc, Options{})
	coll, _ := convertNamed(t, namedCollectionDoc, Options{})
	globals, _ := convertNamed(t, namedGlobalsDoc, Options{})
	MergeDocuments(workspace, []*Document{env, coll, globals}, &Report{})
	saveWorkspace(t, root, secrets, workspace)

	for _, value := range []string{valSyntheticEnvToken, valSyntheticCollToken, valSyntheticGlobal} {
		found := false
		for _, ref := range secrets.Refs() {
			if got, err := secrets.Get(context.Background(), ref); err == nil && got == value {
				found = true
			}
		}
		if !found {
			t.Errorf("a marked variable's value did not reach the secret store")
		}
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, value := range []string{valSyntheticEnvToken, valSyntheticCollToken, valSyntheticGlobal} {
			if strings.Contains(string(data), value) {
				t.Errorf("%s holds a secret value in plaintext", filepath.Base(path))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Re-importing the same export changes nothing and keeps the value the user
// stored for a marked variable.
func TestReimportOfMarkedSecretsIsIdempotent(t *testing.T) {
	root := t.TempDir()
	secrets := store.NewMemorySecrets()
	workspace := &model.Workspace{ID: "w", Name: "W"}
	docs := func() []*Document {
		env, _ := convertNamed(t, namedEnvDoc, Options{})
		coll, _ := convertNamed(t, namedCollectionDoc, Options{})
		globals, _ := convertNamed(t, namedGlobalsDoc, Options{})
		return []*Document{env, coll, globals}
	}
	first := Report{}
	MergeDocuments(workspace, docs(), &first)
	if len(first.Changes.Added) != 3 {
		t.Fatalf("first import: %+v", first.Changes)
	}
	st, workspace := saveWorkspace(t, root, secrets, workspace)

	// The user sets a token for a marked, empty variable.
	const userToken = "synthetic-user-typed-token-0004"
	if err := st.UpdateEnvironmentVariables(context.Background(), "env-preview", func(current []model.Variable) []model.Variable {
		next := append([]model.Variable(nil), current...)
		for i := range next {
			if next[i].Key == "auth_token" {
				next[i].Value = userToken
			}
		}
		return next
	}); err != nil {
		t.Fatal(err)
	}
	_, workspace = saveWorkspace(t, root, secrets, workspace)

	second := Report{}
	MergeDocuments(workspace, docs(), &second)
	if n := len(second.Changes.Added) + len(second.Changes.Changed) + len(second.Changes.Removed); n != 0 {
		t.Fatalf("second import reported %d changes: %+v", n, second.Changes)
	}
	st, workspace = saveWorkspace(t, root, secrets, workspace)
	if _, err := st.Hydrate(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	env := workspace.Environment("env-preview")
	if env == nil || variableByKey(t, env.Variables, "auth_token").Value != userToken {
		t.Fatal("re-import lost the value the user stored for a marked variable")
	}
}
