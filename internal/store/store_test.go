package store

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/model"
	"go.yaml.in/yaml/v3"
)

var update = flag.Bool("update", false, "rewrite golden files under testdata/golden")

var ctx = context.Background()

func newTestStore(t *testing.T) (*Store, *MemorySecrets, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ws")
	ms := NewMemorySecrets()
	s, err := Init(root, "test", WithSecretStore(ms))
	if err != nil {
		t.Fatal(err)
	}
	return s, ms, root
}

// clearScriptFiles removes the store-derived Script.File values so a loaded
// model can be compared with the one that was saved.
func clearScriptFiles(ws *model.Workspace) {
	clear := func(sc *model.Scripts) {
		if sc.PreRequest != nil {
			sc.PreRequest.File = ""
		}
		if sc.Test != nil {
			sc.Test.File = ""
		}
	}
	for _, c := range ws.Collections {
		clear(&c.Scripts)
		_ = c.Walk(func(it model.Item, _ model.Container, _ int) error {
			switch n := it.(type) {
			case *model.Folder:
				clear(&n.Scripts)
			case *model.Request:
				clear(&n.Scripts)
			}
			return nil
		})
	}
}

func dump(v any) string {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func requireEqual(t *testing.T, want, got any) {
	t.Helper()
	if reflect.DeepEqual(want, got) {
		return
	}
	w, g := strings.Split(dump(want), "\n"), strings.Split(dump(got), "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			t.Fatalf("models differ at line %d:\n want: %s\n  got: %s", i+1, a, b)
		}
	}
	t.Fatal("models differ (DeepEqual false but JSON equal; nil vs empty?)")
}

func TestRoundTripRichWorkspace(t *testing.T) {
	s, _, _ := newTestStore(t)
	want := richWorkspace()
	if err := s.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	// Load does not read secrets.
	if v, _ := model.Lookup(got.Environments[0].Variables, "apiKey"); v.Value != "" {
		t.Fatal("Load must not hydrate secrets")
	}
	if missing, err := s.Hydrate(ctx, got); err != nil {
		t.Fatal(err)
	} else if len(missing) != 1 || missing[0] != SecretRefFor(ScopeEnvironment, "env-prod", "empty") {
		t.Fatalf("missing = %v", missing)
	}
	clearScriptFiles(got)
	requireEqual(t, richWorkspace(), got)
	_ = want
}

func TestSaveDoesNotMutateInput(t *testing.T) {
	s, _, _ := newTestStore(t)
	ws := richWorkspace()
	if err := s.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}
	requireEqual(t, richWorkspace(), ws)
}

func TestSecretsNeverWrittenToFiles(t *testing.T) {
	s, ms, root := newTestStore(t)
	if err := s.Save(ctx, richWorkspace()); err != nil {
		t.Fatal(err)
	}
	for _, sentinel := range []string{secretPassword, secretToken, secretRefresh, secretEnvKey} {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				data, _ := os.ReadFile(p)
				if strings.Contains(string(data), sentinel) {
					t.Errorf("secret %q leaked into %s", sentinel, p)
				}
			}
			return nil
		})
	}
	refs := ms.Refs()
	for _, want := range []model.SecretRef{
		SecretRefFor(ScopeEnvironment, "env-dev", "apiKey"),
		SecretRefFor(ScopeGlobals, "globals-1", "gsecret"),
		SecretRefFor(ScopeCollection, "coll-pay", "apiSecret"),
		SecretRefFor(ScopeFolder, "f-charges", "folderSecret"),
		SecretRefFor(ScopeOAuth2, "req-oauth", ""),
	} {
		if _, err := ms.Get(ctx, want); err != nil {
			t.Errorf("missing secret %s (have %v)", want, refs)
		}
	}
	if len(refs) != 5 {
		t.Errorf("unexpected secrets: %v", refs)
	}
}

func TestSecretVariableWithoutStoreFails(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ws")
	s, err := Init(root, "x")
	if err != nil {
		t.Fatal(err)
	}
	env := &model.Environment{ID: "e", Name: "E", Variables: []model.Variable{{Key: "k", Type: model.VarSecret, Value: "v"}}}
	if err := s.SaveEnvironment(ctx, env); !errors.Is(err, ErrNoSecretStore) {
		t.Fatalf("got %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, environmentsDir)); len(entries) != 0 {
		t.Fatal("nothing must be written when a secret cannot be stored")
	}
	// A secret with no value needs no store (only a ref is written).
	env.Variables[0].Value = ""
	if err := s.SaveEnvironment(ctx, env); err != nil {
		t.Fatal(err)
	}
}

func TestSecretCleanup(t *testing.T) {
	s, ms, _ := newTestStore(t)
	env := &model.Environment{ID: "e", Name: "E", Variables: []model.Variable{
		{Key: "a", Type: model.VarSecret, Value: "1"},
		{Key: "b", Type: model.VarSecret, Value: "2"},
	}}
	if err := s.SaveEnvironment(ctx, env); err != nil {
		t.Fatal(err)
	}
	if len(ms.Refs()) != 2 {
		t.Fatalf("refs %v", ms.Refs())
	}
	env.Variables = env.Variables[:1] // drop b
	if err := s.SaveEnvironment(ctx, env); err != nil {
		t.Fatal(err)
	}
	if got := ms.Refs(); len(got) != 1 || got[0] != SecretRefFor(ScopeEnvironment, "e", "a") {
		t.Fatalf("after removing var: %v", got)
	}
	if err := s.DeleteEnvironment(ctx, "e"); err != nil {
		t.Fatal(err)
	}
	if len(ms.Refs()) != 0 {
		t.Fatalf("after delete: %v", ms.Refs())
	}
	if err := s.DeleteEnvironment(ctx, "e"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDeterministicAndIdempotent(t *testing.T) {
	s, _, root := newTestStore(t)
	if err := s.Save(ctx, richWorkspace()); err != nil {
		t.Fatal(err)
	}
	first := readTree(t, root)

	// Saving the same model again changes nothing.
	if err := s.Save(ctx, richWorkspace()); err != nil {
		t.Fatal(err)
	}
	requireEqual(t, first, readTree(t, root))

	// Save -> Load -> Save is a fixed point.
	loaded, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Hydrate(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	requireEqual(t, first, readTree(t, root))

	// A second store in a fresh directory produces identical bytes.
	s2, _, root2 := newTestStore(t)
	ws := richWorkspace()
	if err := s2.SaveMeta(ws); err != nil {
		t.Fatal(err)
	}
	if err := s2.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}
	requireEqual(t, first, readTree(t, root2))
}

func TestUnchangedFilesAreNotRewritten(t *testing.T) {
	s, _, root := newTestStore(t)
	ws := richWorkspace()
	if err := s.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	files := readTree(t, root)
	for rel := range files {
		if err := os.Chtimes(filepath.Join(root, rel), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}
	for rel := range files {
		st, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !st.ModTime().Equal(old) {
			t.Errorf("%s was rewritten", rel)
		}
	}
	for rel := range readTree(t, root) {
		if strings.Contains(rel, ".postctl-tmp-") {
			t.Errorf("temp file left behind: %s", rel)
		}
	}
}

func TestGolden(t *testing.T) {
	s, _, root := newTestStore(t)
	if err := s.Save(ctx, richWorkspace()); err != nil {
		t.Fatal(err)
	}
	got := readTree(t, root)
	delete(got, ".gitignore")
	golden := filepath.Join("testdata", "golden", "workspace")
	if *update {
		if err := os.RemoveAll(golden); err != nil {
			t.Fatal(err)
		}
		for rel, content := range got {
			p := filepath.Join(golden, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	want := readTree(t, golden)
	var names []string
	for n := range want {
		names = append(names, n)
	}
	for n := range got {
		if _, ok := want[n]; !ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		w, wok := want[n]
		g, gok := got[n]
		switch {
		case !wok:
			t.Errorf("unexpected file %s (run go test -update)", n)
		case !gok:
			t.Errorf("missing file %s", n)
		case w != g:
			t.Errorf("golden mismatch for %s (run go test -update to accept)\n--- want\n%s\n--- got\n%s", n, w, g)
		}
	}
}

func TestLayoutAndScriptFiles(t *testing.T) {
	s, _, root := newTestStore(t)
	if err := s.Save(ctx, richWorkspace()); err != nil {
		t.Fatal(err)
	}
	tree := readTree(t, root)
	for _, want := range []string{
		"postctl.yaml",
		"globals.yaml",
		"environments/dev.yaml",
		"environments/prod.yaml",
		"collections/payments-api/collection.yaml",
		"collections/payments-api/pre.js",
		"collections/payments-api/test.js",
		"collections/payments-api/charges/folder.yaml",
		"collections/payments-api/charges/pre.js",
		"collections/payments-api/charges/001-list-charges.yaml",
		"collections/payments-api/charges/001-list-charges.pre.js",
		"collections/payments-api/charges/001-list-charges.test.js",
		"collections/payments-api/charges/002-create-charge.yaml",
		"collections/payments-api/charges/forms-uploads/folder.yaml",
		"collections/payments-api/charges/forms-uploads/001-form-login.yaml",
		"collections/payments-api/002-ping.yaml",
		"collections/payments-api/empty/folder.yaml",
		"collections/payments-api-2/collection.yaml",
		"collections/payments-api-2/001-same-name.yaml",
		"collections/payments-api-2/002-same-name.yaml",
		"collections/payments-api-2/same/folder.yaml",
		"collections/payments-api-2/same-2/folder.yaml",
	} {
		if _, ok := tree[want]; !ok {
			t.Errorf("missing %s", want)
		}
	}
	if got := tree["collections/payments-api/charges/001-list-charges.pre.js"]; got != "pm.variables.set('t', Date.now());\nconsole.log('pre');\n" {
		t.Errorf("script file content %q", got)
	}
	if !strings.Contains(tree["collections/payments-api/charges/001-list-charges.yaml"], "file: 001-list-charges.pre.js") {
		t.Error("request yaml must reference its script file")
	}
	if strings.Contains(tree["collections/payments-api/charges/001-list-charges.yaml"], "pm.variables.set") {
		t.Error("script source must not be inlined in YAML")
	}
	if gi := tree[".gitignore"]; gi != ".postctl/\n" {
		t.Errorf(".gitignore = %q", gi)
	}

	// Script.File is populated on load.
	ws, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	r := ws.Collections[0].Requests()[0]
	if r.Scripts.PreRequest.File != "001-list-charges.pre.js" || r.Scripts.Test.File != "001-list-charges.test.js" {
		t.Errorf("File not set: %+v %+v", r.Scripts.PreRequest, r.Scripts.Test)
	}
}

func TestOrderingExplicitBeatsPrefix(t *testing.T) {
	s, _, root := newTestStore(t)
	c := model.NewCollection("C")
	a := &model.Request{ID: "a", Name: "A", Kind: model.KindHTTP, Method: "GET"}
	b := &model.Request{ID: "b", Name: "B", Kind: model.KindHTTP, Method: "GET"}
	f := &model.Folder{ID: "f", Name: "F"}
	d := &model.Request{ID: "d", Name: "D", Kind: model.KindHTTP, Method: "GET"}
	c.Items = []model.Item{b, f, a, d}
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadCollection(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(itemIDs(got.Items), []model.ID{"b", "f", "a", "d"}) {
		t.Fatalf("order %v", itemIDs(got.Items))
	}

	// Hand-edit the order list: it wins over numeric prefixes.
	p := filepath.Join(root, "collections", "c", collectionYAML)
	data, _ := os.ReadFile(p)
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var cf collectionFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		t.Fatal(err)
	}
	cf.Order = []string{"004-d", "f", "003-a", "001-b"}
	out, _ := marshalYAML(&cf)
	if err := os.WriteFile(p, out, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = s.LoadCollection(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(itemIDs(got.Items), []model.ID{"d", "f", "a", "b"}) {
		t.Fatalf("explicit order ignored: %v", itemIDs(got.Items))
	}

	// Unlisted entries follow, by numeric prefix then name.
	cf.Order = []string{"003-a"}
	out, _ = marshalYAML(&cf)
	_ = os.WriteFile(p, out, 0o644)
	got, _ = s.LoadCollection(c.ID)
	if !reflect.DeepEqual(itemIDs(got.Items), []model.ID{"a", "b", "d", "f"}) {
		t.Fatalf("fallback order: %v", itemIDs(got.Items))
	}
}

func itemIDs(items []model.Item) []model.ID {
	var out []model.ID
	for _, it := range items {
		out = append(out, it.ItemID())
	}
	return out
}

func TestOrderEntries(t *testing.T) {
	got := orderEntries([]string{"zeta", "010-b", "002-a", "alpha", "x", "2-two", "100-c"}, []string{"x"})
	want := []string{"x", "002-a", "2-two", "010-b", "100-c", "alpha", "zeta"}
	// "2-two" and "002-a" both parse as 2; ties break by name.
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Payments API":             "payments-api",
		"  Forms & uploads ":       "forms-uploads",
		"{{baseUrl}}/v1/x":         "baseurl-v1-x",
		"日本語":                      "fallback",
		"":                         "fallback",
		"a..b//c":                  "a-b-c",
		strings.Repeat("abc ", 40): "abc-abc-abc-abc-abc-abc-abc-abc-abc-abc-abc-abc-abc-abc-abc",
		"CamelCase_and-dash":       "camelcase-and-dash",
		"../../etc/passwd":         "etc-passwd",
	} {
		if got := Slug(in, "fallback"); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStructuralEditsAndPruning(t *testing.T) {
	s, _, root := newTestStore(t)
	c := richWorkspace().Collections[0]
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "collections", "payments-api")
	foreign := filepath.Join(dir, "charges", "NOTES.md")
	if err := os.WriteFile(foreign, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Rename a request: old file and its scripts go, new ones appear.
	_, _, _, _ = c.Find("req-list")
	it, _, _, _ := c.Find("req-list")
	it.(*model.Request).Name = "Search charges"
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	tree := readTree(t, dir)
	if _, ok := tree["charges/001-list-charges.yaml"]; ok {
		t.Error("old request file not pruned")
	}
	if _, ok := tree["charges/001-list-charges.pre.js"]; ok {
		t.Error("old script file not pruned")
	}
	if _, ok := tree["charges/001-search-charges.pre.js"]; !ok {
		t.Error("new script file missing")
	}

	// Move a request into another folder.
	if err := c.Move("req-create", "f-forms", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	tree = readTree(t, dir)
	if _, ok := tree["charges/forms-uploads/001-create-charge.yaml"]; !ok {
		t.Errorf("moved request missing: %v", keys(tree))
	}
	if _, ok := tree["charges/002-create-charge.yaml"]; ok {
		t.Error("moved request still at old path")
	}

	// Delete a folder: its files and directory disappear.
	if _, err := c.Remove("f-forms"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	if isDir(filepath.Join(dir, "charges", "forms-uploads")) {
		t.Error("deleted folder directory remains")
	}

	// Foreign files survive everything.
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "keep me" {
		t.Errorf("foreign file lost: %v", err)
	}
	foreign = filepath.Join(root, "collections", "billing", "charges", "NOTES.md")

	// Renaming the collection moves its directory.
	c.Name = "Billing"
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	if isDir(dir) || !isDir(filepath.Join(root, "collections", "billing")) {
		t.Error("collection directory not renamed")
	}
	ws, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.Collections) != 1 || ws.Collections[0].Name != "Billing" {
		t.Fatalf("load after rename: %+v", ws.Collections)
	}

	// Delete the collection (secrets included).
	if err := s.DeleteCollection(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	ws, _ = s.Load()
	if len(ws.Collections) != 0 {
		t.Fatal("collection not deleted")
	}
	if !isFile(foreign) {
		t.Error("foreign file must survive collection delete")
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestSaveRemovesDroppedCollectionsAndEnvs(t *testing.T) {
	s, ms, _ := newTestStore(t)
	ws := richWorkspace()
	if err := s.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}
	ws.Collections = ws.Collections[1:]
	ws.Environments = ws.Environments[1:]
	ws.Globals = nil
	if err := s.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Collections) != 1 || got.Collections[0].ID != "coll-dup" || len(got.Environments) != 1 || got.Globals != nil {
		t.Fatalf("unexpected workspace after prune: %d colls, %d envs, globals=%v", len(got.Collections), len(got.Environments), got.Globals)
	}
	if left := ms.Refs(); len(left) != 0 {
		t.Fatalf("orphaned secrets: %v", left)
	}
	// The remaining collection took over its natural slug.
	ws.Collections[0].Name = "Payments API"
	if err := s.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRequestFastPathAndFallback(t *testing.T) {
	s, _, root := newTestStore(t)
	c := richWorkspace().Collections[0]
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "collections", "payments-api")
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	for rel := range readTree(t, dir) {
		_ = os.Chtimes(filepath.Join(dir, rel), old, old)
	}

	// Content-only edit touches just that request.
	it, _, _, _ := c.Find("req-list")
	r := it.(*model.Request)
	r.Body = model.Body{Mode: model.BodyRaw, Raw: "changed"}
	r.Scripts.Test = nil // its .test.js must be removed
	if err := s.SaveRequest(ctx, c, "req-list"); err != nil {
		t.Fatal(err)
	}
	for rel := range readTree(t, dir) {
		st, _ := os.Stat(filepath.Join(dir, rel))
		changed := !st.ModTime().Equal(old)
		if changed != (rel == "charges/001-list-charges.yaml") {
			t.Errorf("%s changed=%v", rel, changed)
		}
	}
	if isFile(filepath.Join(dir, "charges", "001-list-charges.test.js")) {
		t.Error("stale test script not removed")
	}

	// A new request falls back to a full collection save.
	nr := model.NewRequest("Brand new", "GET", "https://x.test")
	if err := c.Insert("f-charges", 0, nr); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRequest(ctx, c, nr.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadCollection(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := got.Find(nr.ID); !ok {
		t.Fatal("new request not persisted")
	}
	if err := s.SaveRequest(ctx, c, "nope"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing request: %v", err)
	}

	p, err := s.PathOf(c, nr.ID)
	if err != nil || !isFile(p) {
		t.Fatalf("PathOf: %q %v", p, err)
	}
	if p, _ := s.PathOf(c, c.ID); filepath.Base(p) != collectionYAML {
		t.Fatalf("PathOf collection: %s", p)
	}
	if p, _ := s.PathOf(c, "f-charges"); filepath.Base(p) != folderYAML {
		t.Fatalf("PathOf folder: %s", p)
	}
}

func TestBodyStringsSurviveRoundTrip(t *testing.T) {
	tricky := []string{
		"", " leading space", "trailing space ", "line1\nline2", "line1\n\nline3\n", "\nstarts with newline",
		"ends with newline\n", "tab\there", "windows\r\nline", "# not a comment", "- not a list", "key: value",
		"'single'", `"double"`, "true", "null", "123", "1e3", "~", "yes", "@at", "`tick", "{{var}}", "{ \"a\": 1 }",
		"multi\n  indented\n    more", "unicode ✓ 日本語 🎉", "  \n  ", "a: b\n- c", "\\backslash", "%percent", "!bang", "&anchor", "*star",
	}
	s, _, _ := newTestStore(t)
	c := model.NewCollection("Tricky")
	for i, str := range tricky {
		c.Items = append(c.Items, &model.Request{
			ID: model.ID(fmt.Sprintf("t%d", i)), Name: fmt.Sprintf("T%d", i), Kind: model.KindHTTP, Method: "POST",
			Description: str,
			Headers:     []model.KV{{Key: "K", Value: str}},
			Body:        model.Body{Mode: model.BodyRaw, Raw: str},
			Scripts:     model.Scripts{Test: &model.Script{Source: str}},
		})
	}
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadCollection(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, it := range got.Items {
		r := it.(*model.Request)
		want := tricky[i]
		if r.Body.Raw != want || r.Description != want || r.Headers[0].Value != want {
			t.Errorf("case %d %q: body=%q desc=%q header=%q", i, want, r.Body.Raw, r.Description, r.Headers[0].Value)
		}
		// Empty source has no file and reads back as an empty script.
		if r.Scripts.Test == nil || r.Scripts.Test.Source != want {
			t.Errorf("case %d script %q", i, want)
		}
	}
}

func TestMultilineBodyUsesBlockScalar(t *testing.T) {
	s, _, root := newTestStore(t)
	c := model.NewCollection("C")
	c.Items = []model.Item{&model.Request{ID: "r", Name: "R", Kind: model.KindHTTP, Method: "POST",
		Body: model.Body{Mode: model.BodyRaw, Language: "json", Raw: "{\n  \"a\": 1\n}"}}}
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "collections", "c", "001-r.yaml"))
	if !strings.Contains(string(data), "raw: |-\n") {
		t.Fatalf("body not written as a block scalar:\n%s", data)
	}
}

func TestHandWrittenMinimalFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ws")
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("postctl.yaml", "name: hand\n")
	write("collections/api/collection.yaml", "name: API\n")
	write("collections/api/pre.js", "// adopted\n")
	write("collections/api/010-second.yaml", "name: Second\nmethod: POST\nurl:\n  raw: https://x.test\n")
	write("collections/api/010-second.test.js", "pm.test('x', () => {});\n")
	write("collections/api/002-first.yaml", "name: First\nmethod: GET\n")
	write("collections/api/zed/folder.yaml", "name: Zed\n")
	write("collections/api/zed/001-inner.yaml", "name: Inner\n")
	write("environments/local.yaml", "name: Local\nvariables:\n  - key: a\n    value: b\n")

	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	c := ws.Collections[0]
	if got := []string{c.Items[0].ItemName(), c.Items[1].ItemName(), c.Items[2].ItemName()}; !reflect.DeepEqual(got, []string{"First", "Second", "Zed"}) {
		t.Fatalf("order %v", got)
	}
	if c.Scripts.PreRequest == nil || c.Scripts.PreRequest.Source != "// adopted" {
		t.Fatalf("pre.js not adopted: %+v", c.Scripts.PreRequest)
	}
	second := c.Items[1].(*model.Request)
	if second.Kind != model.KindHTTP || second.Scripts.Test == nil || second.Scripts.Test.Source != "pm.test('x', () => {});" {
		t.Fatalf("second: %+v", second)
	}
	// Derived IDs are stable across loads.
	ws2, _ := s.Load()
	if ws.ID == "" || ws.ID != ws2.ID || c.ID != ws2.Collections[0].ID || second.ID != ws2.Collections[0].Items[1].ItemID() {
		t.Fatal("derived IDs are not stable")
	}
	if ws.Environments[0].ID == "" {
		t.Fatal("env id not derived")
	}
	// Saving persists the IDs and normalizes names without duplicating nodes.
	if err := s.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}
	ws3, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(ws3.Collections) != 1 || len(ws3.Collections[0].Items) != 3 || ws3.Collections[0].ID != c.ID {
		t.Fatal("re-save changed structure")
	}
}

func TestLoadErrorsNameTheFile(t *testing.T) {
	s, _, root := newTestStore(t)
	c := model.NewCollection("C")
	c.Items = []model.Item{model.NewRequest("R", "GET", "x")}
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(root, "collections", "c", "001-r.yaml")
	if err := os.WriteFile(bad, []byte("name: R\nmehtod: GET\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.Load()
	var fe *FileError
	if !errors.As(err, &fe) || fe.Path != bad {
		t.Fatalf("want FileError for %s, got %v", bad, err)
	}
	if !strings.Contains(err.Error(), "mehtod") {
		t.Fatalf("error should name the unknown field: %v", err)
	}
}

func TestScriptPathTraversalRejected(t *testing.T) {
	s, _, root := newTestStore(t)
	c := model.NewCollection("C")
	c.Items = []model.Item{model.NewRequest("R", "GET", "x")}
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "collections", "c", "001-r.yaml")
	if err := os.WriteFile(p, []byte("name: R\nscripts:\n  test:\n    file: ../../../secret.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("want ErrUnsafePath, got %v", err)
	}
}

func TestInitAndOpen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "new")
	if _, err := Open(root); !errors.Is(err, ErrNotWorkspace) {
		t.Fatalf("open missing: %v", err)
	}
	s, err := Init(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Init(root, "again"); !errors.Is(err, ErrExists) {
		t.Fatalf("init twice: %v", err)
	}
	ws, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "new" || ws.ID == "" || len(ws.Collections) != 0 {
		t.Fatalf("fresh workspace: %+v", ws)
	}
	if _, err := Open(root); err != nil {
		t.Fatal(err)
	}
}

func TestStateRoundTrip(t *testing.T) {
	s, _, root := newTestStore(t)
	if st, err := s.LoadState(); err != nil || st.ActiveEnvironment != "" {
		t.Fatalf("empty state: %+v %v", st, err)
	}
	if err := s.SaveState(State{ActiveEnvironment: "env-dev"}); err != nil {
		t.Fatal(err)
	}
	st, err := s.LoadState()
	if err != nil || st.ActiveEnvironment != "env-dev" {
		t.Fatalf("state: %+v %v", st, err)
	}
	if !isFile(filepath.Join(root, ".postctl", "state.yaml")) {
		t.Fatal("state not under .postctl/")
	}
}

func TestDiscover(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	deep := filepath.Join(proj, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(proj, "p"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvHome, "")
	t.Setenv(EnvWorkspace, "")
	t.Setenv(EnvXDGConfigHome, filepath.Join(base, "xdg"))

	loc, err := Discover(deep)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Source != SourceProject || !loc.Initialized || evalSymlinks(t, loc.Root) != evalSymlinks(t, proj) {
		t.Fatalf("project discovery: %+v", loc)
	}

	other := filepath.Join(base, "elsewhere")
	_ = os.MkdirAll(other, 0o755)
	loc, err = Discover(other)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Source != SourceGlobal || loc.Root != filepath.Join(base, "xdg", "postctl") || loc.Initialized {
		t.Fatalf("xdg fallback: %+v", loc)
	}

	t.Setenv(EnvHome, filepath.Join(base, "custom"))
	loc, _ = Discover(other)
	if loc.Root != filepath.Join(base, "custom") {
		t.Fatalf("POSTCTL_HOME: %+v", loc)
	}

	t.Setenv(EnvHome, "")
	t.Setenv(EnvXDGConfigHome, "")
	home, _ := os.UserHomeDir()
	if g, _ := GlobalDir(); g != filepath.Join(home, ".config", "postctl") {
		t.Fatalf("default global dir: %s", g)
	}
	// A relative XDG_CONFIG_HOME is ignored per the XDG spec.
	t.Setenv(EnvXDGConfigHome, "relative/path")
	if g, _ := GlobalDir(); g != filepath.Join(home, ".config", "postctl") {
		t.Fatalf("relative XDG not ignored: %s", g)
	}
}

func evalSymlinks(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMigrationHook(t *testing.T) {
	s, _, root := newTestStore(t)
	// A file claiming a newer schema is refused.
	p := filepath.Join(root, workspaceYAML)
	if err := os.WriteFile(p, []byte("schemaVersion: 99\nname: future\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("newer schema: %v", err)
	}

	// Simulate the schema having moved: register a v0->v1 migration for a file
	// that says schemaVersion: 0... peekVersion treats missing/0 as 1, so use
	// a store whose target version is bumped via the exported constant instead:
	// run the migration engine directly.
	ms := &Store{migrations: []Migration{{
		Kind: KindEnvironment, From: 1,
		Apply: func(doc *yaml.Node) error {
			for i := 0; i+1 < len(doc.Content); i += 2 {
				if doc.Content[i].Value == "title" {
					doc.Content[i].Value = "name"
				}
			}
			return nil
		},
	}}}
	// migrate is a no-op at the current version...
	in := []byte("schemaVersion: 1\ntitle: Old\n")
	out, err := ms.migrate(KindEnvironment, model.SchemaVersion, in)
	if err != nil || string(out) != string(in) {
		t.Fatalf("no-op migrate: %q %v", out, err)
	}
	// ...and errors on future versions.
	if _, err := ms.migrate(KindEnvironment, model.SchemaVersion+1, in); !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("future: %v", err)
	}
}

func TestMigrationAppliesWhenOlder(t *testing.T) {
	// Exercise the migration loop with a synthetic target version by calling
	// the step function on a store and a lowered "from".
	orig := model.SchemaVersion
	_ = orig
	ran := 0
	s := &Store{migrations: []Migration{{
		Kind: KindCollection, From: 0,
		Apply: func(doc *yaml.Node) error { ran++; return nil },
	}}}
	out, err := s.migrate(KindCollection, 0, []byte("name: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Fatalf("migration ran %d times", ran)
	}
	if !strings.Contains(string(out), "schemaVersion: 1") {
		t.Fatalf("schemaVersion not stamped:\n%s", out)
	}
	// Non-versioned kinds are not stamped.
	out, err = s.migrate(KindRequest, 0, []byte("name: x\n"))
	if err != nil || strings.Contains(string(out), "schemaVersion") {
		t.Fatalf("request stamped: %q %v", out, err)
	}
}

func TestLoadPerformance1000Requests(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	s, _, _ := newTestStore(t)
	c := model.NewCollection("Big")
	for i := 0; i < 20; i++ {
		f := model.NewFolder(fmt.Sprintf("Folder %02d", i))
		for j := 0; j < 50; j++ {
			r := model.NewRequest(fmt.Sprintf("Request %d", j), "POST", "{{baseUrl}}/v1/things/:id?a=1&b=2")
			r.Headers = []model.KV{{Key: "Accept", Value: "application/json"}, {Key: "X-A", Value: "b"}}
			r.Body = model.Body{Mode: model.BodyRaw, Language: "json", Raw: "{\n  \"a\": 1,\n  \"b\": [1, 2, 3]\n}"}
			r.Scripts.Test = &model.Script{Source: "pm.test('ok', function () {\n  pm.response.to.have.status(200);\n});"}
			f.Items = append(f.Items, r)
		}
		c.Items = append(c.Items, f)
	}
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	ws, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	d := time.Since(start)
	t.Logf("loaded %d requests in %v", len(ws.Collections[0].Requests()), d)
	if d > 2*time.Second { // generous: the goal is 150ms, checked by benchmark
		t.Fatalf("load too slow: %v", d)
	}
}

func TestKeychainCommandsKeepSecretsOffArgv(t *testing.T) {
	type call struct {
		stdin string
		args  []string
	}
	var calls []call
	backing := map[string]string{}
	k := &Keychain{Service: "svc.test"}
	k.run = func(_ context.Context, stdin string, args ...string) (string, int, error) {
		calls = append(calls, call{stdin, args})
		switch args[0] {
		case "-i":
			// parse: add-generic-password -U -s "svc" -a "acct" -w B64
			f := strings.Fields(stdin)
			backing[strings.Trim(f[5], `"`)] = f[len(f)-1]
			return "", 0, nil
		case "find-generic-password":
			acct := args[4]
			v, ok := backing[acct]
			if !ok {
				return "", 44, errors.New("exit 44")
			}
			return v + "\n", 0, nil
		case "delete-generic-password":
			if _, ok := backing[args[4]]; !ok {
				return "", 44, errors.New("exit 44")
			}
			delete(backing, args[4])
			return "", 0, nil
		}
		t.Fatalf("unexpected args %v", args)
		return "", 0, nil
	}

	const value = "pa$$word \"quoted\"\nsecond line"
	if err := k.Set(ctx, "env/e1/apiKey", value); err != nil {
		t.Fatal(err)
	}
	got, err := k.Get(ctx, "env/e1/apiKey")
	if err != nil || got != value {
		t.Fatalf("get: %q %v", got, err)
	}
	if _, err := k.Get(ctx, "env/none"); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := k.Delete(ctx, "env/e1/apiKey"); err != nil {
		t.Fatal(err)
	}
	if err := k.Delete(ctx, "env/e1/apiKey"); err != nil {
		t.Fatalf("delete must be idempotent: %v", err)
	}
	for _, c := range calls {
		for _, a := range c.args {
			if strings.Contains(a, "pa$$word") {
				t.Fatalf("secret on argv: %v", c.args)
			}
		}
	}
	if err := k.Set(ctx, "x", ""); err == nil {
		t.Fatal("empty secret must be refused")
	}
}

// TestKeychainReal talks to the real login Keychain. It is opt-in because it
// can trigger prompts on a locked keychain and writes (then removes) an item.
func TestKeychainReal(t *testing.T) {
	if os.Getenv("POSTCTL_KEYCHAIN_TESTS") != "1" {
		t.Skip("set POSTCTL_KEYCHAIN_TESTS=1 to run against the macOS Keychain")
	}
	k := NewKeychain("com.shubham.postctl.test")
	ref := model.SecretRef("test/" + string(model.NewID()))
	defer func() { _ = k.Delete(ctx, ref) }()
	const value = "line1\nli\"ne2 \\ ✓"
	if err := k.Set(ctx, ref, value); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Get(ctx, ref); err != nil || got != value {
		t.Fatalf("get: %q %v", got, err)
	}
	if err := k.Set(ctx, ref, "second"); err != nil {
		t.Fatal(err)
	}
	if got, _ := k.Get(ctx, ref); got != "second" {
		t.Fatalf("update: %q", got)
	}
	if err := k.Delete(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get(ctx, ref); !errors.Is(err, ErrSecretNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestSaveCollectionVariablesPreservesConcurrentMetadata(t *testing.T) {
	s, _, root := newTestStore(t)
	c := model.NewCollection("C")
	c.Description = "original"
	c.Items = []model.Item{model.NewRequest("R", "GET", "https://example.test")}
	if err := s.SaveCollection(ctx, c); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, collectionsDir, "c", collectionYAML)
	var cf collectionFile
	if _, err := s.decode(path, KindCollection, 0, &cf); err != nil {
		t.Fatal(err)
	}
	cf.Description = "edited while send was running"
	if err := newWriter(true).yaml(path, &cf); err != nil {
		t.Fatal(err)
	}

	vars := []model.Variable{{Key: "plain", Value: "updated"}, {Key: "secret", Type: model.VarSecret, Value: "synthetic"}}
	if err := s.SaveCollectionVariables(ctx, c.ID, vars); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadCollection(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "edited while send was running" || len(got.Items) != 1 {
		t.Fatalf("concurrent collection edits were lost: %#v", got)
	}
	if plain, ok := model.Lookup(got.Variables, "plain"); !ok || plain.Value != "updated" {
		t.Fatalf("variables not updated: %#v", got.Variables)
	}
	if secret, ok := model.Lookup(got.Variables, "secret"); !ok || secret.Value != "" || secret.SecretRef == "" {
		t.Fatalf("secret was not externalized: %#v", got.Variables)
	}
}

func TestLoadGlobalsReadsOnlyGlobals(t *testing.T) {
	s, _, root := newTestStore(t)
	g := &model.Globals{ID: model.NewID(), Name: "Globals", Variables: []model.Variable{{Key: "base", Value: "https://example.test"}}}
	if err := s.SaveGlobals(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, collectionsDir, "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, collectionsDir, "broken", collectionYAML), []byte("not: [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadGlobals()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, g) {
		t.Fatalf("globals = %#v, want %#v", got, g)
	}
}

func TestDiscoverExplicitWorkspace(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	deep := filepath.Join(proj, "a")
	other := filepath.Join(base, "other")
	fresh := filepath.Join(base, "fresh") // not initialized, not even created
	for _, d := range []string{deep, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Init(proj, "p"); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(other, "o"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvHome, "")
	t.Setenv(EnvWorkspace, "")
	t.Setenv(EnvXDGConfigHome, filepath.Join(base, "xdg"))

	// The flag beats a project found by walking up.
	loc, err := Discover(deep, WithWorkspace(other))
	if err != nil || loc.Source != SourceFlag || loc.Root != other || !loc.Initialized {
		t.Fatalf("flag: %+v %v", loc, err)
	}
	// An empty flag value is ignored.
	if loc, _ := Discover(deep, WithWorkspace("")); loc.Source != SourceProject {
		t.Fatalf("empty flag must fall through: %+v", loc)
	}
	// The env var beats discovery; the flag beats the env var.
	t.Setenv(EnvWorkspace, other)
	if loc, _ := Discover(deep); loc.Source != SourceEnv || loc.Root != other || !loc.Initialized {
		t.Fatalf("env: %+v", loc)
	}
	if loc, _ := Discover(deep, WithWorkspace(proj)); loc.Source != SourceFlag || loc.Root != proj {
		t.Fatalf("flag over env: %+v", loc)
	}
	// An explicit workspace need not exist yet; Initialized says so.
	loc, err = Discover(deep, WithWorkspace(fresh))
	if err != nil || loc.Initialized || loc.Root != fresh {
		t.Fatalf("fresh: %+v %v", loc, err)
	}
	// A relative path is made absolute.
	t.Chdir(base)
	if loc, _ := Discover(deep, WithWorkspace("other")); loc.Root != other && evalSymlinks(t, loc.Root) != evalSymlinks(t, other) {
		t.Fatalf("relative: %+v", loc)
	}
	// A file is not a workspace.
	f := filepath.Join(base, "file")
	_ = os.WriteFile(f, nil, 0o644)
	if _, err := Discover(deep, WithWorkspace(f)); err == nil {
		t.Fatal("file must be rejected")
	}
	// The result is usable.
	loc, _ = Discover(deep, WithWorkspace(other))
	if _, err := Open(loc.Root); err != nil {
		t.Fatal(err)
	}
}

func TestLoadLenient(t *testing.T) {
	s, _, root := newTestStore(t)
	if err := s.Save(ctx, richWorkspace()); err != nil {
		t.Fatal(err)
	}

	// Clean workspace: no problems, identical to Load.
	strict, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	lenient, problems, err := s.LoadLenient()
	if err != nil || len(problems) != 0 {
		t.Fatalf("clean: %v %v", err, problems)
	}
	requireEqual(t, strict, lenient)

	pay := filepath.Join(root, "collections", "payments-api")
	corrupt := func(rel, content string) string {
		p := filepath.Join(root, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	badReq := corrupt("collections/payments-api/charges/002-create-charge.yaml", "name: [unterminated\n")
	badTypo := corrupt("collections/payments-api/002-ping.yaml", "name: Ping\nmehtod: HEAD\n")
	badFolder := corrupt("collections/payments-api/auth-schemes/folder.yaml", "name: {\n")
	badColl := corrupt("collections/payments-api-2/collection.yaml", "schemaVersion: 99\nname: future\n")
	badEnv := corrupt("environments/prod.yaml", "schemaVersion: 1\nname: Prod\nbogus: 1\n")
	badGlobals := corrupt("globals.yaml", "id: [\n")
	// A request whose script file has gone missing.
	if err := os.Remove(filepath.Join(pay, "charges", "001-list-charges.test.js")); err != nil {
		t.Fatal(err)
	}
	missingScript := filepath.Join(pay, "charges", "001-list-charges.test.js")

	// Strict Load fails.
	if _, err := s.Load(); err == nil {
		t.Fatal("strict Load must fail on a broken file")
	}

	ws, problems, err := s.LoadLenient()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range problems {
		got[p.Path] = true
		if p.Err == nil || p.Error() == "" {
			t.Errorf("problem without error: %+v", p)
		}
	}
	for _, want := range []string{badReq, badTypo, badFolder, badColl, badEnv, badGlobals, missingScript} {
		if !got[want] {
			t.Errorf("problem for %s not reported (got %v)", want, keys(got))
		}
	}
	if len(problems) != 7 {
		t.Errorf("want 7 problems, got %d: %v", len(problems), problems)
	}
	if !errors.Is(problems[indexOfPath(problems, badColl)], ErrNewerSchema) {
		t.Error("problems must keep the underlying error for errors.Is")
	}

	// Everything else loaded.
	if len(ws.Collections) != 1 || ws.Collections[0].ID != "coll-pay" {
		t.Fatalf("collections: %d", len(ws.Collections))
	}
	c := ws.Collections[0]
	if _, _, _, ok := c.Find("req-create"); ok {
		t.Error("broken request must be skipped")
	}
	if _, _, _, ok := c.Find("req-nobody"); ok {
		t.Error("request with an unknown field must be skipped")
	}
	if _, _, _, ok := c.Find("f-auth"); ok {
		t.Error("folder with a broken folder.yaml must be skipped with its subtree")
	}
	if _, _, _, ok := c.Find("req-hawk"); ok {
		t.Error("subtree of a broken folder must be skipped")
	}
	if _, _, _, ok := c.Find("req-list"); ok {
		t.Error("request with a missing script file must be skipped")
	}
	for _, id := range []model.ID{"f-charges", "f-forms", "req-urlenc", "req-form", "req-bin", "req-gql", "f-empty"} {
		if _, _, _, ok := c.Find(id); !ok {
			t.Errorf("%s should have loaded", id)
		}
	}
	if len(ws.Environments) != 1 || ws.Environments[0].ID != "env-dev" {
		t.Errorf("environments: %v", ws.Environments)
	}
	if ws.Globals != nil {
		t.Error("broken globals must be skipped")
	}

	// A malformed workspace file is fatal even when lenient.
	corrupt("postctl.yaml", "name: [\n")
	if ws, _, err := s.LoadLenient(); err == nil || ws != nil {
		t.Fatalf("broken postctl.yaml must be an error: %v %v", ws, err)
	}
	if err := os.Remove(filepath.Join(root, "postctl.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoadLenient(); !errors.Is(err, ErrNotWorkspace) {
		t.Fatalf("missing postctl.yaml: %v", err)
	}
}

func indexOfPath(ps []*FileError, path string) int {
	for i, p := range ps {
		if p.Path == path {
			return i
		}
	}
	return 0
}

// Saving what a lenient load returned must not destroy the files it skipped.
func TestSaveAfterLenientLoadKeepsBrokenFiles(t *testing.T) {
	s, _, root := newTestStore(t)
	if err := s.Save(ctx, richWorkspace()); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) string {
		p := filepath.Join(root, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	charges := "collections/payments-api/charges/"
	brokenReq := write(charges+"002-create-charge.yaml", "name: [broken\n")
	brokenFolder := write("collections/payments-api/auth-schemes/folder.yaml", "name: {\n")
	brokenColl := write("collections/payments-api-2/collection.yaml", "name: [\n")
	brokenEnv := write("environments/prod.yaml", "name: [\n")
	// list-charges has valid YAML but lost its test script.
	lostScript := filepath.Join(root, charges, "001-list-charges.test.js")
	if err := os.Remove(lostScript); err != nil {
		t.Fatal(err)
	}
	preOfLost := filepath.Join(root, charges, "001-list-charges.pre.js")

	ws, problems, err := s.LoadLenient()
	if err != nil || len(problems) == 0 {
		t.Fatalf("setup: %v %v", err, problems)
	}
	// Edit something that did load, then save via every path.
	ws.Collections[0].Description = "edited"
	ws.Environments[0].Description = "edited"
	if err := s.SaveCollection(ctx, ws.Collections[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, ws); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{brokenReq, brokenFolder, brokenColl, brokenEnv, filepath.Join(root, charges, "001-list-charges.yaml"), preOfLost,
		filepath.Join(root, "collections/payments-api/auth-schemes/001-hawk-signed.yaml")} {
		if !isFile(p) {
			t.Errorf("%s was deleted", p)
		}
	}
	// The same problems are still reported, and the edit landed.
	_, again, err := s.LoadLenient()
	if err != nil || len(again) != len(problems) {
		t.Fatalf("problems changed: %d -> %d (%v)", len(problems), len(again), err)
	}
	ws2, _, _ := s.LoadLenient()
	if ws2.Collections[0].Description != "edited" {
		t.Error("edit was not saved")
	}

	// Explicit deletion still removes everything the store owns.
	if err := s.DeleteCollection(ctx, "coll-pay"); err != nil {
		t.Fatal(err)
	}
	if isDir(filepath.Join(root, "collections", "payments-api")) {
		t.Error("explicit delete must remove broken files too")
	}
}
