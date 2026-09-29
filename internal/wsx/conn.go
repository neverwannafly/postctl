package wsx

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Defaults.
const (
	// DefaultMaxMessageBytes caps one received message (16 MiB).
	DefaultMaxMessageBytes int64 = 16 << 20
	// DefaultHandshakeTimeout bounds connecting and the upgrade.
	DefaultHandshakeTimeout = 30 * time.Second
	// DefaultQueueLimit is how many undelivered events a connection keeps.
	DefaultQueueLimit = 10000

	closeTimeout = 5 * time.Second
	pingTimeout  = 10 * time.Second
)

// Target is a fully resolved handshake: nothing in it is a {{variable}}. It
// can hold credentials (query parameters, headers), so never log or display
// it; use [Target.Display].
type Target struct {
	// URL is the ws:// or wss:// URL, including the query string.
	URL string
	// Header holds the handshake request headers, including auth.
	Header http.Header
	// Host overrides the Host header when non-empty.
	Host string
	// Subprotocols are offered to the server in preference order.
	Subprotocols []string
}

// Display returns the URL without user info or query string, safe to show.
func (t *Target) Display() string { return displayURL(t.URL) }

func displayURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(invalid URL)"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// Options tune a connection. The zero value is usable.
type Options struct {
	// Jar stores and supplies cookies for the handshake. Nil disables them.
	Jar http.CookieJar
	// HandshakeTimeout bounds the connect and upgrade; zero means
	// [DefaultHandshakeTimeout].
	HandshakeTimeout time.Duration
	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool
	// PingInterval sends a ping this often while connected; zero disables it.
	PingInterval time.Duration
	// MaxMessageBytes caps one received message; zero means
	// [DefaultMaxMessageBytes].
	MaxMessageBytes int64
	// QueueLimit caps undelivered events; when the consumer falls behind the
	// oldest message events are dropped (see [Conn.Dropped]). Zero means
	// [DefaultQueueLimit].
	QueueLimit int
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

func (o Options) handshakeTimeout() time.Duration {
	if o.HandshakeTimeout > 0 {
		return o.HandshakeTimeout
	}
	return DefaultHandshakeTimeout
}

func (o Options) maxMessage() int64 {
	if o.MaxMessageBytes > 0 {
		return o.MaxMessageBytes
	}
	return DefaultMaxMessageBytes
}

func (o Options) queueLimit() int {
	if o.QueueLimit > 0 {
		return o.QueueLimit
	}
	return DefaultQueueLimit
}

// Conn is an open WebSocket connection. All methods are safe for concurrent
// use, except that [Conn.Next] must have a single consumer.
type Conn struct {
	target *Target
	opts   Options
	info   Info
	now    func() time.Time

	ws     *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{} // closed once the close event is queued

	seq atomic.Uint64

	qmu     sync.Mutex
	queue   []Event
	dropped int
	wake    chan struct{}

	pmu   sync.Mutex
	pings []time.Time // sent pings awaiting a pong, oldest first

	closeMu     sync.Mutex
	localClosed bool
	localCode   int
	localReason string
}

// Dial connects to t. The handshake is bounded by [Options.HandshakeTimeout]
// and by ctx; afterwards cancelling ctx closes the connection. A refused
// handshake is a [*HandshakeError]; other failures are a [*DialError] (or
// ctx's error when it was cancelled).
func Dial(ctx context.Context, t *Target, o Options) (*Conn, error) {
	if t == nil || strings.TrimSpace(t.URL) == "" {
		return nil, ErrNoURL
	}
	if _, err := normalizeScheme(t.URL); err != nil {
		return nil, err
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	c := &Conn{target: t, opts: o, now: now, done: make(chan struct{}), wake: make(chan struct{}, 1)}

	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: o.handshakeTimeout(), KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: o.InsecureSkipVerify}, //nolint:gosec // opt-in per request, like Postman's SSL verification switch
		TLSHandshakeTimeout:   o.handshakeTimeout(),
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{}, // stay on HTTP/1.1: the upgrade needs it
		ResponseHeaderTimeout: o.handshakeTimeout(),
	}
	client := &http.Client{Transport: tr, Jar: o.Jar}

	hctx, cancel := context.WithTimeout(ctx, o.handshakeTimeout())
	defer cancel()
	ws, resp, err := websocket.Dial(hctx, t.URL, &websocket.DialOptions{
		HTTPClient:     client,
		HTTPHeader:     t.Header.Clone(),
		Host:           t.Host,
		Subprotocols:   t.Subprotocols,
		OnPingReceived: c.onPing,
		OnPongReceived: c.onPong,
	})
	if err != nil {
		tr.CloseIdleConnections()
		return nil, dialError(ctx, err, resp)
	}
	ws.SetReadLimit(o.maxMessage())

	c.ws = ws
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.info = Info{URL: t.Display(), Status: resp.StatusCode, Subprotocol: ws.Subprotocol(), Header: resp.Header.Clone()}
	info := c.info
	c.emit(Event{Type: EventOpen, Info: &info})

	go c.readLoop()
	if o.PingInterval > 0 {
		go c.keepAlive(o.PingInterval)
	}
	return c, nil
}

// dialError classifies a failed dial.
func dialError(ctx context.Context, err error, resp *http.Response) error {
	if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
		he := &HandshakeError{Status: resp.StatusCode, StatusText: http.StatusText(resp.StatusCode), Header: resp.Header}
		if he.StatusText == "" {
			he.StatusText = strings.TrimSpace(strings.TrimPrefix(resp.Status, strconv.Itoa(resp.StatusCode)))
		}
		if resp.Body != nil {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
			_ = resp.Body.Close()
			he.Body = string(b)
		}
		return he
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &DialError{Err: stripProtocol(err)}
}

// stripProtocol drops the library's wrapper text and any embedded URL while
// keeping the cause (for example "dial tcp 127.0.0.1:1: connect: connection
// refused").
func stripProtocol(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	msg := err.Error()
	if i := strings.Index(msg, "WebSocket protocol violation"); i >= 0 {
		msg = msg[i:]
	}
	return errors.New(firstLine(strings.TrimPrefix(msg, "failed to WebSocket dial: ")))
}

// Info returns the connection details from the handshake.
func (c *Conn) Info() Info { return c.info }

// Target returns what the connection was dialed with.
func (c *Conn) Target() *Target { return c.target }

// Done is closed after the final [EventClose] has been queued. Events may
// still be waiting for [Conn.Next].
func (c *Conn) Done() <-chan struct{} { return c.done }

// Dropped reports how many events were discarded because the consumer fell
// more than [Options.QueueLimit] events behind.
func (c *Conn) Dropped() int {
	c.qmu.Lock()
	defer c.qmu.Unlock()
	return c.dropped
}

// Next returns the next event, blocking until one is available. It returns
// io.EOF once the close event and everything before it were delivered, or
// ctx's error. Only one goroutine may call Next.
func (c *Conn) Next(ctx context.Context) (Event, error) {
	for {
		c.qmu.Lock()
		if len(c.queue) > 0 {
			ev := c.queue[0]
			c.queue[0] = Event{}
			c.queue = c.queue[1:]
			c.qmu.Unlock()
			return ev, nil
		}
		c.qmu.Unlock()
		select {
		case <-c.done:
			c.qmu.Lock()
			empty := len(c.queue) == 0
			c.qmu.Unlock()
			if empty {
				return Event{}, io.EOF
			}
		case <-c.wake:
		case <-ctx.Done():
			return Event{}, ctx.Err()
		}
	}
}

// emit records ev with the next sequence number and the current time.
func (c *Conn) emit(ev Event) uint64 {
	ev.Seq = c.seq.Add(1)
	ev.Time = c.now()
	c.qmu.Lock()
	if limit := c.opts.queueLimit(); len(c.queue) >= limit {
		// Drop the oldest message-like event; close and open events survive.
		for i, q := range c.queue {
			if q.Type != EventClose && q.Type != EventOpen {
				c.queue = append(c.queue[:i], c.queue[i+1:]...)
				c.dropped++
				break
			}
		}
	}
	c.queue = append(c.queue, ev)
	c.qmu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return ev.Seq
}

// SendOption adjusts one [Conn.Send].
type SendOption func(*sendConfig)

type sendConfig struct{ display []byte }

// WithDisplay records display instead of the bytes sent in the log event, so
// secret values can be masked while the wire carries the real message.
func WithDisplay(display []byte) SendOption {
	return func(s *sendConfig) { s.display = display }
}

// Send writes one message. The event is recorded before the write so that a
// fast reply is always logged after the message it answers; if the write
// fails an [EventError] with Ref set to the message follows.
func (c *Conn) Send(ctx context.Context, typ MessageType, data []byte, opts ...SendOption) error {
	select {
	case <-c.done:
		return ErrClosed
	default:
	}
	var cfg sendConfig
	for _, o := range opts {
		o(&cfg)
	}
	shown := data
	if cfg.display != nil {
		shown = cfg.display
	}
	wt := websocket.MessageText
	if typ == Binary {
		wt = websocket.MessageBinary
	}
	ref := c.emit(Event{Type: EventMessage, Dir: DirOut, Msg: typ, Data: shown, Size: len(data)})
	if err := c.ws.Write(ctx, wt, data); err != nil {
		err = c.writeErr(err)
		c.emit(Event{Type: EventError, Dir: DirOut, Err: err, Ref: ref})
		return err
	}
	return nil
}

func (c *Conn) writeErr(err error) error {
	select {
	case <-c.done:
		return ErrClosed
	default:
	}
	if c.ctx.Err() != nil {
		return ErrClosed
	}
	return errors.New(friendly(err))
}

// Ping sends a ping and waits for the pong, returning the round-trip time.
func (c *Conn) Ping(ctx context.Context) (time.Duration, error) {
	select {
	case <-c.done:
		return 0, ErrClosed
	default:
	}
	start := c.now()
	c.pmu.Lock()
	c.pings = append(c.pings, start)
	c.pmu.Unlock()
	ref := c.emit(Event{Type: EventPing, Dir: DirOut})
	if err := c.ws.Ping(ctx); err != nil {
		c.pmu.Lock()
		if len(c.pings) > 0 {
			c.pings = c.pings[1:]
		}
		c.pmu.Unlock()
		err = c.writeErr(err)
		c.emit(Event{Type: EventError, Dir: DirOut, Err: err, Ref: ref})
		return 0, err
	}
	return c.now().Sub(start), nil
}

func (c *Conn) keepAlive(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(c.ctx, pingTimeout)
			_, _ = c.Ping(ctx)
			cancel()
		}
	}
}

func (c *Conn) onPing(_ context.Context, payload []byte) bool {
	c.emit(Event{Type: EventPing, Dir: DirIn, Data: append([]byte(nil), payload...), Size: len(payload)})
	return true
}

func (c *Conn) onPong(_ context.Context, payload []byte) {
	var lat time.Duration
	c.pmu.Lock()
	if len(c.pings) > 0 {
		lat = c.now().Sub(c.pings[0])
		c.pings = c.pings[1:]
	}
	c.pmu.Unlock()
	c.emit(Event{Type: EventPong, Dir: DirIn, Data: append([]byte(nil), payload...), Size: len(payload), Latency: lat})
}

// Close performs the closing handshake with the given status and reason
// (reasons longer than 123 bytes are cut), waiting up to five seconds for the
// server before dropping the connection. It returns once the final
// [EventClose] is queued. Closing a closed connection is a no-op.
func (c *Conn) Close(code int, reason string) error {
	select {
	case <-c.done:
		return nil
	default:
	}
	if len(reason) > 123 {
		reason = reason[:123]
	}
	c.closeMu.Lock()
	if !c.localClosed {
		c.localClosed, c.localCode, c.localReason = true, code, reason
	}
	c.closeMu.Unlock()
	err := c.ws.Close(websocket.StatusCode(code), reason)
	select {
	case <-c.done:
	case <-time.After(closeTimeout):
		c.Abort()
		<-c.done
	}
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return nil // the final event tells how the connection ended
	}
	return nil
}

// Abort drops the connection immediately without a closing handshake.
func (c *Conn) Abort() {
	c.cancel()
	_ = c.ws.CloseNow()
}

// Redial connects again with the same target and options. The old connection
// is aborted first if it is still open. The new connection has its own event
// stream and sequence numbers.
func (c *Conn) Redial(ctx context.Context) (*Conn, error) {
	select {
	case <-c.done:
	default:
		c.Abort()
		<-c.done
	}
	return Dial(ctx, c.target, c.opts)
}

func (c *Conn) readLoop() {
	defer close(c.done)
	defer c.cancel()
	for {
		typ, data, err := c.ws.Read(c.ctx)
		if err != nil {
			c.emit(c.closeEvent(err))
			return
		}
		mt := Text
		if typ == websocket.MessageBinary {
			mt = Binary
		}
		c.emit(Event{Type: EventMessage, Dir: DirIn, Msg: mt, Data: data, Size: len(data)})
	}
}

// closeEvent turns the error that ended the read loop into the final event.
func (c *Conn) closeEvent(err error) Event {
	ev := Event{Type: EventClose}
	c.closeMu.Lock()
	local, lcode, lreason := c.localClosed, c.localCode, c.localReason
	c.closeMu.Unlock()
	status := websocket.CloseStatus(err)
	switch {
	case local && (status < 0 || int(status) == lcode):
		ev.Code, ev.Reason = lcode, lreason
	case status >= 0:
		var ce websocket.CloseError
		if errors.As(err, &ce) {
			ev.Reason = ce.Reason
		}
		ev.Code = int(status)
	case errors.Is(err, websocket.ErrMessageTooBig):
		ev.Code = 1009
		ev.Err = errors.New("a message was larger than the limit; raise Max message size in Settings")
	case c.ctx.Err() != nil && errors.Is(err, c.ctx.Err()):
		ev.Code = CloseAbnormal
		ev.Err = context.Cause(c.ctx)
	default:
		ev.Code = CloseAbnormal
		ev.Err = errors.New("the connection was lost: " + friendly(err))
	}
	return ev
}
