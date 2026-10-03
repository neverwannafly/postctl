package wspane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/ui"
	"github.com/shubham/postctl/internal/wsx"
)

// connState is where a request's connection is in its life.
type connState int

const (
	stIdle connState = iota
	stConnecting
	stOpen
	stClosing
)

const (
	// drainBatch is how many queued events one update takes at most.
	drainBatch = 256
	// spinEvery is the connecting animation step.
	spinEvery = 100 * time.Millisecond
)

// live is one request's connection and log. It outlives selection changes, so
// a connection keeps running while another request is open, and it is kept
// after a disconnect so the log stays readable.
type live struct {
	id  model.ID
	gen int // bumped on every attempt; stale messages carry an older one

	state  connState
	ctx    context.Context
	cancel context.CancelFunc
	client Client
	sess   Session

	started  time.Time // when the attempt began
	openedAt time.Time
	target   string // URL without query or user info

	log        *msgLog
	sent, recv int
	rtt        time.Duration

	// failure is why the last attempt or connection ended badly, in plain
	// words ("" after a clean close); failed says it was not clean.
	failure string
	failed  bool
	closed  bool // a connection has ended at least once
	frame   int  // spinner frame

	sending bool
	sendq   []model.WSMessage
}

// Messages private to the pane.
type (
	// connectedMsg reports the outcome of Prepare and Dial.
	connectedMsg struct {
		id     model.ID
		gen    int
		sess   Session
		client Client
		err    error
	}
	// eventsMsg carries connection events; end is set when the stream ended.
	eventsMsg struct {
		id  model.ID
		gen int
		evs []wsx.Event
		end bool
	}
	// sentMsg reports a finished send.
	sentMsg struct {
		id  model.ID
		gen int
		err error
	}
	// pingedMsg reports a finished ping.
	pingedMsg struct {
		id  model.ID
		gen int
		rtt time.Duration
		err error
	}
	// tickMsg animates the connecting state.
	tickMsg struct {
		id  model.ID
		gen int
	}
)

func (p *Pane) liveFor(id model.ID) *live {
	l := p.lives[id]
	if l == nil {
		l = &live{id: id, log: newLog()}
		p.lives[id] = l
	}
	return l
}

// cur is the current request's connection, created on first use.
func (p *Pane) cur() *live {
	if p.req == nil {
		return nil
	}
	return p.liveFor(p.req.ID)
}

// peek is the current request's connection if it has ever had one.
func (p *Pane) peek() *live {
	if p.req == nil {
		return nil
	}
	return p.lives[p.req.ID]
}

func (l *live) note(kind entryKind, at time.Time, text string) {
	l.log.add(entry{at: at, kind: kind, text: text})
}

// ---- connect ------------------------------------------------------------------

// toggleConnect is Enter in the URL bar and the Connect button: connect when
// idle, cancel while connecting, disconnect when open.
func (p *Pane) toggleConnect() tea.Cmd {
	l := p.cur()
	if l == nil {
		return nil
	}
	switch l.state {
	case stIdle:
		return p.withFlush(p.connect())
	case stConnecting:
		return p.cancelConnect()
	case stOpen:
		return p.disconnect()
	}
	return nil
}

// connect starts Prepare and Dial in a command.
func (p *Pane) connect() tea.Cmd {
	l := p.cur()
	if l == nil || l.state != stIdle {
		return nil
	}
	if strings.TrimSpace(p.req.URL.Raw) == "" {
		return p.setNotice("Enter a URL to connect to", true)
	}
	now := p.deps.now()
	l.gen++
	l.state = stConnecting
	l.started = now
	l.failure, l.failed = "", false
	l.sending, l.sendq = false, nil
	l.ctx, l.cancel = context.WithCancel(context.Background())
	l.note(ekNote, now, "Connecting")

	in := p.prepareInput()
	id, gen, ctx := l.id, l.gen, l.ctx
	jar := p.deps.Jar
	connectCmd := func() tea.Msg {
		if jar != nil {
			in.Jar = jar()
		}
		sess, err := p.deps.prepare(ctx, in)
		if err != nil {
			return connectedMsg{id: id, gen: gen, err: err}
		}
		client, err := sess.Dial(ctx)
		if err != nil {
			return connectedMsg{id: id, gen: gen, sess: sess, err: err}
		}
		return connectedMsg{id: id, gen: gen, sess: sess, client: client}
	}
	return tea.Batch(connectCmd, p.tick(l))
}

// prepareInput snapshots what Prepare reads, on the Update goroutine, so the
// command never touches state the shell may change.
func (p *Pane) prepareInput() wsx.PrepareInput {
	in := wsx.PrepareInput{
		Request:  p.req.Clone(),
		Defaults: p.deps.Defaults.Clone(),
		Now:      p.deps.Now,
	}
	if p.coll != nil {
		in.Collection = p.coll.Clone()
	}
	if p.activeEnv != nil {
		in.Environment = p.activeEnv.Clone()
	}
	if p.ws != nil && p.ws.Globals != nil {
		in.Globals = p.ws.Globals.Clone()
	}
	if h, ok := p.env.Backend.(tui.Hydrator); ok {
		in.Hydrator = h
	}
	in.Tokens = p.deps.Tokens
	return in
}

func (p *Pane) tick(l *live) tea.Cmd {
	return p.deps.after(spinEvery, tickMsg{id: l.id, gen: l.gen})
}

func (p *Pane) onTick(m tickMsg) tea.Cmd {
	l := p.lives[m.id]
	if l == nil || l.gen != m.gen || l.state != stConnecting {
		return nil
	}
	l.frame++
	return p.tick(l)
}

func (p *Pane) onConnected(m connectedMsg) tea.Cmd {
	l := p.lives[m.id]
	if l == nil || m.gen != l.gen || l.state != stConnecting {
		if m.client != nil {
			m.client.Abort() // cancelled while dialing
		}
		return nil
	}
	now := p.deps.now()
	if m.err != nil {
		l.state = stIdle
		l.cancel()
		l.failed = true
		l.closed = true
		var he *wsx.HandshakeError
		if errors.As(m.err, &he) {
			l.failure = fmt.Sprintf("Refused: %d %s", he.Status, he.StatusText)
			l.log.add(entry{at: now, kind: ekError, size: len(he.Body), data: []byte(he.Body),
				text: fmt.Sprintf("The server refused the connection: %d %s", he.Status, he.StatusText)})
		} else {
			l.failure = "Could not connect"
			l.log.add(entry{at: now, kind: ekError, text: capitalize(m.err.Error())})
		}
		return nil
	}
	l.state = stOpen
	l.client, l.sess = m.client, m.sess
	l.target = m.sess.Target()
	l.openedAt = now
	l.sent, l.recv, l.rtt = 0, 0, 0
	for _, w := range m.sess.Warnings() {
		l.note(ekNote, now, w)
	}
	return p.pump(l)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ---- events -------------------------------------------------------------------

// pump waits for the next events in a command; each batch arrives as an
// eventsMsg and schedules the next wait.
func (p *Pane) pump(l *live) tea.Cmd {
	c, id, gen, ctx := l.client, l.id, l.gen, l.ctx
	return func() tea.Msg {
		ev, err := c.Next(ctx)
		if err != nil {
			return eventsMsg{id: id, gen: gen, end: true}
		}
		return eventsMsg{id: id, gen: gen, evs: append([]wsx.Event{ev}, c.Drain(drainBatch)...)}
	}
}

func (p *Pane) onEvents(m eventsMsg) tea.Cmd {
	l := p.lives[m.id]
	if l == nil || m.gen != l.gen || (l.state != stOpen && l.state != stClosing) {
		return nil
	}
	closed := false
	for _, ev := range m.evs {
		e := entryFromEvent(ev)
		switch ev.Type {
		case wsx.EventMessage:
			if ev.Dir == wsx.DirIn {
				l.recv++
			} else {
				l.sent++
			}
		case wsx.EventPong:
			if ev.Latency > 0 {
				l.rtt = ev.Latency
			}
		case wsx.EventClose:
			closed = true
			switch ev.Code {
			case wsx.CloseNormal, wsx.CloseGoingAway, wsx.CloseNoStatus:
				l.failed, l.failure = false, ""
			default:
				l.failed, l.failure = true, e.text
			}
		}
		l.log.add(e)
	}
	if closed {
		p.finish(l)
		return nil
	}
	if m.end {
		l.log.add(entry{at: p.deps.now(), kind: ekError, text: "The connection ended unexpectedly"})
		l.failed, l.failure = true, "The connection ended unexpectedly"
		p.finish(l)
		return nil
	}
	return p.pump(l)
}

// finish returns a connection to idle after its close event.
func (p *Pane) finish(l *live) {
	l.state = stIdle
	l.closed = true
	if l.cancel != nil {
		l.cancel()
	}
	l.client, l.sess = nil, nil
	l.sending, l.sendq = false, nil
}

// abort drops a connection or attempt at once.
func (p *Pane) abort(l *live, text string) {
	if l.state == stIdle {
		return
	}
	l.gen++ // whatever is still in flight is stale now
	if l.client != nil {
		l.client.Abort()
	}
	if l.cancel != nil {
		l.cancel()
	}
	l.state = stIdle
	l.closed = true
	l.client, l.sess = nil, nil
	l.sending, l.sendq = false, nil
	l.failure, l.failed = "", false
	if text != "" {
		l.note(ekNote, p.deps.now(), text)
	}
}

// cancelConnect stops a connection attempt.
func (p *Pane) cancelConnect() tea.Cmd {
	l := p.peek()
	if l == nil || l.state != stConnecting {
		return nil
	}
	p.abort(l, "Connection cancelled")
	return nil
}

// disconnect closes an open connection with the closing handshake.
func (p *Pane) disconnect() tea.Cmd {
	l := p.peek()
	if l == nil {
		return nil
	}
	switch l.state {
	case stConnecting:
		return p.cancelConnect()
	case stOpen:
		l.state = stClosing
		c := l.client
		return func() tea.Msg {
			_ = c.Close(wsx.CloseNormal, "")
			return nil
		}
	}
	return nil
}

// reconnect drops the connection, if any, and connects again.
func (p *Pane) reconnect() tea.Cmd {
	l := p.cur()
	if l == nil {
		return nil
	}
	p.abort(l, "Reconnecting")
	return p.withFlush(p.connect())
}

// ---- send and ping ------------------------------------------------------------

// sendMessage queues a message for the connection; messages go out in order.
func (p *Pane) sendMessage(m model.WSMessage) tea.Cmd {
	l := p.peek()
	if l == nil || l.state != stOpen {
		return p.setNotice("Not connected. Press Ctrl-S to connect", true)
	}
	l.sendq = append(l.sendq, m)
	return p.nextSend(l)
}

func (p *Pane) nextSend(l *live) tea.Cmd {
	if l.sending || len(l.sendq) == 0 || l.state != stOpen {
		return nil
	}
	m := l.sendq[0]
	l.sendq = l.sendq[1:]
	l.sending = true
	sess, c, id, gen := l.sess, l.client, l.id, l.gen
	return func() tea.Msg {
		fr, err := sess.Frame(m)
		if err != nil {
			return sentMsg{id: id, gen: gen, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return sentMsg{id: id, gen: gen, err: c.Send(ctx, fr.Type, fr.Data, wsx.WithDisplay(fr.Display))}
	}
}

func (p *Pane) onSent(m sentMsg) tea.Cmd {
	l := p.lives[m.id]
	if l == nil || m.gen != l.gen {
		return nil
	}
	l.sending = false
	cmds := []tea.Cmd{p.nextSend(l)}
	if m.err != nil {
		text := capitalize(m.err.Error())
		if errors.Is(m.err, wsx.ErrClosed) {
			text = "The connection is closed. Press Ctrl-S to reconnect"
		}
		if p.req != nil && p.req.ID == m.id {
			cmds = append(cmds, p.setNotice(text, true))
		}
	}
	return tea.Batch(cmds...)
}

// ping sends a ping and reports the round trip.
func (p *Pane) ping() tea.Cmd {
	l := p.peek()
	if l == nil || l.state != stOpen {
		return p.setNotice("Not connected. Press Ctrl-S to connect", true)
	}
	c, id, gen := l.client, l.id, l.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rtt, err := c.Ping(ctx)
		return pingedMsg{id: id, gen: gen, rtt: rtt, err: err}
	}
}

func (p *Pane) onPinged(m pingedMsg) tea.Cmd {
	l := p.lives[m.id]
	if l == nil || m.gen != l.gen {
		return nil
	}
	if p.req == nil || p.req.ID != m.id {
		return nil
	}
	if m.err != nil {
		return p.setNotice("Ping failed: "+m.err.Error(), true)
	}
	l.rtt = m.rtt
	return p.setNotice("Pong in "+ui.FormatDuration(m.rtt), false)
}
