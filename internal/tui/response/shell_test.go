package response

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/tui/request"
	"github.com/shubham/postctl/internal/tui/tuitest"
)

// shellRig runs the real shell with the real response pane, so a test presses
// the keys a person presses and catches routing regressions that calling the
// pane directly cannot.
type shellRig struct {
	t     *testing.T
	m     tea.Model
	inbox chan tea.Msg
	pane  *Pane
	clip  []string
	quit  bool
	req   *model.Request
}

func newShellRig(t *testing.T) *shellRig {
	t.Helper()
	dir := t.TempDir()
	ws := tuitest.Demo()
	if _, err := tuitest.Write(dir, ws); err != nil {
		t.Fatal(err)
	}
	r := &shellRig{t: t, inbox: make(chan tea.Msg, 1024)}
	for _, req := range ws.Collections[0].Requests() {
		if req.Name == "List charges" {
			r.req = req
		}
	}
	factory := Factory(Deps{Clipboard: func(s string) tea.Cmd { r.clip = append(r.clip, s); return nil }})
	r.m = tui.New(tui.Options{
		Backend:        tui.NewStoreBackend(dir, store.WithSecretStore(store.NewMemorySecrets())),
		NoWatch:        true,
		NewRequestPane: request.Factory(request.Deps{}),
		NewResponsePane: func(env tui.PaneEnv) tui.Pane {
			p := factory(env)
			r.pane = p.(*Pane)
			return p
		},
	})
	r.send(tea.WindowSizeMsg{Width: 140, Height: 40})
	r.run(r.m.Init())
	r.pump(300 * time.Millisecond)
	return r
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

func shellKey(s string) tea.KeyPressMsg {
	parts := strings.Split(s, "+")
	name := parts[len(parts)-1]
	var mod tea.KeyMod
	for _, m := range parts[:len(parts)-1] {
		switch m {
		case "ctrl":
			mod |= tea.ModCtrl
		case "alt":
			mod |= tea.ModAlt
		case "shift":
			mod |= tea.ModShift
		}
	}
	codes := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "down": tea.KeyDown}
	if c, ok := codes[name]; ok {
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

func (r *shellRig) keys(ks ...string) {
	r.t.Helper()
	for _, k := range ks {
		r.send(shellKey(k))
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

// open selects the request in the tree, delivers a response for it, and puts
// the keyboard in the Body tab.
func (r *shellRig) open() {
	r.t.Helper()
	r.keys("/")
	for _, c := range "List charges" {
		if c == ' ' {
			r.keys("space")
		} else {
			r.keys(string(c))
		}
	}
	r.keys("enter", "enter")
	r.send(tui.SendResultMsg{Result: tui.SendResult{
		RequestID: r.req.ID,
		Response: &httpx.Response{
			StatusCode: 200, Status: "OK",
			Headers: []model.KV{{Key: "Content-Type", Value: "application/json"}},
			Body:    []byte(`{"data":[{"id":"ch_1"},{"id":"ch_2"}],"ok":true}`),
		},
	}})
	r.pump(150 * time.Millisecond)
	r.keys("alt+3")
}

func TestShellResponseBodyIsAVimBufferThatHandsKeysToTheShell(t *testing.T) {
	r := newShellRig(t)
	r.open()
	r.mustHave("NORMAL")
	r.mustHave(`"ch_1"`)
	if !r.pane.vimLive() || !r.pane.focused {
		t.Fatalf("the focused Body tab should be a live buffer (live %v, focused %v)", r.pane.vimLive(), r.pane.focused)
	}

	// Motions stay in the buffer: the shell does not see them.
	r.keys("G", "g", "g", "j", "j")
	if row, _ := vimPos(r.pane); row != 2 {
		t.Fatalf("jj: row %d, want 2", row)
	}

	// Space opens the leader (which-key), and its keys act on the shell.
	r.keys("space")
	r.mustHave("Space")
	r.keys("esc")

	// : opens the command line, ? is a backward search in the buffer.
	r.keys(":")
	r.mustHave("Command")
	r.keys("esc", "esc")

	// ] steps to the next tab and [ comes back. Both leave a pending prefix
	// (a repeat key), which Esc closes.
	r.keys("]")
	if r.pane.tab != tabHeaders {
		t.Fatalf("] should step to Headers, tab = %v", r.pane.tab)
	}
	r.keys("[")
	if r.pane.tab != tabBody || !r.pane.vimLive() {
		t.Fatalf("[ should come back to the Body buffer, tab = %v", r.pane.tab)
	}
	r.keys("esc")

	// g then a letter jumps to a section of another pane, and Esc, Tab and the
	// pane-movement keys leave the buffer.
	r.keys("g", "u")
	if r.pane.focused {
		t.Fatal("gu should move focus to the URL bar of the request pane")
	}
	r.keys("alt+3")
	r.keys("esc")
	if r.pane.focused {
		t.Fatal("Esc in normal mode should leave the response pane")
	}
	r.keys("alt+3")
	r.keys("ctrl+h")
	if r.pane.focused {
		t.Fatal("Ctrl-h should move to the pane on the left")
	}
	r.keys("alt+3", "tab")
	if r.pane.focused {
		t.Fatal("Tab should cycle to the next pane")
	}
	if r.quit {
		t.Fatal("none of those keys quits")
	}

	// q reaches the shell's quit.
	r.keys("alt+3", "q")
	if !r.quit {
		t.Fatal("q from the buffer in normal mode should reach the shell")
	}
}

func TestShellResponseBodyKeysAreTextInVisualMode(t *testing.T) {
	r := newShellRig(t)
	r.open()
	r.keys("v")
	r.mustHave("VISUAL")
	r.keys("space", ":", "q", "g", "u")
	if r.quit || !r.pane.focused {
		t.Fatal("in visual mode Space, :, q and g belong to the buffer")
	}
	r.mustHave("VISUAL")
	r.keys("esc")
	r.mustHave("NORMAL")
}

func TestShellResponseYankReachesTheClipboard(t *testing.T) {
	r := newShellRig(t)
	r.open()
	r.keys("y", "y")
	if len(r.clip) != 1 || strings.TrimSpace(r.clip[0]) != "{" {
		t.Fatalf("yy copied %q", r.clip)
	}
	r.mustHave("Copied")
}
