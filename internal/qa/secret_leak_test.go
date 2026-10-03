package qa_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/report"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/diffview"
	"github.com/shubham/postctl/internal/tui/envedit"
	tuihistory "github.com/shubham/postctl/internal/tui/history"
	"github.com/shubham/postctl/internal/tui/request"
	"github.com/shubham/postctl/internal/tui/response"
	"github.com/shubham/postctl/internal/tui/runner"
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
	literal := model.NewRequest("Literal template", http.MethodPost,
		"https://user:"+sentinel+"@example.test/literal?api_key="+sentinel+"&x="+sentinel)
	literal.Headers = []model.KV{
		{Key: "Authorization", Value: "Bearer " + sentinel},
		{Key: "X-Plain", Value: "pre-" + sentinel + "-post"},
	}
	literal.Body = model.Body{
		Mode: model.BodyRaw,
		Raw:  `{"password":"` + sentinel + `"}`,
	}
	literal.Scripts.PreRequest = &model.Script{Source: `pm.request.headers.add({key: "X-Script", value: pm.environment.get("accessToken")});`}
	literalURLEncoded := model.NewRequest("Literal urlencoded template", http.MethodPost, server.URL+"/literal-urlencoded")
	literalURLEncoded.Body = model.Body{
		Mode:       model.BodyURLEncoded,
		URLEncoded: []model.KV{{Key: "note", Value: sentinel}},
	}
	literalFormData := model.NewRequest("Literal form-data template", http.MethodPost, server.URL+"/literal-form-data")
	literalFormData.Body = model.Body{
		Mode: model.BodyFormData,
		FormData: []model.FormPart{{
			Key: "comment", Value: sentinel,
		}},
	}
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
	// Literal secrets stay in memory only so on-disk fixtures never hold the sentinel.
	coll.Items = append(coll.Items, literal, literalURLEncoded, literalFormData)

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
	literalOutcome, err := eng.Send(context.Background(), engine.SendInput{
		Collection: coll, Request: literal, Environment: env,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if literalOutcome.HistoryID == "" {
		t.Fatal("literal send did not record history")
	}
	urlencodedOutcome, err := eng.Send(context.Background(), engine.SendInput{
		Collection: coll, Request: literalURLEncoded, Environment: env,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	formDataOutcome, err := eng.Send(context.Background(), engine.SendInput{
		Collection: coll, Request: literalFormData, Environment: env,
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
	if entry.Template == nil {
		t.Fatal("history entry has no restore template")
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
	literalEntry, err := log.Get(literalOutcome.HistoryID)
	if err != nil || literalEntry.Template == nil {
		t.Fatal("literal history entry missing template")
	}
	urlencodedEntry, err := log.Get(urlencodedOutcome.HistoryID)
	if err != nil || urlencodedEntry.Template == nil ||
		len(urlencodedEntry.Template.BodyURLEncoded) != 1 ||
		!urlencodedEntry.Template.BodyURLEncoded[0].Masked {
		t.Fatal("literal urlencoded history template was not masked")
	}
	formDataEntry, err := log.Get(formDataOutcome.HistoryID)
	if err != nil || formDataEntry.Template == nil ||
		len(formDataEntry.Template.BodyFormData) != 1 ||
		!formDataEntry.Template.BodyFormData[0].ValueMasked {
		t.Fatal("literal form-data history template was not masked")
	}
	tmplRaw, _ := json.Marshal(literalEntry.Template)
	assertAbsent(t, sentinel, "literal template JSON", tmplRaw)

	restoreFrame, diffFrame := renderRestoreAndDiffFrames(t, ws, env, coll, bearer, entries, log)
	frames["history restore"] = restoreFrame
	frames["history diff"] = diffFrame
	frames["literal restore"] = renderRestoreLiteralFrame(t, ws, coll, bearer, literalEntry)
	coll.Items = coll.Items[:len(coll.Items)-1]
	if err := persistRestoredRequest(t, st, ws, coll, bearer, literalEntry.Template); err != nil {
		t.Fatal(err)
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

	leakTheme := ui.NewTheme(true, true, false)
	envEditScr := tui.ScreenEnv{
		PaneEnv: tui.PaneEnv{
			Theme: leakTheme,
			Backend: leakEnvHydrator{
				Backend: tui.NewStoreBackend(root, store.WithSecretStore(secrets)),
				secrets: secrets,
			},
		},
		Workspace: ws, ActiveEnv: env.ID,
	}
	maskGlyph := ui.NewTheme(true, true, false).Glyphs.Mask
	for _, w := range []int{80, 140} {
		var envEdit tui.Screen = envedit.New(envEditScr, envedit.Deps{})
		envEdit.SetSize(w, 40)
		envEditKey := func(k tea.KeyPressMsg) {
			next, cmd := envEdit.Update(k)
			if next != nil {
				envEdit = next
			}
			for cmd != nil {
				msg := cmd()
				if msg == nil {
					break
				}
				if batch, ok := msg.(tea.BatchMsg); ok {
					for _, c := range batch {
						if c == nil {
							continue
						}
						_, cmd = envEdit.Update(c())
					}
					continue
				}
				next, cmd = envEdit.Update(msg)
				if next != nil {
					envEdit = next
				}
			}
		}
		envEditKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		envEditKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
		envEditKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		envEditKey(tea.KeyPressMsg{Code: 'r', Text: "r", Mod: tea.ModCtrl})
		envEditKey(tea.KeyPressMsg{Code: tea.KeyEsc})
		frames[fmt.Sprintf("env editor secret normal %d", w)] = envEdit.View()
		envEditKey(tea.KeyPressMsg{Code: 'i', Text: "i"})
		frames[fmt.Sprintf("env editor secret insert %d", w)] = envEdit.View()
		envEditKey(tea.KeyPressMsg{Code: tea.KeyEsc})
		envEditKey(tea.KeyPressMsg{Code: 'v', Text: "v"})
		frames[fmt.Sprintf("env editor secret visual %d", w)] = envEdit.View()
	}
	runScr := tui.ScreenEnv{
		PaneEnv: tui.PaneEnv{
			Theme:   leakTheme,
			Backend: tui.NewStoreBackend(root, store.WithSecretStore(secrets)),
		},
		Workspace: ws, ActiveEnv: env.ID,
		RunScope: &tui.RunScope{Collection: coll},
	}
	var runSc tui.Screen = runner.New(runScr, runner.Deps{})
	runSc.SetSize(140, 40)
	runSc, _ = runSc.Update(tea.KeyPressMsg{Code: tea.KeyTab, Text: "\t"})
	runSc, _ = runSc.Update(tea.KeyPressMsg{Code: tea.KeyTab, Text: "\t"})
	runSc, _ = runSc.Update(tea.KeyPressMsg{Code: tea.KeyTab, Text: "\t"})
	frames["runner setup data path"] = runSc.View()
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
			if strings.HasPrefix(name, "env editor secret") {
				assertMaskGlyphPresent(t, maskGlyph, name+" frame", []byte(frame))
			}
			if strings.HasSuffix(name, "response cookies") {
				// The cookie column clips long values, so also look for the start.
				assertAbsent(t, secret[:16], name+" frame (clipped value)", []byte(frame))
			}
		}
		assertWorkspaceFilesClean(t, root, secret)
		assertMaskGlyphsAbsent(t, root)
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

func renderRestoreAndDiffFrames(
	t *testing.T,
	ws *model.Workspace,
	env *model.Environment,
	coll *model.Collection,
	req *model.Request,
	entries []history.Entry,
	log *history.Log,
) (restore, diff string) {
	t.Helper()
	theme := ui.NewTheme(true, true, false)
	paneEnv := tui.PaneEnv{Theme: theme, Actions: &tui.Registry{}}
	full, err := log.Get(entries[0].ID)
	if err != nil || full.Template == nil {
		t.Fatal("need history entry with template")
	}
	rp := request.New(paneEnv, request.Deps{})
	rp.SetSize(140, 40)
	_, _ = rp.Update(tui.WorkspaceReloadedMsg{Workspace: ws})
	_, _ = rp.Update(tui.EnvironmentChangedMsg{Env: env})
	_, _ = rp.Update(tui.SelectionMsg{Ref: tui.RequestRef{Collection: coll, Request: req, Path: []string{coll.Name, req.Name}}})
	_, _ = rp.Update(tui.ApplyRequestPatchMsg{RequestID: req.ID, Template: *full.Template})
	restore = rp.View()
	if len(entries) < 2 {
		return restore, ""
	}
	left, _ := log.Get(entries[1].ID)
	right := full
	dm := diffview.New(theme,
		diffview.SideSummary{Label: "Marked", Status: "200"},
		diffview.SideSummary{Label: "Current", Status: "200"},
		responseBodyForDiff(left), responseBodyForDiff(right),
	)
	dm.SetSize(140, 40)
	diff = dm.View()
	return restore, diff
}

func responseBodyForDiff(e *history.Entry) string {
	if e == nil || e.Response == nil {
		return ""
	}
	return e.Response.Body
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

func assertMaskGlyphPresent(t *testing.T, glyph, artifact string, raw []byte) {
	t.Helper()
	if glyph == "" || !strings.Contains(string(raw), glyph) {
		t.Errorf("expected mask glyph in %s", artifact)
	}
}

func assertMaskGlyphsAbsent(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		if strings.Contains(path, filepath.Join(".postctl", "history")) {
			return nil
		}
		if strings.Contains(text, history.Mask) || strings.Contains(text, "****") {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("mask glyph persisted in workspace file %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func renderRestoreLiteralFrame(
	t *testing.T,
	ws *model.Workspace,
	coll *model.Collection,
	req *model.Request,
	entry *history.Entry,
) string {
	t.Helper()
	theme := ui.NewTheme(true, true, false)
	paneEnv := tui.PaneEnv{Theme: theme, Actions: &tui.Registry{}}
	rp := request.New(paneEnv, request.Deps{})
	rp.SetSize(140, 40)
	_, _ = rp.Update(tui.WorkspaceReloadedMsg{Workspace: ws})
	_, cmd := rp.Update(tui.SelectionMsg{Ref: tui.RequestRef{Collection: coll, Request: req, Path: []string{coll.Name, req.Name}}})
	if cmd != nil {
		_, _ = rp.Update(cmd())
	}
	_, cmd = rp.Update(tui.ApplyRequestPatchMsg{RequestID: req.ID, Template: *entry.Template})
	if cmd != nil {
		_, _ = rp.Update(cmd())
	}
	return rp.View()
}

func persistRestoredRequest(
	t *testing.T,
	st *store.Store,
	ws *model.Workspace,
	coll *model.Collection,
	req *model.Request,
	tmpl *history.RequestTemplate,
) error {
	t.Helper()
	theme := ui.NewTheme(true, true, false)
	rp := request.New(tui.PaneEnv{Theme: theme}, request.Deps{
		After: func(_ time.Duration, msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } },
	})
	_, _ = rp.Update(tui.WorkspaceReloadedMsg{Workspace: ws})
	_, _ = rp.Update(tui.SelectionMsg{Ref: tui.RequestRef{Collection: coll, Request: req, Path: []string{coll.Name, req.Name}}})
	_, cmd := rp.Update(tui.ApplyRequestPatchMsg{RequestID: req.ID, Template: *tmpl})
	for cmd != nil {
		_, cmd = rp.Update(cmd())
	}
	return st.SaveRequest(context.Background(), coll, req.ID)
}

type sweepBackend struct {
	tui.Backend
	ws *model.Workspace
}

func (b sweepBackend) Load(context.Context) (*model.Workspace, []*store.FileError, error) {
	return b.ws, nil, nil
}

type leakEnvHydrator struct {
	tui.Backend
	secrets store.SecretStore
}

func (leakEnvHydrator) HydrateAuth(context.Context, *model.Auth) error { return nil }

func (h leakEnvHydrator) HydrateVariables(ctx context.Context, vs []model.Variable) error {
	for i := range vs {
		if !vs[i].IsSecret() || vs[i].SecretRef == "" {
			continue
		}
		v, err := h.secrets.Get(ctx, vs[i].SecretRef)
		if err != nil {
			return err
		}
		vs[i].Value = v
	}
	return nil
}
