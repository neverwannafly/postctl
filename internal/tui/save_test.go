package tui

import (
	"errors"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// msgPane records the messages it is given, for asserting what the shell
// forwards to panes.
type msgPane struct {
	mu   sync.Mutex
	msgs []tea.Msg
	// blur is returned by BlurCmd.
	blur  tea.Cmd
	blurs int
}

func (p *msgPane) Update(msg tea.Msg) (Pane, tea.Cmd) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append(p.msgs, msg)
	return p, nil
}
func (p *msgPane) View() string        { return "" }
func (p *msgPane) SetSize(int, int)    {}
func (p *msgPane) Focus() tea.Cmd      { return nil }
func (p *msgPane) Blur()               {}
func (p *msgPane) KeyHints() []ui.Hint { return nil }
func (p *msgPane) BlurCmd() tea.Cmd {
	p.blurs++
	return p.blur
}

func (p *msgPane) saveDone() []SaveDoneMsg {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []SaveDoneMsg
	for _, m := range p.msgs {
		if d, ok := m.(SaveDoneMsg); ok {
			out = append(out, d)
		}
	}
	return out
}

func saveFixture(t *testing.T, ws *model.Workspace) (*harness, *msgPane) {
	t.Helper()
	pane := &msgPane{}
	h := newHarness(t, ws, 120, 30, func(o *Options) {
		o.NewRequestPane = func(PaneEnv) Pane { return pane }
	})
	return h, pane
}

func TestBackendsSupportTheOptionalWritePaths(t *testing.T) {
	var _ EnvironmentDeleter = (*storeBackend)(nil)
	var _ GlobalsSaver = (*storeBackend)(nil)
	var _ Hydrator = (*storeBackend)(nil)
}

func TestSaveMsgEnvironmentWritesASnapshotAndReportsDone(t *testing.T) {
	h, pane := saveFixture(t, demo())
	env := h.a.ws.Environments[0]
	env.Variables = append(env.Variables, model.Variable{Key: "added", Value: "1"})
	settle(t, h, func() tea.Msg { return SaveMsg{Kind: SaveEnvironment, ID: env.ID} })

	if len(h.be.envSaved) != 1 || h.be.envSaved[0].ID != env.ID {
		t.Fatalf("saved = %+v", h.be.envSaved)
	}
	last := h.be.envSaved[0].Variables
	if last[len(last)-1].Key != "added" {
		t.Fatal("snapshot lacks the edit")
	}
	done := pane.saveDone()
	if len(done) != 1 || done[0].Kind != SaveEnvironment || done[0].ID != env.ID || done[0].Err != nil {
		t.Fatalf("done = %+v", done)
	}
}

func TestSaveSnapshotIsTakenWhenAskedNotWhenWritten(t *testing.T) {
	h, _ := saveFixture(t, demo())
	h.be.envGate = make(chan struct{})
	env := h.a.ws.Environments[0]
	env.Name = "first"
	_, cmd := h.a.Update(SaveMsg{Kind: SaveEnvironment, ID: env.ID})
	// The write is now in flight (blocked on the gate). Later edits must not
	// leak into it.
	env.Name = "second"
	go func() { close(h.be.envGate) }()
	settle(t, h, cmd)
	if len(h.be.envSaved) != 1 || h.be.envSaved[0].Name != "first" {
		t.Fatalf("saved = %+v", h.be.envSaved)
	}
}

func TestQueuedSavesOfTheSameThingCoalesce(t *testing.T) {
	h, _ := saveFixture(t, demo())
	h.be.envGate = make(chan struct{})
	env := h.a.ws.Environments[0]
	var cmds []tea.Cmd
	for _, name := range []string{"a", "b", "c", "d"} {
		env.Name = name
		_, cmd := h.a.Update(SaveMsg{Kind: SaveEnvironment, ID: env.ID})
		cmds = append(cmds, cmd)
	}
	if n := len(h.a.saver.ops); n != 2 {
		t.Fatalf("queue holds %d ops, want the running one plus one coalesced", n)
	}
	go func() { close(h.be.envGate) }()
	settle(t, h, tea.Batch(cmds...))
	names := []string{}
	for _, e := range h.be.envSaved {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "a,d" {
		t.Fatalf("writes = %v, want a then the latest (d)", names)
	}
}

func TestSaveKindsReachTheBackend(t *testing.T) {
	h, pane := saveFixture(t, demo())
	ws := h.a.ws
	coll := ws.Collections[0]
	req := firstRequest(t, coll)
	ws.Globals = &model.Globals{ID: "globals-id", Variables: []model.Variable{{Key: "region", Value: "eu"}}}
	env := ws.Environments[0]

	for _, m := range []SaveMsg{
		{Kind: SaveRequest, Collection: coll.ID, ID: req.ID},
		{Kind: SaveCollection, Collection: coll.ID},
		{Kind: SaveGlobals},
		{Kind: DeleteEnvironment, ID: env.ID},
	} {
		settle(t, h, func() tea.Msg { return m })
	}
	if len(h.be.reqSaved) != 1 || h.be.reqSaved[0] != req.ID {
		t.Fatalf("requests = %v", h.be.reqSaved)
	}
	if len(h.be.saved) != 1 || h.be.saved[0] != coll.ID {
		t.Fatalf("collections = %v", h.be.saved)
	}
	if len(h.be.globalsSaved) != 1 || h.be.globalsSaved[0].Variables[0].Key != "region" {
		t.Fatalf("globals = %+v", h.be.globalsSaved)
	}
	if len(h.be.envDeleted) != 1 || h.be.envDeleted[0] != env.ID {
		t.Fatalf("deleted = %v", h.be.envDeleted)
	}
	kinds := []SaveKind{}
	for _, d := range pane.saveDone() {
		kinds = append(kinds, d.Kind)
	}
	want := []SaveKind{SaveRequest, SaveCollection, SaveGlobals, DeleteEnvironment}
	if len(kinds) != len(want) {
		t.Fatalf("done kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("done kinds = %v, want %v", kinds, want)
		}
	}
}

func TestSaveOfMissingThingsIsIgnored(t *testing.T) {
	h, pane := saveFixture(t, demo())
	h.a.ws.Globals = nil
	for _, m := range []SaveMsg{
		{Kind: SaveRequest, Collection: "nope", ID: "x"},
		{Kind: SaveCollection, Collection: "nope"},
		{Kind: SaveEnvironment, ID: "nope"},
		{Kind: SaveGlobals},
	} {
		settle(t, h, func() tea.Msg { return m })
	}
	if len(pane.saveDone()) != 0 || h.a.saver.busy() {
		t.Fatal("a save of something that does not exist did work")
	}
}

func TestSaveFailureIsReportedToPaneAndUser(t *testing.T) {
	h, pane := saveFixture(t, demo())
	h.be.saveErr = errors.New("disk full")
	env := h.a.ws.Environments[0]
	settle(t, h, func() tea.Msg { return SaveMsg{Kind: SaveEnvironment, ID: env.ID} })
	done := pane.saveDone()
	if len(done) != 1 || done[0].Err == nil {
		t.Fatalf("done = %+v", done)
	}
	h.mustHave("Could not save")
	h.mustHave("disk full")
	if !h.a.saver.failed {
		t.Fatal("failure not remembered")
	}
	// The next successful write clears the failure.
	h.be.saveErr = nil
	settle(t, h, func() tea.Msg { return SaveMsg{Kind: SaveEnvironment, ID: env.ID} })
	if h.a.saver.failed {
		t.Fatal("failure not cleared by a later success")
	}
}

func TestFlushDirtySavesEveryUnsavedRequest(t *testing.T) {
	h, _ := saveFixture(t, demo())
	coll := h.a.ws.Collections[0]
	req := firstRequest(t, coll)
	other := lastRequest(t, coll)
	h.msg(DirtyMsg{ID: req.ID, Dirty: true})
	h.msg(DirtyMsg{ID: other.ID, Dirty: true})
	h.msg(DirtyMsg{ID: other.ID, Dirty: false})
	cmd, n := h.a.flushDirty()
	if n != 1 {
		t.Fatalf("flushed %d, want 1", n)
	}
	settle(t, h, cmd)
	if len(h.be.reqSaved) != 1 || h.be.reqSaved[0] != req.ID {
		t.Fatalf("saved = %v", h.be.reqSaved)
	}
}

func TestBlurCmdRunsWhenFocusLeavesTheRequestPane(t *testing.T) {
	ran := 0
	pane := &msgPane{blur: func() tea.Msg { ran++; return nil }}
	h := newHarness(t, demo(), 160, 30, func(o *Options) {
		o.NewRequestPane = func(PaneEnv) Pane { return pane }
	})
	h.run(h.a.setFocus(SlotRequest))
	if pane.blurs != 0 {
		t.Fatal("BlurCmd asked on focus in")
	}
	h.run(h.a.setFocus(SlotResponse))
	if pane.blurs != 1 || ran != 1 {
		t.Fatalf("blurs = %d, ran = %d", pane.blurs, ran)
	}
	h.run(h.a.setFocus(SlotResponse)) // no change, no blur
	if pane.blurs != 1 {
		t.Fatal("BlurCmd asked without a focus change")
	}
}

func requests(t *testing.T, c *model.Collection) []*model.Request {
	t.Helper()
	var out []*model.Request
	err := c.Walk(func(it model.Item, _ model.Container, _ int) error {
		if r, ok := it.(*model.Request); ok {
			out = append(out, r)
		}
		return nil
	})
	if err != nil || len(out) < 2 {
		t.Fatalf("requests = %d, err = %v", len(out), err)
	}
	return out
}

func firstRequest(t *testing.T, c *model.Collection) *model.Request { return requests(t, c)[0] }

func lastRequest(t *testing.T, c *model.Collection) *model.Request {
	rs := requests(t, c)
	return rs[len(rs)-1]
}
