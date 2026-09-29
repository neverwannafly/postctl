package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

func TestPathResolution(t *testing.T) {
	getA := &model.Request{ID: "req-a", Name: "Get", Method: "GET", Kind: model.KindHTTP}
	getB := &model.Request{ID: "req-b", Name: "Get", Method: "POST", Kind: model.KindHTTP}
	users := &model.Folder{ID: "fold-users", Name: "Users", Items: []model.Item{getA}}
	orders := &model.Folder{ID: "fold-orders", Name: "Orders", Items: []model.Item{getB}}
	col := &model.Collection{ID: "col-pay", Name: "Payments API", Items: []model.Item{users, orders}}
	other := &model.Collection{ID: "col-other", Name: "Other", Items: []model.Item{
		&model.Request{ID: "req-health", Name: "Health", Method: "GET", Kind: model.KindHTTP},
	}}
	dup := &model.Collection{ID: "col-api-2", Name: "api", Items: nil}
	ws := &model.Workspace{Collections: []*model.Collection{col, other, {ID: "col-api", Name: "API"}, dup}}

	must := func(path string) node {
		t.Helper()
		n, err := resolvePath(ws, path)
		if err != nil {
			t.Fatalf("resolve %q: %v", path, err)
		}
		return n
	}
	if n := must("payments-api/users/get"); n.request() == nil || n.request().ID != "req-a" {
		t.Fatalf("slug/case path: %+v", n.names)
	}
	if n := must("PAYMENTS API/Users/GET"); n.request() == nil || n.request().ID != "req-a" {
		t.Fatalf("display case path: %+v", n.names)
	}
	if n := must("req-b"); n.request() == nil || n.request().Method != "POST" {
		t.Fatalf("id lookup: %+v", n)
	}
	if n := must("Health"); n.request() == nil || n.request().ID != "req-health" {
		t.Fatalf("unique name: %+v", n.names)
	}
	if n := must("Payments API/Orders"); n.kind() != "folder" || n.folder().ID != "fold-orders" {
		t.Fatalf("folder: %s", n.kind())
	}
	_, err := resolvePath(ws, "Get")
	if err == nil || !strings.Contains(err.Error(), "Payments API/Orders/Get") || !strings.Contains(err.Error(), "Payments API/Users/Get") {
		t.Fatalf("ambiguity: %v", err)
	}
	_, err = resolvePath(ws, "api")
	if err == nil || !strings.Contains(err.Error(), "matches more than one") {
		t.Fatalf("collection case clash: %v", err)
	}
	if _, err := resolvePath(ws, "nope"); err == nil || !strings.Contains(err.Error(), "Nothing matches") {
		t.Fatalf("missing: %v", err)
	}
}

func TestSubcommandWorkspaceFlags(t *testing.T) {
	for _, args := range [][]string{
		{"Payments", "--workspace", "/tmp/long"},
		{"-w", "/tmp/short", "Payments"},
		{"Payments", "--workspace=/tmp/equal"},
	} {
		got, rest, err := extractWorkspace(args)
		if err != nil {
			t.Fatal(err)
		}
		if got == "" || len(rest) != 1 || rest[0] != "Payments" {
			t.Fatalf("extractWorkspace(%v) = %q, %v", args, got, rest)
		}
	}
	if _, _, err := extractWorkspace([]string{"Payments", "-w"}); err == nil {
		t.Fatal("missing -w value was accepted")
	}
}

func TestLsTextAndJSON(t *testing.T) {
	st := demoWorkspace(t, nil)
	code, out, errOut := exec(st, "ls")
	if code != ExitOK || errOut != "" {
		t.Fatalf("ls: code=%d err=%q\n%s", code, errOut, out)
	}
	want := "" +
		"Payments\n" +
		"  Billing\n" +
		"    POST    Create\n" +
		"  GET     List charges\n" +
		"  Users\n" +
		"    GET     Get\n" +
		"  Orders\n" +
		"    GET     Get\n"
	if out != want {
		t.Fatalf("ls text:\n%s", out)
	}

	code, out, errOut = exec(st, "ls", "--flat")
	if code != ExitOK || errOut != "" {
		t.Fatalf("flat: %d %q", code, errOut)
	}
	if !strings.Contains(out, "POST    Payments/Billing/Create\n") || !strings.Contains(out, "GET     Payments/List charges\n") {
		t.Fatalf("flat:\n%s", out)
	}

	code, out, errOut = exec(st, "ls", "payments/billing", "--json")
	if code != ExitOK {
		t.Fatalf("json: %d %q", code, errOut)
	}
	var doc lsDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Items) != 1 || doc.Items[0].Name != "Billing" || doc.Items[0].Type != "folder" {
		t.Fatalf("json root: %+v", doc.Items)
	}
	if len(doc.Items[0].Children) != 1 || doc.Items[0].Children[0].Method != "POST" || doc.Items[0].Children[0].ID != "req-create" {
		t.Fatalf("json child: %+v", doc.Items[0].Children)
	}

	code, _, errOut = exec(st, "ls", "Get")
	if code != ExitUsage || !strings.Contains(errOut, "matches more than one") {
		t.Fatalf("ambiguous ls: %d %q", code, errOut)
	}
}

func TestSendAndExitCodes(t *testing.T) {
	const secret = "synthetic-secret-value"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Echo", r.Header.Get("X-Token"))
		_, _ = w.Write([]byte(`{"ok":true,"token":"` + r.Header.Get("X-Token") + `"}`))
	}))
	t.Cleanup(srv.Close)

	st := demoWorkspace(t, srv)
	code, out, errOut := exec(st, "send", "Payments/List charges")
	if code != ExitOK || errOut != "" {
		t.Fatalf("send: code=%d err=%q\n%s", code, errOut, out)
	}
	if strings.Contains(out, secret) {
		t.Fatal("send output contains the secret")
	}
	if !strings.Contains(out, "200 OK") || !strings.Contains(out, "✓ status is 200") || !strings.Contains(out, `{"ok":true`) {
		t.Fatalf("send text:\n%s", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatal("non-tty send should not use color")
	}

	code, out, errOut = exec(st, "send", "list-charges", "--json", "--include", "--verbose")
	if code != ExitOK {
		t.Fatalf("json send: %d %q", code, errOut)
	}
	if strings.Contains(out, secret) {
		t.Fatal("json send contains the secret")
	}
	var doc sendDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Status != 200 || len(doc.Tests) != 1 || !doc.Tests[0].Passed || doc.Sent == nil || len(doc.Headers) == 0 || len(doc.Timeline) == 0 {
		t.Fatalf("json doc: status=%d tests=%d sent=%v headers=%d timeline=%d", doc.Status, len(doc.Tests), doc.Sent != nil, len(doc.Headers), len(doc.Timeline))
	}
	for _, h := range doc.Sent.Headers {
		if strings.EqualFold(h.Key, "X-Token") && h.Value == secret {
			t.Fatal("verbose sent header kept the secret")
		}
	}

	code, out, _ = exec(st, "send", "Payments/List charges", "--no-scripts")
	if code != ExitOK || strings.Contains(out, "status is 200") {
		t.Fatalf("no-scripts: code=%d\n%s", code, out)
	}

	fail := demoWith(t, srv, `pm.test("fails", function () { pm.expect(1).to.eql(2); });`, srv.URL)
	code, out, _ = exec(fail, "send", "Payments/Billing/Create")
	if code != ExitTests || !strings.Contains(out, "× fails") {
		t.Fatalf("failing test: code=%d\n%s", code, out)
	}

	down := closedWorkspace(t)
	code, _, _ = exec(down, "send", "Payments/List charges")
	if code != ExitRequest {
		t.Fatalf("transport exit: %d", code)
	}

	code, _, errOut = exec(st, "send")
	if code != ExitUsage || errOut == "" {
		t.Fatalf("usage: %d %q", code, errOut)
	}
	code, _, errOut = exec(st, "send", "Payments")
	if code != ExitUsage || !strings.Contains(errOut, "takes a request") {
		t.Fatalf("collection send: %d %q", code, errOut)
	}
}

func TestRunAcceptsRepeatableVarOverrides(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	st := demoWorkspace(t, srv)
	ws, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	req := ws.Collections[0].Requests()[0]
	req.URL = model.ParseURL("{{baseUrl}}/charges/{{suffix}}/{{second}}")
	if err := st.SaveCollection(context.Background(), ws.Collections[0]); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := exec(st, "run", "Payments", "--var", "suffix=one", "--var", "second=two")
	if code != ExitOK || errOut != "" {
		t.Fatalf("run --var: code=%d err=%q", code, errOut)
	}
	if !slices.Contains(paths, "/charges/one/two") {
		t.Fatalf("run did not apply local overrides: %q", paths)
	}
}

func TestRunReportersBailAndCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	st := demoWorkspace(t, srv)

	code, out, errOut := exec(st, "run", "Payments")
	if code != ExitOK || errOut != "" {
		t.Fatalf("run: code=%d err=%q\n%s", code, errOut, out)
	}
	for _, want := range []string{"Iteration 1 of 1", "POST    Payments/Billing/Create", "GET     Payments/List charges", "✓ status is 200", "iterations", "assertions", "average response time:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("run missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Fatal("run used color without a tty")
	}

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "run.ndjson")
	junitPath := filepath.Join(dir, "run.xml")
	code, out, errOut = exec(st, "run", "payments", "--reporter", "cli,json,junit",
		"--reporter-json-export", jsonPath, "--reporter-junit-export", junitPath)
	if code != ExitOK || errOut != "" {
		t.Fatalf("multi: %d %q\n%s", code, errOut, out)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var finished bool
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("ndjson: %v\n%s", err, line)
		}
		if ev["type"] == "run-finished" {
			finished = true
		}
	}
	if !finished {
		t.Fatal("ndjson missing run-finished")
	}
	xmlRaw, err := os.ReadFile(junitPath)
	if err != nil {
		t.Fatal(err)
	}
	var suites junitSuites
	if err := xml.Unmarshal(xmlRaw, &suites); err != nil {
		t.Fatalf("junit: %v\n%s", err, xmlRaw)
	}
	if suites.Tests < 1 || suites.Failures != 0 || len(suites.Suites) < 2 {
		t.Fatalf("junit suites: tests=%d failures=%d suites=%d", suites.Tests, suites.Failures, len(suites.Suites))
	}
	var sawCase bool
	for _, s := range suites.Suites {
		for _, c := range s.Cases {
			if c.Name == "status is 200" && c.Failure == nil {
				sawCase = true
			}
		}
	}
	if !sawCase {
		t.Fatal("junit missing the passing testcase")
	}

	code, out, errOut = exec(st, "run", "Payments", "--json")
	if code != ExitOK || !strings.Contains(out, `"type":"run-finished"`) && !strings.Contains(out, `"type": "run-finished"`) {
		t.Fatalf("json flag: %d %q\n%s", code, errOut, out)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(slow.Close)
	fail := demoWith(t, slow, `pm.test("fails", function () { pm.expect(false).to.eql(true); });`, "")
	code, out, _ = exec(fail, "run", "Payments", "--bail")
	if code != ExitTests || !strings.Contains(out, "Failures") || !strings.Contains(out, "Stopped after the first failure") {
		t.Fatalf("bail: code=%d\n%s", code, out)
	}

	started := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
		select {
		case <-started:
		default:
			close(started)
		}
	}))
	t.Cleanup(hang.Close)
	hangWS := demoWith(t, hang, "", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	var stdout, stderr bytes.Buffer
	code = Execute(ctx, options(hangWS, &stdout, &stderr), []string{"run", "Payments", "--delay", "30s"})
	if code != ExitRequest || !strings.Contains(stdout.String(), "canceled") {
		t.Fatalf("cancel: code=%d err=%q\n%s", code, stderr.String(), stdout.String())
	}

	data := filepath.Join(t.TempDir(), "rows.csv")
	if err := os.WriteFile(data, []byte("name\nada\nbea\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = exec(st, "run", "Payments/Billing", "-d", data, "-n", "2")
	if code != ExitOK || !strings.Contains(out, "Iteration 1 of 2") || !strings.Contains(out, "Iteration 2 of 2") {
		t.Fatalf("data: %d %q\n%s", code, errOut, out)
	}

	code, _, errOut = exec(st, "run", "Payments/List charges")
	if code != ExitUsage || !strings.Contains(errOut, "takes a collection or folder") {
		t.Fatalf("run request: %d %q", code, errOut)
	}
}

func TestWebSocketSendIsRejectedAndRunReportersSkip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	st := demoWorkspace(t, srv)
	ws, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	stream := model.NewWebSocketRequest("Events", "wss://socket.example.test/events")
	stream.ID = "req-events"
	ws.Collections[0].Items = append(ws.Collections[0].Items, stream)
	if err := st.SaveCollection(context.Background(), ws.Collections[0]); err != nil {
		t.Fatal(err)
	}

	code, _, errOut := exec(st, "send", "Payments/Events")
	const sendMessage = "Open WebSocket requests in the TUI; `postctl send` supports HTTP requests."
	if code != ExitUsage || !strings.Contains(errOut, sendMessage) {
		t.Fatalf("send: code=%d stderr=%q", code, errOut)
	}

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "run.json")
	xmlPath := filepath.Join(dir, "run.xml")
	code, out, errOut := exec(st, "run", "Payments", "--reporter", "cli,json,junit",
		"--reporter-json-export", jsonPath, "--reporter-junit-export", xmlPath)
	if code != ExitOK || errOut != "" {
		t.Fatalf("run: code=%d stderr=%q\n%s", code, errOut, out)
	}
	if !strings.Contains(out, engine.WebSocketSkipReason) || !strings.Contains(out, "skipped") {
		t.Fatalf("CLI reporter did not list the skip:\n%s", out)
	}
	rawJSON, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawJSON), `"skipped":true`) ||
		!strings.Contains(string(rawJSON), `"skipReason":"`+engine.WebSocketSkipReason+`"`) {
		t.Fatalf("JSON reporter did not mark the request skipped:\n%s", rawJSON)
	}
	rawXML, err := os.ReadFile(xmlPath)
	if err != nil {
		t.Fatal(err)
	}
	var suites junitSuites
	if err := xml.Unmarshal(rawXML, &suites); err != nil {
		t.Fatal(err)
	}
	if suites.Skipped != 1 || !strings.Contains(string(rawXML), engine.WebSocketSkipReason) {
		t.Fatalf("JUnit reporter did not mark the request skipped:\n%s", rawXML)
	}
}

func TestEnvAndCurlMasking(t *testing.T) {
	const secret = "synthetic-secret-value"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	st := demoWorkspace(t, srv)

	code, out, errOut := exec(st, "env", "ls")
	if code != ExitOK || !strings.Contains(out, "* Staging") || !strings.Contains(out, "! prod") && !strings.Contains(out, "Production") {
		t.Fatalf("env ls: %d %q\n%s", code, errOut, out)
	}
	if strings.Contains(out, secret) {
		t.Fatal("env ls leaked a secret")
	}

	code, out, _ = exec(st, "env", "show", "staging")
	if code != ExitOK || strings.Contains(out, secret) || !strings.Contains(out, "token") || !strings.Contains(out, "secret") {
		t.Fatalf("show masked:\n%s", out)
	}
	code, out, _ = exec(st, "env", "show", "Staging", "--reveal")
	if code != ExitOK || !strings.Contains(out, secret) {
		t.Fatalf("reveal without a tty should show the value, code=%d", code)
	}

	var stdout, stderr bytes.Buffer
	in := strings.NewReader("n\n")
	code = Execute(context.Background(), Options{
		Store: st, Stdin: in, Stdout: &stdout, Stderr: &stderr, Version: "test",
		IsTTY: func(any) bool { return true },
	}, []string{"env", "show", "staging", "--reveal"})
	if code != ExitUsage || strings.Contains(stdout.String(), secret) {
		t.Fatalf("declined reveal: code=%d out=%q err=%q", code, stdout.String(), stderr.String())
	}

	code, out, errOut = exec(st, "env", "set", "staging", "region", "us", "--secret")
	if code != ExitOK || strings.Contains(out, "us") {
		t.Fatalf("set secret echoed the value: %d %q\n%s", code, errOut, out)
	}
	code, out, _ = exec(st, "env", "show", "staging", "--json")
	if code != ExitOK || strings.Contains(out, secret) || strings.Contains(out, `"value": "us"`) {
		t.Fatalf("json show leaked:\n%s", out)
	}
	code, _, errOut = exec(st, "env", "unset", "staging", "region")
	if code != ExitOK {
		t.Fatalf("unset: %d %q", code, errOut)
	}
	code, out, _ = exec(st, "env", "use", "production")
	if code != ExitOK || !strings.Contains(out, "Using Production") || !strings.Contains(out, "prod") {
		t.Fatalf("use: %s", out)
	}

	code, out, errOut = exec(st, "curl", "Payments/List charges", "-e", "staging")
	if code != ExitOK || strings.Contains(out, secret) || !strings.Contains(out, "curl ") || !strings.Contains(out, "••••") {
		t.Fatalf("curl: code=%d err=%q leaked=%v\n%s", code, errOut, strings.Contains(out, secret), out)
	}
	code, out, errOut = exec(st, "curl", "list-charges", "-e", "staging", "--with-secrets")
	if code != ExitOK || !strings.Contains(out, secret) {
		t.Fatalf("with-secrets code=%d err=%q out=%s", code, errOut, out)
	}

	asciiOut, asciiErr := &bytes.Buffer{}, &bytes.Buffer{}
	code = Execute(context.Background(), Options{
		Store: st, Stdout: asciiOut, Stderr: asciiErr, ASCII: true, NoColor: true, Version: "test",
	}, []string{"send", "Payments/List charges"})
	if code != ExitOK || !strings.Contains(asciiOut.String(), "+ status is 200") || strings.Contains(asciiOut.String(), "✓") {
		t.Fatalf("ascii: code=%d err=%q\n%s", code, asciiErr.String(), asciiOut.String())
	}
}

func TestNoHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	st := demoWorkspace(t, srv)
	if code, _, errOut := exec(st, "send", "Payments/List charges", "--no-history"); code != ExitOK {
		t.Fatalf("send: %d %q", code, errOut)
	}
	hist := filepath.Join(st.Root(), ".postctl", "history", "history.jsonl")
	if _, err := os.Stat(hist); err == nil {
		t.Fatal("history file was written with --no-history")
	}
}

func exec(st *store.Store, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), options(st, &stdout, &stderr), args)
	return code, stdout.String(), stderr.String()
}

func options(st *store.Store, stdout, stderr *bytes.Buffer) Options {
	return Options{
		Store: st, Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr,
		NoColor: true, Version: "test",
		IsTTY: func(any) bool { return false },
	}
}

func demoWorkspace(t *testing.T, srv *httptest.Server) *store.Store {
	t.Helper()
	base := "http://127.0.0.1:1"
	if srv != nil {
		base = srv.URL
	}
	return demoWith(t, srv, `pm.test("status is 200", function () { pm.response.to.have.status(200); });`, base)
}

func demoWith(t *testing.T, srv *httptest.Server, listTest, base string) *store.Store {
	t.Helper()
	if base == "" && srv != nil {
		base = srv.URL
	}
	if base == "" {
		base = "http://127.0.0.1:1"
	}
	dir := t.TempDir()
	secrets := store.NewMemorySecrets()
	st, err := store.Init(dir, "demo", store.WithSecretStore(secrets))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	list := model.NewRequest("List charges", "GET", "{{baseUrl}}/charges")
	list.ID = "req-list"
	list.Headers = []model.KV{{Key: "X-Token", Value: "{{token}}"}}
	if listTest != "" {
		list.Scripts.Test = &model.Script{Source: listTest}
	}
	create := model.NewRequest("Create", "POST", "{{baseUrl}}/charges")
	create.ID = "req-create"
	create.Headers = []model.KV{{Key: "Content-Type", Value: "application/json"}}
	create.Body = model.Body{Mode: model.BodyRaw, Raw: `{"amount":1}`}
	if listTest != "" && strings.Contains(listTest, "fails") {
		create.Scripts.Test = &model.Script{Source: listTest}
	}
	folder := model.NewFolder("Billing")
	folder.ID = "fold-bill"
	folder.Items = []model.Item{create}
	// A second Get makes the bare name ambiguous.
	getA := model.NewRequest("Get", "GET", "{{baseUrl}}/a")
	getA.ID = "req-get-a"
	getB := model.NewRequest("Get", "GET", "{{baseUrl}}/b")
	getB.ID = "req-get-b"
	users := model.NewFolder("Users")
	users.ID = "fold-users"
	users.Items = []model.Item{getA}
	orders := model.NewFolder("Orders")
	orders.ID = "fold-orders"
	orders.Items = []model.Item{getB}
	col := model.NewCollection("Payments")
	col.ID = "col-pay"
	col.Items = []model.Item{folder, list, users, orders}
	const secret = "synthetic-secret-value"
	staging := &model.Environment{ID: "env-stg", Name: "Staging", Variables: []model.Variable{
		{Key: "baseUrl", Value: base},
		{Key: "token", Value: secret, Type: model.VarSecret},
	}}
	prod := &model.Environment{ID: "env-prod", Name: "Production", Production: true, Variables: []model.Variable{
		{Key: "baseUrl", Value: base},
	}}
	ws.Collections = []*model.Collection{col}
	ws.Environments = []*model.Environment{staging, prod}
	if err := st.Save(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveState(store.State{ActiveEnvironment: staging.ID}); err != nil {
		t.Fatal(err)
	}
	return st
}

func closedWorkspace(t *testing.T) *store.Store {
	t.Helper()
	return demoWith(t, nil, "", "http://127.0.0.1:1")
}
