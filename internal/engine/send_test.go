package engine

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/oauth2"
)

func TestSendFullPipeline(t *testing.T) {
	const (
		oldSecret = "old-secret-value-aaaa"
		newSecret = "rotated-secret-value-bbbb"
	)
	srv := newServer(t, jsonHandler(`{"id":"42","echo":"ok"}`))
	f := newFixture(t, srv)
	f.env.Variables = append(f.env.Variables,
		model.Variable{Key: "token", Value: oldSecret, Type: model.VarSecret})

	req := model.NewRequest("Get item", "GET", "{{baseUrl}}/items/{{itemId}}?k={{token}}")
	req.Headers = []model.KV{{Key: "X-Trace", Value: "{{token}}"}}
	req.Scripts.PreRequest = js(`
		pm.environment.set("itemId", "42");
		pm.environment.set("token", "rotated-secret-" + "value-bbbb");
	`)
	req.Scripts.Test = js(`
		pm.test("status is 200", () => pm.response.to.have.status(200));
		const body = pm.response.json();
		pm.test("id echoed", () => pm.expect(body.id).to.eql(pm.environment.get("itemId")));
		pm.environment.set("returned", body.id);
		console.log("done", body.echo);
	`)
	f.col.Items = append(f.col.Items, req)
	f.save()

	var steps []Step
	out := f.send(f.findRequest("Get item"), func(s Step) { steps = append(steps, s) })

	if out.Err != nil || out.ScriptError != nil {
		t.Fatalf("err=%v scriptErr=%v", out.Err, out.ScriptError)
	}
	if hits := srv.hits(); len(hits) != 1 || !strings.HasPrefix(hits[0], "/items/42?k=") {
		t.Fatalf("server saw %v", hits)
	}
	if out.Response == nil || out.Response.StatusCode != 200 {
		t.Fatalf("response: %+v", out.Response)
	}
	if c := out.TestCounts(); c.Passed != 2 || c.Failed != 0 {
		t.Fatalf("tests: %+v %+v", c, out.Tests)
	}
	if len(out.Console) != 1 || out.Console[0].Text != "done ok" || out.Console[0].Event != "test" {
		t.Fatalf("console: %+v", out.Console)
	}
	want := []string{"Running pre-request script", "Resolving variables", "Waiting for response", "Running tests", "Saving variables"}
	if got := stepTexts(steps); !reflect.DeepEqual(got, want) {
		t.Fatalf("steps:\n got %q\nwant %q", got, want)
	}
	if !reflect.DeepEqual(out.Persisted, []string{"environment"}) {
		t.Fatalf("persisted: %v", out.Persisted)
	}

	// The change reached the environment file and, for the secret, the
	// SecretStore - never the file.
	env := f.reloadEnv()
	if v, _ := varValue(env.Variables, "itemId"); v != "42" {
		t.Fatalf("itemId on disk = %q", v)
	}
	if v, _ := varValue(env.Variables, "returned"); v != "42" {
		t.Fatalf("returned on disk = %q", v)
	}
	var token model.Variable
	for _, v := range env.Variables {
		if v.Key == "token" {
			token = v
		}
	}
	if !token.IsSecret() || token.Value != "" || token.SecretRef == "" {
		t.Fatalf("token should stay a secret held by reference: %+v", token)
	}
	if got, err := f.secrets.Get(context.Background(), token.SecretRef); err != nil || got != newSecret {
		t.Fatalf("secret store has %q, %v", got, err)
	}

	// History has one redacted entry and no file holds a secret value.
	entries, err := f.eng.cfg.History.(*history.Log).List(history.Filter{})
	if err != nil || len(entries) != 1 || out.HistoryID != entries[0].ID {
		t.Fatalf("history: %v %+v (outcome %q)", err, entries, out.HistoryID)
	}
	full, err := f.eng.cfg.History.(*history.Log).Get(entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Status != 200 || full.Tests.Passed != 2 || full.RequestName != "Get item" ||
		full.Response == nil || !strings.Contains(full.Response.Body, `"id":"42"`) {
		t.Fatalf("history entry: %+v", full)
	}
	for path, content := range filesUnder(t, f.dir) {
		for _, secret := range []string{oldSecret, newSecret} {
			if strings.Contains(content, secret) {
				t.Errorf("secret value found in %s", strings.TrimPrefix(path, f.dir))
			}
		}
	}
}

func TestSendDoesNotMutateCallerModels(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.env.Variables = append(f.env.Variables, model.Variable{Key: "s", Value: "secret-value-1", Type: model.VarSecret})
	f.col.Variables = []model.Variable{{Key: "cv", Value: "1"}}
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Scripts.PreRequest = js(`pm.environment.set("a","b"); pm.collectionVariables.set("cv","2"); pm.request.headers.add({key:"X-A",value:"1"});`)
	f.col.Items = append(f.col.Items, req)
	f.save()

	col, env, r := f.col.Clone(), f.env.Clone(), f.findRequest("R").Clone()
	out := f.send(f.findRequest("R"), nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if !reflect.DeepEqual(col, f.col) || !reflect.DeepEqual(env, f.env) || !reflect.DeepEqual(r, f.findRequest("R")) {
		t.Fatal("Send mutated a caller model")
	}
	// ...yet the writes reached the disk.
	if v, _ := varValue(f.reloadEnv().Variables, "a"); v != "b" {
		t.Fatalf("env var not persisted: %q", v)
	}
	if v, _ := varValue(f.reloadCollection().Variables, "cv"); v != "2" {
		t.Fatalf("collection var not persisted: %q", v)
	}
	if !reflect.DeepEqual(out.Persisted, []string{"environment", "collection"}) {
		t.Fatalf("persisted: %v", out.Persisted)
	}
}

func TestSendNothingChangedWritesNothing(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Scripts.Test = js(`pm.variables.set("local", 1); pm.test("t", () => {});`)
	f.col.Items = append(f.col.Items, req)
	f.save()
	before := filesUnder(t, f.dir)
	delete(before, f.dir+"/.postctl/history/history.jsonl")

	var steps []Step
	out := f.send(f.findRequest("R"), func(s Step) { steps = append(steps, s) })
	if len(out.Persisted) != 0 {
		t.Fatalf("persisted %v", out.Persisted)
	}
	for _, s := range steps {
		if s.Kind == StepSave {
			t.Fatal("no save step expected")
		}
	}
	after := filesUnder(t, f.dir)
	for p := range after {
		if strings.Contains(p, ".postctl") {
			delete(after, p)
		}
	}
	for p := range before {
		if strings.Contains(p, ".postctl") {
			delete(before, p)
		}
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("workspace files changed although no persisted variable did")
	}
}

func TestChainOrder(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.col.Scripts.PreRequest = js(`pm.variables.set("o", "C"); console.log("pre C");`)
	f.col.Scripts.Test = js(`console.log("test C");`)
	outer := model.NewFolder("Outer")
	outer.Scripts.PreRequest = js(`pm.variables.set("o", pm.variables.get("o") + "O"); console.log("pre O");`)
	outer.Scripts.Test = js(`console.log("test O");`)
	inner := model.NewFolder("Inner")
	inner.Scripts.PreRequest = js(`pm.variables.set("o", pm.variables.get("o") + "I"); console.log("pre I");`)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/{{o}}")
	req.Scripts.PreRequest = js(`pm.variables.set("o", pm.variables.get("o") + "R"); console.log("pre R");`)
	req.Scripts.Test = js(`console.log("test R");`)
	inner.Items = []model.Item{req}
	outer.Items = []model.Item{inner}
	f.col.Items = []model.Item{outer}
	f.save()

	var steps []Step
	out := f.send(f.findRequest("R"), func(s Step) { steps = append(steps, s) })
	if out.Err != nil || out.ScriptError != nil {
		t.Fatalf("err=%v scriptErr=%v", out.Err, out.ScriptError)
	}
	if hits := srv.hits(); len(hits) != 1 || hits[0] != "/COIR" {
		t.Fatalf("server saw %v", hits)
	}
	var lines []string
	for _, c := range out.Console {
		lines = append(lines, c.Text)
	}
	want := []string{"pre C", "pre O", "pre I", "pre R", "test C", "test O", "test R"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("console order:\n got %q\nwant %q", lines, want)
	}
	if !reflect.DeepEqual(out.Path, []string{"API", "Outer", "Inner", "R"}) {
		t.Fatalf("path: %v", out.Path)
	}
	wantSteps := []string{
		"Running collection pre-request script", "Running folder pre-request script",
		"Running folder pre-request script", "Running pre-request script",
		"Resolving variables", "Waiting for response",
		"Running collection tests", "Running folder tests", "Running tests",
	}
	if got := stepTexts(steps); !reflect.DeepEqual(got, wantSteps) {
		t.Fatalf("steps:\n got %q\nwant %q", got, wantSteps)
	}
}

func TestPreRequestSeesUnresolvedRequest(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Scripts.PreRequest = js(`pm.environment.set("seen", pm.request.url.toString());`)
	f.col.Items = append(f.col.Items, req)
	f.save()
	f.send(f.findRequest("R"), nil)
	if v, _ := varValue(f.reloadEnv().Variables, "seen"); v != "{{baseUrl}}/x" {
		t.Fatalf("pm.request.url in a pre-request script = %q, want it unresolved", v)
	}
}

func TestPreRequestMutationIsSent(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Scripts.PreRequest = js(`pm.request.headers.add({key: "X-From-Script", value: "yes"}); pm.request.url.addQueryParams("a=1");`)
	f.col.Items = append(f.col.Items, req)
	f.save()
	out := f.send(f.findRequest("R"), nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if hits := srv.hits(); len(hits) != 1 || hits[0] != "/x?a=1" {
		t.Fatalf("server saw %v", hits)
	}
	if srv.hdrs[0].Get("X-From-Script") != "yes" {
		t.Fatal("header added by the pre-request script was not sent")
	}
}

func TestSkipRequest(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.col.Scripts.PreRequest = js(`pm.execution.skipRequest(); pm.environment.set("skipped", "yes");`)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Scripts.PreRequest = js(`console.log("must not run");`)
	req.Scripts.Test = js(`pm.test("must not run", () => {});`)
	f.col.Items = append(f.col.Items, req)
	f.save()

	out := f.send(f.findRequest("R"), nil)
	if !out.Skipped || out.Response != nil || out.Err != nil {
		t.Fatalf("outcome: %+v", out)
	}
	if len(srv.hits()) != 0 || len(out.Tests) != 0 || len(out.Console) != 0 {
		t.Fatalf("hits=%v tests=%v console=%v", srv.hits(), out.Tests, out.Console)
	}
	if out.HistoryID != "" {
		t.Fatal("a skipped request must not enter history")
	}
	if v, _ := varValue(f.reloadEnv().Variables, "skipped"); v != "yes" {
		t.Fatal("variables set before skipRequest should still persist")
	}
}

func TestPreRequestErrorStopsSend(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Scripts.PreRequest = js(`pm.environment.set("before", "1"); console.log("hi"); throw new Error("boom");`)
	req.Scripts.Test = js(`pm.test("must not run", () => {});`)
	f.col.Items = append(f.col.Items, req)
	f.save()

	out := f.send(f.findRequest("R"), nil)
	if out.ScriptError == nil || out.ScriptError.Event != "prerequest" || out.ScriptError.Origin != "request" ||
		!strings.Contains(out.ScriptError.Text, "boom") {
		t.Fatalf("script error: %+v", out.ScriptError)
	}
	if out.Response != nil || len(srv.hits()) != 0 || len(out.Tests) != 0 {
		t.Fatal("the request must not be sent after a failing pre-request script")
	}
	if len(out.Console) != 1 || out.Console[0].Text != "hi" {
		t.Fatalf("console before the failure should be kept: %+v", out.Console)
	}
	if v, _ := varValue(f.reloadEnv().Variables, "before"); v != "1" {
		t.Fatal("variables set before the error should persist")
	}
	if !out.Failed() {
		t.Fatal("a script error is a failure")
	}
}

func TestTestScriptErrorDoesNotStopSend(t *testing.T) {
	srv := newServer(t, jsonHandler(`{"a":1}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Scripts.Test = js(`
		pm.test("first", () => {});
		pm.environment.set("kept", "yes");
		nope.nope();
	`)
	f.col.Items = append(f.col.Items, req)
	f.save()

	out := f.send(f.findRequest("R"), nil)
	if out.Response == nil || out.Response.StatusCode != 200 {
		t.Fatal("the response should be kept")
	}
	if out.ScriptError == nil || out.ScriptError.Event != "test" {
		t.Fatalf("script error: %+v", out.ScriptError)
	}
	if len(out.Tests) != 1 || !out.Tests[0].Passed {
		t.Fatalf("tests before the failure should be kept: %+v", out.Tests)
	}
	if v, _ := varValue(f.reloadEnv().Variables, "kept"); v != "yes" {
		t.Fatal("variables should persist after a failing test script")
	}
	if out.HistoryID == "" {
		t.Fatal("the send should be recorded in history")
	}
}

func TestUnresolvedVariableWarns(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.col.Items = append(f.col.Items, model.NewRequest("R", "GET", "{{baseUrl}}/{{missing}}/{{other}}"))
	f.save()
	out := f.send(f.findRequest("R"), nil)
	if out.Err != nil {
		t.Fatalf("an unresolved variable is a warning, not an error: %v", out.Err)
	}
	got := strings.Join(out.Warnings, "|")
	if !strings.Contains(got, "{{missing}}") || !strings.Contains(got, "{{other}}") || strings.Contains(got, "baseUrl") {
		t.Fatalf("warnings: %v", out.Warnings)
	}
}

func TestOAuth2NoTokenError(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Auth = &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
		GrantType: model.GrantClientCredentials, AccessTokenURL: srv.URL + "/token", ClientID: "id",
	}}
	f.col.Items = append(f.col.Items, req)
	f.save()

	out := f.send(f.findRequest("R"), nil)
	var missing *OAuthTokenMissingError
	if !errors.As(out.Err, &missing) || !errors.Is(out.Err, oauth2.ErrNoToken) {
		t.Fatalf("err = %v", out.Err)
	}
	if out.Err.Error() != "No OAuth 2.0 token. Get a new token in the Auth tab." {
		t.Fatalf("message = %q", out.Err.Error())
	}
	if len(srv.hits()) != 0 {
		t.Fatal("the engine must not fetch a token or send without one")
	}
}

func TestOAuth2StoredTokenIsUsed(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	tok := &model.OAuth2Token{AccessToken: "stored-access-token-zzzz", TokenType: "Bearer"}
	req.Auth = &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
		GrantType: model.GrantClientCredentials, AccessTokenURL: srv.URL + "/token", ClientID: "id", Token: tok,
	}}
	f.col.Items = append(f.col.Items, req)
	f.save() // the store moves the token into the secret store

	if got := f.findRequest("R").Auth.OAuth2.Token; got.AccessToken != "" || got.Ref == "" {
		t.Fatalf("loaded token should be a reference only: %+v", got)
	}
	out := f.send(f.findRequest("R"), nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if got := srv.hdrs[0].Get("Authorization"); got != "Bearer stored-access-token-zzzz" {
		t.Fatalf("Authorization = %q", got)
	}
	for path, content := range filesUnder(t, f.dir) {
		if strings.Contains(content, "stored-access-token-zzzz") {
			t.Errorf("token found in %s", strings.TrimPrefix(path, f.dir))
		}
	}
}

func TestInheritedAuthAndCollectionVariables(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.col.Variables = []model.Variable{{Key: "tok", Value: "col-token"}}
	f.col.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{tok}}"}}
	folder := model.NewFolder("F")
	folder.Items = []model.Item{model.NewRequest("R", "GET", "{{baseUrl}}/x")}
	f.col.Items = []model.Item{folder}
	f.save()
	out := f.send(f.findRequest("R"), nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if got := srv.hdrs[0].Get("Authorization"); got != "Bearer col-token" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestUnsavedRequestCopyIsSent(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.col.Items = append(f.col.Items, model.NewRequest("R", "GET", "{{baseUrl}}/saved"))
	f.save()
	edited := f.findRequest("R").Clone()
	edited.URL = model.ParseURL("{{baseUrl}}/edited")
	out := f.send(edited, nil)
	if out.Err != nil {
		t.Fatal(out.Err)
	}
	if hits := srv.hits(); len(hits) != 1 || hits[0] != "/edited" {
		t.Fatalf("server saw %v", hits)
	}
}

func TestCookiesSetByServerAreReadByScripts(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc123", Path: "/"})
		}
		_, _ = w.Write([]byte("{}"))
	})
	f := newFixture(t, srv)
	login := model.NewRequest("Login", "POST", "{{baseUrl}}/login")
	login.Scripts.Test = js(`pm.test("set-cookie parsed", () => pm.expect(pm.response.cookies.get("sid")).to.eql("abc123"));`)
	next := model.NewRequest("Next", "GET", "{{baseUrl}}/me")
	next.Scripts.PreRequest = js(`
		pm.cookies.jar().get(pm.environment.get("baseUrl"), "sid", (err, value) => pm.environment.set("sidBefore", value));
	`)
	next.Scripts.Test = js(`
		pm.test("jar cookie visible", () => pm.expect(pm.cookies.get("sid")).to.eql("abc123"));
		pm.test("jar count", () => pm.expect(pm.cookies.count()).to.eql(1));
	`)
	f.col.Items = []model.Item{login, next}
	f.save()

	if out := f.send(f.findRequest("Login"), nil); out.TestCounts().Failed != 0 || out.ScriptError != nil {
		t.Fatalf("login: %+v %+v", out.Tests, out.ScriptError)
	}
	out := f.send(f.findRequest("Next"), nil)
	if out.ScriptError != nil || out.TestCounts().Passed != 2 || out.TestCounts().Failed != 0 {
		t.Fatalf("next: tests=%+v err=%+v", out.Tests, out.ScriptError)
	}
	if srv.hdrs[1].Get("Cookie") != "sid=abc123" {
		t.Fatalf("the jar cookie was not sent: %q", srv.hdrs[1].Get("Cookie"))
	}
	if v, _ := varValue(f.reloadEnv().Variables, "sidBefore"); v != "abc123" {
		t.Fatalf("pm.cookies.jar().get in a pre-request script = %q", v)
	}
}

func TestSendRequestFromPreRequest(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"token":"from-token-endpoint"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/data?t={{fetched}}")
	req.Scripts.PreRequest = js(`
		pm.sendRequest("{{baseUrl}}/token", (err, res) => {
			pm.expect(err).to.eql(null);
			pm.environment.set("fetched", res.json().token);
		});
	`)
	f.col.Items = append(f.col.Items, req)
	f.save()

	out := f.send(f.findRequest("R"), nil)
	if out.Err != nil || out.ScriptError != nil {
		t.Fatalf("err=%v script=%+v", out.Err, out.ScriptError)
	}
	hits := srv.hits()
	if len(hits) != 2 || hits[0] != "/token" || hits[1] != "/data?t=from-token-endpoint" {
		t.Fatalf("server saw %v", hits)
	}
	entries, _ := f.eng.cfg.History.(*history.Log).List(history.Filter{})
	if len(entries) != 1 {
		t.Fatalf("pm.sendRequest must not record history, got %d entries", len(entries))
	}
}

func TestTransportErrorIsRecordedAndRedacted(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "http://127.0.0.1:1/x?access_token=very-private-1234&q=1")
	f.col.Items = append(f.col.Items, req)
	f.save()
	out := f.send(f.findRequest("R"), nil)
	if out.Err == nil || out.Response != nil {
		t.Fatalf("expected a transport error, got %+v", out)
	}
	if strings.Contains(out.Err.Error(), "very-private-1234") {
		t.Fatalf("the error leaks a token: %v", out.Err)
	}
	if out.HistoryID == "" {
		t.Fatal("failed sends are recorded")
	}
	// The request file legitimately holds the URL the user typed; the state
	// the engine writes (history) must not.
	for path, content := range filesUnder(t, f.dir+"/.postctl") {
		if strings.Contains(content, "very-private-1234") {
			t.Errorf("token found in %s", strings.TrimPrefix(path, f.dir))
		}
	}
}

func TestCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/slow")
	req.Scripts.Test = js(`pm.test("must not run", () => {});`)
	f.col.Items = append(f.col.Items, req)
	f.save()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for len(srv.hits()) == 0 {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()
	out, err := f.eng.Send(ctx, SendInput{Collection: f.col, Request: f.findRequest("R"), Environment: f.env}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(out.Err, context.Canceled) || out.Response != nil || len(out.Tests) != 0 {
		t.Fatalf("outcome: err=%v", out.Err)
	}
	if out.HistoryID != "" {
		t.Fatal("cancelled sends are not recorded")
	}
}

func TestScriptTimeout(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Scripts.PreRequest = js(`while (true) {}`)
	f.col.Items = append(f.col.Items, req)
	f.save(func(c *Config) { c.ScriptTimeout = 100 * time.Millisecond })
	start := time.Now()
	out := f.send(f.findRequest("R"), nil)
	if out.ScriptError == nil || !out.ScriptError.TimedOut {
		t.Fatalf("script error: %+v", out.ScriptError)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("the runaway script was not interrupted promptly")
	}
	if len(srv.hits()) != 0 {
		t.Fatal("the request must not be sent")
	}
}

func TestGlobalsAreLoadedAndPersisted(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.ws.Globals = &model.Globals{ID: model.NewID(), Name: "Globals", Variables: []model.Variable{{Key: "g", Value: "one"}}}
	req := model.NewRequest("R", "GET", "{{baseUrl}}/{{g}}")
	req.Scripts.Test = js(`pm.globals.set("g", "two"); pm.globals.set("fresh", "x");`)
	f.col.Items = append(f.col.Items, req)
	f.save()

	f.send(f.findRequest("R"), nil)
	f.send(f.findRequest("R"), nil)
	if hits := srv.hits(); len(hits) != 2 || hits[0] != "/one" || hits[1] != "/two" {
		t.Fatalf("server saw %v (globals must persist between sends)", hits)
	}
	ws, err := f.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := varValue(ws.Globals.Variables, "g"); v != "two" {
		t.Fatalf("g = %q", v)
	}
	if v, _ := varValue(ws.Globals.Variables, "fresh"); v != "x" {
		t.Fatalf("fresh = %q", v)
	}
}

func TestNoEnvironmentWarnsWhenScriptWritesOne(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", srv.URL+"/x")
	req.Scripts.Test = js(`pm.environment.set("a", "1");`)
	f.col.Items = append(f.col.Items, req)
	f.save()
	out, err := f.eng.Send(context.Background(), SendInput{Collection: f.col, Request: f.findRequest("R")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "No environment is selected") {
		t.Fatalf("warnings: %v", out.Warnings)
	}
}

func TestMissingSecretWarns(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.env.Variables = append(f.env.Variables, model.Variable{Key: "s", Value: "v-value-1234", Type: model.VarSecret})
	f.col.Items = append(f.col.Items, model.NewRequest("R", "GET", "{{baseUrl}}/x"))
	f.save()
	for _, ref := range f.secrets.Refs() {
		_ = f.secrets.Delete(context.Background(), ref)
	}
	out := f.send(f.findRequest("R"), nil)
	if out.Err != nil || len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "1 secret variable(s)") {
		t.Fatalf("err=%v warnings=%v", out.Err, out.Warnings)
	}
}

func TestInvalidInput(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	f.save()
	if _, err := f.eng.Send(context.Background(), SendInput{Request: model.NewRequest("R", "GET", "x")}, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
	if _, err := f.eng.Send(context.Background(), SendInput{Collection: f.col}, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v", err)
	}
	if _, err := New(Config{}); err == nil {
		t.Fatal("New without a Store must fail")
	}
}

func TestIterationDataAndLocals(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/{{user}}/{{tag}}")
	req.Scripts.Test = js(`
		pm.test("iteration info", () => {
			pm.expect(pm.info.iteration).to.eql(2);
			pm.expect(pm.info.iterationCount).to.eql(5);
			pm.expect(pm.iterationData.get("user")).to.eql("carol");
		});
	`)
	f.col.Items = append(f.col.Items, req)
	f.save()
	out, err := f.eng.Send(context.Background(), SendInput{
		Collection: f.col, Request: f.findRequest("R"), Environment: f.env,
		Data: map[string]any{"user": "carol"}, Locals: map[string]any{"tag": "t1"},
		Iteration: 2, IterationCount: 5,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.TestCounts().Failed != 0 || out.TestCounts().Passed != 1 {
		t.Fatalf("tests: %+v", out.Tests)
	}
	if hits := srv.hits(); len(hits) != 1 || hits[0] != "/carol/t1" {
		t.Fatalf("server saw %v", hits)
	}
}

func TestGlobalsFileIsCreatedWhenMissing(t *testing.T) {
	srv := newServer(t, jsonHandler(`{}`))
	f := newFixture(t, srv)
	req := model.NewRequest("R", "GET", "{{baseUrl}}/x")
	req.Scripts.Test = js(`pm.globals.set("created", "yes");`)
	f.col.Items = append(f.col.Items, req)
	f.save()
	out := f.send(f.findRequest("R"), nil)
	if !reflect.DeepEqual(out.Persisted, []string{"globals"}) || len(out.Warnings) != 0 {
		t.Fatalf("persisted=%v warnings=%v", out.Persisted, out.Warnings)
	}
	ws, err := f.store.Load()
	if err != nil || ws.Globals == nil {
		t.Fatalf("globals not written: %v", err)
	}
	if v, _ := varValue(ws.Globals.Variables, "created"); v != "yes" {
		t.Fatalf("created = %q", v)
	}
}
