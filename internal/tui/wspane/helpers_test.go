package wspane

import (
	"context"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
	"github.com/shubham/postctl/internal/wsx"
)

// ---- fake connection ---------------------------------------------------------------

// sentFrame is one message the pane sent.
type sentFrame struct {
	typ  wsx.MessageType
	data []byte
}

// fakeClient is a connection the test drives: push queues events for the pane
// and everything the pane sends or asks is recorded.
type fakeClient struct {
	mu      sync.Mutex
	queue   []wsx.Event
	wake    chan struct{}
	done    chan struct{}
	closed  bool
	seq     uint64
	now     func() time.Time
	sent    []sentFrame
	pings   int
	closes  []int
	aborted bool
	sendErr error
	// echo makes Send queue the message back as a received event.
	echo bool
}

func newFakeClient(now func() time.Time) *fakeClient {
	return &fakeClient{wake: make(chan struct{}, 1), done: make(chan struct{}), now: now}
}

func (c *fakeClient) push(ev wsx.Event) {
	c.mu.Lock()
	c.seq++
	ev.Seq = c.seq
	ev.Time = c.now()
	c.queue = append(c.queue, ev)
	if ev.Type == wsx.EventClose && !c.closed {
		c.closed = true
		close(c.done)
	}
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *fakeClient) pushIn(text string) {
	c.push(wsx.Event{Type: wsx.EventMessage, Dir: wsx.DirIn, Msg: wsx.Text, Data: []byte(text), Size: len(text)})
}

func (c *fakeClient) pushBinary(b []byte) {
	c.push(wsx.Event{Type: wsx.EventMessage, Dir: wsx.DirIn, Msg: wsx.Binary, Data: b, Size: len(b)})
}

func (c *fakeClient) pushClose(code int, reason string) {
	c.push(wsx.Event{Type: wsx.EventClose, Code: code, Reason: reason})
}

func (c *fakeClient) Next(ctx context.Context) (wsx.Event, error) {
	for {
		c.mu.Lock()
		if len(c.queue) > 0 {
			ev := c.queue[0]
			c.queue = c.queue[1:]
			c.mu.Unlock()
			return ev, nil
		}
		closed := c.closed
		c.mu.Unlock()
		if closed {
			return wsx.Event{}, io.EOF
		}
		select {
		case <-c.wake:
		case <-ctx.Done():
			return wsx.Event{}, ctx.Err()
		}
	}
}

func (c *fakeClient) Drain(n int) []wsx.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := min(n, len(c.queue))
	if k <= 0 {
		return nil
	}
	out := append([]wsx.Event(nil), c.queue[:k]...)
	c.queue = c.queue[k:]
	return out
}

func (c *fakeClient) Send(_ context.Context, typ wsx.MessageType, data []byte, _ ...wsx.SendOption) error {
	c.mu.Lock()
	if c.sendErr != nil {
		err := c.sendErr
		c.mu.Unlock()
		return err
	}
	c.sent = append(c.sent, sentFrame{typ, append([]byte(nil), data...)})
	echo := c.echo
	c.mu.Unlock()
	c.push(wsx.Event{Type: wsx.EventMessage, Dir: wsx.DirOut, Msg: typ, Data: data, Size: len(data)})
	if echo {
		c.push(wsx.Event{Type: wsx.EventMessage, Dir: wsx.DirIn, Msg: typ, Data: data, Size: len(data)})
	}
	return nil
}

func (c *fakeClient) Ping(context.Context) (time.Duration, error) {
	c.mu.Lock()
	c.pings++
	c.mu.Unlock()
	c.push(wsx.Event{Type: wsx.EventPing, Dir: wsx.DirOut})
	c.push(wsx.Event{Type: wsx.EventPong, Dir: wsx.DirIn, Latency: 12 * time.Millisecond})
	return 12 * time.Millisecond, nil
}

func (c *fakeClient) Close(code int, reason string) error {
	c.mu.Lock()
	c.closes = append(c.closes, code)
	closed := c.closed
	c.mu.Unlock()
	if !closed {
		c.pushClose(code, reason)
	}
	return nil
}

func (c *fakeClient) Abort() {
	c.mu.Lock()
	c.aborted = true
	c.mu.Unlock()
}

func (c *fakeClient) sentTexts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, f := range c.sent {
		out = append(out, string(f.data))
	}
	return out
}

func (c *fakeClient) wasAborted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.aborted
}

func (c *fakeClient) closeCodes() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.closes...)
}

// fakeSession resolves nothing: frames are built from the message as saved.
type fakeSession struct {
	h        *harness
	client   *fakeClient
	dialErr  error
	gate     chan struct{} // Dial waits for it to close when set
	warnings []string
}

func (s *fakeSession) Dial(ctx context.Context) (Client, error) {
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.dialErr != nil {
		return nil, s.dialErr
	}
	s.client.push(wsx.Event{Type: wsx.EventOpen, Info: &wsx.Info{URL: "wss://example.test/live", Status: 101}})
	return s.client, nil
}

func (s *fakeSession) Frame(m model.WSMessage) (wsx.Frame, error) {
	body := m.Body
	if s.h != nil {
		body = strings.ReplaceAll(body, "{{token}}", "resolved-token")
	}
	return wsx.BuildFrame(m.Format, body)
}

func (s *fakeSession) Warnings() []string { return s.warnings }
func (s *fakeSession) Target() string     { return "wss://example.test/live" }

// ---- harness -----------------------------------------------------------------------

// timerMsg is what the fake After returns: nothing runs until the test
// advances the clock.
type timerMsg struct {
	due time.Time
	msg tea.Msg
}

// harness drives a Pane the way the shell does, with a fake clock and fake
// connections.
type harness struct {
	t      *testing.T
	p      *Pane
	ws     *model.Workspace
	coll   *model.Collection
	folder *model.Folder
	req    *model.Request
	env    *model.Environment
	now    time.Time
	clock  sync.Mutex
	timers []timerMsg
	out    []tea.Msg // messages the pane sent toward the shell
	clip   []string
	deps   Deps
	be     tui.Backend
	theme  ui.Theme
	w, h   int

	// Connection scripting.
	client     *fakeClient
	sess       *fakeSession
	dialErr    error
	prepareErr error
	gate       chan struct{}
	inputs     []wsx.PrepareInput
	real       bool // use the real Prepare and wsx client

	inbox   chan tea.Msg
	running atomic.Int32
}

func newRequest() *model.Request {
	r := model.NewWebSocketRequest("Live prices", "{{baseUrl}}/stream?channel=btc")
	r.Headers = []model.KV{
		{Key: "X-Client", Value: "postctl"},
		{Key: "X-Debug", Value: "1", Disabled: true},
	}
	r.WebSocket = &model.WebSocketSettings{
		Messages: []model.WSMessage{
			{ID: "m1", Name: "subscribe", Body: `{"type":"subscribe","channel":"btc"}`, Format: model.WSJSON},
			{ID: "m2", Name: "heartbeat", Body: "ping"},
			{ID: "m3", Name: "auth", Body: `{"type":"auth","token":"{{token}}"}`, Format: model.WSJSON},
		},
	}
	return r
}

func newWorkspace() (*model.Workspace, *model.Collection, *model.Folder, *model.Request, *model.Environment) {
	r := newRequest()
	f := model.NewFolder("Streams")
	f.Items = []model.Item{r}
	c := model.NewCollection("Trading")
	c.Items = []model.Item{f}
	env := &model.Environment{ID: model.NewID(), Name: "staging", Variables: []model.Variable{
		{Key: "baseUrl", Value: "wss://staging.example.test"},
		{Key: "token", Value: "s3cr3t-value", Type: model.VarSecret},
	}}
	ws := &model.Workspace{ID: model.NewID(), Name: "demo", Collections: []*model.Collection{c},
		Environments: []*model.Environment{env}, Globals: &model.Globals{}}
	return ws, c, f, r, env
}

type option func(*harness)

func newHarness(t *testing.T, opts ...option) *harness {
	t.Helper()
	h := &harness{t: t, now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), inbox: make(chan tea.Msg, 4096)}
	h.ws, h.coll, h.folder, h.req, h.env = newWorkspace()
	h.deps = Deps{
		After: func(d time.Duration, msg tea.Msg) tea.Cmd {
			return func() tea.Msg { return timerMsg{due: h.tnow().Add(d), msg: msg} }
		},
		Now:       h.tnow,
		Clipboard: func(text string) tea.Cmd { h.clip = append(h.clip, text); return nil },
	}
	for _, o := range opts {
		o(h)
	}
	if h.deps.Prepare == nil && !h.real {
		h.deps.Prepare = h.prepare
	}
	if h.w == 0 {
		h.w, h.h = 80, 24
	}
	if h.theme.Glyphs.Select == "" {
		h.theme = ui.NewTheme(true, false, false)
	}
	h.p = New(tui.PaneEnv{Theme: h.theme, Actions: &tui.Registry{}, Backend: h.be}, h.deps)
	h.p.SetSize(h.w, h.h)
	h.send(tui.WorkspaceReloadedMsg{Workspace: h.ws})
	h.send(tui.EnvironmentChangedMsg{Env: h.env})
	h.send(tui.SelectionMsg{Ref: h.ref(), Open: true})
	h.p.Focus()
	return h
}

// prepare is the fake Deps.Prepare: it records the input and hands out the
// scripted session.
func (h *harness) prepare(_ context.Context, in wsx.PrepareInput) (Session, error) {
	h.inputs = append(h.inputs, in)
	if h.prepareErr != nil {
		return nil, h.prepareErr
	}
	h.client = newFakeClient(h.tnow)
	h.sess = &fakeSession{h: h, client: h.client, dialErr: h.dialErr, gate: h.gate}
	return h.sess, nil
}

func (h *harness) tnow() time.Time {
	h.clock.Lock()
	defer h.clock.Unlock()
	return h.now
}

func (h *harness) ref() tui.RequestRef {
	return tui.RequestRef{Collection: h.coll, Request: h.req, Path: []string{"Streams", h.req.Name}}
}

// send delivers a message and collects what its commands produce.
func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.p.Update(msg)
	h.run(cmd)
	h.settle()
}

// run starts a command in the background. A command that has not finished
// after a moment (the event pump waiting for the server) is parked; its
// message is delivered whenever it arrives.
func (h *harness) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	h.running.Add(1)
	go func() {
		res := make(chan tea.Msg, 1)
		go func() { res <- cmd() }()
		select {
		case m := <-res:
			h.inbox <- m
			h.running.Add(-1)
		case <-time.After(5 * time.Millisecond):
			h.running.Add(-1)
			go func() { h.inbox <- <-res }()
		}
	}()
}

// settle processes messages until nothing is running and nothing is waiting.
func (h *harness) settle() {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	quiet := 0
	for {
		select {
		case m := <-h.inbox:
			quiet = 0
			h.process(m)
			continue
		default:
		}
		if h.running.Load() == 0 {
			quiet++
			if quiet > 3 {
				return
			}
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("commands did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func (h *harness) process(msg tea.Msg) {
	switch m := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range m {
			h.run(c)
		}
	case timerMsg:
		h.timers = append(h.timers, m)
	default:
		// The pane's own messages come straight back to it, as the runtime
		// would deliver them.
		if reflect.TypeOf(m).PkgPath() == reflect.TypeOf(harness{}).PkgPath() {
			_, cmd := h.p.Update(m)
			h.run(cmd)
			return
		}
		h.out = append(h.out, m)
	}
}

// advance moves the fake clock and fires due timers.
func (h *harness) advance(d time.Duration) {
	h.t.Helper()
	h.clock.Lock()
	h.now = h.now.Add(d)
	h.clock.Unlock()
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
		switch r {
		case ' ':
			h.key("space")
		case '\n':
			h.key("enter")
		default:
			h.key(string(r))
		}
	}
}

func (h *harness) paste(s string) { h.t.Helper(); h.send(tea.PasteMsg{Content: s}) }

func (h *harness) view() string { return ansi.Strip(h.p.View()) }

func (h *harness) has(sub string) bool { return strings.Contains(h.view(), sub) }

func (h *harness) mustHave(sub string) {
	h.t.Helper()
	if !h.has(sub) {
		h.t.Fatalf("view lacks %q:\n%s", sub, h.view())
	}
}

// toasts lists the shell toasts the pane asked for, in order. Action results
// are shown there only; the pane draws no copy of its own.
func (h *harness) toasts() []string {
	var out []string
	for _, m := range h.out {
		if t, ok := m.(tui.ToastMsg); ok {
			out = append(out, t.Text)
		}
	}
	return out
}

// mustToast fails unless some toast contains sub.
func (h *harness) mustToast(sub string) {
	h.t.Helper()
	for _, t := range h.toasts() {
		if strings.Contains(t, sub) {
			return
		}
	}
	h.t.Fatalf("no toast contains %q (toasts: %q)", sub, h.toasts())
}

func (h *harness) mustNotHave(sub string) {
	h.t.Helper()
	if h.has(sub) {
		h.t.Fatalf("view has %q:\n%s", sub, h.view())
	}
}

// gotoTab moves to a tab's bar position.
func (h *harness) gotoTab(t tabID) {
	h.t.Helper()
	h.p.tab = t
	h.p.setZone(zTabs)
}

// connect connects and waits for the open state.
func (h *harness) connect() *fakeClient {
	h.t.Helper()
	h.key("ctrl+s")
	h.waitState(stOpen)
	return h.client
}

// waitState settles until the current request reaches state st.
func (h *harness) waitState(st connState) {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for h.p.state() != st {
		if time.Now().After(deadline) {
			h.t.Fatalf("state is %d, want %d\n%s", h.p.state(), st, h.view())
		}
		h.settle()
	}
}

// waitLog settles until the log has n entries.
func (h *harness) waitLog(n int) {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		l := h.p.peek()
		if l != nil && len(l.log.entries) >= n {
			return
		}
		if time.Now().After(deadline) {
			got := 0
			if l != nil {
				got = len(l.log.entries)
			}
			h.t.Fatalf("log has %d entries, want %d\n%s", got, n, h.view())
		}
		h.settle()
	}
}

func (h *harness) saves() []tui.SaveMsg {
	var out []tui.SaveMsg
	for _, m := range h.out {
		if s, ok := m.(tui.SaveMsg); ok {
			out = append(out, s)
		}
	}
	return out
}

func (h *harness) logTexts() []string {
	l := h.p.peek()
	if l == nil {
		return nil
	}
	var out []string
	for i := range l.log.entries {
		out = append(out, l.log.entries[i].summary())
	}
	return out
}
