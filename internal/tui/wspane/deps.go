// Package wspane is the pane for WebSocket requests: the URL bar with
// Connect and Disconnect, the Messages, Params, Headers, Auth and Settings
// tabs, the message composer, saved message templates and the live log.
//
// The shell uses it in the Request slot whenever the selected request has
// kind websocket (see tui.Options.NewPaneForKind). Everything that touches the
// network goes through the [Client] and [Session] interfaces, so the pane is
// tested with a fake connection; the real implementation wraps package wsx.
package wspane

import (
	"context"
	"net/http"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/shubham/postctl/internal/httpx"
	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/tui"
	"github.com/shubham/postctl/internal/wsx"
)

// Client is one open connection as the pane sees it. *wsx.Conn implements it.
type Client interface {
	// Next blocks for the next event; io.EOF after the close event.
	Next(ctx context.Context) (wsx.Event, error)
	// Drain returns up to n queued events without blocking.
	Drain(n int) []wsx.Event
	// Send writes one message.
	Send(ctx context.Context, typ wsx.MessageType, data []byte, opts ...wsx.SendOption) error
	// Ping sends a ping and waits for the pong.
	Ping(ctx context.Context) (time.Duration, error)
	// Close performs the closing handshake.
	Close(code int, reason string) error
	// Abort drops the connection at once.
	Abort()
}

// Session is a request resolved for connecting: variables, inherited auth and
// cookies are settled, and saved messages can be turned into frames. It may
// hold secret values in memory.
type Session interface {
	// Dial connects. Cancelling ctx closes the connection.
	Dial(ctx context.Context) (Client, error)
	// Frame resolves a saved message into what goes on the wire and what the
	// log shows (secret values masked).
	Frame(m model.WSMessage) (wsx.Frame, error)
	// Warnings are non-fatal problems found while resolving.
	Warnings() []string
	// Target is the URL without query string or user info, for display.
	Target() string
}

// Deps are the effects the pane performs, injectable for tests. The zero
// value selects the real implementations.
type Deps struct {
	// Prepare resolves a request into a [Session]; wsx.Prepare when nil.
	Prepare func(ctx context.Context, in wsx.PrepareInput) (Session, error)
	// Jar returns the cookie jar for handshakes, or nil for none. It is
	// called from a command, never from Update, so it may do I/O.
	Jar func() http.CookieJar
	// Tokens obtains OAuth 2.0 tokens for inherited OAuth auth.
	Tokens httpx.TokenSource
	// Defaults are the workspace request settings (TLS verification and
	// timeout) used when the request does not set its own.
	Defaults model.RequestSettings
	// After returns a command that delivers msg after d. Tests replace it to
	// drive timers with a fake clock.
	After func(d time.Duration, msg tea.Msg) tea.Cmd
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// Debounce is the autosave delay after the last edit; 500 ms when zero.
	Debounce time.Duration
	// Clipboard puts text on the system clipboard; tea.SetClipboard when nil.
	Clipboard func(text string) tea.Cmd
}

// Factory returns the pane factory for tui.Options.NewPaneForKind.
func Factory(d Deps) tui.PaneFactory {
	return func(env tui.PaneEnv) tui.Pane { return New(env, d) }
}

func (d Deps) after(dur time.Duration, msg tea.Msg) tea.Cmd {
	if d.After != nil {
		return d.After(dur, msg)
	}
	return tea.Tick(dur, func(time.Time) tea.Msg { return msg })
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Deps) debounce() time.Duration {
	if d.Debounce > 0 {
		return d.Debounce
	}
	return 500 * time.Millisecond
}

func (d Deps) clip(text string) tea.Cmd {
	if d.Clipboard != nil {
		return d.Clipboard(text)
	}
	return tea.SetClipboard(text)
}

func (d Deps) prepare(ctx context.Context, in wsx.PrepareInput) (Session, error) {
	if d.Prepare != nil {
		return d.Prepare(ctx, in)
	}
	p, err := wsx.Prepare(ctx, in)
	if err != nil {
		return nil, err
	}
	return realSession{p}, nil
}

// realSession adapts *wsx.Prepared to [Session].
type realSession struct{ p *wsx.Prepared }

// Dial connects with the prepared request.
func (s realSession) Dial(ctx context.Context) (Client, error) {
	c, err := s.p.Dial(ctx)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Frame builds the wire and display forms of a saved message.
func (s realSession) Frame(m model.WSMessage) (wsx.Frame, error) { return s.p.Frame(m) }

// Warnings lists non-fatal problems found while resolving.
func (s realSession) Warnings() []string { return s.p.Warnings }

// Target is the URL for display, without query or user info.
func (s realSession) Target() string { return s.p.Target.Display() }
