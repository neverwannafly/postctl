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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"authorization": r.Header.Get("Authorization")})
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
	bearer := model.NewRequest("Bearer request", http.MethodGet, "{{baseUrl}}/bearer")
	bearer.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{accessToken}}"}}
	oauth := model.NewRequest("OAuth request", http.MethodGet, "{{baseUrl}}/oauth")
	oauth.Auth = &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
		GrantType: model.GrantClientCredentials,
		Token:     &model.OAuth2Token{AccessToken: sentinel, TokenType: "Bearer"},
	}}
	coll := model.NewCollection("Secrets")
	coll.Items = []model.Item{bearer, oauth}
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
	bearer, oauth = coll.Requests()[0], coll.Requests()[1]

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
	if bearerOutcome.Response == nil || !bytes.Contains(bearerOutcome.Response.Body, []byte(sentinel)) {
		t.Fatal("server did not echo the resolved bearer credential in memory")
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

	redactor := history.NewRedactor([]string{sentinel})
	var jsonReport, junitReport bytes.Buffer
	if err := report.Write(report.NewJSON(&jsonReport, report.Options{Mask: redactor.Text}), events); err != nil {
		t.Fatal(err)
	}
	if err := report.Write(report.NewJUnit(&junitReport, report.Options{Mask: redactor.Text}), events); err != nil {
		t.Fatal(err)
	}

	entries, err := log.List(history.Filter{Limit: 20})
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

	frames := renderLeakSweepFrames(t, ws, env, coll, oauth, bearerOutcome, log)
	assertAbsent(t, sentinel, "JSON report", jsonReport.Bytes())
	assertAbsent(t, sentinel, "JUnit report", junitReport.Bytes())
	assertAbsent(t, sentinel, "masked curl", []byte(curl))
	for name, frame := range frames {
		assertAbsent(t, sentinel, name+" frame", []byte(frame))
	}
	assertWorkspaceFilesClean(t, root, sentinel)
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

	responsePane := response.New(paneEnv)
	responsePane.SetSize(140, 40)
	_, _ = responsePane.Update(tui.SelectionMsg{Ref: tui.RequestRef{
		Collection: coll, Request: coll.Requests()[0],
	}})
	_, _ = responsePane.Update(tui.SendResultMsg{Result: tuiengine.OutcomeToResult(out)})
	_ = responsePane.Focus()
	for range 5 {
		_, _ = responsePane.Update(tea.KeyPressMsg{Code: tea.KeyRight})
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

	return map[string]string{
		"request":  requestPane.View(),
		"response": responsePane.View(),
		"history":  historyScreen.View(),
	}
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
