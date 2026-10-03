package request

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
)

const tickEvery = 100 * time.Millisecond

// sendInfo is what the pane knows about one in-flight send.
type sendInfo struct {
	step    string
	started time.Time
}

// sendState is the pane's view of in-flight sends, for the status row.
type sendState struct {
	flights map[model.ID]*sendInfo
	frame   int
	ticking bool
}

type sendTickMsg struct{}

// sending returns the in-flight send of the open request, if any.
func (p *Pane) sending() (*sendInfo, bool) {
	if p.req == nil {
		return nil, false
	}
	si, ok := p.send.flights[p.req.ID]
	return si, ok
}

// sendNow asks the shell to send the open request. Sending never needs a
// confirmation; an empty URL is the one thing worth a word first.
func (p *Pane) sendNow() tea.Cmd {
	if p.req == nil {
		return nil
	}
	if p.req.URL.Raw == "" {
		p.setZone(zBar)
		return p.setNotice("Enter a URL first")
	}
	req := tui.SendRequest{Collection: p.coll, Request: p.req, Environment: p.activeEnv}
	// The send carries the live request; the pending autosave is written with
	// it so the file on disk is the request that went out.
	return tea.Batch(p.flush(p.req.ID), msgCmd(tui.SendRequestMsg{Request: req}))
}

// cancelSend asks the shell to stop the in-flight send.
func (p *Pane) cancelSend() tea.Cmd {
	return msgCmd(tui.CancelSendMsg{RequestID: p.req.ID})
}

func (p *Pane) onSendStarted(m tui.SendStartedMsg) tea.Cmd {
	if p.send.flights == nil {
		p.send.flights = map[model.ID]*sendInfo{}
	}
	p.send.flights[m.RequestID] = &sendInfo{step: "Sending request", started: p.deps.now()}
	return p.ensureTick()
}

// ensureTick starts the animation ticker if it is not already running.
func (p *Pane) ensureTick() tea.Cmd {
	if p.send.ticking {
		return nil
	}
	p.send.ticking = true
	return p.deps.after(tickEvery, sendTickMsg{})
}

func (p *Pane) onSendProgress(m tui.SendProgressMsg) {
	if si, ok := p.send.flights[m.RequestID]; ok && m.Step.Text != "" {
		si.step = m.Step.Text
	}
}

func (p *Pane) onSendTick(sendTickMsg) tea.Cmd {
	if len(p.send.flights) == 0 && p.oauth == nil {
		p.send.ticking = false
		return nil
	}
	p.send.frame++
	return p.deps.after(tickEvery, sendTickMsg{})
}

func (p *Pane) onSendResult(m tui.SendResultMsg) tea.Cmd {
	id := m.Result.RequestID
	delete(p.send.flights, id)
	if p.req == nil || p.req.ID != id {
		return nil
	}
	if errors.Is(m.Result.Err, context.Canceled) {
		return p.setNotice("Send cancelled")
	}
	return nil
}
