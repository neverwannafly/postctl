package request

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	hist "github.com/shubham/postctl/internal/history"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/tuitest"
)

// shellRig runs the real shell with the real request pane, so a test presses
// the keys a person presses (Space then a letter, g then a letter) and
// catches routing regressions that calling an action directly cannot.
type shellRig struct {
	t     *testing.T
	m     tea.Model
	inbox chan tea.Msg
	clip  []string
	token string
	quit  bool // the shell asked to quit
	ws    *model.Workspace
}

const shellBearer = "shell-test-bearer-0001"

func newShellRig(t *testing.T, editor string) *shellRig {
	t.Helper()
	dir := t.TempDir()
	ws := tuitest.Demo()
	var target *model.Request
	for _, it := range ws.Collections[0].Items[0].(*model.Folder).Items {
		if r, ok := it.(*model.Request); ok && r.Name == "Create charge" {
			target = r
		}
	}
	target.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: shellBearer}}
	if _, err := tuitest.Write(dir, ws); err != nil {
		t.Fatal(err)
	}
	r := &shellRig{t: t, inbox: make(chan tea.Msg, 1024), token: shellBearer}
	deps := Deps{
		Clipboard: func(text string) tea.Cmd { r.clip = append(r.clip, text); return nil },
		TempDir:   t.TempDir(),
	}
	if editor != "" {
		deps.EditorCommand = func(path string) *exec.Cmd { return exec.Command("sh", "-c", editor, "sh", path) }
		deps.Exec = func(cmd *exec.Cmd, done tea.ExecCallback) tea.Cmd {
			return func() tea.Msg { return done(cmd.Run()) }
		}
	}
	r.m = tui.New(tui.Options{
		Backend:        tui.NewStoreBackend(dir, store.WithSecretStore(store.NewMemorySecrets())),
		NoWatch:        true,
		NewRequestPane: Factory(deps),
	})
	r.ws = ws
	r.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	r.run(r.m.Init())
	r.pump(300 * time.Millisecond)
	r.open("Create charge")
	return r
}

func (r *shellRig) requestByName(name string) *model.Request {
	for _, req := range r.ws.Collections[0].Requests() {
		if req.Name == name {
			return req
		}
	}
	return nil
}

func (r *shellRig) run(cmd tea.Cmd) {
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

func (r *shellRig) send(msg tea.Msg) {
	switch m := msg.(type) {
	case tea.BatchMsg:
		for _, c := range m {
			r.run(c)
		}
		return
	case tea.QuitMsg:
		r.quit = true
		return
	}
	var cmd tea.Cmd
	r.m, cmd = r.m.Update(msg)
	r.run(cmd)
}

func (r *shellRig) pump(d time.Duration) {
	deadline := time.After(d)
	for {
		select {
		case m := <-r.inbox:
			r.send(m)
		case <-deadline:
			return
		case <-time.After(60 * time.Millisecond):
			return
		}
	}
}

func (r *shellRig) keys(ks ...string) {
	r.t.Helper()
	for _, k := range ks {
		r.send(keyMsg(k))
		r.pump(150 * time.Millisecond)
	}
}

func (r *shellRig) screen() string { return ansi.Strip(r.m.View().Content) }

func (r *shellRig) mustHave(s string) {
	r.t.Helper()
	if !strings.Contains(r.screen(), s) {
		r.t.Fatalf("screen does not show %q:\n%s", s, r.screen())
	}
}

// open selects a request in the tree with the filter and opens it.
func (r *shellRig) open(name string) {
	r.t.Helper()
	r.keys("/")
	for _, c := range name {
		if c == ' ' {
			r.keys("space")
		} else {
			r.keys(string(c))
		}
	}
	r.keys("enter", "enter")
	r.mustHave("POST")
}

func TestSpaceYAndSpaceYCopyCurl(t *testing.T) {
	r := newShellRig(t, "")
	r.keys("2", "space", "y")
	if len(r.clip) != 1 || !strings.HasPrefix(r.clip[0], "curl") {
		t.Fatalf("Space y copied %d items", len(r.clip))
	}
	if strings.Contains(r.clip[0], r.token) {
		t.Fatal("Space y must copy the masked curl")
	}
	r.keys("space", "Y")
	if len(r.clip) != 2 || !strings.Contains(r.clip[1], r.token) {
		t.Fatalf("Space Y copied %d items; the revealed curl must carry the credential", len(r.clip))
	}
}

func TestSpaceVRevealsSecretsOnTheAuthTab(t *testing.T) {
	r := newShellRig(t, "")
	r.keys("g", "a")
	r.mustHave("Bearer Token")
	if strings.Contains(r.screen(), r.token) {
		t.Fatal("the credential must be masked before Space v")
	}
	r.keys("space", "v")
	r.mustHave(r.token)
}

func TestSpaceOOpensTheEditor(t *testing.T) {
	r := newShellRig(t, `printf '{"edited": "in the shell"}\n' > "$1"`)
	r.keys("g", "b")
	r.keys("space", "o")
	r.mustHave("Updated from the editor")
}

func TestShellPendingRestoreAppliesWhenRequestOpens(t *testing.T) {
	r := newShellRig(t, "")
	create := r.requestByName("Create charge")
	if create == nil {
		t.Fatal("demo request missing")
	}
	r.open("List charges")
	r.send(tui.ApplyRequestPatchMsg{
		RequestID: create.ID,
		Template:  hist.RequestTemplate{Method: "TRACE", URL: create.URL.Raw},
	})
	r.pump(400 * time.Millisecond)
	r.open("Create charge")
	r.pump(400 * time.Millisecond)
	if !strings.Contains(r.screen(), "TRACE") {
		t.Fatal("restore should apply when the target request opens")
	}
}

func TestShellPendingRestoreDropsOnFirstDifferentBinding(t *testing.T) {
	r := newShellRig(t, "")
	create := r.requestByName("Create charge")
	if create == nil {
		t.Fatal("demo request missing")
	}
	r.send(tui.SelectionMsg{})
	r.send(tui.ApplyRequestPatchMsg{
		RequestID: create.ID,
		Template:  hist.RequestTemplate{Method: "TRACE", URL: create.URL.Raw},
	})
	r.open("List charges")
	r.open("Create charge")
	if create.Method == "TRACE" {
		t.Fatal("stale restore applied after first binding selected another request")
	}
}

func TestSpaceIInsertsASnippet(t *testing.T) {
	r := newShellRig(t, "")
	r.keys("g", "t")
	r.keys("esc") // the script editor starts in normal mode; Esc leaves it
	r.keys("space", "i")
	r.mustHave("Insert a snippet")
	r.keys("enter")
	if strings.Contains(r.screen(), "Insert a snippet") {
		t.Fatal("Enter inserts the snippet and closes the picker")
	}
	r.mustHave("Status code is 200")
}

// ---- keys from a field in normal mode ---------------------------------------------------

func TestShellKeysWorkFromAFieldInNormalMode(t *testing.T) {
	r := newShellRig(t, "")
	r.keys("g", "u") // the URL field, in normal mode
	r.mustHave("NORMAL")
	r.keys("space", "?")
	r.mustHave("Everywhere") // the help overlay
	r.keys("esc")
	r.keys(":")
	r.mustHave("Command")
	r.keys("esc", "esc") // the command line is modal: insert to normal, then close
	r.keys("g", "b") // the Body tab
	r.mustHave("Body")
	if r.quit {
		t.Fatal("none of those keys quits")
	}
	r.keys("g", "u", "q")
	if !r.quit {
		t.Fatal("q from a field in normal mode should reach the shell's quit")
	}
}

func TestShellKeysAreTextInInsertMode(t *testing.T) {
	r := newShellRig(t, "")
	r.keys("g", "u", "A")
	r.mustHave("INSERT")
	r.keys("space", ":", "q", "g", "b")
	r.mustHave(" :qgb")
	if r.quit {
		t.Fatal("q in insert mode is text")
	}
}
