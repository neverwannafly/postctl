package qa_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/engine"
	"github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/envedit"
	tuihistory "github.com/shubham/postctl/internal/tui/history"
	"github.com/shubham/postctl/internal/tui/request"
	"github.com/shubham/postctl/internal/tui/response"
	"github.com/shubham/postctl/internal/tui/runner"
	"github.com/shubham/postctl/internal/tui/tuiengine"
	"github.com/shubham/postctl/internal/tui/wspane"
)

// uxSecrets are the sentinels of the UX sweeps. Each has its unique part
// first, because several views shorten values.
type uxSecrets struct {
	env, folder, coll, global string // secret variables of each scope
	password, wsToken         string // literals typed into secret auth rows
	oauth                     string // the stored OAuth 2.0 access token the engine sends
}

func newUXSecrets(t *testing.T) uxSecrets {
	t.Helper()
	return uxSecrets{
		env: uniqueSentinel(t, "env"), folder: uniqueSentinel(t, "folder"),
		coll: uniqueSentinel(t, "coll"), global: uniqueSentinel(t, "global"),
		password: uniqueSentinel(t, "password"), wsToken: uniqueSentinel(t, "wstoken"),
		oauth: uniqueSentinel(t, "oauth"),
	}
}

func (s uxSecrets) all() []string {
	return []string{s.env, s.folder, s.coll, s.global, s.password, s.wsToken, s.oauth}
}

// uxFixture is a synthetic workspace on disk plus the real engine over it.
type uxFixture struct {
	root    string
	secrets store.SecretStore
	st      *store.Store
	eng     *engine.Engine
	log     *history.Log
	sec     uxSecrets
}

// newUXFixture writes a workspace with secret variables in every scope, an
// "Echo" request that uses all of them, a request with literal credentials in
// its auth rows, and a WebSocket request with a literal token. The server
// echoes secrets in a response header and a cookie but never in the body, so
// whole-shell frames can be searched without the exempt Body tab.
func newUXFixture(t *testing.T, sec uxSecrets) *uxFixture {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Echo known secrets back in a header and an expired cookie (the cookie
		// jar then keeps nothing on disk), never in the body.
		echo := r.Header.Get("X-Folder")
		if strings.HasPrefix(r.URL.Path, "/oauth") {
			echo = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") // a header name the redactor does not treat as sensitive
		}
		w.Header().Set("X-Echo", echo)
		w.Header().Set("Set-Cookie", "session="+strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")+"; Max-Age=0; Path=/")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "items": []int{1, 2, 3}, "nested": map[string]any{"count": 2},
		})
	}))
	t.Cleanup(server.Close)

	root := t.TempDir()
	secrets := store.NewMemorySecrets()
	st, err := store.Init(root, "UX sweep", store.WithSecretStore(secrets))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	echo := model.NewRequest("Echo", http.MethodPost,
		"{{baseUrl}}/echo/{{accessToken}}?token={{accessToken}}&f={{folderToken}}&c={{collToken}}&g={{globalToken}}")
	echo.Headers = []model.KV{
		{Key: "Authorization", Value: "Bearer {{accessToken}}"},
		{Key: "X-Folder", Value: "{{folderToken}}"},
	}
	echo.Body = model.Body{Mode: model.BodyRaw, Language: "json", Raw: `{"t":"{{accessToken}}"}`}
	echo.Scripts.PreRequest = &model.Script{Source: `console.log(pm.environment.get("accessToken"));`}
	echo.Scripts.Test = &model.Script{Source: `pm.test("folder secret must not surface", () => {
	pm.expect("wrong").to.eql(pm.variables.get("folderToken"));
});`}
	folder := model.NewFolder("Folder")
	folder.Variables = []model.Variable{{Key: "folderToken", Value: sec.folder, Type: model.VarSecret}}
	folder.Items = []model.Item{echo}
	literal := model.NewRequest("Literal auth", http.MethodPost, "{{baseUrl}}/literal")
	literal.Auth = &model.Auth{Type: model.AuthBasic, Basic: &model.BasicAuth{Username: "alice", Password: sec.password}}
	// The token is the stored one the engine sends (saving moves it into the
	// secret store); the server echoes it into an expired cookie, so the
	// response headers and cookies carry it unless the redactor knows it.
	oauthReq := model.NewRequest("OAuth", http.MethodGet, "{{baseUrl}}/oauth")
	oauthReq.Auth = &model.Auth{Type: model.AuthOAuth2, OAuth2: &model.OAuth2Config{
		GrantType: model.GrantClientCredentials,
		Token:     &model.OAuth2Token{AccessToken: sec.oauth, TokenType: "Bearer"},
	}}
	socket := model.NewRequest("Socket", http.MethodGet, "wss://example.test/stream")
	socket.Kind = model.KindWebSocket
	socket.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: sec.wsToken}}
	coll := model.NewCollection("UX")
	coll.Variables = []model.Variable{{Key: "collToken", Value: sec.coll, Type: model.VarSecret}}
	coll.Items = []model.Item{folder, literal, oauthReq, socket}
	env := &model.Environment{ID: model.NewID(), Name: "Secret environment", Variables: []model.Variable{
		{Key: "baseUrl", Value: server.URL},
		{Key: "accessToken", Value: sec.env, Type: model.VarSecret},
	}}
	ws.Collections = []*model.Collection{coll}
	ws.Environments = []*model.Environment{env}
	if ws.Globals == nil {
		ws.Globals = &model.Globals{ID: model.NewID()}
	}
	ws.Globals.Variables = []model.Variable{{Key: "globalToken", Value: sec.global, Type: model.VarSecret}}
	if err := st.Save(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	log, err := history.Open(filepath.Join(st.StateDir(), "history"), history.Options{})
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(engine.Config{Store: st, History: log, Version: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	return &uxFixture{root: root, secrets: secrets, st: st, eng: eng, log: log, sec: sec}
}

// uxFrame is one captured screen.
type uxFrame struct{ name, text string }

// uxRig runs the real shell with the real panes and screens over a fixture
// and records every frame and clipboard write.
type uxRig struct {
	t       *testing.T
	fx      *uxFixture
	m       tea.Model
	inbox   chan tea.Msg
	frames  []uxFrame
	clips   []uxFrame // clipboard writes, named by the step that made them
	w       int
	step    string
	tmpDir  string
	editors []uxFrame // what the fake $EDITOR found in its file, named by step
}

func newUXRig(t *testing.T, fx *uxFixture, w, h int) *uxRig {
	t.Helper()
	r := &uxRig{t: t, fx: fx, w: w, inbox: make(chan tea.Msg, 4096), tmpDir: t.TempDir()}
	r.step = "start"
	rdeps := request.Deps{
		TempDir:       r.tmpDir,
		EditorCommand: func(path string) *exec.Cmd { return exec.Command("true", path) },
		Exec: func(cmd *exec.Cmd, done tea.ExecCallback) tea.Cmd {
			return func() tea.Msg {
				// The "editor" only reads the file, the way a real one would.
				r.noteEditorFile(cmd.Args[len(cmd.Args)-1])
				return done(cmd.Run())
			}
		},
	}
	wdeps := wspane.Deps{
		TempDir:       r.tmpDir,
		EditorCommand: rdeps.EditorCommand,
		Exec:          rdeps.Exec,
	}
	opts := tui.Options{
		Backend:        tui.NewStoreBackend(fx.root, store.WithSecretStore(fx.secrets)),
		NoWatch:        true,
		Sender:         tuiengine.New(fx.eng),
		NewRequestPane: request.Factory(rdeps),
		NewResponsePane: response.Factory(response.Deps{
			Log: func() (tui.HistoryReader, error) { return fx.log, nil },
		}),
		NewPaneForKind: map[model.Kind]tui.PaneFactory{model.KindWebSocket: wspane.Factory(wdeps)},
		NewEnvEditor:   envedit.Factory(envedit.Deps{}),
		NewRunner:      runner.Factory(runner.Deps{Runner: tuiengine.NewRunner(fx.eng)}),
		NewHistory: tuihistory.Factory(tuihistory.Deps{
			Log:  func() (tui.HistoryReader, error) { return fx.log, nil },
			Curl: tuihistory.DefaultCurl,
		}),
	}
	r.m = tui.New(opts)
	r.send(tea.WindowSizeMsg{Width: w, Height: h})
	r.run(r.m.Init())
	r.pump(400 * time.Millisecond)
	return r
}

// noteEditorFile records what the temporary file held, as a count and a
// search result only: the rig keeps the text for the sentinel search, never
// for display.
func (r *uxRig) noteEditorFile(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	r.editors = append(r.editors, uxFrame{r.step, string(raw)})
}

func (r *uxRig) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		if msg := cmd(); msg != nil {
			select {
			case r.inbox <- msg:
			default:
			}
		}
	}()
}

// clipboardText returns the text of a clipboard write message.
func clipboardText(msg tea.Msg) (string, bool) {
	rv := reflect.ValueOf(msg)
	if rv.IsValid() && rv.Kind() == reflect.String && strings.HasSuffix(rv.Type().Name(), "ClipboardMsg") {
		return rv.String(), true
	}
	return "", false
}

var cmdType = reflect.TypeOf(tea.Cmd(nil))

// asCmds expands a message that carries commands (batch or sequence).
func asCmds(msg tea.Msg) ([]tea.Cmd, bool) {
	if b, ok := msg.(tea.BatchMsg); ok {
		return b, true
	}
	rv := reflect.ValueOf(msg)
	if rv.IsValid() && rv.Kind() == reflect.Slice && rv.Type().Elem() == cmdType {
		out := make([]tea.Cmd, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			if c, ok := rv.Index(i).Interface().(tea.Cmd); ok {
				out = append(out, c)
			}
		}
		return out, true
	}
	return nil, false
}

func (r *uxRig) send(msg tea.Msg) {
	if cmds, ok := asCmds(msg); ok {
		for _, c := range cmds {
			r.run(c)
		}
		return
	}
	if text, ok := clipboardText(msg); ok {
		r.clips = append(r.clips, uxFrame{r.step, text})
	}
	if _, ok := msg.(tea.QuitMsg); ok {
		return
	}
	var cmd tea.Cmd
	r.m, cmd = r.m.Update(msg)
	r.run(cmd)
}

// pump delivers command results until nothing arrives for a while or d passes.
func (r *uxRig) pump(d time.Duration) {
	deadline := time.After(d)
	for {
		select {
		case m := <-r.inbox:
			r.send(m)
		case <-deadline:
			return
		case <-time.After(30 * time.Millisecond):
			return
		}
	}
}

// wait delivers command results for the whole duration d (timers such as the
// which-key delay need the clock to pass).
func (r *uxRig) wait(d time.Duration) {
	deadline := time.After(d)
	for {
		select {
		case m := <-r.inbox:
			r.send(m)
		case <-deadline:
			return
		}
	}
}

// uxKey builds a key press from "ctrl+s", "enter", "space", "j", "J" ...
func uxKey(s string) tea.KeyPressMsg {
	parts := strings.Split(s, "+")
	name := parts[len(parts)-1]
	var mod tea.KeyMod
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case "ctrl":
			mod |= tea.ModCtrl
		case "alt":
			mod |= tea.ModAlt
		}
	}
	named := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab,
		"down": tea.KeyDown, "up": tea.KeyUp, "left": tea.KeyLeft, "right": tea.KeyRight,
	}
	if c, ok := named[name]; ok {
		return tea.KeyPressMsg(tea.Key{Code: c, Mod: mod})
	}
	if name == "space" {
		return tea.KeyPressMsg(tea.Key{Code: ' ', Text: " ", Mod: mod})
	}
	rs := []rune(name)
	if mod != 0 {
		return tea.KeyPressMsg(tea.Key{Code: rs[0], Mod: mod})
	}
	return tea.KeyPressMsg(tea.Key{Code: rs[0], Text: name})
}

// keys presses keys one at a time, letting commands settle after each.
func (r *uxRig) keys(ks ...string) {
	r.t.Helper()
	for _, k := range ks {
		r.send(uxKey(k))
		r.pump(120 * time.Millisecond)
	}
}

// typ types text a rune at a time.
func (r *uxRig) typ(s string) {
	r.t.Helper()
	for _, c := range s {
		if c == ' ' {
			r.keys("space")
			continue
		}
		r.keys(string(c))
	}
}

// snap records the screen under the current step's name.
func (r *uxRig) snap(name string) string {
	r.t.Helper()
	frame := r.m.View().Content
	r.frames = append(r.frames, uxFrame{r.step + ": " + name, frame})
	return ansi.Strip(frame)
}

// begin names the step that the next captures belong to.
func (r *uxRig) begin(step string) { r.step = step }

// screen is the current screen without styling.
func (r *uxRig) screen() string { return ansi.Strip(r.m.View().Content) }

// open picks a request through the switcher (Space Space), the way a person
// switches requests.
func (r *uxRig) open(name string) {
	r.t.Helper()
	r.keys("esc", "esc") // leave any field, overlay or screen
	r.keys("space", "space")
	r.typ(name)
	r.keys("enter")
	r.pump(200 * time.Millisecond)
}

// waitFor pumps until the screen shows text or the time runs out.
func (r *uxRig) waitFor(text string, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		r.pump(150 * time.Millisecond)
		if strings.Contains(r.screen(), text) {
			return true
		}
	}
	return false
}
