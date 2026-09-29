package request

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
)

// timerMsg is what the fake After returns: nothing runs until the test
// advances the clock.
type timerMsg struct {
	due time.Time
	msg tea.Msg
}

// harness drives a Pane the way the shell does, with a fake clock.
type harness struct {
	t      *testing.T
	p      *Pane
	ws     *model.Workspace
	coll   *model.Collection
	folder *model.Folder
	req    *model.Request
	env    *model.Environment
	now    time.Time
	timers []timerMsg
	out    []tea.Msg // messages the pane sent toward the shell
	clip   []string
	deps   Deps
}

func newRequest() *model.Request {
	r := model.NewRequest("Create charge", "POST", "{{baseUrl}}/v1/charges/:id?limit=10&debug=1")
	r.Headers = []model.KV{
		{Key: "Accept", Value: "application/json"},
		{Key: "X-Trace", Value: "1", Disabled: true},
	}
	r.URL.Query[1].Description = "verbose"
	r.Body = model.Body{Mode: model.BodyRaw, Language: "json", Raw: `{"amount": {{amount}}}`}
	return r
}

func newWorkspace() (*model.Workspace, *model.Collection, *model.Folder, *model.Request, *model.Environment) {
	r := newRequest()
	f := model.NewFolder("Charges")
	f.Items = []model.Item{r}
	c := model.NewCollection("Payments")
	c.Items = []model.Item{f}
	c.Auth = &model.Auth{Type: model.AuthBearer, Bearer: &model.BearerAuth{Token: "{{token}}"}}
	c.Variables = []model.Variable{{Key: "amount", Value: "1200"}}
	env := &model.Environment{ID: model.NewID(), Name: "staging", Variables: []model.Variable{
		{Key: "baseUrl", Value: "https://staging.example.com"},
		{Key: "token", Value: "s3cr3t-value", Type: model.VarSecret},
	}}
	ws := &model.Workspace{ID: model.NewID(), Name: "demo", Collections: []*model.Collection{c},
		Environments: []*model.Environment{env}, Globals: &model.Globals{}}
	return ws, c, f, r, env
}

type option func(*harness)

func newHarness(t *testing.T, opts ...option) *harness {
	t.Helper()
	h := &harness{t: t, now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	h.ws, h.coll, h.folder, h.req, h.env = newWorkspace()
	h.deps = Deps{
		After: func(d time.Duration, msg tea.Msg) tea.Cmd {
			return func() tea.Msg { return timerMsg{due: h.now.Add(d), msg: msg} }
		},
		Now:       func() time.Time { return h.now },
		Clipboard: func(text string) tea.Cmd { h.clip = append(h.clip, text); return nil },
		TempDir:   t.TempDir(),
	}
	for _, o := range opts {
		o(h)
	}
	th := ui.NewTheme(true, false, false)
	h.p = New(tui.PaneEnv{Theme: th, Actions: &tui.Registry{}}, h.deps)
	h.p.SetSize(80, 24)
	h.send(tui.WorkspaceReloadedMsg{Workspace: h.ws})
	h.send(tui.EnvironmentChangedMsg{Env: h.env})
	h.send(tui.SelectionMsg{Ref: h.ref(), Open: true})
	h.p.Focus()
	return h
}

func (h *harness) ref() tui.RequestRef {
	return tui.RequestRef{Collection: h.coll, Request: h.req, Path: []string{"Charges", h.req.Name}}
}

// send delivers a message and collects what its commands produce.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.p.Update(msg)
	h.run(cmd)
}

// run executes a command tree, keeping shell-bound messages in h.out and
// holding timers until the clock advances.
func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(3 * time.Second):
		h.t.Fatalf("command blocked")
	}
	switch m := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range m {
			h.run(c)
		}
	case timerMsg:
		h.timers = append(h.timers, m)
	default:
		h.out = append(h.out, m)
	}
}

// advance moves the fake clock and fires due timers.
func (h *harness) advance(d time.Duration) {
	h.t.Helper()
	h.now = h.now.Add(d)
	for {
		fired := false
		for i, tm := range h.timers {
			if !tm.due.After(h.now) {
				h.timers = append(h.timers[:i], h.timers[i+1:]...)
				h.send(tm.msg)
				fired = true
				break
			}
		}
		if !fired {
			return
		}
	}
}

// keyMsg parses "ctrl+s", "shift+tab", "down" or a single character.
func keyMsg(s string) tea.KeyPressMsg {
	parts := strings.Split(s, "+")
	name := parts[len(parts)-1]
	if s == "+" {
		name, parts = "+", []string{"+"}
	}
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
	codes := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "home": tea.KeyHome, "end": tea.KeyEnd,
		"backspace": tea.KeyBackspace, "delete": tea.KeyDelete, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
	}
	if c, ok := codes[name]; ok {
		return tea.KeyPressMsg(tea.Key{Code: c, Mod: mod})
	}
	if name == "space" {
		return tea.KeyPressMsg(tea.Key{Code: ' ', Text: " ", Mod: mod})
	}
	r := []rune(name)
	if len(r) != 1 {
		panic("keyMsg: unknown key " + s)
	}
	k := tea.Key{Code: r[0], Mod: mod}
	if mod&(tea.ModCtrl|tea.ModAlt) == 0 {
		k.Text = name
	}
	return tea.KeyPressMsg(k)
}

// key presses each named key in turn.
func (h *harness) key(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.send(keyMsg(k))
	}
}

// typ types text a rune at a time.
func (h *harness) typ(s string) {
	h.t.Helper()
	for _, r := range s {
		if r == ' ' {
			h.key("space")
			continue
		}
		h.key(string(r))
	}
}

func (h *harness) paste(s string) { h.t.Helper(); h.send(tea.PasteMsg{Content: s}) }

func (h *harness) view() string { return ansi.Strip(h.p.View()) }

// gotoTab moves to a tab and into its content.
func (h *harness) gotoTab(t tabID) {
	h.t.Helper()
	h.p.tab = t
	h.p.setZone(zTabs)
}

// saved lists the requests the pane asked the shell to save.
func (h *harness) saves() []tui.SaveMsg {
	var out []tui.SaveMsg
	for _, m := range h.out {
		if s, ok := m.(tui.SaveMsg); ok {
			out = append(out, s)
		}
	}
	return out
}

func (h *harness) dirty() (n int, last bool) {
	for _, m := range h.out {
		if d, ok := m.(tui.DirtyMsg); ok {
			n++
			last = d.Dirty
		}
	}
	return
}

func (h *harness) has(sub string) bool { return strings.Contains(h.view(), sub) }

func (h *harness) mustHave(sub string) {
	h.t.Helper()
	if !h.has(sub) {
		h.t.Fatalf("view lacks %q:\n%s", sub, h.view())
	}
}

func (h *harness) mustNotHave(sub string) {
	h.t.Helper()
	if h.has(sub) {
		h.t.Fatalf("view has %q:\n%s", sub, h.view())
	}
}

var _ = fmt.Sprint
var _ = context.Background
