package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// gateSender reports its steps one at a time: each waits for a value on gate
// (or cancellation), so tests control timing without sleeping.
type gateSender struct {
	steps []string
	gate  chan struct{}

	mu    sync.Mutex
	calls int
}

func (g *gateSender) Send(ctx context.Context, req SendRequest, progress func(SendStep)) SendResult {
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	for _, s := range g.steps {
		progress(SendStep{Text: s})
		select {
		case <-g.gate:
		case <-ctx.Done():
			return SendResult{Err: ctx.Err()}
		}
	}
	return SendResult{Response: &httpx.Response{StatusCode: 200, Status: "OK"}}
}

func (g *gateSender) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

// sendRecorder is a pane that records the send messages it receives.
type sendRecorder struct {
	mu  sync.Mutex
	log []string
	res []SendResult
}

func (p *sendRecorder) Update(msg tea.Msg) (Pane, tea.Cmd) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch m := msg.(type) {
	case SendStartedMsg:
		p.log = append(p.log, "started")
	case SendProgressMsg:
		p.log = append(p.log, "step:"+m.Step.Text)
	case SendResultMsg:
		p.log = append(p.log, "result")
		p.res = append(p.res, m.Result)
	}
	return p, nil
}
func (p *sendRecorder) View() string        { return "" }
func (p *sendRecorder) SetSize(int, int)    {}
func (p *sendRecorder) Focus() tea.Cmd      { return nil }
func (p *sendRecorder) Blur()               {}
func (p *sendRecorder) KeyHints() []ui.Hint { return nil }
func (p *sendRecorder) joined() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.log, ",")
}

// settle feeds the messages of cmd (and of the commands they cause) back into
// Update until nothing arrives for a short idle period. Unlike harness.run it
// waits for slow commands, which is what a listening send command is.
func settle(t *testing.T, h *harness, cmd tea.Cmd) {
	t.Helper()
	msgs := make(chan tea.Msg, 64)
	start := func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			if m := c(); m != nil {
				msgs <- m
			}
		}()
	}
	start(cmd)
	for {
		select {
		case m := <-msgs:
			if b, ok := m.(tea.BatchMsg); ok {
				for _, c := range b {
					start(c)
				}
				continue
			}
			if isSequence(m) {
				continue
			}
			_, next := h.a.Update(m)
			start(next)
		case <-time.After(250 * time.Millisecond):
			return
		}
	}
}

func sendFixture(t *testing.T, sender Sender) (*harness, *sendRecorder, SendRequest) {
	t.Helper()
	rec := &sendRecorder{}
	h := newHarness(t, demo(), 160, 30, func(o *Options) {
		o.Sender = sender
		o.NewResponsePane = func(PaneEnv) Pane { return rec }
	})
	h.selectByName("Create charge")
	n := h.a.tree.nodes[h.a.tree.sel]
	return h, rec, SendRequest{Collection: n.coll, Request: n.item.(*model.Request), Environment: h.a.activeEnvironment()}
}

func TestSendDeliversStartedProgressThenResultInOrder(t *testing.T) {
	g := &gateSender{steps: []string{"Resolving variables", "Waiting for response"}, gate: make(chan struct{})}
	h, rec, sr := sendFixture(t, g)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, cmd := h.a.Update(SendRequestMsg{Request: sr})
		// Release each step once it has been reported.
		go func() {
			g.gate <- struct{}{}
			g.gate <- struct{}{}
		}()
		settle(t, h, cmd)
	}()
	<-done
	want := "started,step:Resolving variables,step:Waiting for response,result"
	if got := rec.joined(); got != want {
		t.Fatalf("messages %q, want %q", got, want)
	}
	if rec.res[0].RequestID != sr.Request.ID || rec.res[0].Response == nil {
		t.Fatalf("result: %+v", rec.res[0])
	}
	if len(h.a.sends) != 0 {
		t.Fatal("finished send still tracked")
	}
}

func TestOneSendInFlightPerRequest(t *testing.T) {
	g := &gateSender{steps: []string{"Waiting for response"}, gate: make(chan struct{})}
	h, rec, sr := sendFixture(t, g)

	_, first := h.a.Update(SendRequestMsg{Request: sr})
	_, second := h.a.Update(SendRequestMsg{Request: sr})
	if second != nil && h.a.toast == nil {
		t.Fatal("a second send should explain itself")
	}
	if h.a.toast == nil || !strings.Contains(h.a.toast.text, "already sending") {
		t.Fatalf("toast: %+v", h.a.toast)
	}
	go func() { g.gate <- struct{}{} }()
	settle(t, h, first)
	if g.count() != 1 {
		t.Fatalf("sender ran %d times, want 1", g.count())
	}
	if strings.Count(rec.joined(), "result") != 1 {
		t.Fatalf("messages %q", rec.joined())
	}

	// After the result the request can be sent again.
	go func() { g.gate <- struct{}{} }()
	_, again := h.a.Update(SendRequestMsg{Request: sr})
	settle(t, h, again)
	if g.count() != 2 {
		t.Fatalf("sender ran %d times, want 2", g.count())
	}
}

func TestCancelSendEndsWithCancelledResult(t *testing.T) {
	g := &gateSender{steps: []string{"Waiting for response"}, gate: make(chan struct{})}
	h, rec, sr := sendFixture(t, g)
	_, cmd := h.a.Update(SendRequestMsg{Request: sr})
	f := h.a.sends[sr.Request.ID]
	go func() {
		time.Sleep(50 * time.Millisecond)
		f.cancel() // what CancelSendMsg does
	}()
	settle(t, h, cmd)
	if len(rec.res) != 1 || !errors.Is(rec.res[0].Err, context.Canceled) {
		t.Fatalf("result: %+v", rec.res)
	}
	if len(h.a.sends) != 0 {
		t.Fatal("cancelled send still tracked")
	}
}

func TestCancelSendMsgCancelsTheFlight(t *testing.T) {
	g := &gateSender{steps: []string{"Waiting for response"}, gate: make(chan struct{})}
	h, rec, sr := sendFixture(t, g)
	_, cmd := h.a.Update(SendRequestMsg{Request: sr})
	h.a.Update(CancelSendMsg{RequestID: "not-in-flight"}) // unknown IDs are ignored
	h.a.Update(CancelSendMsg{RequestID: sr.Request.ID})
	settle(t, h, cmd)
	if len(rec.res) != 1 || !errors.Is(rec.res[0].Err, context.Canceled) {
		t.Fatalf("result: %+v", rec.res)
	}
}

func TestSenderGetsSnapshotsNotLivePointers(t *testing.T) {
	var got SendRequest
	s := senderFunc(func(_ context.Context, r SendRequest, _ func(SendStep)) SendResult {
		got = r
		return SendResult{}
	})
	h, _, sr := sendFixture(t, s)
	_, cmd := h.a.Update(SendRequestMsg{Request: sr})
	settle(t, h, cmd)
	if got.Request == sr.Request || got.Collection == sr.Collection {
		t.Fatal("the sender must receive clones so later edits cannot race with it")
	}
	if got.Request.ID != sr.Request.ID {
		t.Fatal("clone changed the request")
	}
}

func TestRetrySendMsgResendsTheRequest(t *testing.T) {
	g := &gateSender{steps: nil, gate: make(chan struct{})}
	h, rec, sr := sendFixture(t, g)
	_, cmd := h.a.Update(RetrySendMsg{RequestID: sr.Request.ID})
	settle(t, h, cmd)
	if g.count() != 1 || !strings.HasSuffix(rec.joined(), "result") {
		t.Fatalf("calls=%d messages=%q", g.count(), rec.joined())
	}
}

func TestSendWithoutSenderExplains(t *testing.T) {
	h, _, sr := sendFixture(t, nil)
	h.msg(SendRequestMsg{Request: sr})
	if h.a.toast == nil || !strings.Contains(h.a.toast.text, "not available") {
		t.Fatalf("toast: %+v", h.a.toast)
	}
}

type senderFunc func(context.Context, SendRequest, func(SendStep)) SendResult

func (f senderFunc) Send(ctx context.Context, r SendRequest, p func(SendStep)) SendResult {
	return f(ctx, r, p)
}
