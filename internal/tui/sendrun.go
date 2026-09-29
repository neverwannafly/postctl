package tui

import (
	"context"
	"errors"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/ui"
)

// One in-flight send per request. The shell runs [Options.Sender] in a
// command; the sender's progress callback and its final result travel to
// Update through a small mutex-guarded queue that a listening command drains,
// so no goroutine ever touches the model and delivery order is preserved.

// flight is a send in progress.
type flight struct {
	cancel context.CancelFunc
	q      *sendQueue
}

// sendQueue passes messages from the sender's goroutine to Update.
type sendQueue struct {
	mu   sync.Mutex
	msgs []tea.Msg
	wake chan struct{}
}

func newSendQueue() *sendQueue { return &sendQueue{wake: make(chan struct{}, 1)} }

func (q *sendQueue) push(m tea.Msg) {
	q.mu.Lock()
	q.msgs = append(q.msgs, m)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// drain blocks until at least one message is queued, then returns them all.
func (q *sendQueue) drain() []tea.Msg {
	for {
		q.mu.Lock()
		out := q.msgs
		q.msgs = nil
		q.mu.Unlock()
		if len(out) > 0 {
			return out
		}
		<-q.wake
	}
}

// sendEventsMsg carries progress steps and, last, the result of one send.
type sendEventsMsg struct {
	id   model.ID
	q    *sendQueue
	msgs []tea.Msg
}

func waitSend(id model.ID, q *sendQueue) tea.Cmd {
	return func() tea.Msg { return sendEventsMsg{id: id, q: q, msgs: q.drain()} }
}

// startSend begins a send unless one is already running for the request.
func (a *app) startSend(sr SendRequest) tea.Cmd {
	if sr.Request == nil {
		return nil
	}
	id := sr.Request.ID
	sender := a.opts.Sender
	if sender == nil {
		return a.toastWarn("Sending is not available in this build.")
	}
	if _, busy := a.sends[id]; busy {
		return a.toastInfo("This request is already sending. Press Esc to cancel it.")
	}
	// The sender works on snapshots: it runs on another goroutine while edits
	// keep changing the live workspace.
	sr.Request = sr.Request.Clone()
	if sr.Collection != nil {
		sr.Collection = sr.Collection.Clone()
	}
	if sr.Environment != nil {
		sr.Environment = sr.Environment.Clone()
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := newSendQueue()
	if a.sends == nil {
		a.sends = map[model.ID]*flight{}
	}
	a.sends[id] = &flight{cancel: cancel, q: q}
	started := a.now()

	run := func() tea.Msg {
		defer cancel()
		q.push(runSender(ctx, sender, sr, started, func(s SendStep) {
			q.push(SendProgressMsg{RequestID: id, Step: s})
		}))
		return nil
	}
	return tea.Batch(
		a.broadcast(SendStartedMsg{RequestID: id}),
		run,
		waitSend(id, q),
	)
}

// runSender calls the sender, turning a panic or a cancelled context into a
// well-formed result.
func runSender(ctx context.Context, sender Sender, sr SendRequest, started time.Time, progress func(SendStep)) (msg tea.Msg) {
	var res SendResult
	func() {
		defer func() {
			if recover() != nil {
				res = SendResult{Err: errors.New("the send stopped unexpectedly. Your request is unchanged")}
			}
		}()
		res = sender.Send(ctx, sr, progress)
	}()
	res.RequestID = sr.Request.ID
	if res.Started.IsZero() {
		res.Started = started
	}
	if res.Err == nil && res.Response == nil && !res.Skipped && ctx.Err() != nil {
		res.Err = context.Canceled
	}
	return SendResultMsg{Result: res}
}

// onSendEvents delivers queued progress and result messages to the panes and
// keeps listening until the result arrives.
func (a *app) onSendEvents(msg sendEventsMsg) tea.Cmd {
	var cmds []tea.Cmd
	finished := false
	for _, m := range msg.msgs {
		cmds = append(cmds, a.broadcast(m))
		if r, ok := m.(SendResultMsg); ok {
			finished = true
			delete(a.sends, r.Result.RequestID)
			// Narrow layouts show one pane at a time, so bring the response
			// into view; the wide layout already shows it.
			if a.focus == SlotRequest && a.layoutKind() != ui.LayoutThreePane && r.Result.Err == nil {
				cmds = append(cmds, a.setFocus(SlotResponse))
			}
		}
	}
	if !finished {
		cmds = append(cmds, waitSend(msg.id, msg.q))
	}
	return tea.Batch(cmds...)
}

// cancelSend cancels the in-flight send for a request, if any.
func (a *app) cancelSend(id model.ID) {
	if f, ok := a.sends[id]; ok {
		f.cancel()
	}
}

// cancelAllSends is called when quitting.
func (a *app) cancelAllSends() {
	for _, f := range a.sends {
		f.cancel()
	}
}

// retrySend resends a request by ID with the live workspace state.
func (a *app) retrySend(id model.ID) tea.Cmd {
	n, ok := a.tree.nodes[id]
	if !ok || n.kind != rowRequest {
		return a.toastWarn("That request no longer exists.")
	}
	return a.startSend(SendRequest{Collection: n.coll, Request: n.item.(*model.Request), Environment: a.activeEnvironment()})
}
