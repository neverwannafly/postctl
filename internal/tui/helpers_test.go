package tui

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
	"github.com/shubham/postctl/internal/tui/tuitest"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fakeBackend is an in-memory Backend that records every write.
type fakeBackend struct {
	mu       sync.Mutex
	root     string
	ws       *model.Workspace // what Load returns (deep-copied)
	problems []*store.FileError
	loadErr  error
	inited   bool
	state    store.State
	saved    []model.ID // collection IDs, in order of SaveCollection calls
	last     map[model.ID]*model.Collection
	deleted  []model.ID
	saveErr  error
	loads    int
	gate     chan struct{} // when set, SaveCollection blocks until it is closed

	// Recorded by the WS6 write paths (see save_test.go).
	envSaved     []*model.Environment
	envDeleted   []model.ID
	globalsSaved []*model.Globals
	reqSaved     []model.ID
	envGate      chan struct{} // when set, SaveEnvironment blocks until it is closed
}

func newFake(ws *model.Workspace) *fakeBackend {
	return &fakeBackend{root: "/tmp/postctl-test/payments-api", ws: ws, last: map[model.ID]*model.Collection{}}
}

func (f *fakeBackend) Root() string { return f.root }

func (f *fakeBackend) Load(context.Context) (*model.Workspace, []*store.FileError, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads++
	if f.loadErr != nil {
		return nil, nil, f.loadErr
	}
	if f.ws == nil {
		return nil, nil, store.ErrNotWorkspace
	}
	return deepCopy(f.ws), f.problems, nil
}

func (f *fakeBackend) Init(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inited = true
	f.ws = &model.Workspace{ID: model.NewID(), Name: name}
	return nil
}

func (f *fakeBackend) SaveCollection(_ context.Context, c *model.Collection) error {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, c.ID)
	f.last[c.ID] = deepCopy(c)
	// Mirror the store: the saved state is what the next Load returns.
	if f.ws != nil {
		found := false
		for i, x := range f.ws.Collections {
			if x.ID == c.ID {
				f.ws.Collections[i] = deepCopy(c)
				found = true
			}
		}
		if !found {
			f.ws.Collections = append(f.ws.Collections, deepCopy(c))
		}
	}
	return nil
}

func (f *fakeBackend) DeleteCollection(_ context.Context, id model.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	if f.ws != nil {
		out := f.ws.Collections[:0:0]
		for _, c := range f.ws.Collections {
			if c.ID != id {
				out = append(out, c)
			}
		}
		f.ws.Collections = out
	}
	return nil
}

func (f *fakeBackend) SaveRequest(_ context.Context, _ *model.Collection, id model.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqSaved = append(f.reqSaved, id)
	return f.saveErr
}

func (f *fakeBackend) SaveEnvironment(_ context.Context, e *model.Environment) error {
	if f.envGate != nil {
		<-f.envGate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.envSaved = append(f.envSaved, e.Clone())
	return nil
}

func (f *fakeBackend) DeleteEnvironment(_ context.Context, id model.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.envDeleted = append(f.envDeleted, id)
	return f.saveErr
}

func (f *fakeBackend) SaveGlobals(_ context.Context, g *model.Globals) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.globalsSaved = append(f.globalsSaved, g.Clone())
	return f.saveErr
}

func (f *fakeBackend) LoadState() (store.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}

func (f *fakeBackend) SaveState(st store.State) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = st
	return nil
}

func (f *fakeBackend) savedState() store.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

// harness drives an app like the Bubble Tea runtime would, but synchronously.
type harness struct {
	t    testing.TB
	a    *app
	be   *fakeBackend
	quit bool
}

type harnessOpt func(*Options)

// newHarness builds an app over a fake backend, sizes it, loads the workspace
// and settles all commands.
func newHarness(t testing.TB, ws *model.Workspace, w, h int, opts ...harnessOpt) *harness {
	t.Helper()
	be := newFake(ws)
	o := Options{Backend: be, NoWatch: true}
	for _, f := range opts {
		f(&o)
	}
	hs := &harness{t: t, a: newApp(o), be: be}
	hs.msg(tea.WindowSizeMsg{Width: w, Height: h})
	hs.run(hs.a.Init())
	return hs
}

// run executes cmd and feeds every resulting message back, recursively.
// Commands that do not finish quickly (timers, blocking subscriptions) are
// abandoned, as in a test nothing waits for them.
func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(40 * time.Millisecond):
			continue
		}
		switch m := msg.(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, m...)
		case tea.QuitMsg:
			h.quit = true
		default:
			if isSequence(msg) {
				continue
			}
			_, next := h.a.Update(msg)
			queue = append(queue, next)
		}
	}
}

// isSequence skips tea.Sequence messages, which carry commands we cannot
// introspect; the shell does not use them.
func isSequence(msg tea.Msg) bool { return strings.Contains(fmt.Sprintf("%T", msg), "sequenceMsg") }

// msg delivers one message and settles its commands.
func (h *harness) msg(m tea.Msg) {
	h.t.Helper()
	_, cmd := h.a.Update(m)
	h.run(cmd)
}

// keys presses each named key in turn.
func (h *harness) keys(names ...string) {
	h.t.Helper()
	for _, n := range names {
		h.msg(keyPress(n))
	}
}

// overlayDismiss closes a vim overlay field (insert → normal → close).
func (h *harness) overlayDismiss() {
	h.t.Helper()
	h.keys("esc", "esc")
}

// treeFilterDismiss exits the tree filter and clears a kept query.
func (h *harness) treeFilterDismiss() {
	h.t.Helper()
	h.keys("esc", "esc", "esc")
}

// typeText types s one rune at a time.
func (h *harness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		h.msg(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// screen is the plain-text view.
func (h *harness) screen() string {
	return ansi.Strip(h.a.View().Content)
}

// rawScreen keeps escape sequences.
func (h *harness) rawScreen() string { return h.a.View().Content }

// has reports whether the screen contains s.
func (h *harness) has(s string) bool { return strings.Contains(h.screen(), s) }

func (h *harness) mustHave(s string) {
	h.t.Helper()
	if !h.has(s) {
		h.t.Fatalf("screen does not contain %q:\n%s", s, h.screen())
	}
}

func (h *harness) mustNotHave(s string) {
	h.t.Helper()
	if h.has(s) {
		h.t.Fatalf("screen unexpectedly contains %q:\n%s", s, h.screen())
	}
}

// selectByName moves the tree cursor onto the first row with the given label.
func (h *harness) selectByName(name string) {
	h.t.Helper()
	for i, r := range h.a.tree.rows {
		if r.label == name {
			h.a.tree.cursor = i
			h.a.tree.commit()
			h.run(h.a.syncSelection(false))
			return
		}
	}
	h.t.Fatalf("no tree row %q in:\n%s", name, h.screen())
}

// keyPress builds a key press from a name like "ctrl+p", "shift+tab", "esc",
// "down", "E" or "alt+1".
func keyPress(name string) tea.KeyPressMsg {
	var k tea.Key
	rest := name
	for {
		switch {
		case strings.HasPrefix(rest, "ctrl+") && len(rest) > 5:
			k.Mod |= tea.ModCtrl
			rest = rest[5:]
			continue
		case strings.HasPrefix(rest, "alt+") && len(rest) > 4:
			k.Mod |= tea.ModAlt
			rest = rest[4:]
			continue
		case strings.HasPrefix(rest, "shift+") && len(rest) > 6:
			k.Mod |= tea.ModShift
			rest = rest[6:]
			continue
		}
		break
	}
	named := map[string]rune{
		"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "home": tea.KeyHome, "end": tea.KeyEnd,
		"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "backspace": tea.KeyBackspace, "space": tea.KeySpace,
	}
	if code, ok := named[rest]; ok {
		k.Code = code
		if rest == "space" {
			k.Text = " "
		}
		return tea.KeyPressMsg(k)
	}
	r := []rune(rest)
	k.Code = r[0]
	if k.Mod == 0 || k.Mod == tea.ModShift {
		k.Text = rest
	}
	return tea.KeyPressMsg(k)
}

func TestKeyPressHelper(t *testing.T) {
	for _, n := range []string{"ctrl+p", "shift+tab", "esc", "down", "E", "alt+1", "space", "ctrl+enter", "n", "?"} {
		if got := keyPress(n).String(); got != n {
			t.Errorf("keyPress(%q).String() = %q", n, got)
		}
	}
}

// golden compares got with testdata/<name>.golden, or rewrites it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run: go test ./internal/tui -update): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from golden.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func demo() *model.Workspace { return tuitest.Demo() }

var errBoom = errors.New("disk on fire")

// brokenFile builds a load problem for a file under root.
func brokenFile(root, rel, reason string) *store.FileError {
	return &store.FileError{Path: filepath.Join(root, rel), Err: errors.New(reason)}
}

func largeWorkspace() *model.Workspace { return tuitest.Large(1000) }

func sizeMsg(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }

// newHarnessWith is newHarness over a prepared fake backend.
func newHarnessWith(t testing.TB, be *fakeBackend, w, h int, opts ...harnessOpt) *harness {
	t.Helper()
	o := Options{Backend: be, NoWatch: true}
	for _, f := range opts {
		f(&o)
	}
	hs := &harness{t: t, a: newApp(o), be: be}
	hs.msg(tea.WindowSizeMsg{Width: w, Height: h})
	hs.run(hs.a.Init())
	return hs
}

func waitMs(n int) { time.Sleep(time.Duration(n) * time.Millisecond) }
