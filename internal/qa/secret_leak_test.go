package qa_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/report"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
	tuihistory "github.com/shubham/postctl/internal/tui/history"
	"github.com/shubham/postctl/internal/tui/request"
	"github.com/shubham/postctl/internal/tui/response"
	"github.com/shubham/postctl/internal/tui/tuiengine"
	"github.com/shubham/postctl/internal/ui"
)

func TestSecretDoesNotLeakAcrossPersistenceReportsCurlOrTUI(t *testing.T) {
	sentinel := "postctl-leak-sentinel-" + model.NewID().String()
	// folderSentinel is the value of a secret variable defined on a folder, so
	// the folder-variable path (hydration, resolution, masking) is swept too.
	folderSentinel := "postctl-folder-sentinel-" + model.NewID().String()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "session="+sentinel+"; Max-Age=0; Path=/")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"jwt": sentinel, "requestPath": r.URL.Path, "queryEcho": r.URL.RawQuery,
			"folderHeader": r.Header.Get("X-Folder"),
		})
	}))
	t.Cleanup(server.Close)

	root := t.TempDir()
	secrets := store.NewMemorySecrets()
	st, err := store.Init(root, "Leak sweep", store.WithSecretStore(secrets))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	env := &model.Environment{
		ID:   model.NewID(),
		Name: "Secret environment",
		Variables: []model.Variable{
			{Key: "baseUrl", Value: server.URL},
			{Key: "accessToken", Value: sentinel, Type: model.VarSecret},
		},
	}
	bearer := model.NewRequest("Bearer request", http.MethodGet, "{{baseUrl}}/bearer/{{accessToken}}?api_key={{accessToken}}")
	bearer.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{accessToken}}"}}
	bearer.Scripts.Test = &model.Script{Source: `console.log(pm.environment.get("accessToken"));`}
	oauth := model.NewRequest("OAuth request", http.MethodGet, "{{baseUrl}}/oauth")
	oauth.Auth = &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
		GrantType: model.GrantClientCredentials,
		Token:     &model.OAuth2Token{AccessToken: sentinel, TokenType: "Bearer"},
	}}
	folderRequest := model.NewRequest("Folder request", http.MethodPost,
		"{{baseUrl}}/folder/{{folderToken}}?token={{folderToken}}")
	folderRequest.Headers = []model.KV{{Key: "X-Folder", Value: "{{folderToken}}"}}
	folderRequest.Body = model.Body{Mode: model.BodyRaw, Raw: `{"token":"{{folderToken}}"}`}
	folderRequest.Scripts.PreRequest = &model.Script{Source: `console.log(pm.variables.get("folderToken"));`}
	folderRequest.Scripts.Test = &model.Script{Source: `
		pm.test("folder secret must not surface", () => {
			pm.expect("wrong").to.eql(pm.variables.get("folderToken"));
		});`}
	folder := model.NewFolder("Folder secrets")
	folder.Variables = []model.Variable{{Key: "folderToken", Value: folderSentinel, Type: model.VarSecret}}
	folder.Items = []model.Item{folderRequest}
	coll := model.NewCollection("Secrets")
	coll.Items = []model.Item{bearer, oauth, folder}
	ws.Collections = []*model.Collection{coll}
	ws.Environments = []*model.Environment{env}
	if err := st.Save(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	ws, err = st.Load()
	if err != nil {
		t.Fatal(err)
	}
	coll, env = ws.Collections[0], ws.Environments[0]
	bearer, oauth, folderRequest = coll.Requests()[0], coll.Requests()[1], coll.Requests()[2]

	log, err := history.Open(filepath.Join(st.StateDir(), "history"), history.Options{})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(engine.Config{Store: st, History: log, Version: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	bearerOutcome, err := eng.Send(context.Background(), engine.SendInput{
		Collection: coll, Request: bearer, Environment: env,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	oauthForSend := oauth.Clone()
	oauthForSend.URL = model.ParseURL(server.URL + "/oauth/" + sentinel)
	oauthOutcome, err := eng.Send(context.Background(), engine.SendInput{
		Collection: coll, Request: oauthForSend, Environment: env,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if oauthOutcome.Response == nil || !bytes.Contains(oauthOutcome.Response.Body, []byte(sentinel)) {
		t.Fatal("server did not echo the OAuth credential in the exempt in-memory response body")
	}

	folderOutcome, err := eng.Send(context.Background(), engine.SendInput{
		Collection: coll, Request: folderRequest, Environment: env,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if folderOutcome.Response == nil || !bytes.Contains(folderOutcome.Response.Body, []byte(folderSentinel)) {
		t.Fatal("folder secret variable was not resolved on the wire")
	}
	if folderOutcome.Redactor == nil {
		t.Fatal("outcome has no redactor")
	}

	var events []engine.RunEvent
	runEvents, err := eng.RunAsync(context.Background(), engine.RunInput{
		Collection: coll, Environment: env, Iterations: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for ev := range runEvents {
		events = append(events, ev)
	}

	// The folder value is masked by the engine's own redactor (built from the
	// hydrated folder variables), not by a list the test hands over.
	redactor := history.NewRedactor([]string{sentinel})
	mask := func(text string) string { return folderOutcome.Redactor.Text(redactor.Text(text)) }
	var jsonReport, junitReport bytes.Buffer
	if err := report.Write(report.NewJSON(&jsonReport, report.Options{Mask: mask}), events); err != nil {
		t.Fatal(err)
	}
	if err := report.Write(report.NewJUnit(&junitReport, report.Options{Mask: mask}), events); err != nil {
		t.Fatal(err)
	}

	entries, err := log.List(history.Filter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("engine did not write history")
	}
	entry, err := log.Get(entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	curl, err := tuihistory.DefaultCurl(context.Background(), entry)
	if err != nil {
		t.Fatal(err)
	}
	// Every history entry, not only the newest, and each one as masked curl.
	var historyDump, curlDump strings.Builder
	for _, summary := range entries {
		full, err := log.Get(summary.ID)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(full)
		if err != nil {
			t.Fatal(err)
		}
		historyDump.Write(raw)
		masked, err := tuihistory.DefaultCurl(context.Background(), full)
		if err != nil {
			t.Fatal(err)
		}
		curlDump.WriteString(masked)
	}

	querySecretReq := model.NewRequest("Query secret", http.MethodGet, server.URL+"/items?token="+sentinel)
	ws.Collections[0].Items = append(ws.Collections[0].Items, querySecretReq)

	frames := renderLeakSweepFrames(t, ws, env, coll, oauth, bearerOutcome, log)
	for name, frame := range renderLeakSweepFrames(t, ws, env, coll, folderRequest, folderOutcome, log) {
		frames["folder "+name] = frame
	}

	shell := tui.New(tui.Options{
		Backend: sweepBackend{
			Backend: tui.NewStoreBackend(root, store.WithSecretStore(secrets)),
			ws:      ws,
		},
		NoWatch: true,
	})
	if initCmd := shell.Init(); initCmd != nil {
		if batch, ok := initCmd().(tea.BatchMsg); ok && len(batch) > 0 {
			shell, _ = shell.Update(batch[0]())
		}
	}
	shell, _ = shell.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	shell, _ = shell.Update(tui.OpenSwitcherMsg{})
	frames["switcher"] = shell.View().Content
	pager := response.MaskBodyForExternalOutput(tuiengine.OutcomeToResult(oauthOutcome))
	folderPager := response.MaskBodyForExternalOutput(tuiengine.OutcomeToResult(folderOutcome))
	for _, secret := range []string{sentinel, folderSentinel} {
		assertAbsent(t, secret, "JSON report", jsonReport.Bytes())
		assertAbsent(t, secret, "JUnit report", junitReport.Bytes())
		assertAbsent(t, secret, "masked curl", []byte(curl))
		assertAbsent(t, secret, "masked curl (all entries)", []byte(curlDump.String()))
		assertAbsent(t, secret, "history entries", []byte(historyDump.String()))
		assertAbsent(t, secret, "pager output", pager)
		assertAbsent(t, secret, "folder pager output", folderPager)
		for name, frame := range frames {
			if strings.HasSuffix(name, "response body") {
				continue // the in-memory body is exempt: it shows what the server sent
			}
			assertAbsent(t, secret, name+" frame", []byte(frame))
			if strings.HasSuffix(name, "response cookies") {
				// The cookie column clips long values, so also look for the start.
				assertAbsent(t, secret[:16], name+" frame (clipped value)", []byte(frame))
			}
		}
		assertWorkspaceFilesClean(t, root, secret)
	}
}

func renderLeakSweepFrames(
	t *testing.T,
	ws *model.Workspace,
	env *model.Environment,
	coll *model.Collection,
	req *model.Request,
	out *engine.Outcome,
	log *history.Log,
) map[string]string {
	t.Helper()
	theme := ui.NewTheme(true, true, false)
	paneEnv := tui.PaneEnv{Theme: theme, Actions: &tui.Registry{}}

	requestPane := request.New(paneEnv, request.Deps{})
	requestPane.SetSize(140, 40)
	_, _ = requestPane.Update(tui.WorkspaceReloadedMsg{Workspace: ws})
	_, _ = requestPane.Update(tui.EnvironmentChangedMsg{Env: env})
	_, _ = requestPane.Update(tui.SelectionMsg{Ref: tui.RequestRef{
		Collection: coll, Request: req, Path: []string{coll.Name, req.Name},
	}})
	_ = requestPane.Focus()
	_, _ = requestPane.Update(tea.KeyPressMsg{Code: tea.KeyRight})

	// The response pane shows a result only for the request it was sent for.
	shown := req
	for _, r := range coll.Requests() {
		if r.ID == out.RequestID {
			shown = r
		}
	}
	if shown.ID != out.RequestID {
		t.Fatal("outcome request is not in the collection, so the response frames would be empty states")
	}
	responsePane := response.New(paneEnv)
	responsePane.SetSize(140, 40)
	_, _ = responsePane.Update(tui.SelectionMsg{Ref: tui.RequestRef{
		Collection: coll, Request: shown,
	}})
	_, _ = responsePane.Update(tui.SendResultMsg{Result: tuiengine.OutcomeToResult(out)})
	_ = responsePane.Focus()
	// Walk all six tabs the way ] does and keep every frame.
	step := tui.SectionStepMsg{Delta: 1}
	tabNames := []string{"body", "headers", "cookies", "tests", "console", "timeline"}
	tabFrames := map[string]string{"body": responsePane.View()}
	for _, name := range tabNames[1:] {
		_, _ = responsePane.Update(step)
		tabFrames[name] = responsePane.View()
	}
	if !strings.Contains(tabFrames["cookies"], "session") {
		t.Fatal("cookies tab shows no session cookie, so its masking is not exercised")
	}
	if len(tabFrames) != len(tabNames) {
		t.Fatalf("captured %d response tabs, want %d", len(tabFrames), len(tabNames))
	}

	// Reveal (u) lasts for one visit: moving to another tab and back, or
	// blurring the pane, masks again. Compare frames, never print them.
	revealChanged := false
	for _, name := range tabNames {
		_, _ = responsePane.Update(tui.FocusSectionMsg{Section: tui.SectionRespBody})
		for range slices.Index(tabNames, name) {
			_, _ = responsePane.Update(step)
		}
		masked := responsePane.View()
		_, _ = responsePane.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
		if responsePane.View() != masked {
			revealChanged = true
		}
		_, _ = responsePane.Update(step) // another tab, then back
		_, _ = responsePane.Update(tui.SectionStepMsg{Delta: -1})
		if responsePane.View() != masked {
			t.Errorf("response %s tab stayed revealed after changing tabs", name)
		}
		_, _ = responsePane.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
		responsePane.Blur()
		_ = responsePane.Focus()
		if responsePane.View() != masked {
			t.Errorf("response %s tab stayed revealed after blur", name)
		}
	}
	if !revealChanged {
		t.Error("reveal changed no response tab, so the reset check proves nothing")
	}

	historyScreen := tuihistory.New(tui.ScreenEnv{
		PaneEnv: paneEnv, Workspace: ws, ActiveEnv: env.ID,
	}, tuihistory.Deps{
		Log:  func() (tui.HistoryReader, error) { return log, nil },
		Curl: tuihistory.DefaultCurl,
	})
	historyScreen.SetSize(140, 40)
	if _, cmd := historyScreen.Update(struct{}{}); cmd != nil {
		_, _ = historyScreen.Update(cmd())
	}
	if _, cmd := historyScreen.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		_, _ = historyScreen.Update(cmd())
	}

	frames := map[string]string{
		"request": requestPane.View(),
		"history": historyScreen.View(),
	}
	for name, frame := range tabFrames {
		frames["response "+name] = frame
	}
	return frames
}

func assertWorkspaceFilesClean(t *testing.T, root, sentinel string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(raw, []byte(sentinel)) {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("secret leaked to workspace file %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertAbsent(t *testing.T, sentinel, artifact string, raw []byte) {
	t.Helper()
	if strings.Contains(string(raw), sentinel) {
		t.Errorf("secret leaked to %s", artifact)
	}
}

type sweepBackend struct {
	tui.Backend
	ws *model.Workspace
}

func (b sweepBackend) Load(context.Context) (*model.Workspace, []*store.FileError, error) {
	return b.ws, nil, nil
}
