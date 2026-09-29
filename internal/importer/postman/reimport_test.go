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

// exportV1 is a synthetic collection with a nested folder, scripts, a secret
// variable and an OAuth 2.0 request.
const exportV1 = `{
 "info":{"_postman_id":"col-1","name":"Shop","schema":"` + schemaV21 + `"},
 "variable":[{"key":"host","value":"example.invalid"},{"key":"apiKey","value":"synthetic-key-value-1","type":"secret"}],
 "auth":{"type":"bearer","bearer":[{"key":"token","value":"synthetic-collection-token-1"}]},
 "event":[{"listen":"prerequest","script":{"exec":["console.log('col')"]}}],
 "item":[
  {"id":"f-orders","name":"Orders","item":[
    {"id":"r-list","name":"List","event":[{"listen":"test","script":{"exec":["pm.test('ok', function(){});"]}}],
     "request":{"method":"GET","url":"https://{{host}}/orders"}},
    {"id":"r-refund","name":"Refund","request":{"method":"POST","url":"https://{{host}}/refund",
     "auth":{"type":"oauth2","oauth2":[{"key":"grant_type","value":"client_credentials"},
       {"key":"accessTokenUrl","value":"https://example.invalid/token"},{"key":"accessToken","value":"synthetic-access-1"}]}}},
    {"id":"f-inner","name":"Inner","item":[
      {"id":"r-deep","name":"Deep","request":{"method":"GET","url":"https://{{host}}/deep"}}]}]},
  {"id":"r-ping","name":"Ping","request":{"method":"GET","url":"https://{{host}}/ping"}}
 ]}`

func convertDoc(t *testing.T, source string) *Document {
	t.Helper()
	document, _, err := Convert([]byte(source), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// saveWorkspace writes workspace to a fresh store over secrets and reloads it,
// the way a real import followed by the next run would see it.
func saveWorkspace(t *testing.T, root string, secrets store.SecretStore, workspace *model.Workspace) (*store.Store, *model.Workspace) {
	t.Helper()
	st, err := store.Open(root, store.WithSecretStore(secrets))
	if err != nil {
		st, err = store.Init(root, "W", store.WithSecretStore(secrets))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Save(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	return st, loaded
}

func importInto(workspace *model.Workspace, source *Document) Report {
	report := Report{}
	MergeDocuments(workspace, []*Document{source}, &report)
	return report
}

func TestReimportOverwritesByID(t *testing.T) {
	root := t.TempDir()
	secrets := store.NewMemorySecrets()
	workspace := &model.Workspace{ID: "w", Name: "W"}
	first := importInto(workspace, convertDoc(t, exportV1))
	if len(first.Changes.Added) != 1 || first.Changes.Added[0] != "Shop" || len(first.Changes.Removed) != 0 {
		t.Fatalf("first import: %+v", first.Changes)
	}
	_, workspace = saveWorkspace(t, root, secrets, workspace)

	// The user edits locally: a header on Ping, a local-only request.
	local := model.NewRequest("Local scratch", "GET", "https://example.invalid/scratch")
	workspace.Collections[0].Items = append(workspace.Collections[0].Items, local)
	ping, _, _, _ := workspace.Collections[0].Find("r-ping")
	ping.(*model.Request).Headers = []model.KV{{Key: "X-Local", Value: "edit"}}

	// Postman: Refund and the Inner folder (with Deep) were deleted, List's
	// URL changed, Ping is untouched, Audit is new.
	doc := convertDoc(t, exportV1)
	col := doc.Collection
	orders := col.Items[0].(*model.Folder)
	orders.Items = orders.Items[:1] // keep only List; drops Refund and Inner/Deep
	orders.Items[0].(*model.Request).URL = model.ParseURL("https://{{host}}/orders?limit=5")
	col.Items = append(col.Items, func() model.Item {
		r := model.NewRequest("Audit", "GET", "https://{{host}}/audit")
		r.ID = "r-audit"
		return r
	}())
	report := importInto(workspace, doc)

	wantChanges := Changes{
		Added:   []string{"Shop › Audit"},
		Changed: []string{"Shop › Orders › List"},
		Removed: []string{"Shop › Local scratch", "Shop › Orders › Inner", "Shop › Orders › Inner › Deep", "Shop › Orders › Refund"},
	}
	// Ping's local edit is replaced by the export, so Ping is a change too.
	wantChanges.Changed = append([]string{"Shop › Ping"}, wantChanges.Changed...)
	sort.Strings(wantChanges.Changed)
	if got := report.Changes; !reflect.DeepEqual(got.Added, wantChanges.Added) ||
		!reflect.DeepEqual(got.Changed, wantChanges.Changed) || !reflect.DeepEqual(got.Removed, wantChanges.Removed) {
		t.Fatalf("changes:\n got %+v\nwant %+v", got, wantChanges)
	}
	if len(report.Changes.Kept) != 0 {
		t.Fatalf("kept = %v; items missing from the export are removed, not kept", report.Changes.Kept)
	}

	_, saved := saveWorkspace(t, root, secrets, workspace)
	names := requestNames(saved.Collections[0])
	if want := []string{"List", "Audit", "Ping"}; !sameSet(names, want) {
		t.Fatalf("requests after re-import = %v, want %v", names, want)
	}
	pingAfter, _, _, _ := saved.Collections[0].Find("r-ping")
	if len(pingAfter.(*model.Request).Headers) != 0 {
		t.Fatal("a local edit survived the re-import: fields must be replaced, not merged")
	}
	if _, err := os.Stat(filepath.Join(root, "collections", "shop", "orders", "inner")); !os.IsNotExist(err) {
		t.Fatal("the removed folder's directory is still on disk")
	}
}

func TestReimportReportIsExactWhenNothingChanged(t *testing.T) {
	root := t.TempDir()
	secrets := store.NewMemorySecrets()
	workspace := &model.Workspace{ID: "w", Name: "W"}
	env := `{"id":"env-1","name":"Staging","_postman_variable_scope":"environment","values":[
	  {"key":"a","value":"1","enabled":true},{"key":"s","value":"synthetic-env-secret","type":"secret","enabled":true}]}`
	globals := `{"id":"g-1","name":"Globals","_postman_variable_scope":"globals","values":[{"key":"g","value":"1","enabled":true}]}`
	documents := func() []*Document {
		return []*Document{convertDoc(t, exportV1), convertDoc(t, env), convertDoc(t, globals)}
	}
	MergeDocuments(workspace, documents(), &Report{})
	_, workspace = saveWorkspace(t, root, secrets, workspace)

	// A dry run of the same export over the saved workspace changes nothing.
	report := Report{}
	MergeDocuments(workspace, documents(), &report)
	if c := report.Changes; len(c.Added)+len(c.Changed)+len(c.Removed)+len(c.Kept) != 0 {
		t.Fatalf("re-importing an identical export reported changes: %+v", c)
	}
}

func TestReimportReportsOnlyWhatChanged(t *testing.T) {
	root := t.TempDir()
	secrets := store.NewMemorySecrets()
	workspace := &model.Workspace{ID: "w", Name: "W"}
	MergeDocuments(workspace, []*Document{convertDoc(t, exportV1)}, &Report{})
	_, workspace = saveWorkspace(t, root, secrets, workspace)

	doc := convertDoc(t, exportV1)
	deep, _, _, _ := doc.Collection.Find("r-deep")
	deep.(*model.Request).Scripts.Test = &model.Script{Source: "pm.test('x', function(){});"}
	report := Report{}
	MergeDocuments(workspace, []*Document{doc}, &report)
	// Only Deep's script changed. Its folders are unchanged: a folder is
	// reported for its own fields, not for what is inside it.
	if got := report.Changes.Changed; !reflect.DeepEqual(got, []string{"Shop › Orders › Inner › Deep"}) ||
		len(report.Changes.Added) != 0 || len(report.Changes.Removed) != 0 {
		t.Fatalf("changes = %+v", report.Changes)
	}
}

func TestReimportMovedItemIsNotRemovedAndAdded(t *testing.T) {
	workspace := &model.Workspace{ID: "w", Name: "W"}
	MergeDocuments(workspace, []*Document{convertDoc(t, exportV1)}, &Report{})
	doc := convertDoc(t, exportV1)
	moved, err := doc.Collection.Remove("r-ping")
	if err != nil {
		t.Fatal(err)
	}
	inner, _, _, _ := doc.Collection.Find("f-inner")
	folder := inner.(*model.Folder)
	folder.Items = append(folder.Items, moved)
	report := Report{}
	MergeDocuments(workspace, []*Document{doc}, &report)
	if len(report.Changes.Added) != 0 || len(report.Changes.Removed) != 0 ||
		!reflect.DeepEqual(report.Changes.Changed, []string{"Shop › Orders › Inner › Ping"}) {
		t.Fatalf("a moved request must be one change: %+v", report.Changes)
	}
}

func TestReimportKeepsUserLocalState(t *testing.T) {
	root := t.TempDir()
	secrets := store.NewMemorySecrets()
	workspace := &model.Workspace{ID: "w", Name: "W"}
	MergeDocuments(workspace, []*Document{convertDoc(t, exportV1)}, &Report{})
	_, workspace = saveWorkspace(t, root, secrets, workspace)

	// User-local files under .postctl/ (history, cookies, UI state) and an
	// environment and a second collection the export knows nothing about.
	local := map[string]string{
		".postctl/state.yaml":             "activeEnvironment: keep\n",
		".postctl/history/history.jsonl":  "{\"id\":\"h1\"}\n",
		".postctl/cookies.json":           "[]\n",
		".postctl/notes-from-user.txt":    "mine\n",
		"collections/notes-from-user.txt": "not owned by the store\n",
	}
	for name, content := range local {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	other := model.NewCollection("Other")
	workspace.Collections = append(workspace.Collections, other)
	workspace.Environments = append(workspace.Environments,
		&model.Environment{ID: "env-local", Name: "Local", Variables: []model.Variable{{Key: "k", Value: "v"}}})

	doc := convertDoc(t, exportV1)
	doc.Collection.Items = doc.Collection.Items[:1] // Ping removed
	report := importInto(workspace, doc)
	if len(report.Changes.Removed) == 0 {
		t.Fatal("test setup: nothing removed")
	}
	if !contains(report.Changes.Kept, "Other") || !contains(report.Changes.Kept, "Environment › Local") {
		t.Fatalf("untouched nodes must be listed as kept: %+v", report.Changes.Kept)
	}
	_, saved := saveWorkspace(t, root, secrets, workspace)

	for name, content := range local {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(data) != content {
			t.Errorf("%s was lost or modified by the re-import (err=%v)", name, err)
		}
	}
	if len(saved.Collections) != 2 || len(saved.Environments) != 1 {
		t.Fatalf("collections=%d environments=%d, want the untouched ones kept", len(saved.Collections), len(saved.Environments))
	}
}

func TestReimportKeepsLocallyFetchedOAuthTokenAndDropsRemovedOnes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	secrets := store.NewMemorySecrets()
	// First import without tokens in the export.
	noToken := strings.Replace(exportV1, `,{"key":"accessToken","value":"synthetic-access-1"}`, "", 1)
	workspace := &model.Workspace{ID: "w", Name: "W"}
	MergeDocuments(workspace, []*Document{convertDoc(t, noToken)}, &Report{})
	st, workspace := saveWorkspace(t, root, secrets, workspace)

	// The user fetches a token for Refund (Get new token) and one for Deep.
	for id, value := range map[model.ID]string{"r-refund": "synthetic-fetched-1", "r-deep": "synthetic-fetched-2"} {
		item, _, _, _ := workspace.Collections[0].Find(id)
		request := item.(*model.Request)
		if request.Auth == nil {
			request.Auth = &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{GrantType: model.GrantClientCredentials}}
		}
		ref := store.SecretRefFor(store.ScopeOAuth2, id, "")
		request.Auth.OAuth2.Token = &model.OAuth2Token{Ref: ref}
		if err := secrets.Set(ctx, ref, `{"access":"`+value+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Save(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	before := workspace.Collections[0].Clone()

	// Re-import: Refund stays (export has an oauth2 block, no token), Deep
	// is gone from the export.
	doc := convertDoc(t, noToken)
	inner, _, _, _ := doc.Collection.Find("f-inner")
	inner.(*model.Folder).Items = nil
	report := importInto(workspace, doc)
	st, saved := saveWorkspace(t, root, secrets, workspace)
	st.DeleteRemovedSecrets(ctx, before)

	refund, _, _, _ := saved.Collections[0].Find("r-refund")
	token := refund.(*model.Request).Auth.OAuth2.Token
	if token == nil || token.Ref == "" {
		t.Fatal("re-import dropped the token the user fetched for a node that is still in the export")
	}
	if _, err := secrets.Get(ctx, token.Ref); err != nil {
		t.Fatal("the kept token's secret was deleted")
	}
	if _, err := secrets.Get(ctx, store.SecretRefFor(store.ScopeOAuth2, "r-deep", "")); err == nil {
		t.Fatal("the removed request's token secret was left in the secret store")
	}
	if contains(report.Changes.Changed, "Shop › Orders › Refund") {
		t.Fatalf("the carried token must not make Refund look changed: %+v", report.Changes)
	}
	// The collection's own secret variable is still in the store.
	col := saved.Collections[0]
	variable, _ := model.Lookup(col.Variables, "apiKey")
	if v, err := secrets.Get(ctx, variable.SecretRef); err != nil || v != "synthetic-key-value-1" {
		t.Fatal("a secret variable that is still in the export was deleted")
	}
}

func requestNames(c *model.Collection) []string {
	var names []string
	for _, r := range c.Requests() {
		names = append(names, r.Name)
	}
	return names
}

func sameSet(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return reflect.DeepEqual(x, y)
}
